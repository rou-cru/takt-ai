package setup

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/claude"
	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

func TestBuildTargetPlans(t *testing.T) {
	tests := []struct {
		name        string
		targets     []model.AgentID
		clearClaude bool
		clearCodex  bool
		wantTargets []string
	}{
		{name: "Claude only", targets: []model.AgentID{model.AgentClaudeCode}, clearCodex: true, wantTargets: []string{string(model.AgentClaudeCode)}},
		{name: "Codex only", targets: []model.AgentID{model.AgentCodex}, clearClaude: true, wantTargets: []string{string(model.AgentCodex)}},
		{name: "OpenCode only", targets: []model.AgentID{model.AgentOpenCode}, clearClaude: true, clearCodex: true, wantTargets: []string{string(model.AgentOpenCode)}},
		{name: "all targets", targets: []model.AgentID{model.AgentOpenCode, model.AgentCodex, model.AgentClaudeCode}, wantTargets: []string{string(model.AgentClaudeCode), string(model.AgentCodex), string(model.AgentOpenCode)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := validPlanRequest()
			request.Targets = tc.targets
			if tc.clearClaude {
				request.Claude = ClaudePlanOptions{}
				for index := range request.Content {
					request.Content[index].ClaudeTools = []string{""}
				}
			}
			if tc.clearCodex {
				request.Codex = CodexPlanOptions{}
				for index := range request.Content {
					request.Content[index].CodexSandboxMode = ""
					request.Content[index].CodexWebSearch = ""
				}
			}
			plans, err := BuildTargetPlans(request)
			if err != nil {
				t.Fatalf("BuildTargetPlans() error = %v", err)
			}
			if got := planTargets(plans); !slices.Equal(got, tc.wantTargets) {
				t.Fatalf("plan targets = %v, want %v", got, tc.wantTargets)
			}
			for _, plan := range plans {
				if got := artifactPaths(plan.Artifacts); !slices.Equal(got, plan.ManagedPaths) {
					t.Errorf("%s manifest paths = %v, artifact paths = %v", plan.Target, plan.ManagedPaths, got)
				}
				for _, artifact := range plan.Artifacts {
					if strings.Contains(artifact.Path, "/default.") {
						t.Errorf("default fallback was rendered: %q", artifact.Path)
					}
				}
			}
		})
	}
}

func TestBuildTargetPlansUsesCatalogAssignmentsAndOverrides(t *testing.T) {
	request := validPlanRequest()
	request.Targets = []model.AgentID{model.AgentClaudeCode, model.AgentCodex}
	request.ClaudeModelOverrides = map[string]model.ModelAssignment{
		"takt-init": {Model: "opus", Effort: "max"},
	}
	request.CodexModelOverrides = map[string]model.ModelAssignment{
		"takt-dev": {Model: model.CodexModelSol, Effort: "high"},
	}
	plans, err := BuildTargetPlans(request)
	if err != nil {
		t.Fatal(err)
	}
	claudeAgent := findArtifact(plans[0], ".claude/agents/takt-init.md")
	if !bytes.Contains(claudeAgent.Content, []byte("model: opus\n")) || !bytes.Contains(claudeAgent.Content, []byte("effort: max\n")) {
		t.Errorf("Claude override was not rendered: %s", claudeAgent.Content)
	}
	codexPlan := plans[1]
	codexAgent := findArtifact(codexPlan, ".codex/agents/takt-dev.toml")
	if !bytes.Contains(codexAgent.Content, []byte("model = \"openai/gpt-5.6-sol\"\n")) {
		t.Errorf("Codex override was not rendered: %s", codexAgent.Content)
	}
	config := findArtifact(codexPlan, ".codex/config.toml")
	if !bytes.Contains(config.Content, []byte("model = \"openai/gpt-5.6-terra\"\n")) {
		t.Errorf("Codex global assignment did not come from catalog default: %s", config.Content)
	}
}

func TestBuildTargetPlansDeterministicRegardlessOfInputOrder(t *testing.T) {
	firstRequest := validPlanRequest()
	first, err := BuildTargetPlans(firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := validPlanRequest()
	secondRequest.Targets = []model.AgentID{model.AgentCodex, model.AgentClaudeCode}
	secondRequest.Content = reverseContent(secondRequest.Content)
	second, err := BuildTargetPlans(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(first, second, equalTargetPlan) {
		t.Fatalf("plans differ for equivalent input order:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestBuildTargetPlansValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PlanRequest)
		want   string
	}{
		{name: "no targets", mutate: func(request *PlanRequest) { request.Targets = nil }, want: "at least one target"},
		{name: "unsupported target", mutate: func(request *PlanRequest) { request.Targets = []model.AgentID{"unknown"} }, want: "unsupported target"},
		{name: "duplicate target", mutate: func(request *PlanRequest) {
			request.Targets = []model.AgentID{model.AgentClaudeCode, model.AgentClaudeCode}
		}, want: "duplicate target"},
		{name: "missing native content", mutate: func(request *PlanRequest) { request.Content = nil }, want: "missing native content"},
		{name: "missing Claude global prompt", mutate: func(request *PlanRequest) {
			request.Targets = []model.AgentID{model.AgentClaudeCode}
			request.Claude.GlobalPrompt = ""
		}, want: "claude global prompt is required"},
		{name: "invalid Codex sandbox", mutate: func(request *PlanRequest) {
			request.Targets = []model.AgentID{model.AgentCodex}
			request.Codex.SandboxMode = "invalid"
		}, want: "invalid Codex sandbox mode"},
		{name: "invalid Codex limits", mutate: func(request *PlanRequest) {
			request.Targets = []model.AgentID{model.AgentCodex}
			request.Codex.MaxDepth = 0
		}, want: "limits must be positive"},
		{name: "both require Codex content options", mutate: func(request *PlanRequest) {
			request.Content[0].CodexSandboxMode = ""
		}, want: "invalid Codex sandbox"},
		{name: "both require Claude content options", mutate: func(request *PlanRequest) {
			request.Content[0].ClaudeTools = []string{""}
		}, want: "invalid Claude tool"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := validPlanRequest()
			tc.mutate(&request)
			plans, err := BuildTargetPlans(request)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BuildTargetPlans() plans=%#v error=%v, want substring %q", plans, err, tc.want)
			}
			if plans != nil {
				t.Errorf("plans = %#v, want nil on validation failure", plans)
			}
		})
	}
}

func TestBuildTargetPlansOpenCodeUsesDefaultModel(t *testing.T) {
	request := validPlanRequest()
	request.Targets = []model.AgentID{model.AgentOpenCode}
	request.Claude = ClaudePlanOptions{}
	request.Codex = CodexPlanOptions{}
	plans, err := BuildTargetPlans(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Target != string(model.AgentOpenCode) {
		t.Fatalf("plans = %#v, want one OpenCode plan", plans)
	}
	plan := plans[0]
	config := findArtifact(plan, ".config/opencode/opencode.json")
	if !bytes.Contains(config.Content, []byte("\"model\": \"openai/gpt-5.6-luna\"")) {
		t.Errorf("OpenCode config did not use the default model: %s", config.Content)
	}
	agent := findArtifact(plan, ".config/opencode/agents/takt-dev.md")
	if !bytes.Contains(agent.Content, []byte("model: openai/gpt-5.6-luna\n")) {
		t.Errorf("OpenCode sub-agent did not use the default model: %s", agent.Content)
	}
}

func TestBuildTargetPlansOpenCodeModelOverride(t *testing.T) {
	request := validPlanRequest()
	request.Targets = []model.AgentID{model.AgentOpenCode}
	request.OpenCode = OpenCodePlanOptions{Model: "anthropic/claude-sonnet-4-5"}
	plans, err := BuildTargetPlans(request)
	if err != nil {
		t.Fatal(err)
	}
	config := findArtifact(plans[0], ".config/opencode/opencode.json")
	if !bytes.Contains(config.Content, []byte("\"model\": \"anthropic/claude-sonnet-4-5\"")) {
		t.Errorf("OpenCode model override was not rendered: %s", config.Content)
	}
}

func validPlanRequest() PlanRequest {
	content := make([]catalog.NativeSubAgentContent, 0, len(catalog.CanonicalSubAgents())-1)
	for _, id := range catalog.CanonicalSubAgents() {
		if id == "default" {
			continue
		}
		content = append(content, catalog.NativeSubAgentContent{
			ID:               id,
			Description:      id + " specialist",
			Instructions:     "Follow the supplied instructions.",
			ClaudeTools:      []string{"Write", "Read"},
			CodexSandboxMode: codex.SandboxWorkspaceWrite,
			CodexWebSearch:   codex.WebSearchDisabled,
		})
	}
	return PlanRequest{
		Targets: []model.AgentID{model.AgentClaudeCode, model.AgentCodex},
		Content: content,
		Claude: ClaudePlanOptions{
			GlobalPrompt: "Use the supplied Claude harness.",
			Settings:     claude.SettingsRequest{Settings: map[string]any{"enabled": true}},
		},
		Codex: CodexPlanOptions{
			GlobalPrompt: "Use the supplied Codex harness.",
			SandboxMode:  codex.SandboxWorkspaceWrite,
			WebSearch:    codex.WebSearchDisabled,
			MultiAgent:   true,
			MaxThreads:   4,
			MaxDepth:     2,
		},
	}
}

func reverseContent(content []catalog.NativeSubAgentContent) []catalog.NativeSubAgentContent {
	reversed := append([]catalog.NativeSubAgentContent(nil), content...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	return reversed
}

func planTargets(plans []TargetPlan) []string {
	targets := make([]string, len(plans))
	for index, plan := range plans {
		targets[index] = plan.Target
	}
	return targets
}

func artifactPaths(artifacts []Artifact) []string {
	paths := make([]string, len(artifacts))
	for index, artifact := range artifacts {
		paths[index] = artifact.Path
	}
	return paths
}

func findArtifact(plan TargetPlan, path string) Artifact {
	for _, artifact := range plan.Artifacts {
		if artifact.Path == path {
			return artifact
		}
	}
	return Artifact{}
}

func equalTargetPlan(a, b TargetPlan) bool {
	if a.Target != b.Target || !slices.Equal(a.ManagedPaths, b.ManagedPaths) || len(a.Artifacts) != len(b.Artifacts) {
		return false
	}
	for index := range a.Artifacts {
		if a.Artifacts[index].Path != b.Artifacts[index].Path || !bytes.Equal(a.Artifacts[index].Content, b.Artifacts[index].Content) {
			return false
		}
	}
	return true
}
