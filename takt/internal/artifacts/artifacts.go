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

// Package artifacts normalizes relative paths shared by agent-file renderers
// and the deployer.
package artifacts

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// NormalizeRelPath validates and cleans a slash-separated relative path.
// It rejects empty paths, backslashes, parent-directory components, and absolute paths.
func NormalizeRelPath(candidate string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.Contains(candidate, "\\") {
		return "", fmt.Errorf("path must use slash separators")
	}
	if slices.Contains(strings.Split(candidate, "/"), "..") {
		return "", fmt.Errorf("path must not contain '..'")
	}
	clean := path.Clean(candidate)
	if clean == "." || path.IsAbs(clean) {
		return "", fmt.Errorf("path must be relative")
	}
	return clean, nil
}
