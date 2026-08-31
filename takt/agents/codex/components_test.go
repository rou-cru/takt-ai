// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package codex

import (
	"bytes"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func testConfigRequest() ConfigRequest {
	return ConfigRequest{
		Assignment:  model.ModelAssignment{Model: "openai/gpt-5.6-terra", Effort: "high"},
		SandboxMode: SandboxWorkspaceWrite,
		WebSearch:   WebSearchLive,
		MultiAgent:  true,
		MaxThreads:  4,
		MaxDepth:    2,
	}
}

func TestRenderConfigContext7AndPermissions(t *testing.T) {
	request := testConfigRequest()
	request.Context7 = true
	request.Permissions = true
	artifact, err := RenderConfig(request)
	if err != nil {
		t.Fatalf("RenderConfig() error = %v", err)
	}
	want := `model = "openai/gpt-5.6-terra"
model_reasoning_effort = "high"
sandbox_mode = "workspace-write"
web_search = "live"
approval_policy = "on-request"
default_permissions = "takt-dev"

[features]
multi_agent = true

[agents]
max_threads = 4
max_depth = 2

[mcp_servers.context7]
url = "https://mcp.context7.com/mcp"

[permissions.takt-dev]
description = "Comfortable local development profile with workspace writes, network access, Git metadata writes, Nix/Home Manager support, and secret-file protections."

[permissions.takt-dev.network]
enabled = true

[permissions.takt-dev.network.domains]
"*" = "allow"

[permissions.takt-dev.filesystem]
":minimal" = "read"
"~/.config/git" = "read"
"~/.gitconfig" = "read"
"~/.local/state/nix/profiles/home-manager/home-path" = "read"
"~/.nix-profile" = "read"
"/nix/store" = "read"
":tmpdir" = "write"
":slash_tmp" = "write"

[permissions.takt-dev.filesystem.":workspace_roots"]
"." = "write"
".git/**" = "write"
"**/.env" = "deny"
"**/.env.local" = "deny"
"**/.env.*.local" = "deny"
"**/.aws/credentials" = "deny"
"**/.config/gh/hosts.yml" = "deny"
"**/.credentials/**" = "deny"
"**/.ssh/**" = "deny"
"**/Library/Keychains/**" = "deny"
"**/credentials.json" = "deny"
"**/*.pem" = "deny"
"**/*.key" = "deny"
"**/secrets/**" = "deny"

[permissions.takt-dev.workspace_roots]
"~" = true
`
	if !bytes.Equal(artifact.Content, []byte(want)) {
		t.Fatalf("config with context7 and permissions mismatch:\n%s", artifact.Content)
	}
}

func TestRenderConfigContext7Only(t *testing.T) {
	request := testConfigRequest()
	request.Context7 = true
	artifact, err := RenderConfig(request)
	if err != nil {
		t.Fatalf("RenderConfig() error = %v", err)
	}
	want := `model = "openai/gpt-5.6-terra"
model_reasoning_effort = "high"
sandbox_mode = "workspace-write"
web_search = "live"

[features]
multi_agent = true

[agents]
max_threads = 4
max_depth = 2

[mcp_servers.context7]
url = "https://mcp.context7.com/mcp"
`
	if !bytes.Equal(artifact.Content, []byte(want)) {
		t.Fatalf("config with context7 mismatch:\n%s", artifact.Content)
	}
}

func TestRenderConfigPermissionsOnly(t *testing.T) {
	request := testConfigRequest()
	request.Permissions = true
	artifact, err := RenderConfig(request)
	if err != nil {
		t.Fatalf("RenderConfig() error = %v", err)
	}
	content := string(artifact.Content)
	for _, required := range []string{
		"approval_policy = \"on-request\"\n",
		"default_permissions = \"takt-dev\"\n",
		"[permissions.takt-dev]\n",
	} {
		if !bytes.Contains([]byte(content), []byte(required)) {
			t.Fatalf("config missing %q:\n%s", required, content)
		}
	}
	if bytes.Contains([]byte(content), []byte("[mcp_servers.context7]")) {
		t.Fatalf("permissions-only config must not add the context7 block:\n%s", content)
	}
	if !bytes.HasPrefix([]byte(content), []byte("model = \"openai/gpt-5.6-terra\"\n")) {
		t.Fatalf("top-level policy keys must stay below the base keys:\n%s", content)
	}
}
