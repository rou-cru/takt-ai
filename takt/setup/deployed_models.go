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

package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
	"github.com/rou-cru/takt-ai/takt/model"
)

// LoadDeployedModels reads the model each agent actually runs on from the
// OpenCode config on disk, the sole owner of that fact: the installed record
// misses every edit made outside Takt. Agents with no model of their own
// inherit the harness model and are absent from the result.
func LoadDeployedModels(rootDir string) (map[string]model.ModelAssignment, error) {
	path := opencode.ConfigPath()
	raw, err := os.ReadFile(filepath.Join(rootDir, filepath.FromSlash(path)))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]model.ModelAssignment{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read deployed models from %q: %w", path, err)
	}
	var config struct {
		Agents map[string]struct {
			Model any `json:"model"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(filemerge.NormalizeJSON(raw), &config); err != nil {
		return nil, fmt.Errorf("parse deployed models in %q: %w", path, err)
	}
	deployed := make(map[string]model.ModelAssignment, len(config.Agents))
	for id, agent := range config.Agents {
		if ref, ok := agent.Model.(string); ok && ref != "" {
			deployed[id] = model.ModelAssignment{Model: ref}
		}
	}
	return deployed, nil
}
