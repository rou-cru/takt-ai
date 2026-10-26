// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package opencode_test

import (
	"encoding/json"
	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

// TestRenderConfigComponents checks each requested component reaches its key
// in the rendered config, and leaves it out when not requested.
func TestRenderConfigComponents(t *testing.T) {
	const assigned = "openai/gpt-5.6-luna"
	for _, tc := range []struct {
		name       string
		components bool
	}{{"with components", true}, {"without components", false}} {
		t.Run(tc.name, func(t *testing.T) {
			artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
				Assignment:  model.ModelAssignment{Model: assigned},
				Context7:    tc.components,
				Permissions: tc.components,
			})
			if err != nil {
				t.Fatalf("opencode.RenderConfig() error = %v", err)
			}
			if artifact.Path != ".config/opencode/opencode.json" {
				t.Fatalf("path = %q", artifact.Path)
			}
			var config struct {
				Model string `json:"model"`
				MCP   struct {
					Servers map[string]struct {
						Disabled bool `json:"disabled"`
					} `json:"servers"`
				} `json:"mcp"`
				Permissions []rule `json:"permissions"`
			}
			if err := json.Unmarshal(artifact.Content, &config); err != nil {
				t.Fatal(err)
			}
			if config.Model != assigned {
				t.Errorf("model = %q, want %q", config.Model, assigned)
			}
			context7, ok := config.MCP.Servers["context7"]
			if ok != tc.components || context7.Disabled {
				t.Errorf("context7 server present = %v (disabled %v), want present and enabled = %v", ok, context7.Disabled, tc.components)
			}
			if (len(config.Permissions) > 0) != tc.components {
				t.Errorf("permissions = %v, want them rendered = %v", config.Permissions, tc.components)
			}
		})
	}
}

// TestTaktDagPluginArtifact checks the rendered artifact is the embedded TUI
// plugin at OpenCode's discovery path, with the takt-ai binary injected. The
// plugin's behavior is covered by its own tests in assets/.
func TestTaktDagPluginArtifact(t *testing.T) {
	const binary = "/opt/takt/bin/takt-ai"
	artifact := opencode.TaktDagPluginArtifact(binary)
	// OpenCode v2 discovers <config>/plugins/<name>/tui.tsx with no config entry.
	if artifact.Path != ".config/opencode/plugins/takt-dag/tui.tsx" {
		t.Fatalf("path = %q, want .config/opencode/plugins/takt-dag/tui.tsx", artifact.Path)
	}
	source, err := os.ReadFile("assets/takt-dag.tsx")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(string(source), `"__TAKT_AI_BINARY__"`, `"`+binary+`"`, 1)
	if want == string(source) {
		t.Fatal("plugin source no longer carries the takt-ai binary placeholder")
	}
	if string(artifact.Content) != want {
		t.Error("artifact differs from the plugin source with the takt-ai binary injected")
	}
}

func TestTaktVFSPluginArtifact(t *testing.T) {
	artifact := opencode.TaktVFSPluginArtifact("/usr/local/bin/takt-ai", true)
	if artifact.Path != ".config/opencode/plugins/takt-vfs.ts" {
		t.Fatalf("path = %q, want .config/opencode/plugins/takt-vfs.ts", artifact.Path)
	}
	content := string(artifact.Content)
	if !strings.Contains(content, `"/usr/local/bin/takt-ai"`) {
		t.Error("plugin missing the injected takt-ai binary path")
	}
	if !strings.Contains(content, `const ORCHESTRATOR_ID = "takt"`) || strings.Contains(content, "__TAKT_ORCHESTRATOR_ID__") {
		t.Error("plugin missing the injected orchestrator identity")
	}
	if !strings.Contains(content, `const VFS_SHELL_ENFORCED: boolean = true as`) || strings.Contains(content, "__TAKT_VFS_SHELL_ENFORCED__") {
		t.Error("plugin missing the injected shell enforcement switch")
	}
	off := string(opencode.TaktVFSPluginArtifact("/usr/local/bin/takt-ai", false).Content)
	if !strings.Contains(off, `const VFS_SHELL_ENFORCED: boolean = false as`) {
		t.Error("plugin rendered with enforcement off keeps shell interception on")
	}
	for _, toolName := range []string{
		"vfs_bind", "vfs_write", "vfs_read", "vfs_verify", "vfs_consolidate",
		"dispatch_commit", "dispatch_declare_recovery", "dispatch_close_recovery",
		"dispatch_restore", "dispatch_exception", "dispatch_contest",
	} {
		if !strings.Contains(content, toolName) {
			t.Errorf("plugin missing the %s tool", toolName)
		}
	}
	if !strings.Contains(content, `id: "takt.vfs"`) || !strings.Contains(content, "export default Plugin.define(") {
		t.Error("plugin missing the V2 default export")
	}
	if !strings.Contains(content, `"codegraph", "ensure-index"`) || !strings.Contains(content, "await Promise.all([") {
		t.Error("plugin must await workspace CodeGraph initialization during setup")
	}

}

func TestOpenCodePluginPackageArtifact(t *testing.T) {
	artifact := opencode.OpenCodePluginPackageArtifact()
	if artifact.Path != ".config/opencode/plugins/package.json" {
		t.Fatalf("path = %q", artifact.Path)
	}
	var manifest struct {
		Private      bool              `json:"private"`
		Type         string            `json:"type"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(artifact.Content, &manifest); err != nil {
		t.Fatalf("invalid manifest: %v", err)
	}
	if !manifest.Private || manifest.Type != "module" {
		t.Errorf("manifest metadata = private:%v type:%q", manifest.Private, manifest.Type)
	}
	for _, name := range []string{"@opencode/plugin", "@opencode/client"} {
		if got := manifest.Dependencies[name]; got != opencode.OpenCodePluginSDKVersion {
			t.Errorf("dependency %s = %q, want %q", name, got, opencode.OpenCodePluginSDKVersion)
		}
	}
	if got := manifest.Dependencies["@anthropic-ai/sandbox-runtime"]; got != opencode.SandboxRuntimeVersion {
		t.Errorf("sandbox-runtime = %q, want %q", got, opencode.SandboxRuntimeVersion)
	}
}

// The installer pin must match the manifest the adapter's probe test runs
// against; Dependabot bumps only the manifest.
func TestSandboxRuntimeVersionMatchesAdapterManifest(t *testing.T) {
	content, err := os.ReadFile("../../runtime/sandbox/package.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatalf("invalid sandbox manifest: %v", err)
	}
	if got := manifest.Dependencies["@anthropic-ai/sandbox-runtime"]; got != opencode.SandboxRuntimeVersion {
		t.Errorf("takt/runtime/sandbox/package.json pins sandbox-runtime %q, SandboxRuntimeVersion is %q", got, opencode.SandboxRuntimeVersion)
	}
}

func TestTaktMemoryPluginArtifact(t *testing.T) {
	artifact := opencode.TaktMemoryPluginArtifact(`/opt/takt "ai"/takt-ai`)
	if artifact.Path != ".config/opencode/plugins/takt-memory.ts" {
		t.Fatalf("path = %q", artifact.Path)
	}
	content := string(artifact.Content)
	if !strings.Contains(content, `const TAKT_AI = "/opt/takt \"ai\"/takt-ai"`) || strings.Contains(content, "__TAKT_AI_BINARY__") {
		t.Fatalf("binary path not rendered as a JS string literal:\n%s", content[:400])
	}
	for _, want := range []string{
		`name: "memory_record"`, `name: "memory_continue_session"`, `name: "memory_close_session"`,
		"previous_session_id: str(", "objective: str(", "state: str(",
		`user_order: { type: "boolean"`, "author: c.agent", "directory: pluginDirectory",
		`"session.deleted"`, "fallback: true", "caller_session: c.sessionID",
		// The two confirmations survive the V2 plugin API: ctx has no
		// permission.create, so they ride the local service endpoint.
		`confirm("memory_personal"`, `confirm("memory_user_order"`, "api.permission.create(",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("plugin missing %q", want)
		}
	}
	if strings.Contains(content, "dispose:") {
		t.Error("plugin must not close sessions on dispose: exiting OpenCode is not a session end")
	}
}

// toolDescription matches the text a model reads for a plugin tool: its
// description, the description argument of the dispatch/gc helpers, and a
// parameter's str(...) description.
var toolDescription = regexp.MustCompile(`(?:description:\s*|(?:dispatch|gc)\("[a-z_]+",\s*|str\()"((?:[^"\\]|\\.)*)"`)

func TestPluginToolDescriptionsCarryConductOnly(t *testing.T) {
	source := string(opencode.TaktVFSPluginArtifact("/test/takt-ai", true).Content)
	matches := toolDescription.FindAllStringSubmatch(source, -1)
	if len(matches) < 20 {
		t.Fatalf("found %d tool descriptions; the pattern no longer matches the plugin", len(matches))
	}
	for _, m := range matches {
		for _, term := range []string{"PR-", "IR-", "harness", "plugin"} {
			if strings.Contains(m[1], term) {
				t.Errorf("tool description explains mechanism with %q: %s", term, m[1])
			}
		}
	}
}

func TestRenderCLIConfigPlacesAttentionInCLIConfig(t *testing.T) {
	artifact, err := opencode.RenderCLIConfig()
	if err != nil {
		t.Fatalf("opencode.RenderCLIConfig() error = %v", err)
	}
	if artifact.Path != ".config/opencode/cli.json" {
		t.Fatalf("cli config path = %q, want .config/opencode/cli.json", artifact.Path)
	}
	var config struct {
		Attention struct {
			Notifications bool `json:"notifications"`
		} `json:"attention"`
	}
	if err := json.Unmarshal(artifact.Content, &config); err != nil || !config.Attention.Notifications {
		t.Fatalf("cli config = %s (%v), want attention.notifications enabled", artifact.Content, err)
	}
}
