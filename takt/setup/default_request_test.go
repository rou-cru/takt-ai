package setup

import (
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

func TestDefaultPlanRequestCoversRenderableCatalog(t *testing.T) {
	request, err := DefaultPlanRequest([]model.AgentID{model.AgentClaudeCode})
	if err != nil {
		t.Fatalf("DefaultPlanRequest() error = %v", err)
	}
	semantic, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load() error = %v", err)
	}

	want := make([]string, 0, len(semantic)-1)
	for _, definition := range semantic {
		if definition.Name != "default" {
			want = append(want, definition.Name)
		}
	}
	got := make([]string, len(request.Content))
	for index, content := range request.Content {
		got[index] = content.ID
	}
	if !slices.Equal(got, want) {
		t.Fatalf("content IDs = %v, want catalog order %v", got, want)
	}
	if slices.Contains(got, "default") {
		t.Fatal("default content must not be renderable")
	}

	productDesigner := catalog.NativeSubAgentContent{}
	for _, content := range request.Content {
		if content.ID == "takt-product-designer" {
			productDesigner = content
			break
		}
	}
	if productDesigner.ID != "takt-product-designer" || !slices.Equal(productDesigner.ClaudeTools, []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch"}) || productDesigner.CodexSandboxMode != codex.SandboxReadOnly || productDesigner.CodexWebSearch != codex.WebSearchLive {
		t.Fatalf("product designer native options = %+v", productDesigner)
	}
}

func TestDefaultPlanRequestBuildsSelectedTargets(t *testing.T) {
	tests := []struct {
		name    string
		targets []model.AgentID
		want    []string
	}{
		{"Claude", []model.AgentID{model.AgentClaudeCode}, []string{"claude-code"}},
		{"Codex", []model.AgentID{model.AgentCodex}, []string{"codex"}},
		{"OpenCode", []model.AgentID{model.AgentOpenCode}, []string{"opencode"}},
		{"Claude and Codex", []model.AgentID{model.AgentCodex, model.AgentClaudeCode}, []string{"claude-code", "codex"}},
		{"Claude and OpenCode", []model.AgentID{model.AgentOpenCode, model.AgentClaudeCode}, []string{"claude-code", "opencode"}},
		{"Codex and OpenCode", []model.AgentID{model.AgentOpenCode, model.AgentCodex}, []string{"codex", "opencode"}},
		{"all in noncanonical order", []model.AgentID{model.AgentOpenCode, model.AgentCodex, model.AgentClaudeCode}, []string{"claude-code", "codex", "opencode"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := DefaultPlanRequest(tt.targets)
			if err != nil {
				t.Fatalf("DefaultPlanRequest() error = %v", err)
			}
			plans, err := BuildTargetPlans(request)
			if err != nil {
				t.Fatalf("BuildTargetPlans() error = %v", err)
			}
			if got := planTargets(plans); !slices.Equal(got, tt.want) {
				t.Fatalf("plan targets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultPlanRequestReturnsIndependentValues(t *testing.T) {
	first, err := DefaultPlanRequest([]model.AgentID{model.AgentClaudeCode, model.AgentCodex})
	if err != nil {
		t.Fatalf("first DefaultPlanRequest() error = %v", err)
	}
	second, err := DefaultPlanRequest([]model.AgentID{model.AgentClaudeCode, model.AgentCodex})
	if err != nil {
		t.Fatalf("second DefaultPlanRequest() error = %v", err)
	}
	first.Targets[0] = model.AgentOpenCode
	first.Content[0].ClaudeTools[0] = "mutated"
	first.Claude.Settings.Settings["mutated"] = true
	if second.Targets[0] != model.AgentClaudeCode || second.Content[0].ClaudeTools[0] == "mutated" {
		t.Fatalf("second request was mutated: %+v", second)
	}
	if _, exists := second.Claude.Settings.Settings["mutated"]; exists {
		t.Fatal("second Claude settings were mutated")
	}
}

func TestDefaultPlanRequestRejectsInvalidTargets(t *testing.T) {
	for _, targets := range [][]model.AgentID{nil, {"unknown"}, {model.AgentCodex, model.AgentCodex}} {
		_, err := DefaultPlanRequest(targets)
		if err == nil {
			t.Fatalf("DefaultPlanRequest(%v) error = nil", targets)
		}
		if !strings.Contains(err.Error(), "default setup request") {
			t.Fatalf("error %q does not identify the default request", err)
		}
	}
}
