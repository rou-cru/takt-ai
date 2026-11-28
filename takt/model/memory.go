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

// MemoryServerKey is the MCP server key clients register the memory service
// under; it is distinctive so it never replaces or collides with a user's
// server under a generic key such as "memory".
const MemoryServerKey = "takt_memory"

// MemoryPathAdmin is the admin HTTP path of the Takt memory service.
const MemoryPathAdmin = "/admin"

// Names of the six tools served identically on /crew and /external, the single source of tool names.
const (
	MemoryToolRecord          = "record"
	MemoryToolSearch          = "search"
	MemoryToolRead            = "read"
	MemoryToolSessions        = "sessions"
	MemoryToolContinueSession = "continue_session"
	MemoryToolCloseSession    = "close_session"
)

// Names of the /admin memory tools referenced by host integrations.
const (
	MemoryAdminBind     = "bind"
	MemoryAdminDescribe = "describe"
)

// MemoryPrefixedToolName is the name OpenCode gives a tool of the memory MCP
// server: the server key, an underscore, and the tool.
func MemoryPrefixedToolName(tool string) string {
	return MemoryServerKey + "_" + tool
}
