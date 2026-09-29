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
	"strconv"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
	"github.com/rou-cru/takt-ai/takt/model"
)

// semverComponents is the number of numeric components in a semantic version.
const semverComponents = 3

// NewManagedPaths returns OpenCode's owned paths, normalized and sorted for
// stable manifests, so uninstalls never touch user files.
func NewManagedPaths(generated []string) ([]string, error) {
	return NormalizeManagedPaths("OpenCode", []string{model.OpenCodeConfigRelativePath}, generated)
}

// NormalizeManagedPaths cleans, dedupes, and sorts managed paths so manifests stay deterministic.
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
	slices.Sort(normalized)
	return normalized, nil
}

// VersionAtLeast reports whether the numeric MAJOR.MINOR.PATCH prefix of have
// is not older than want.
func VersionAtLeast(have, want string) bool {
	parsedHave, ok := parseSemver(have)
	if !ok {
		return false
	}
	parsedWant, _ := parseSemver(want)
	for i := range parsedHave {
		if parsedHave[i] != parsedWant[i] {
			return parsedHave[i] > parsedWant[i]
		}
	}
	return true
}

// parseSemver accepts numeric MAJOR.MINOR.PATCH; pre-release tags are rejected as not yet the pinned release.
func parseSemver(value string) ([3]int, bool) {
	var parsed [3]int
	value, _, _ = strings.Cut(strings.TrimPrefix(value, "v"), "+")
	parts := strings.Split(value, ".")
	if len(parts) != semverComponents {
		return parsed, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsed, false
		}
		parsed[i] = n
	}
	return parsed, true
}
