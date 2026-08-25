package catalog

import (
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/model"
)

func joinBoth(catalog []model.CanonicalSubAgent, content []NativeSubAgentContent) ([]NativeSubAgent, error) {
	return JoinNativeContentForTargets(catalog, content, []model.AgentID{model.AgentClaudeCode, model.AgentCodex})
}

func TestJoinNativeContent(t *testing.T) {
	semantic := []model.CanonicalSubAgent{
		{Name: "first", Assignments: map[model.AgentID]model.ModelAssignment{
			model.AgentClaudeCode: {Model: "sonnet"},
			model.AgentCodex:      {Model: model.CodexModelLuna, Effort: "high"},
		}},
		{Name: "default", Assignments: map[model.AgentID]model.ModelAssignment{
			model.AgentClaudeCode: {Model: "sonnet"},
			model.AgentCodex:      {Model: model.CodexModelTerra, Effort: "medium"},
		}},
		{Name: "second", Assignments: map[model.AgentID]model.ModelAssignment{
			model.AgentClaudeCode: {Model: "haiku"},
			model.AgentCodex:      {Model: model.CodexModelSol, Effort: "low"},
		}},
	}
	joined, err := joinBoth(semantic, []NativeSubAgentContent{
		{ID: "second", Description: "Second", Instructions: "Second instructions", ClaudeTools: []string{"Write", "Read"}, CodexSandboxMode: codex.SandboxWorkspaceWrite, CodexWebSearch: codex.WebSearchLive},
		{ID: "first", Description: "First", Instructions: "First instructions", ClaudeTools: []string{"Bash"}, CodexSandboxMode: codex.SandboxReadOnly, CodexWebSearch: codex.WebSearchDisabled},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{joined[0].ID, joined[1].ID}; !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("joined IDs = %v, want catalog order without default", got)
	}
	if got, want := joined[0].Assignments[model.AgentCodex], (model.ModelAssignment{Model: model.CodexModelLuna, Effort: "high"}); got != want {
		t.Errorf("first Codex assignment = %#v, want %#v", got, want)
	}
	if got, want := joined[1].ClaudeTools, []string{"Read", "Write"}; !slices.Equal(got, want) {
		t.Errorf("second Claude tools = %v, want %v", got, want)
	}
}

func TestJoinNativeContentValidation(t *testing.T) {
	semantic := validSemanticCatalog()
	validContent := validNativeContent("takt-dev")
	tests := []struct {
		name    string
		catalog []model.CanonicalSubAgent
		content []NativeSubAgentContent
		wantErr string
	}{
		{name: "empty catalog", catalog: nil, content: nil, wantErr: "catalog is empty"},
		{name: "missing content", catalog: semantic, content: nil, wantErr: "missing native content"},
		{name: "duplicate content ID", catalog: semantic, content: []NativeSubAgentContent{validContent, validContent}, wantErr: "duplicate native content id"},
		{name: "unknown content ID", catalog: semantic, content: []NativeSubAgentContent{validNativeContent("unknown")}, wantErr: "not in the catalog"},
		{name: "default content is rejected", catalog: semantic, content: []NativeSubAgentContent{validNativeContent("default")}, wantErr: "not renderable"},
		{name: "missing description", catalog: semantic, content: []NativeSubAgentContent{{ID: "takt-dev", Instructions: "work", CodexSandboxMode: codex.SandboxReadOnly, CodexWebSearch: codex.WebSearchDisabled}}, wantErr: "description is required"},
		{name: "missing instructions", catalog: semantic, content: []NativeSubAgentContent{{ID: "takt-dev", Description: "Agent", CodexSandboxMode: codex.SandboxReadOnly, CodexWebSearch: codex.WebSearchDisabled}}, wantErr: "instructions are required"},
		{name: "invalid ID whitespace", catalog: semantic, content: []NativeSubAgentContent{{ID: " takt-dev", Description: "Agent", Instructions: "work", CodexSandboxMode: codex.SandboxReadOnly, CodexWebSearch: codex.WebSearchDisabled}}, wantErr: "non-empty and trimmed"},
		{name: "invalid Claude tool", catalog: semantic, content: []NativeSubAgentContent{{ID: "takt-dev", Description: "Agent", Instructions: "work", ClaudeTools: []string{""}, CodexSandboxMode: codex.SandboxReadOnly, CodexWebSearch: codex.WebSearchDisabled}}, wantErr: "invalid Claude tool"},
		{name: "duplicate Claude tool", catalog: semantic, content: []NativeSubAgentContent{{ID: "takt-dev", Description: "Agent", Instructions: "work", ClaudeTools: []string{"Read", "Read"}, CodexSandboxMode: codex.SandboxReadOnly, CodexWebSearch: codex.WebSearchDisabled}}, wantErr: "duplicate Claude tool"},
		{name: "invalid Codex sandbox", catalog: semantic, content: []NativeSubAgentContent{{ID: "takt-dev", Description: "Agent", Instructions: "work", CodexSandboxMode: "unsafe", CodexWebSearch: codex.WebSearchDisabled}}, wantErr: "invalid Codex sandbox"},
		{name: "invalid Codex web search", catalog: semantic, content: []NativeSubAgentContent{{ID: "takt-dev", Description: "Agent", Instructions: "work", CodexSandboxMode: codex.SandboxReadOnly, CodexWebSearch: "unsafe"}}, wantErr: "invalid Codex web search"},
		{name: "duplicate catalog ID", catalog: append(semantic, semantic[0]), content: []NativeSubAgentContent{validContent}, wantErr: "duplicate catalog id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := joinBoth(tc.catalog, tc.content)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("joinBoth() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestJoinNativeContentDoesNotShareAssignments(t *testing.T) {
	semantic := validSemanticCatalog()
	joined, err := joinBoth(semantic, []NativeSubAgentContent{validNativeContent("takt-dev")})
	if err != nil {
		t.Fatal(err)
	}
	joined[0].Assignments[model.AgentClaudeCode] = model.ModelAssignment{Model: "mutated"}
	if got := semantic[0].Assignments[model.AgentClaudeCode].Model; got == "mutated" {
		t.Fatal("joined assignments share catalog map state")
	}
}

func TestJoinNativeContentForTargetsSelectsValidation(t *testing.T) {
	tests := []struct {
		name    string
		targets []model.AgentID
		mutate  func(*NativeSubAgentContent)
		wantErr string
	}{
		{
			name:    "Claude does not require Codex options",
			targets: []model.AgentID{model.AgentClaudeCode},
			mutate: func(entry *NativeSubAgentContent) {
				entry.CodexSandboxMode = ""
				entry.CodexWebSearch = ""
			},
		},
		{
			name:    "Codex does not require Claude tools",
			targets: []model.AgentID{model.AgentCodex},
			mutate: func(entry *NativeSubAgentContent) {
				entry.ClaudeTools = []string{""}
			},
		},
		{
			name:    "both require Codex options",
			targets: []model.AgentID{model.AgentClaudeCode, model.AgentCodex},
			mutate: func(entry *NativeSubAgentContent) {
				entry.CodexSandboxMode = ""
			},
			wantErr: "invalid Codex sandbox",
		},
		{
			name:    "both require Claude tools",
			targets: []model.AgentID{model.AgentClaudeCode, model.AgentCodex},
			mutate: func(entry *NativeSubAgentContent) {
				entry.ClaudeTools = []string{""}
			},
			wantErr: "invalid Claude tool",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := validNativeContent("takt-dev")
			tc.mutate(&entry)
			joined, err := JoinNativeContentForTargets(validSemanticCatalog(), []NativeSubAgentContent{entry}, tc.targets)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("JoinNativeContentForTargets() error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("JoinNativeContentForTargets() error = %v", err)
			}
			if len(joined) != 1 || joined[0].ID != "takt-dev" {
				t.Fatalf("joined = %#v, want takt-dev", joined)
			}
		})
	}
}

func validSemanticCatalog() []model.CanonicalSubAgent {
	assignment := map[model.AgentID]model.ModelAssignment{
		model.AgentClaudeCode: {Model: "sonnet"},
		model.AgentCodex:      {Model: model.CodexModelLuna, Effort: "high"},
	}
	return []model.CanonicalSubAgent{
		{Name: "takt-dev", Assignments: assignment},
		{Name: "default", Assignments: assignment},
	}
}

func validNativeContent(id string) NativeSubAgentContent {
	return NativeSubAgentContent{
		ID:               id,
		Description:      "Agent",
		Instructions:     "Work",
		ClaudeTools:      []string{"Read"},
		CodexSandboxMode: codex.SandboxReadOnly,
		CodexWebSearch:   codex.WebSearchDisabled,
	}
}
