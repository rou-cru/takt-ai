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

package catalog

import (
	"sync"

	"github.com/rou-cru/takt-ai/takt/model"
)

// loadNativeContent shares one parsed content map because the asset catalog never changes at runtime.
var loadNativeContent = sync.OnceValues(func() (map[string]NativeSubAgentContent, error) {
	c, err := LoadPackages()
	if err != nil {
		return nil, err
	}
	return buildNativeContent(c)
})

// LoadNativeContent returns specialist prose keyed by instance ID for joining with catalog models.
func LoadNativeContent() (map[string]NativeSubAgentContent, error) {
	return loadNativeContent()
}

// buildNativeContent expands each agent definition into its declared instances (the orchestrator
// agent "takt" has no semantic catalog entry and is composed separately by the orchestrator itself).
func buildNativeContent(c Catalog) (map[string]NativeSubAgentContent, error) {
	fsys := AssetFS()
	content := make(map[string]NativeSubAgentContent)
	for _, def := range c.Agents {
		if def.Role == model.RoleOrchestrator {
			continue
		}
		text, err := def.ComposeText(fsys)
		if err != nil {
			return nil, err
		}
		for _, instanceID := range def.Instances {
			profile := def.Profile(instanceID)
			content[instanceID] = NativeSubAgentContent{
				ID:           instanceID,
				Description:  profile.Description,
				Instructions: text,
				Role:         profile.Role,
			}
		}
	}
	return content, nil
}
