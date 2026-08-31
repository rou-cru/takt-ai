package setup_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/claude"
	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestApplyIsIdempotent(t *testing.T) {
	root := t.TempDir()
	request := testPlanRequest(model.AgentClaudeCode, model.AgentCodex)

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
			plans, err := setup.BuildTargetPlans(testPlanRequest(tc.target))
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

func testPlanRequest(targets ...model.AgentID) setup.PlanRequest {
	semantic, err := catalog.Load()
	if err != nil {
		panic(err)
	}
	content := make([]catalog.NativeSubAgentContent, 0, len(semantic)-1)
	for _, entry := range semantic {
		if entry.Name == "default" {
			continue
		}
		native := catalog.NativeSubAgentContent{
			ID:           entry.Name,
			Description:  entry.Name + " test specialist",
			Instructions: "Follow the explicit test instructions.",
		}
		if slices.Contains(targets, model.AgentClaudeCode) {
			native.ClaudeTools = []string{"Read"}
		}
		if slices.Contains(targets, model.AgentCodex) {
			native.CodexSandboxMode = codex.SandboxWorkspaceWrite
			native.CodexWebSearch = codex.WebSearchDisabled
		}
		content = append(content, native)
	}

	request := setup.PlanRequest{Targets: targets, Content: content}
	if slices.Contains(targets, model.AgentClaudeCode) {
		request.Claude = setup.ClaudePlanOptions{
			GlobalPrompt: "Use the explicit Claude test prompt.",
			Settings:     claude.SettingsRequest{Settings: map[string]any{"enabled": true}},
		}
	}
	if slices.Contains(targets, model.AgentCodex) {
		request.Codex = setup.CodexPlanOptions{
			GlobalPrompt: "Use the explicit Codex test prompt.",
			SandboxMode:  codex.SandboxWorkspaceWrite,
			WebSearch:    codex.WebSearchDisabled,
			MaxThreads:   2,
			MaxDepth:     1,
		}
	}
	return request
}
