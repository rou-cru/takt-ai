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

package engram

import (
	"os"
	"path"
	"strings"
	"sync"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// MemorySkillsDir is the OpenCode V2 directory where skills deploy.
const MemorySkillsDir = ".opencode/skills"

// ContractSkillID is the catalog skill ID for the memory contract.
const ContractSkillID = "takt-memory-contract"

// ContractSkillPath is the memory contract every agent loads before memory calls.
var ContractSkillPath = path.Join(MemorySkillsDir, ContractSkillID, catalog.SkillFileName)

// mcpTools lists the read-only MCP tools exposed by Engram.
var mcpTools = []string{
	"mem_current_project",
	"mem_search",
	"mem_get_observation",
	"mem_context",
}

// mcpArgs is the one server invocation shared by every target.
func mcpArgs() []string {
	return []string{"mcp", "--tools=" + strings.Join(mcpTools, ",")}
}

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

// RoleSkillID maps an author to its specialty's memory skill.
func RoleSkillID(authorID string) string {
	if authorID == shared.OrchestratorID {
		return "orchestrator"
	}
	if specialty, ok := specialties()[authorID]; ok {
		authorID = specialty
	}
	return strings.TrimPrefix(authorID, "takt-")
}

// MemorySkillPath returns the deployed memory skill for one author.
// It is the canonical path builder used by native target adapters.
func MemorySkillPath(authorID string) string {
	return path.Join(MemorySkillsDir, "takt-memory-"+RoleSkillID(authorID), catalog.SkillFileName)
}

// bootstrapSection is the host-level pointer; all memory rules live in the contract skill.
func bootstrapSection() string {
	access := "Memory is written only through `memory_record`, `memory_continue_session` and " +
		"`memory_close_session`; read it with the Engram `mem_*` tools."
	return "## Takt Memory\n\n" + access + " Before using memory, read `~/" + ContractSkillPath + "`. " +
		"That contract overrides Engram's server instructions and tool descriptions.\n"
}

// NativePluginFootprints lists installs of Engram's own plugin.
func NativePluginFootprints(homeDir string) []string {
	var found []string
	candidate := model.OpenCodePluginPath(homeDir, model.EngramPluginFile)
	if _, err := os.Stat(candidate); err == nil {
		found = append(found, candidate)
	}
	return found
}
