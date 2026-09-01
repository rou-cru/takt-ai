package setup_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

func TestApplyIsIdempotent(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest(model.AgentClaudeCode, model.AgentCodex)

	plans, err := setup.BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	first, err := setup.Apply(root, plans)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for _, path := range []string{".claude/CLAUDE.md", ".codex/AGENTS.md"} {
		if !slices.Contains(first.Changed, path) {
			t.Errorf("Apply() changed = %v, want %q", first.Changed, path)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Errorf("installed %q: %v", path, err)
		}
	}

	plans, err = setup.BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	second, err := setup.Apply(root, plans)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(second.Changed) != 0 || len(second.Unchanged) != len(first.Changed)+len(first.Unchanged) {
		t.Fatalf("second Apply() result = %#v, want all artifacts unchanged", second)
	}
}

func TestTargetsDoNotRequireUnselectedNativeOptions(t *testing.T) {
	tests := []struct {
		name      string
		target    model.AgentID
		ownPrompt string
		otherPath string
	}{
		{name: "Claude only", target: model.AgentClaudeCode, ownPrompt: ".claude/CLAUDE.md", otherPath: ".codex/AGENTS.md"},
		{name: "Codex only", target: model.AgentCodex, ownPrompt: ".codex/AGENTS.md", otherPath: ".claude/CLAUDE.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plans, err := setup.BuildTargetPlans(setuputil.TestPlanRequest(tc.target))
			if err != nil {
				t.Fatalf("BuildTargetPlans() error = %v", err)
			}
			result, err := setup.Apply(t.TempDir(), plans)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if !slices.Contains(result.Changed, tc.ownPrompt) {
				t.Errorf("Apply() changed = %v, want %q", result.Changed, tc.ownPrompt)
			}
			if slices.Contains(result.Changed, tc.otherPath) {
				t.Errorf("Apply() changed unselected target path %q", tc.otherPath)
			}
		})
	}
}
