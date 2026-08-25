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
		if subAgent.Persona != officialPersona {
			t.Errorf("%q persona = %q, want %q", subAgent.Name, subAgent.Persona, officialPersona)
		}
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
	claude := ClaudeDefaultPreset()
	codex := CodexDefaultPreset()
	if len(claude) != len(codex) || len(claude) != len(CanonicalSubAgents()) {
		t.Fatalf("preset sizes differ: Claude=%d Codex=%d catalog=%d", len(claude), len(codex), len(CanonicalSubAgents()))
	}
	if got := claude["takt-init"]; got != (model.ModelAssignment{Model: "haiku"}) {
		t.Errorf("ClaudeDefaultPreset()[takt-init] = %#v, want haiku", got)
	}
	if got := codex["takt-dev"]; got != (model.ModelAssignment{Model: model.CodexModelLuna, Effort: "high"}) {
		t.Errorf("CodexDefaultPreset()[takt-dev] = %#v, want Luna/high", got)
	}
}

func TestResolveAssignmentsPreservePrecedence(t *testing.T) {
	tests := []struct {
		name     string
		subAgent string
		override map[string]model.ModelAssignment
		want     model.ModelAssignment
		wantErr  string
	}{
		{name: "Claude specialist override", subAgent: "takt-dev", override: map[string]model.ModelAssignment{"takt-dev": {Model: "opus", Effort: "max"}}, want: model.ModelAssignment{Model: "opus", Effort: "max"}},
		{name: "Claude default override for unknown", subAgent: "unknown", override: map[string]model.ModelAssignment{"default": {Model: "haiku"}}, want: model.ModelAssignment{Model: "haiku"}},
		{name: "Claude invalid model override", subAgent: "takt-dev", override: map[string]model.ModelAssignment{"takt-dev": {Model: "unknown"}}, wantErr: "invalid model assignment"},
		{name: "Claude unsupported effort override", subAgent: "takt-dev", override: map[string]model.ModelAssignment{"takt-dev": {Model: "haiku", Effort: "high"}}, wantErr: "does not support effort"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveClaudeSubAgentAssignment(tc.subAgent, tc.override)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ResolveClaudeSubAgentAssignment() error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveClaudeSubAgentAssignment() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("assignment = %#v, want %#v", got, tc.want)
			}
		})
	}
	modelID, effort, err := ResolveCodexSubAgentAssignment("takt-dev", map[string]model.ModelAssignment{
		"takt-dev": {Model: model.CodexModelSol, Effort: "high"},
	})
	if err != nil {
		t.Fatalf("ResolveCodexSubAgentAssignment() error = %v", err)
	}
	if modelID != model.CodexModelSol || effort != "high" {
		t.Errorf("Codex assignment = (%q, %q), want Sol/high", modelID, effort)
	}
}

func TestResolveCodexSubAgentAssignment_AllowsProviderOverride(t *testing.T) {
	const customModel = "openai/custom-model"
	modelID, effort, err := ResolveCodexSubAgentAssignment("takt-dev", map[string]model.ModelAssignment{
		"takt-dev": {Model: customModel, Effort: "custom-effort"},
	})
	if err != nil {
		t.Fatalf("ResolveCodexSubAgentAssignment() error = %v", err)
	}
	if modelID != customModel || effort != "custom-effort" {
		t.Errorf("assignment = (%q, %q), want (%q, %q)", modelID, effort, customModel, "custom-effort")
	}
}

func names(catalog []model.CanonicalSubAgent) []string {
	result := make([]string, len(catalog))
	for index, subAgent := range catalog {
		result[index] = subAgent.Name
	}
	return result
}
