// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package codex

import (
	"bytes"
	"fmt"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
)

// Context7RemoteURL is the canonical context7 remote MCP endpoint deployed
// into Codex config.toml by the context7 component.
const Context7RemoteURL = shared.Context7RemoteURL

// appendContext7Server writes the context7 remote MCP server block.
func appendContext7Server(content *bytes.Buffer) {
	content.WriteString("\n[mcp_servers.context7]\n")
	fmt.Fprintf(content, "url = %s\n", tomlString(Context7RemoteURL))
}

// permissionsProfileName is the canonical Codex permission profile deployed by
// the permissions component.
const permissionsProfileName = "takt-dev"

// profileTablePath builds a "permissions.takt-dev<suffix>" TOML table path.
func profileTablePath(suffix string) string {
	return "permissions." + permissionsProfileName + suffix
}

// appendPermissionsProfile writes the canonical takt-dev permission profile:
// top-level policy keys plus the permissions.takt-dev table tree. It mirrors
// the workspace-write sandbox with network access, Git metadata writes,
// Nix/Home Manager support, and secret-file protections.
func appendPermissionsProfile(content *bytes.Buffer) {
	fmt.Fprintf(content, "\n[%s]\n", profileTablePath(""))
	fmt.Fprintf(content, "description = %s\n", tomlString("Comfortable local development profile with workspace writes, network access, Git metadata writes, Nix/Home Manager support, and secret-file protections."))

	fmt.Fprintf(content, "\n[%s]\n", profileTablePath(".network"))
	content.WriteString("enabled = true\n")

	fmt.Fprintf(content, "\n[%s]\n", profileTablePath(".network.domains"))
	content.WriteString(`"*" = "allow"` + "\n")

	fmt.Fprintf(content, "\n[%s]\n", profileTablePath(".filesystem"))
	for _, path := range []string{
		":minimal",
		"~/.config/git",
		"~/.gitconfig",
		"~/.local/state/nix/profiles/home-manager/home-path",
		"~/.nix-profile",
		"/nix/store",
	} {
		fmt.Fprintf(content, "%s = \"read\"\n", tomlString(path))
	}
	for _, path := range []string{
		":tmpdir",
		":slash_tmp",
	} {
		fmt.Fprintf(content, "%s = \"write\"\n", tomlString(path))
	}

	fmt.Fprintf(content, "\n[%s]\n", profileTablePath(`.filesystem.":workspace_roots"`))
	fmt.Fprintf(content, `"." = "write"`+"\n")
	fmt.Fprintf(content, `".git/**" = "write"`+"\n")
	for _, glob := range shared.SensitivePathGlobs {
		fmt.Fprintf(content, "%s = \"deny\"\n", tomlString("**/"+glob))
	}

	fmt.Fprintf(content, "\n[%s]\n", profileTablePath(".workspace_roots"))
	fmt.Fprintf(content, `%s = true`+"\n", tomlString("~"))
}
