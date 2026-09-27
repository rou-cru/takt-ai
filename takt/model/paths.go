// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package model

import "path/filepath"

// Home-relative install paths shared by inject, remove, verify and doctor.
// Manifest slash-paths live with setup; these are OS paths from a home root.

// Home-relative file and directory names for each harness, plus the plugin
// filenames Takt deploys into OpenCode's plugin directory.
const (
	OpenCodeConfigDir  = ".config/opencode"
	OpenCodeConfigFile = "opencode.json"
	// OpenCodeConfigRelativePath is the slash-separated manifest path owned by OpenCode.
	OpenCodeConfigRelativePath = OpenCodeConfigDir + "/" + OpenCodeConfigFile
	AgentsPromptFile           = "AGENTS.md"

	OpenCodePluginsDir = "plugins"
	EngramPluginFile   = "engram.ts"
	VFSPluginFile      = "takt-vfs.ts"
)

// MCP object keys per target native config.
const (
	// MCPKeyOpenCode is the top-level MCP map key in opencode.json.
	MCPKeyOpenCode = "mcp"
	// MCPServersKeyOpenCode is the object under mcp that holds one entry per
	// server; OpenCode V2 nests them there instead of directly under mcp.
	MCPServersKeyOpenCode = "servers"
)

// Engram endpoint single source of truth: env override name and its default.
const (
	// EnvEngramURL names the environment variable overriding the default endpoint.
	EnvEngramURL = "ENGRAM_BASE_URL"
	// DefaultEngramURL is the local Engram server endpoint used without the override.
	DefaultEngramURL = "http://127.0.0.1:7437"
)

// SectionEngramProtocol is the markdown section ID shared by inject and remove.
const SectionEngramProtocol = "engram-protocol"

// OpenCodeConfigPath returns <home>/.config/opencode/opencode.json.
func OpenCodeConfigPath(home string) string {
	return filepath.Join(home, OpenCodeConfigDir, OpenCodeConfigFile)
}

// OpenCodePromptPath returns <home>/.config/opencode/AGENTS.md.
func OpenCodePromptPath(home string) string {
	return filepath.Join(home, OpenCodeConfigDir, AgentsPromptFile)
}

// OpenCodePluginPath returns <home>/.config/opencode/plugins/<file>.
func OpenCodePluginPath(home, file string) string {
	return filepath.Join(home, OpenCodeConfigDir, OpenCodePluginsDir, file)
}
