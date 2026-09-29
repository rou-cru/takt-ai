// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

// Package filemerge merges Takt-managed content into user-owned JSON, TOML,
// and markdown configuration files so user edits survive deploys.
package filemerge

import (
	"errors"
	"fmt"
	"os"
)

// MergeJSONFile reads the JSON file at path (treating a missing file as
// empty/absent input), merges overlay into it, and atomically writes the
// result back to path.
func MergeJSONFile(path string, overlay []byte) (WriteResult, error) {
	baseJSON, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			baseJSON = nil
		} else {
			return WriteResult{}, fmt.Errorf("read json file %q: %w", path, err)
		}
	}

	merged, err := MergeJSONObjects(baseJSON, overlay)
	if err != nil {
		return WriteResult{}, err
	}

	return WriteFileAtomic(path, merged, DefaultFileMode)
}

// ReadFileOrEmpty reads the file at path, returning an empty string instead
// of an error when the file does not exist.
func ReadFileOrEmpty(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read file %q: %w", path, err)
	}
	return string(data), nil
}
