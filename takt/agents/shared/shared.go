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

// Package shared holds helpers reused by the Claude, Codex, and OpenCode
// target adapters. Each adapter keeps its own exported types and API.
package shared

import (
	"fmt"
	"sort"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
)

// NormalizeManagedPaths normalizes, deduplicates, and sorts a target adapter's
// native plus generated managed paths. Errors are labeled with the adapter's
// target name.
func NormalizeManagedPaths(target string, pathGroups ...[]string) ([]string, error) {
	total := 0
	for _, group := range pathGroups {
		total += len(group)
	}
	normalized := make([]string, 0, total)
	seen := make(map[string]struct{}, total)
	for _, group := range pathGroups {
		for _, candidate := range group {
			clean, err := artifacts.NormalizeRelPath(candidate)
			if err != nil {
				return nil, fmt.Errorf("invalid %s managed path %s: %w", target, candidate, err)
			}
			if _, exists := seen[clean]; exists {
				return nil, fmt.Errorf("duplicate %s managed path %s", target, clean)
			}
			seen[clean] = struct{}{}
			normalized = append(normalized, clean)
		}
	}
	sort.Strings(normalized)
	return normalized, nil
}

// RenderSorted renders every request and returns the artifacts sorted by
// deterministic path. It centralizes the identical RenderSubAgents loop of
// the three target adapters.
func RenderSorted[Request, ArtifactT any](requests []Request, render func(Request) (ArtifactT, error), pathOf func(ArtifactT) string, target string) ([]ArtifactT, error) {
	rendered := make([]ArtifactT, 0, len(requests))
	for _, request := range requests {
		artifact, err := render(request)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, artifact)
	}
	return artifacts.SortUniqueByPath(rendered, pathOf, target)
}
