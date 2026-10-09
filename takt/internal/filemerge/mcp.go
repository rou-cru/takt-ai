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
	"fmt"

	"github.com/rou-cru/takt-ai/takt/model"
)

// InjectMCPServer registers a local stdio MCP server under name in the OpenCode
// config at path. OpenCode requires command as an array for type:local servers
// (a separate "args" field is rejected); ReplaceSentinel swaps the whole server
// object so upgrades from the old shape converge instead of accumulating both,
// and the merge patch leaves shared configs' user keys untouched.
func InjectMCPServer(path, name string, command []string) (model.InjectionResult, error) {
	overlay := map[string]any{
		model.MCPKeyOpenCode: map[string]any{
			model.MCPServersKeyOpenCode: map[string]any{
				name: map[string]any{
					ReplaceSentinel: map[string]any{
						"command":  command,
						"type":     "local",
						"disabled": false,
					},
				},
			},
		},
	}
	encoded, _ := json.MarshalIndent(overlay, "", "  ")
	write, err := MergeJSONFile(path, append(encoded, '\n'))
	if err != nil {
		return model.InjectionResult{}, fmt.Errorf("merge opencode %s mcp config: %w", name, err)
	}
	// An install carried over from OpenCode V1 still holds the flat mcp.<name>
	// entry. Left behind it would register the server twice, since V2
	// normalizes the V1 shape alongside its own.
	dropped, err := RemoveJSONKey(path, name, model.MCPKeyOpenCode)
	if err != nil {
		return model.InjectionResult{}, fmt.Errorf("drop the V1 opencode %s mcp entry: %w", name, err)
	}
	return model.InjectionResult{Changed: write.Changed || dropped, Files: []string{path}}, nil
}

// RemoveMCPServer strips the name entry so uninstalls leave no orphan keys and
// merged user keys are never touched.
func RemoveMCPServer(path, name string) (model.InjectionResult, error) {
	changed, err := RemoveJSONKey(path, name, model.MCPKeyOpenCode, model.MCPServersKeyOpenCode)
	if err != nil {
		return model.InjectionResult{}, fmt.Errorf("remove %s from %s: %w", name, path, err)
	}
	if !changed {
		return model.InjectionResult{}, nil
	}
	return model.InjectionResult{Changed: true, Files: []string{path}}, nil
}
