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

// Package shared holds helpers reused by the OpenCode target adapter.
package shared

import (
	"fmt"
	"slices"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
	"github.com/rou-cru/takt-ai/takt/model"
)

// NewManagedPaths returns OpenCode's owned paths, normalized and sorted for
// stable manifests, so uninstalls never touch user files.
func NewManagedPaths(generated []string) ([]string, error) {
	all := append([]string{model.OpenCodeConfigRelativePath}, generated...)
	normalized := make([]string, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, candidate := range all {
		clean, err := artifacts.NormalizeRelPath(candidate)
		if err != nil {
			return nil, fmt.Errorf("invalid OpenCode managed path %s: %w", candidate, err)
		}
		if _, exists := seen[clean]; exists {
			return nil, fmt.Errorf("duplicate OpenCode managed path %s", clean)
		}
		seen[clean] = struct{}{}
		normalized = append(normalized, clean)
	}
	slices.Sort(normalized)
	return normalized, nil
}
