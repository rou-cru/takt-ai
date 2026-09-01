// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package claude

import (
	"bytes"
	"testing"
)

func TestContext7ServerArtifact(t *testing.T) {
	artifact := Context7ServerArtifact()
	if artifact.Path != ".claude/mcp/context7.json" {
		t.Fatalf("path = %q, want .claude/mcp/context7.json", artifact.Path)
	}
	want := `{
  "command": "npx",
  "args": [
    "-y",
    "--package=@upstash/context7-mcp@2.2.5",
    "--",
    "context7-mcp"
  ]
}
`
	if !bytes.Equal(artifact.Content, []byte(want)) {
		t.Fatalf("context7 server content mismatch:\n%s", artifact.Content)
	}
}

func TestTaktThemeArtifact(t *testing.T) {
	artifact := TaktThemeArtifact()
	if artifact.Path != ".claude/themes/takt.json" {
		t.Fatalf("path = %q, want .claude/themes/takt.json", artifact.Path)
	}
	want := `{
  "name": "Takt",
  "base": "dark",
  "overrides": {
    "briefLabelYou": "#DCA561",
    "chromeYellow": "#DCA561",
    "diffAdded": "#3F4A2D",
    "diffAddedWord": "#76946A",
    "diffRemoved": "#5C3838",
    "diffRemovedWord": "#C34043",
    "rainbow_yellow": "#DCA561",
    "yellow_FOR_SUBAGENTS_ONLY": "#DCA561"
  }
}
`
	if !bytes.Equal(artifact.Content, []byte(want)) {
		t.Fatalf("Takt theme content mismatch:\n%s", artifact.Content)
	}
}

func TestDefaultPermissions(t *testing.T) {
	permissions := DefaultPermissions()
	if permissions.DefaultMode != "auto" {
		t.Fatalf("defaultMode = %q, want auto", permissions.DefaultMode)
	}
	want := []string{
		"Bash(rm -rf /)",
		"Bash(sudo rm -rf /)",
		"Bash(rm -rf ~)",
		"Bash(sudo rm -rf ~)",
		"Read(**/.env)",
		"Edit(**/.env)",
		"Read(**/.env.*)",
		"Edit(**/.env.*)",
		"Read(**/.ssh/**)",
		"Edit(**/.ssh/**)",
		"Read(**/.credentials/**)",
		"Edit(**/.credentials/**)",
		"Read(**/Library/Keychains/**)",
		"Edit(**/Library/Keychains/**)",
		"Read(**/.aws/credentials)",
		"Edit(**/.aws/credentials)",
		"Read(**/.config/gh/hosts.yml)",
		"Edit(**/.config/gh/hosts.yml)",
		"Read(**/*.pem)",
		"Edit(**/*.pem)",
		"Read(**/*.key)",
		"Edit(**/*.key)",
		"Read(**/secrets/**)",
		"Edit(**/secrets/**)",
		"Read(**/credentials.json)",
		"Edit(**/credentials.json)",
	}
	if !bytes.Equal([]byte(join(permissions.Deny)), []byte(join(want))) {
		t.Fatalf("deny rules = %v, want %v", permissions.Deny, want)
	}
}

func TestRenderSettingsMergesPermissionsIntoSingleSettingsFile(t *testing.T) {
	permissions := DefaultPermissions()
	artifact, err := RenderSettings(SettingsRequest{
		Settings:    map[string]any{"enabled": true},
		Permissions: &permissions,
	})
	if err != nil {
		t.Fatalf("RenderSettings() error = %v", err)
	}
	want := `{
  "enabled": true,
  "permissions": {
    "defaultMode": "auto",
    "deny": [
      "Bash(rm -rf /)",
      "Bash(sudo rm -rf /)",
      "Bash(rm -rf ~)",
      "Bash(sudo rm -rf ~)",
      "Read(**/.env)",
      "Edit(**/.env)",
      "Read(**/.env.*)",
      "Edit(**/.env.*)",
      "Read(**/.ssh/**)",
      "Edit(**/.ssh/**)",
      "Read(**/.credentials/**)",
      "Edit(**/.credentials/**)",
      "Read(**/Library/Keychains/**)",
      "Edit(**/Library/Keychains/**)",
      "Read(**/.aws/credentials)",
      "Edit(**/.aws/credentials)",
      "Read(**/.config/gh/hosts.yml)",
      "Edit(**/.config/gh/hosts.yml)",
      "Read(**/*.pem)",
      "Edit(**/*.pem)",
      "Read(**/*.key)",
      "Edit(**/*.key)",
      "Read(**/secrets/**)",
      "Edit(**/secrets/**)",
      "Read(**/credentials.json)",
      "Edit(**/credentials.json)"
    ]
  }
}
`
	if !bytes.Equal(artifact.Content, []byte(want)) {
		t.Fatalf("settings with permissions mismatch:\n%s", artifact.Content)
	}
}

func TestRenderSettingsRejectsSettingPermissionsConflict(t *testing.T) {
	permissions := DefaultPermissions()
	if _, err := RenderSettings(SettingsRequest{
		Settings:    map[string]any{"permissions": map[string]any{}},
		Permissions: &permissions,
	}); err == nil {
		t.Fatal("RenderSettings() error = nil, want permissions key conflict")
	}
}

func join(values []string) string {
	out := ""
	for _, value := range values {
		out += value + "\n"
	}
	return out
}
