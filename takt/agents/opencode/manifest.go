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

// Package opencode renders OpenCode agent files: sub-agent Markdown
// frontmatter and opencode.json configuration.
package opencode

import (
	"github.com/rou-cru/takt-ai/takt/agents/shared"
)

var nativeManagedPaths = []string{
	".config/opencode/opencode.json",
}

// NewManagedPaths returns Takt-owned OpenCode paths relative to the user's
// home directory: the native paths plus the renderer-generated ones,
// normalized and sorted. Every path not listed is outside this ownership
// contract and is preserved by future lifecycle consumers.
func NewManagedPaths(generated []string) ([]string, error) {
	return shared.NewManagedPaths("OpenCode", nativeManagedPaths, generated)
}
