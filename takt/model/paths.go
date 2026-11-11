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

// OpenCode's home-relative configuration paths and the plugin filenames Takt
// deploys into its plugin directory.
const (
	// OpenCodeConfigDir is OpenCode's configuration directory under home.
	OpenCodeConfigDir = ".config/opencode"
	// OpenCodeConfigFile is OpenCode's configuration filename.
	OpenCodeConfigFile = "opencode.json"
	// OpenCodeConfigRelativePath is the slash-separated manifest path owned by OpenCode.
	OpenCodeConfigRelativePath = OpenCodeConfigDir + "/" + OpenCodeConfigFile
	// OpenCodeCLIConfigFile is the terminal client's configuration filename.
	OpenCodeCLIConfigFile = "cli.json"
	// OpenCodeCLIConfigRelativePath is the slash-separated manifest path of the terminal client configuration.
	OpenCodeCLIConfigRelativePath = OpenCodeConfigDir + "/" + OpenCodeCLIConfigFile
	// OpenCodeSkillsDir is the home-relative directory OpenCode V2 loads skills from.
	OpenCodeSkillsDir = ".opencode/skills"

	// OpenCodePluginsDir is the directory OpenCode loads plugins from.
	OpenCodePluginsDir = "plugins"
	// VFSPluginFile is the deployed governed-VFS plugin filename.
	VFSPluginFile = "takt-vfs.ts"
)

// MCP object keys per target native config.
const (
	// MCPKeyOpenCode is the top-level MCP map key in opencode.json.
	MCPKeyOpenCode = "mcp"
	// MCPServersKeyOpenCode is the object under mcp that holds one entry per
	// server; OpenCode V2 nests them there instead of directly under mcp.
	MCPServersKeyOpenCode = "servers"
)

// EnvSandboxAllowedDomains names the environment variable with which the user
// opts shell sandboxes into a network allowlist: comma-separated domain
// patterns such as "github.com,*.npmjs.org". Unset or empty leaves the network
// unrestricted; Takt imposes no domain of its own.
const EnvSandboxAllowedDomains = "TAKT_SANDBOX_ALLOWED_DOMAINS"

// OpenCodeConfigPath returns <home>/.config/opencode/opencode.json.
func OpenCodeConfigPath(home string) string {
	return filepath.Join(home, OpenCodeConfigDir, OpenCodeConfigFile)
}

// OpenCodePluginPath returns <home>/.config/opencode/plugins/<file>.
func OpenCodePluginPath(home, file string) string {
	return filepath.Join(home, OpenCodeConfigDir, OpenCodePluginsDir, file)
}
