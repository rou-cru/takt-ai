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

package shared

// Artifact is a file ready to deploy, with its home-relative path and content.
type Artifact struct {
	Path    string
	Content []byte
}

// Context7RemoteURL is the shared docs-server endpoint so every target reaches the same library docs.
const Context7RemoteURL = "https://mcp.context7.com/mcp"
