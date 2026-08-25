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

package codex

import (
	"github.com/rou-cru/takt-ai/takt/agents/shared"
)

var nativeManagedPaths = []string{
	".codex/AGENTS.md",
	".codex/config.toml",
}

// Manifest lists only Takt-owned Codex paths relative to the user's home
// directory. Every path not listed is outside this ownership contract and is
// preserved by future lifecycle consumers.
type Manifest struct {
	paths []string
}

// NewManifest creates a Codex manifest from renderer-generated paths.
// NewManifest creates a manifest containing native Codex files and generated paths.
// It normalizes and sorts the paths, returning an error for invalid or duplicate paths.
func NewManifest(generatedPaths []string) (Manifest, error) {
	paths, err := shared.NormalizeManagedPaths("Codex", nativeManagedPaths, generatedPaths)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{paths: paths}, nil
}

// ManagedPaths returns a copy of the deterministic Takt-owned path list.
func (m Manifest) ManagedPaths() []string {
	return append([]string(nil), m.paths...)
}
