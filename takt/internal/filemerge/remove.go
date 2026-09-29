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

package filemerge

import (
	"encoding/json"
	"errors"
	"os"
)

// RemoveJSONKey deletes key from the object at parents, keeping all other
// user content. Objects left empty are pruned up the path, so an untouched
// config never keeps a bare "mcp": {}.
func RemoveJSONKey(path string, key string, parents ...string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return false, nil
	}
	chain, ok := findJSONPath(root, parents)
	if !ok {
		return false, nil
	}
	leaf := chain[len(chain)-1]
	if _, exists := leaf[key]; !exists {
		return false, nil
	}
	delete(leaf, key)
	pruneJSONPath(chain, parents)
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, err
	}
	encoded = append(encoded, '\n')
	if _, err := WriteFileAtomic(path, encoded, DefaultFileMode); err != nil {
		return false, err
	}
	return true, nil
}

func findJSONPath(root map[string]any, parents []string) ([]map[string]any, bool) {
	chain := []map[string]any{root}
	for _, parent := range parents {
		nested, ok := chain[len(chain)-1][parent].(map[string]any)
		if !ok {
			return nil, false
		}
		chain = append(chain, nested)
	}
	return chain, true
}

func pruneJSONPath(chain []map[string]any, parents []string) {
	for i := len(chain) - 1; i > 0 && len(chain[i]) == 0; i-- {
		delete(chain[i-1], parents[i-1])
	}
}
