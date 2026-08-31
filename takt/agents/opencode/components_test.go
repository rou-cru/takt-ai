// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package opencode

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestRenderConfigComponents(t *testing.T) {
	artifact, err := RenderConfig(ConfigRequest{
		Assignment:  model.ModelAssignment{Model: "openai/gpt-5.6-luna"},
		Context7:    true,
		Permissions: true,
		Theme:       TaktTheme,
	})
	if err != nil {
		t.Fatalf("RenderConfig() error = %v", err)
	}
	if artifact.Path != ".config/opencode/opencode.json" {
		t.Fatalf("path = %q", artifact.Path)
	}
	// The merged file keeps OpenCode's canonical JSON shape: the map-based
	// marshal matches the deep-merge output of the reference overlays.
	want := `{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "context7": {
      "enabled": true,
      "type": "remote",
      "url": "https://mcp.context7.com/mcp"
    }
  },
  "model": "openai/gpt-5.6-luna",
  "permission": {
    "bash": {
      "*": "allow",
      "git commit *": "ask",
      "git push": "ask",
      "git push *": "ask",
      "git push --force *": "ask",
      "git rebase *": "ask",
      "git reset --hard *": "ask"
    },
    "read": {
      "*": "allow",
      "**/*.key": "deny",
      "**/*.pem": "deny",
      "**/.aws/credentials": "deny",
      "**/.config/gh/hosts.yml": "deny",
      "**/.credentials/**": "deny",
      "**/.env": "deny",
      "**/.env.*": "deny",
      "**/.ssh/**": "deny",
      "**/Library/Keychains/**": "deny",
      "**/credentials.json": "deny",
      "**/secrets/**": "deny",
      "*.env": "deny",
      "*.env.*": "deny"
    }
  },
  "theme": "takt-kanagawa"
}
`
	if !bytes.Equal(artifact.Content, []byte(want)) {
		t.Fatalf("opencode config with components mismatch:\n%s", artifact.Content)
	}
}

func TestRenderConfigWithoutComponentsKeepsBaseShape(t *testing.T) {
	artifact, err := RenderConfig(ConfigRequest{Assignment: model.ModelAssignment{Model: "openai/gpt-5.6-luna"}})
	if err != nil {
		t.Fatalf("RenderConfig() error = %v", err)
	}
	want := "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"model\": \"openai/gpt-5.6-luna\"\n}\n"
	if !bytes.Equal(artifact.Content, []byte(want)) {
		t.Fatalf("base config mismatch:\n%s", artifact.Content)
	}
}

func TestTaktLogoPluginArtifact(t *testing.T) {
	artifact := TaktLogoPluginArtifact()
	if artifact.Path != ".config/opencode/tui-plugins/takt-logo.tsx" {
		t.Fatalf("path = %q, want .config/opencode/tui-plugins/takt-logo.tsx", artifact.Path)
	}
	content := string(artifact.Content)
	if !strings.Contains(content, "id = \"takt-logo\"") {
		t.Error("plugin missing the takt-logo id")
	}
	if !strings.Contains(content, "home_logo") {
		t.Error("plugin missing the home_logo slot registration")
	}
	if !strings.Contains(content, "const plugin = { id: \"takt-logo\", tui }") || !strings.Contains(content, "export default plugin") {
		t.Error("plugin missing the default TUI plugin export")
	}
	if strings.Contains(content, "server") {
		t.Error("TUI plugin must not export or define a server shape")
	}
}

func TestTaktLogoRegistrationArtifact(t *testing.T) {
	artifact := TaktLogoRegistrationArtifact()
	if artifact.Path != ".config/opencode/tui.json" {
		t.Fatalf("path = %q, want .config/opencode/tui.json", artifact.Path)
	}
	// Plugin paths are resolved by OpenCode relative to the config file that
	// declares them, so the registration stays home-independent.
	want := `{
  "$schema": "https://opencode.ai/tui.json",
  "plugin": [
    "./tui-plugins/takt-logo.tsx"
  ]
}
`
	if !bytes.Equal(artifact.Content, []byte(want)) {
		t.Fatalf("tui.json mismatch:\n%s", artifact.Content)
	}
}
