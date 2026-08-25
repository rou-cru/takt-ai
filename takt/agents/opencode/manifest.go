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

package opencode

import (
	"fmt"
	"sort"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
)

var nativeManagedPaths = []string{
	".config/opencode/opencode.json",
}

// Manifest lists only Takt-owned OpenCode paths relative to the user's home
// directory. Every path not listed is outside this ownership contract and is
// preserved by future lifecycle consumers.
type Manifest struct {
	paths []string
}

// NewManifest creates an OpenCode manifest from renderer-generated paths.
// The native global configuration file is always included.
func NewManifest(generatedPaths []string) (Manifest, error) {
	paths := make([]string, 0, len(nativeManagedPaths)+len(generatedPaths))
	paths = append(paths, nativeManagedPaths...)
	paths = append(paths, generatedPaths...)
	normalized := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, candidate := range paths {
		clean, err := artifacts.NormalizeRelPath(candidate)
		if err != nil {
			return Manifest{}, fmt.Errorf("invalid OpenCode managed path %s: %w", candidate, err)
		}
		if _, exists := seen[clean]; exists {
			return Manifest{}, fmt.Errorf("duplicate OpenCode managed path %s", clean)
		}
		seen[clean] = struct{}{}
		normalized = append(normalized, clean)
	}
	sort.Strings(normalized)
	return Manifest{paths: normalized}, nil
}

// ManagedPaths returns a copy of the deterministic Takt-owned path list.
func (m Manifest) ManagedPaths() []string {
	return append([]string(nil), m.paths...)
}
