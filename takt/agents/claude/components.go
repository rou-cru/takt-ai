// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package claude

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
)

// Context7MCPVersion pins the @upstash/context7-mcp package version deployed
// by the context7 component.
const Context7MCPVersion = "2.2.5"

// context7ServerJSON is the standalone MCP server definition Claude Code loads
// from its per-server MCP file strategy.
const context7ServerJSON = `{
  "command": "npx",
  "args": [
    "-y",
    "--package=@upstash/context7-mcp@` + Context7MCPVersion + `",
    "--",
    "context7-mcp"
  ]
}
`

// Context7ServerArtifact returns the context7 MCP server file deployed at the
// Claude-specific MCP path.
func Context7ServerArtifact() Artifact {
	return Artifact{
		Path:    ".claude/mcp/context7.json",
		Content: []byte(context7ServerJSON),
	}
}

// Permissions is the Claude Code settings.json permission projection.
type Permissions struct {
	DefaultMode string   `json:"defaultMode"`
	Deny        []string `json:"deny"`
}

// DefaultPermissions returns the canonical Takt permission rules for Claude
// Code: auto mode by default (classifier-routed tool calls that block
// destructive actions) with explicit deny rules for secret files,
// credentials, and destructive shell commands.
func DefaultPermissions() Permissions {
	deny := []string{
		"Bash(rm -rf /)",
		"Bash(sudo rm -rf /)",
		"Bash(rm -rf ~)",
		"Bash(sudo rm -rf ~)",
	}
	for _, glob := range shared.SensitivePathGlobs {
		pattern := "**/" + glob
		deny = append(deny, fmt.Sprintf("Read(%s)", pattern), fmt.Sprintf("Edit(%s)", pattern))
	}
	return Permissions{
		DefaultMode: "auto",
		Deny:        deny,
	}
}

// taktTheme is the Claude Code color theme definition deployed by the
// claude-theme component.
type taktTheme struct {
	Name      string            `json:"name"`
	Base      string            `json:"base"`
	Overrides map[string]string `json:"overrides"`
}

var taktThemeDefinition = taktTheme{
	Name: "Takt",
	Base: "dark",
	Overrides: map[string]string{
		"diffAdded":                 "#3F4A2D",
		"diffRemoved":               "#5C3838",
		"diffAddedWord":             "#76946A",
		"diffRemovedWord":           "#C34043",
		"chromeYellow":              "#DCA561",
		"briefLabelYou":             "#DCA561",
		"rainbow_yellow":            "#DCA561",
		"yellow_FOR_SUBAGENTS_ONLY": "#DCA561",
	},
}

// TaktThemeArtifact returns the Claude Code theme file deployed by the
// claude-theme component.
func TaktThemeArtifact() Artifact {
	content, err := json.MarshalIndent(taktThemeDefinition, "", "  ")
	if err != nil {
		// A fixed string/string map cannot fail to marshal; guard anyway so a
		// future field type change cannot deploy a truncated theme file.
		panic(fmt.Sprintf("marshal Claude Takt theme: %v", err))
	}
	return Artifact{
		Path:    ".claude/themes/takt.json",
		Content: append(content, '\n'),
	}
}
