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

// Package artifacts holds helpers shared by the agent-file renderers and the
// deployer: relative-path normalization, ID validation, and deterministic
// sorted output.
package artifacts

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// NormalizeRelPath validates and cleans a slash-separated relative path.
// It rejects empty paths, backslashes, ".." components, and absolute or
// NormalizeRelPath validates and cleans a slash-separated relative path.
// It rejects empty paths, backslashes, parent-directory components, and absolute paths.
func NormalizeRelPath(candidate string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.Contains(candidate, "\\") {
		return "", fmt.Errorf("path must use slash separators")
	}
	for _, component := range strings.Split(candidate, "/") {
		if component == ".." {
			return "", fmt.Errorf("path must not contain '..'")
		}
	}
	clean := path.Clean(candidate)
	if clean == "." || path.IsAbs(clean) || isWindowsAbsolute(clean) {
		return "", fmt.Errorf("path must be relative")
	}
	return clean, nil
}

// isWindowsAbsolute reports whether candidate has a Windows-style drive-letter prefix.
func isWindowsAbsolute(candidate string) bool {
	return len(candidate) >= 2 && candidate[1] == ':'
}

// ValidateID rejects empty, untrimmed, separator-bearing, and dot-only IDs.
// ValidateID validates a sub-agent ID and includes the owning target label in any error message.
func ValidateID(label, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s sub-agent id is required", label)
	}
	if id != strings.TrimSpace(id) || strings.ContainsAny(id, "/\\\x00") || id == "." || id == ".." {
		return fmt.Errorf("invalid %s sub-agent id %q", label, id)
	}
	return nil
}

// EnsureTrailingNewline appends a newline unless one is already present.
func EnsureTrailingNewline(content string) string {
	if strings.HasSuffix(content, "\n") {
		return content
	}
	return content + "\n"
}

// SortUniqueByPath renders items deterministically: it rejects two items that
// normalize to the same path and sorts by path. Label names the owning target
// SortUniqueByPath returns a copy of items sorted by their paths, or an error if
// multiple items have the same path. The label identifies the artifact type in
// the duplicate-path error.
func SortUniqueByPath[T any](items []T, pathOf func(T) string, label string) ([]T, error) {
	out := make([]T, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		itemPath := pathOf(item)
		if _, exists := seen[itemPath]; exists {
			return nil, fmt.Errorf("duplicate %s artifact path %q", label, itemPath)
		}
		seen[itemPath] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return pathOf(out[i]) < pathOf(out[j]) })
	return out, nil
}
