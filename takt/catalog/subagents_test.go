package catalog

import (
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestLoadEmbeddedCatalog(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	wantNames := []string{
		"takt-init", "takt-analyst", "takt-pm", "takt-spec", "takt-architect",
		"takt-product-designer", "takt-tpm", "takt-dev", "takt-verify",
		"takt-judge-a", "takt-judge-b", "takt-fix", "default",
	}
	if got := names(catalog); !slices.Equal(got, wantNames) {
		t.Fatalf("catalog names = %v, want %v", got, wantNames)
	}
	for _, subAgent := range catalog {
		for _, agent := range []model.AgentID{model.AgentClaudeCode, model.AgentCodex} {
			assignment, ok := subAgent.Assignments[agent]
			if !ok || assignment.Model == "" {
				t.Errorf("%q missing valid %s assignment: %#v", subAgent.Name, agent, assignment)
			}
		}
	}
}

func TestLoadYAMLValidation(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "missing id",
			data: "- persona: takt\n  assignments: {claude-code: {model: sonnet}, codex: {model: openai/gpt-5.6-luna}}\n",
			want: "missing id",
		},
		{
			name: "duplicate id",
			data: "- id: same\n  persona: takt\n  assignments: {claude-code: {model: sonnet}, codex: {model: openai/gpt-5.6-luna}}\n- id: same\n  persona: takt\n  assignments: {claude-code: {model: sonnet}, codex: {model: openai/gpt-5.6-luna}}\n",
			want: "duplicate id",
		},
		{
			name: "missing target",
			data: "- id: only-claude\n  persona: takt\n  assignments: {claude-code: {model: sonnet}}\n",
			want: "Claude and Codex assignments",
		},
		{
			name: "unknown target",
			data: "- id: unknown-target\n  persona: takt\n  assignments: {claude-code: {model: sonnet}, opencode: {model: model}}\n",
			want: "unsupported assignment target",
		},
		{
			name: "empty model",
			data: "- id: empty-model\n  persona: takt\n  assignments: {claude-code: {model: ''}, codex: {model: openai/gpt-5.6-luna}}\n",
			want: "has no model",
		},
		{
			name: "invalid Claude model",
			data: "- id: invalid-claude\n  persona: takt\n  assignments: {claude-code: {model: unknown}, codex: {model: openai/gpt-5.6-luna}}\n",
			want: "invalid model assignment",
		},
		{
			name: "invalid persona",
			data: "- id: invalid-persona\n  persona: other\n  assignments: {claude-code: {model: sonnet}, codex: {model: openai/gpt-5.6-luna}}\n",
			want: "persona must be",
		},
		{
			name: "invalid Claude effort",
			data: "- id: invalid-effort\n  persona: takt\n  assignments: {claude-code: {model: haiku, effort: high}, codex: {model: openai/gpt-5.6-luna}}\n",
			want: "does not support effort",
		},
		{
			name: "invalid Codex model",
			data: "- id: invalid-codex-model\n  persona: takt\n  assignments: {claude-code: {model: sonnet}, codex: {model: openai/unknown, effort: high}}\n",
			want: "invalid model assignment",
		},
		{
			name: "invalid Codex effort",
			data: "- id: invalid-codex-effort\n  persona: takt\n  assignments: {claude-code: {model: sonnet}, codex: {model: openai/gpt-5.6-luna, effort: xhigh}}\n",
			want: "invalid effort",
		},
		{
			name: "unknown field",
			data: "- id: unknown-field\n  persona: takt\n  extra: value\n  assignments: {claude-code: {model: sonnet}, codex: {model: openai/gpt-5.6-luna}}\n",
			want: "field extra not found",
		},
		{
			name: "missing default",
			data: "- id: specialist\n  persona: takt\n  assignments: {claude-code: {model: sonnet}, codex: {model: openai/gpt-5.6-luna}}\n",
			want: "missing default",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load([]byte(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("load() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestCanonicalQueries(t *testing.T) {
	if got, want := CanonicalSubAgents(), names(CanonicalSubAgentCatalog()); !slices.Equal(got, want) {
		t.Errorf("CanonicalSubAgents() = %v, want %v", got, want)
	}
}

func names(catalog []model.CanonicalSubAgent) []string {
	result := make([]string, len(catalog))
	for index, subAgent := range catalog {
		result[index] = subAgent.Name
	}
	return result
}
