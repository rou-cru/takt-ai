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

// Package claude renders Claude Code agent files: sub-agent Markdown
// frontmatter, global prompt, and settings JSON.
package claude

import (
	"github.com/rou-cru/takt-ai/takt/agents/shared"
)

var nativeManagedPaths = []string{
	".claude/CLAUDE.md",
	".claude/settings.json",
}

// Manifest lists only Takt-owned Claude paths relative to the user's home
// directory. Every path not listed is outside this ownership contract and is
// preserved by future lifecycle consumers.
type Manifest struct {
	paths []string
}

// NewManifest creates a Claude manifest from renderer-generated paths.
// NewManifest constructs a manifest containing native and generated Claude-managed
// paths, normalized and sorted deterministically. It returns an error for invalid
// paths or duplicate normalized paths.
func NewManifest(generatedPaths []string) (Manifest, error) {
	paths, err := shared.NormalizeManagedPaths("Claude", nativeManagedPaths, generatedPaths)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{paths: paths}, nil
}

// ManagedPaths returns a copy of the deterministic Takt-owned path list.
func (m Manifest) ManagedPaths() []string {
	return append([]string(nil), m.paths...)
}
