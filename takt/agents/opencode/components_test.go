// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package opencode_test

import (
	"bytes"
	"encoding/json"
	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"regexp"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestRenderConfigComponents(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Assignment:  model.ModelAssignment{Model: "openai/gpt-5.6-luna"},
		Context7:    true,
		Permissions: true,
	})
	if err != nil {
		t.Fatalf("opencode.RenderConfig() error = %v", err)
	}
	if artifact.Path != ".config/opencode/opencode.json" {
		t.Fatalf("path = %q", artifact.Path)
	}
	// The merged file keeps OpenCode's canonical JSON shape: the map-based
	// marshal matches the deep-merge output of the reference overlays.
	want := `{
  "$schema": "https://opencode.ai/config.json",
  "attention": {
    "enabled": true,
    "notifications": true
  },
  "default_agent": "takt",
  "formatter": true,
  "mcp": {
    "servers": {
      "context7": {
        "disabled": false,
        "type": "remote",
        "url": "https://mcp.context7.com/mcp"
      }
    }
  },
  "model": "openai/gpt-5.6-luna",
  "permissions": [
    {
      "action": "shell",
      "effect": "allow",
      "resource": "*"
    },
    {
      "action": "shell",
      "effect": "ask",
      "resource": "git commit *"
    },
    {
      "action": "shell",
      "effect": "ask",
      "resource": "git push"
    },
    {
      "action": "shell",
      "effect": "ask",
      "resource": "git push *"
    },
    {
      "action": "shell",
      "effect": "ask",
      "resource": "git push --force *"
    },
    {
      "action": "shell",
      "effect": "ask",
      "resource": "git rebase *"
    },
    {
      "action": "shell",
      "effect": "ask",
      "resource": "git reset --hard *"
    },
    {
      "action": "shell",
      "effect": "deny",
      "resource": "takt-ai memory*"
    },
    {
      "action": "shell",
      "effect": "deny",
      "resource": "*takt-ai memory*"
    },
    {
      "action": "shell",
      "effect": "deny",
      "resource": "takt-ai gc*"
    },
    {
      "action": "shell",
      "effect": "deny",
      "resource": "*takt-ai gc*"
    },
    {
      "action": "shell",
      "effect": "deny",
      "resource": "takt-ai dispatch*"
    },
    {
      "action": "shell",
      "effect": "deny",
      "resource": "*takt-ai dispatch*"
    },
    {
      "action": "read",
      "effect": "allow",
      "resource": "*"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "*.env"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "*.env.*"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/.env"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/.env.*"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/.ssh/**"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/.credentials/**"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/Library/Keychains/**"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/.aws/credentials"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/.config/gh/hosts.yml"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/*.pem"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/*.key"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/secrets/**"
    },
    {
      "action": "read",
      "effect": "deny",
      "resource": "**/credentials.json"
    },
    {
      "action": "edit",
      "effect": "deny",
      "resource": "*"
    }
  ],
  "snapshot": true
}
`
	if !bytes.Equal(withoutOrchestrator(t, artifact.Content), []byte(want)) {
		t.Fatalf("opencode config with components mismatch:\n%s", artifact.Content)
	}
}

func TestRenderConfigWithoutComponentsKeepsBaseShape(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{Assignment: model.ModelAssignment{Model: "openai/gpt-5.6-luna"}})
	if err != nil {
		t.Fatalf("opencode.RenderConfig() error = %v", err)
	}
	want := "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"attention\": {\n    \"enabled\": true,\n    \"notifications\": true\n  },\n  \"default_agent\": \"takt\",\n  \"formatter\": true,\n  \"model\": \"openai/gpt-5.6-luna\",\n  \"snapshot\": true\n}\n"
	if !bytes.Equal(withoutOrchestrator(t, artifact.Content), []byte(want)) {
		t.Fatalf("base config mismatch:\n%s", artifact.Content)
	}
}

func TestTaktDagPluginArtifact(t *testing.T) {
	artifact := opencode.TaktDagPluginArtifact("/opt/takt/bin/takt-ai")
	// OpenCode v2 discovers <config>/plugins/<name>/tui.tsx with no config entry.
	if artifact.Path != ".config/opencode/plugins/takt-dag/tui.tsx" {
		t.Fatalf("path = %q, want .config/opencode/plugins/takt-dag/tui.tsx", artifact.Path)
	}
	content := string(artifact.Content)
	assertDagPluginBinaryAndPolling(t, content)
	assertDagPluginRouteRegistration(t, content)
	assertDagPluginScrollBehavior(t, content)
	assertDagPluginKeybinding(t, content)
	assertDagPluginNodeKindMarkers(t, content)
	assertDagPluginExports(t, content)
}

// assertDagPluginBinaryAndPolling checks the plugin embeds the substituted
// takt-ai binary path (not the placeholder) and polls dag status with it.
func assertDagPluginBinaryAndPolling(t *testing.T, content string) {
	t.Helper()
	if !strings.Contains(content, `"/opt/takt/bin/takt-ai"`) || strings.Contains(content, "__TAKT_AI_BINARY__") {
		t.Error("plugin missing the substituted takt-ai binary path")
	}
	if !strings.Contains(content, `"dag", "status"`) {
		t.Error("plugin must poll takt-ai dag status")
	}
}

// assertDagPluginRouteRegistration checks the plugin registers its route,
// id, slash command and sidebar projection slot.
func assertDagPluginRouteRegistration(t *testing.T, content string) {
	t.Helper()
	if !strings.Contains(content, `id: "takt.dag"`) {
		t.Error("plugin missing the takt.dag id")
	}
	if !strings.Contains(content, `name: "takt.dag"`) {
		t.Error("plugin missing the takt.dag route registration")
	}
	if !strings.Contains(content, `slash: { name: "takt-dag" }`) {
		t.Error("plugin missing the /takt-dag slash command")
	}
	if !strings.Contains(content, `prepend: "sidebar.content"`) {
		t.Error("plugin missing the sidebar projection slot")
	}
}

// assertDagPluginScrollBehavior checks the sidebar graph scrolls sideways
// only, while the route graph scrolls both ways with keyboard focus.
func assertDagPluginScrollBehavior(t *testing.T, content string) {
	t.Helper()
	if !strings.Contains(content, `"route" | "sidebar"`) || !strings.Contains(content, "scrollY: false") || !strings.Contains(content, "horizontalScrollbarOptions") {
		t.Error("sidebar graph must scroll sideways only")
	}
	if !strings.Contains(content, `props.mode === "route"`) || !strings.Contains(content, "scrollY: true, focused: true") {
		t.Error("route graph must scroll both ways with keyboard focus")
	}
}

// assertDagPluginKeybinding checks the plugin binds <leader>d, not
// OpenCode's default input.delete.line chord (ctrl+shift+d).
func assertDagPluginKeybinding(t *testing.T, content string) {
	t.Helper()
	if !strings.Contains(content, `bind: "<leader>d"`) || strings.Contains(content, `bind: "ctrl+shift+d"`) {
		t.Error("plugin must bind <leader>d, not OpenCode's input.delete.line chord")
	}
}

// assertDagPluginNodeKindMarkers checks every explicit node-kind rendering
// marker is present in the plugin source.
func assertDagPluginNodeKindMarkers(t *testing.T, content string) {
	t.Helper()
	for _, marker := range []string{`readonly node_kind?: "delegated"`, `type DagActivityKind = "orchestrator" | "maintenance"`, `interface DagActivity`, `readonly activities?: readonly DagActivity[]`, `GC activity`} {
		if !strings.Contains(content, marker) {
			t.Errorf("plugin missing explicit node-kind rendering marker %q", marker)
		}
	}
}

// assertDagPluginExports checks the plugin uses the V2 default export and
// imports the V2 TUI plugin package.
func assertDagPluginExports(t *testing.T, content string) {
	t.Helper()
	if !strings.Contains(content, "export default Plugin.define(") {
		t.Error("plugin missing the V2 default export")
	}
	if !strings.Contains(content, `from "@opencode/plugin/tui"`) {
		t.Error("plugin must import the V2 TUI plugin package")
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

func withoutOrchestrator(t *testing.T, content []byte) []byte {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatal(err)
	}
	delete(config, "agents")
	rendered, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(rendered, '\n')
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
		`"session.deleted"`, "fallback: true", "ctx.session.get",
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
