// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

// serverConfig covers the native fields emitted by Takt's OpenCode installer.
type serverConfig struct {
	Command     json.RawMessage   `json:"command"`
	Args        json.RawMessage   `json:"args"`
	URL         string            `json:"url"`
	Type        string            `json:"type"`
	Disabled    *bool             `json:"disabled"`
	Enabled     json.RawMessage   `json:"enabled"`
	Env         map[string]string `json:"env"`
	Environment map[string]string `json:"environment"`
	Headers     map[string]string `json:"headers"`
	HTTPHeaders map[string]string `json:"http_headers"`
}

const (
	// memoryServiceCheckID names the memory service status probe.
	memoryServiceCheckID = "memory:service"
)

// memoryServiceCheck probes the memory service's status without starting it (PR-MSV-33), so an
// unreachable service stays distinguishable from one that is not ready.
func memoryServiceCheck(ctx context.Context) CheckResult {
	dir, err := memory.ResolveDir()
	if err != nil {
		return CheckResult{memoryServiceCheckID, NotVerifiable, fmt.Sprintf("Cannot resolve the memory directory: %v", err)}
	}
	status, health, err := memory.Assess(ctx, dir)
	switch health {
	case memory.HealthUnreachable:
		return CheckResult{memoryServiceCheckID, NotVerified, fmt.Sprintf("No memory service answers in %s.", dir)}
	case memory.HealthMalformed:
		return CheckResult{memoryServiceCheckID, NotVerified, fmt.Sprintf("The memory service in %s answered with a malformed response: %v", dir, err)}
	case memory.HealthNewerStore:
		return CheckResult{memoryServiceCheckID, NotVerified, fmt.Sprintf("The memory store is newer than this Takt build: %s", status.Reason)}
	case memory.HealthNotReady:
		return CheckResult{memoryServiceCheckID, NotVerified, fmt.Sprintf("The memory service %s is not ready (%s): %s", status.Version, status.State, status.Reason)}
	case memory.HealthUncovered:
		// Setup has just acquired the model, so entries still without a vector mean the service did not get that capability.
		return CheckResult{memoryServiceCheckID, NotVerified, fmt.Sprintf("The memory service %s is ready, but only %d of %d entries have semantic vectors.", status.Version, status.Covered, status.Total)}
	}
	return CheckResult{memoryServiceCheckID, Verified, fmt.Sprintf("The memory service %s reports %s; %d of %d entries have semantic vectors.", status.Version, status.State, status.Covered, status.Total)}
}

func managedMCPChecks(ctx context.Context, root string, target setup.OwnershipTarget) []CheckResult {
	var checks []CheckResult
	var status map[string]opencodeapi.MCPServer
	var statusErr error
	statusLoaded := false
	// The memory server and Codegraph are unconditional; Context7 is the only selectable MCP component.
	for _, name := range []string{memory.ServerKey, string(model.ComponentCodegraph), string(model.ComponentContext7)} {
		id := fmt.Sprintf("mcp:%s:%s", target, name)
		_, present, err := installedServer(root, target, name)
		switch {
		case err != nil:
			checks = append(checks, CheckResult{id, NotVerifiable, fmt.Sprintf("Cannot read installed MCP configuration: %v", err)})
		case !present && name != string(model.ComponentContext7):
			checks = append(checks, CheckResult{id, NotVerified, fmt.Sprintf("Required managed %s integration is absent from installed configuration.", name)})
		case present:
			if !statusLoaded {
				status, statusErr = managedMCPStatus(ctx, root)
				statusLoaded = true
			}
			if statusErr != nil {
				checks = append(checks, CheckResult{id, NotVerifiable, fmt.Sprintf("Cannot query OpenCode MCP status: %v", statusErr)})
				continue
			}
			server, ok := status[name]
			if !ok {
				checks = append(checks, CheckResult{id, NotVerified, "OpenCode did not report the managed MCP server as registered."})
				continue
			}
			checks = append(checks, mcpResult(id, server))
		}
	}
	return append(checks, memoryServiceCheck(ctx))
}

func installedServer(root string, target setup.OwnershipTarget, name string) (serverConfig, bool, error) {
	if target != setup.TargetOpenCode {
		return serverConfig{}, false, fmt.Errorf("unsupported MCP target %s", target)
	}
	servers, err := openCodeServers(root)
	if err != nil {
		return serverConfig{}, false, err
	}
	config, present := servers[name]
	return config, present, nil
}

// openCodeServers reads and validates the managed OpenCode MCP servers. A
// config without an mcp section has no servers.
func openCodeServers(root string) (map[string]serverConfig, error) {
	data, err := os.ReadFile(model.OpenCodeConfigPath(root))
	if err != nil {
		return nil, err
	}
	var document struct {
		MCP json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.MCP) == 0 {
		return nil, nil
	}
	var mcp map[string]json.RawMessage
	if err := json.Unmarshal(document.MCP, &mcp); err != nil {
		return nil, fmt.Errorf("OpenCode mcp must be an object containing servers: %w", err)
	}
	serversRaw, ok := mcp[model.MCPServersKeyOpenCode]
	if !ok {
		return nil, fmt.Errorf("OpenCode MCP servers must be nested under mcp.%s; flat MCP entries are not valid OpenCode V2", model.MCPServersKeyOpenCode)
	}
	for key := range mcp {
		if key != model.MCPServersKeyOpenCode {
			return nil, fmt.Errorf("OpenCode MCP entry %q is flat; MCP servers must be nested under mcp.%s", key, model.MCPServersKeyOpenCode)
		}
	}
	if bytes.Equal(bytes.TrimSpace(serversRaw), []byte("null")) {
		return nil, fmt.Errorf("OpenCode mcp.%s must be an object", model.MCPServersKeyOpenCode)
	}
	var servers map[string]serverConfig
	if err := json.Unmarshal(serversRaw, &servers); err != nil {
		return nil, fmt.Errorf("OpenCode mcp.%s must be an object: %w", model.MCPServersKeyOpenCode, err)
	}
	for serverName, server := range servers {
		if err := validateServerConfig(serverName, server); err != nil {
			return nil, err
		}
	}
	return servers, nil
}

func validateServerConfig(name string, config serverConfig) error {
	if strings.TrimSpace(config.Type) == "" {
		return fmt.Errorf("OpenCode MCP server %q is missing required type", name)
	}
	if len(config.Enabled) > 0 {
		return fmt.Errorf("OpenCode MCP server %q uses legacy enabled; use disabled instead", name)
	}
	if len(config.Args) > 0 {
		return fmt.Errorf("OpenCode MCP server %q uses legacy args; put arguments in the command array", name)
	}
	if config.Type != "local" {
		return nil
	}
	if len(config.Command) == 0 || bytes.Equal(bytes.TrimSpace(config.Command), []byte("null")) {
		return fmt.Errorf("OpenCode local MCP server %q is missing required command array", name)
	}
	var command []string
	if err := json.Unmarshal(config.Command, &command); err != nil {
		return fmt.Errorf("OpenCode local MCP server %q command must be an array: %w", name, err)
	}
	if len(command) == 0 {
		return fmt.Errorf("OpenCode local MCP server %q command array must not be empty", name)
	}
	for index, part := range command {
		if strings.TrimSpace(part) == "" {
			return fmt.Errorf("OpenCode local MCP server %q command[%d] must not be empty", name, index)
		}
	}
	return nil
}
