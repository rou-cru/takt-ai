// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
	"github.com/rou-cru/takt-ai/takt/skills"
)

func nativeOpenCodeClient(root string) *opencodeapi.Client {
	return opencodeapi.New(
		opencodeapi.WithDir(root),
		opencodeapi.WithEnv(inventoryEnvironment(root)),
	)
}

func nativeOpenCodeChecks(ctx context.Context, root string) []CheckResult {
	client := nativeOpenCodeClient(root)
	checks := []CheckResult{
		nativeOrchestratorCheck(ctx, client),
		nativeDefaultModelCheck(ctx, client),
		nativeSkillsCheck(ctx, client),
		nativePluginsCheck(ctx, client),
	}
	return checks
}

func nativeOrchestratorCheck(ctx context.Context, client *opencodeapi.Client) CheckResult {
	id := "orchestrator:opencode:" + shared.OrchestratorID
	agents, err := client.Agents(ctx)
	if err != nil {
		return CheckResult{id, NotVerifiable, fmt.Sprintf("Cannot query native OpenCode agent inventory: %v", err)}
	}
	for _, agent := range agents {
		if agent.ID == shared.OrchestratorID && !agent.Hidden && (agent.Mode == "primary" || agent.Mode == "all") {
			return CheckResult{id, Verified, "Native OpenCode API exposes the Takt orchestrator as a selectable primary agent."}
		}
	}
	return CheckResult{id, NotVerified, "Native OpenCode API does not expose a selectable Takt orchestrator."}
}

func nativeDefaultModelCheck(ctx context.Context, client *opencodeapi.Client) CheckResult {
	const id = "model:opencode:default"
	model, err := client.DefaultModel(ctx)
	if err != nil {
		return CheckResult{id, NotVerifiable, fmt.Sprintf("Cannot resolve OpenCode's default model: %v", err)}
	}
	if model == nil {
		return CheckResult{id, NotVerified, "OpenCode did not resolve a default model for the unassigned case of PR-SET-7."}
	}
	return CheckResult{id, Verified, "OpenCode resolved the default model " + model.Ref.String() + "."}
}

func nativeSkillsCheck(ctx context.Context, client *opencodeapi.Client) CheckResult {
	const id = "skills:opencode:takt"
	expected, err := skills.LoadSkills()
	if err != nil {
		return CheckResult{id, NotVerifiable, fmt.Sprintf("Cannot load Takt skill catalog: %v", err)}
	}
	actual, err := client.Skills(ctx)
	if err != nil {
		return CheckResult{id, NotVerifiable, fmt.Sprintf("Cannot query native OpenCode skills: %v", err)}
	}
	seen := make(map[string]bool, len(actual))
	for _, entry := range actual {
		seen[entry.ID] = true
	}
	var missing []string
	for _, entry := range expected {
		if !seen[entry.Name] {
			missing = append(missing, entry.Name)
		}
	}
	if len(missing) > 0 {
		return CheckResult{id, NotVerified, "Native OpenCode is missing Takt skills: " + strings.Join(missing, ", ")}
	}
	return CheckResult{id, Verified, fmt.Sprintf("Native OpenCode discovered all %d Takt skills.", len(expected))}
}

func nativePluginsCheck(ctx context.Context, client *opencodeapi.Client) CheckResult {
	const id = "plugins:opencode"
	plugins, err := client.Plugins(ctx)
	if err != nil {
		return CheckResult{id, NotVerifiable, fmt.Sprintf("Cannot query native OpenCode plugins: %v", err)}
	}
	for _, plugin := range plugins {
		if plugin.Status == "failed" {
			message := plugin.ID
			if message == "" {
				message = plugin.Type
			}
			if plugin.Error != "" {
				message += ": " + plugin.Error
			}
			return CheckResult{id, NotVerified, "OpenCode reports a failed plugin: " + message}
		}
	}
	return CheckResult{id, Verified, fmt.Sprintf("OpenCode reports %d active plugins and no failed plugin.", len(plugins))}
}

func inventoryEnvironment(root string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch {
		case key == "HOME", strings.HasPrefix(key, "XDG_"), strings.HasPrefix(key, "OPENCODE_CONFIG"):
			continue
		}
		env = append(env, entry)
	}
	return append(env, "HOME="+root,
		"XDG_CONFIG_HOME="+filepath.Join(root, ".config"),
		"XDG_DATA_HOME="+filepath.Join(root, ".local", "share"),
		"XDG_CACHE_HOME="+filepath.Join(root, ".cache"),
		"XDG_STATE_HOME="+filepath.Join(root, ".local", "state"),
		"NO_COLOR=1")
}

func managedMCPStatus(ctx context.Context, root string) (map[string]opencodeapi.MCPServer, error) {
	servers, err := nativeOpenCodeClient(root).MCPStatus(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]opencodeapi.MCPServer, len(servers))
	for _, server := range servers {
		byName[server.Name] = server
	}
	return byName, nil
}

func mcpResult(id string, server opencodeapi.MCPServer) CheckResult {
	if server.State == opencodeapi.MCPConnected {
		return CheckResult{id, Verified, "OpenCode API reports the MCP server connected."}
	}
	detail := fmt.Sprintf("OpenCode API reports MCP state %q.", server.State)
	if server.Error != "" {
		detail += " " + server.Error
	}
	return CheckResult{id, NotVerified, detail}
}
