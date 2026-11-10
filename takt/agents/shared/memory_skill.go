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

package shared

import (
	"path"
	"strings"
	"sync"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// contractSkillID is the catalog skill ID for the memory contract.
const contractSkillID = "takt-memory-contract"

// ContractSkillPath is the memory contract every agent loads before memory calls.
var ContractSkillPath = path.Join(model.OpenCodeSkillsDir, contractSkillID, catalog.SkillFileName)

// specialties maps every declared instance to the id of the specialty definition it belongs to.
var specialties = sync.OnceValue(func() map[string]string {
	byInstance := map[string]string{}
	if cat, err := catalog.LoadPackages(); err == nil {
		for _, def := range cat.Agents {
			for _, instance := range def.Instances {
				byInstance[instance] = def.ID
			}
		}
	}
	return byInstance
})

// roleSkillID maps an author to its specialty's memory skill.
func roleSkillID(authorID string) string {
	if authorID == OrchestratorID {
		return "orchestrator"
	}
	if specialty, ok := specialties()[authorID]; ok {
		authorID = specialty
	}
	return strings.TrimPrefix(authorID, "takt-")
}

// MemorySkillPath returns the deployed memory skill for one author.
func MemorySkillPath(authorID string) string {
	return path.Join(model.OpenCodeSkillsDir, "takt-memory-"+roleSkillID(authorID), catalog.SkillFileName)
}
