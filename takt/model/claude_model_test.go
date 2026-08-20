package model_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

// TestClaudeModelAliasValid verifies that Valid accepts exactly the four
// known aliases and rejects empty, unknown, uppercase, and full-model-ID inputs.
func TestClaudeModelAliasValid(t *testing.T) {
	tests := []struct {
		name  string
		input model.ClaudeModelAlias
		want  bool
	}{
		{"fable", model.ClaudeModelFable, true},
		{"opus", model.ClaudeModelOpus, true},
		{"sonnet", model.ClaudeModelSonnet, true},
		{"haiku", model.ClaudeModelHaiku, true},
		{"empty", model.ClaudeModelAlias(""), false},
		{"junk", model.ClaudeModelAlias("junk"), false},
		{"uppercase", model.ClaudeModelAlias("FABLE"), false},
		{"full model id", model.ClaudeModelAlias("not-a-model-id"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("ClaudeModelAlias(%q).Valid() = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestClaudeModelAliasString verifies that the fable alias renders as its
// literal string value.
func TestClaudeModelAliasString(t *testing.T) {
	if got := model.ClaudeModelFable.String(); got != "fable" {
		t.Errorf("ClaudeModelFable.String() = %q, want %q", got, "fable")
	}
}

func TestClaudeDefaultPreset_CoversCanonicalCatalog(t *testing.T) {
	preset := model.ClaudeDefaultPreset()
	if len(preset) != len(model.CanonicalSubAgents()) {
		t.Fatalf("preset has %d entries, want %d", len(preset), len(model.CanonicalSubAgents()))
	}
	for _, subAgent := range model.CanonicalSubAgents() {
		assignment, ok := preset[subAgent]
		if !ok || !model.ClaudeModelAlias(assignment.Model).Valid() {
			t.Errorf("missing or invalid assignment for %q: %#v", subAgent, assignment)
		}
	}
}

func TestClaudeDefaultPreset_Assignments(t *testing.T) {
	preset := model.ClaudeDefaultPreset()
	want := map[string]model.ModelAssignment{
		"takt-init":    {Model: "haiku"},
		"takt-judge-a": {Model: "sonnet"},
		"takt-judge-b": {Model: "sonnet"},
		"default":      {Model: "sonnet"},
		"takt-verify":  {Model: "sonnet", Effort: "high"},
	}
	for subAgent, expected := range want {
		if preset[subAgent] != expected {
			t.Errorf("preset[%q] = %#v, want %#v", subAgent, preset[subAgent], expected)
		}
	}
}

func TestClaudeEffortsForModelOfficialMatrix(t *testing.T) {
	tests := []struct {
		name  string
		alias model.ClaudeModelAlias
		want  []model.ClaudeEffort
	}{
		{"fable", model.ClaudeModelFable, []model.ClaudeEffort{"", "low", "medium", "high", "xhigh", "max"}},
		{"opus", model.ClaudeModelOpus, []model.ClaudeEffort{"", "low", "medium", "high", "xhigh", "max"}},
		{"sonnet", model.ClaudeModelSonnet, []model.ClaudeEffort{"", "low", "medium", "high", "max"}},
		{"haiku", model.ClaudeModelHaiku, []model.ClaudeEffort{""}},
		{"invalid", model.ClaudeModelAlias("bogus"), []model.ClaudeEffort{""}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := model.ClaudeEffortsForModel(tc.alias)
			if len(got) != len(tc.want) {
				t.Fatalf("ClaudeEffortsForModel(%q) len = %d, want %d (%v)", tc.alias, len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("ClaudeEffortsForModel(%q)[%d] = %q, want %q (all: %v)", tc.alias, i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
