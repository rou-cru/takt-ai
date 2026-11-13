package shared_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
)

// TestMemorySkillPathSharesSpecialtyAndNamesOrchestrator verifies every instance of a specialty resolves
// to that specialty's one deployed skill.
func TestMemorySkillPathSharesSpecialtyAndNamesOrchestrator(t *testing.T) {
	cases := map[string]string{
		"architect":           ".opencode/skills/takt-memory-architect/SKILL.md",
		"judge-a":             ".opencode/skills/takt-memory-judge/SKILL.md",
		"judge-b":             ".opencode/skills/takt-memory-judge/SKILL.md",
		"simplify":            ".opencode/skills/takt-memory-simplify/SKILL.md",
		shared.OrchestratorID: ".opencode/skills/takt-memory-orchestrator/SKILL.md",
	}
	for author, want := range cases {
		if got := shared.MemorySkillPath(author); got != want {
			t.Errorf("MemorySkillPath(%q) = %q, want %q", author, got, want)
		}
	}
}

func TestMemorySkillContractPath(t *testing.T) {
	if want := ".opencode/skills/takt-memory-contract/SKILL.md"; shared.ContractSkillPath != want {
		t.Errorf("ContractSkillPath = %q, want %q", shared.ContractSkillPath, want)
	}
}
