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
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
)

// TestMCPArgsExcludeWriteTools verifies the registered tool set is read-only.
func TestMCPArgsExcludeWriteTools(t *testing.T) {
	args := strings.Join(mcpArgs(), " ")
	if strings.Contains(args, "--tools=agent") {
		t.Fatalf("tools not restricted: %s", args)
	}
	for _, forbidden := range []string{"mem_update", "mem_capture_passive", "mem_judge", "mem_delete", "mem_save", "mem_session_summary", "mem_session_start", "mem_session_end"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("forbidden tool %s registered: %s", forbidden, args)
		}
	}
	for _, required := range []string{"mem_current_project", "mem_context", "mem_search", "mem_get_observation"} {
		if !strings.Contains(args, required) {
			t.Fatalf("required read tool %s missing from %s", required, args)
		}
	}
}

// TestMemorySkillPathSharesSpecialtyAndNamesOrchestrator verifies every instance of a specialty resolves to that specialty's one deployed skill.
func TestMemorySkillPathSharesSpecialtyAndNamesOrchestrator(t *testing.T) {
	cases := map[string]string{
		"architect":           ".opencode/skills/takt-memory-architect/SKILL.md",
		"judge-a":             ".opencode/skills/takt-memory-judge/SKILL.md",
		"judge-b":             ".opencode/skills/takt-memory-judge/SKILL.md",
		shared.OrchestratorID: ".opencode/skills/takt-memory-orchestrator/SKILL.md",
	}
	for _, instance := range []string{"simplify"} {
		cases[instance] = ".opencode/skills/takt-memory-simplify/SKILL.md"
	}
	for author, want := range cases {
		if got := MemorySkillPath(author); got != want {
			t.Errorf("MemorySkillPath(%q) = %q, want %q", author, got, want)
		}
	}
}

func TestMemorySkillContractPath(t *testing.T) {
	if ContractSkillPath != ".opencode/skills/takt-memory-contract/SKILL.md" {
		t.Errorf("ContractSkillPath = %q, want %q", ContractSkillPath, ".opencode/skills/takt-memory-contract/SKILL.md")
	}
}
