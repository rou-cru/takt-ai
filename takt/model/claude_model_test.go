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

func TestResolveClaudeSubAgentAssignment(t *testing.T) {
	tests := []struct {
		name      string
		subAgent  string
		overrides map[string]model.ModelAssignment
		want      model.ModelAssignment
		wantErr   bool
	}{
		{name: "nil uses preset", subAgent: model.SubAgentTaktInit, want: model.ModelAssignment{Model: "haiku"}},
		{name: "empty uses preset", subAgent: model.SubAgentTaktDev, overrides: map[string]model.ModelAssignment{}, want: model.ModelAssignment{Model: "sonnet"}},
		{name: "partial override preserves defaults", subAgent: model.SubAgentTaktDev, overrides: map[string]model.ModelAssignment{model.SubAgentTaktPM: {Model: "fable", Effort: "high"}}, want: model.ModelAssignment{Model: "sonnet"}},
		{name: "sub-agent override wins", subAgent: model.SubAgentTaktDev, overrides: map[string]model.ModelAssignment{model.SubAgentTaktDev: {Model: "opus", Effort: "max"}}, want: model.ModelAssignment{Model: "opus", Effort: "max"}},
		{name: "explicit default handles unknown", subAgent: "unknown", overrides: map[string]model.ModelAssignment{model.SubAgentDefault: {Model: "haiku"}}, want: model.ModelAssignment{Model: "haiku"}},
		{name: "preset default handles unknown", subAgent: "unknown", want: model.ModelAssignment{Model: "sonnet"}},
		{name: "missing model is invalid", subAgent: model.SubAgentTaktDev, overrides: map[string]model.ModelAssignment{model.SubAgentTaktDev: {}}, wantErr: true},
		{name: "unknown model is invalid", subAgent: model.SubAgentTaktDev, overrides: map[string]model.ModelAssignment{model.SubAgentTaktDev: {Model: "unknown"}}, wantErr: true},
		{name: "unsupported effort is invalid", subAgent: model.SubAgentTaktDev, overrides: map[string]model.ModelAssignment{model.SubAgentTaktDev: {Model: "haiku", Effort: "high"}}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := model.ResolveClaudeSubAgentAssignment(tc.subAgent, tc.overrides)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("assignment = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRenderClaudeEffortFrontmatter(t *testing.T) {
	tests := []struct {
		name       string
		assignment model.ModelAssignment
		want       string
		wantErr    bool
	}{
		{name: "default effort omitted", assignment: model.ModelAssignment{Model: "sonnet"}},
		{name: "explicit effort rendered", assignment: model.ModelAssignment{Model: "sonnet", Effort: "high"}, want: "effort: high"},
		{name: "invalid assignment rejected", assignment: model.ModelAssignment{Model: "haiku", Effort: "high"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := model.RenderClaudeEffortFrontmatter(tc.assignment)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("frontmatter = %q, want %q", got, tc.want)
			}
		})
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

// TestClaudeEffortValid verifies that Valid accepts exactly the known effort
// levels and rejects unknown or malformed values.
func TestClaudeEffortValid(t *testing.T) {
	tests := []struct {
		name  string
		input model.ClaudeEffort
		want  bool
	}{
		{"default", model.ClaudeEffortDefault, true},
		{"low", model.ClaudeEffortLow, true},
		{"medium", model.ClaudeEffortMedium, true},
		{"high", model.ClaudeEffortHigh, true},
		{"xhigh", model.ClaudeEffortXHigh, true},
		{"max", model.ClaudeEffortMax, true},
		{"junk", model.ClaudeEffort("junk"), false},
		{"uppercase", model.ClaudeEffort("HIGH"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("ClaudeEffort(%q).Valid() = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestClaudeEffortAllowedForModel verifies the effort/model compatibility
// matrix, including rejection of invalid efforts and unknown model aliases.
func TestClaudeEffortAllowedForModel(t *testing.T) {
	tests := []struct {
		name   string
		alias  model.ClaudeModelAlias
		effort model.ClaudeEffort
		want   bool
	}{
		{"fable allows xhigh", model.ClaudeModelFable, model.ClaudeEffortXHigh, true},
		{"opus allows max", model.ClaudeModelOpus, model.ClaudeEffortMax, true},
		{"sonnet allows high", model.ClaudeModelSonnet, model.ClaudeEffortHigh, true},
		{"sonnet rejects xhigh", model.ClaudeModelSonnet, model.ClaudeEffortXHigh, false},
		{"haiku rejects low", model.ClaudeModelHaiku, model.ClaudeEffortLow, false},
		{"haiku allows default", model.ClaudeModelHaiku, model.ClaudeEffortDefault, true},
		{"invalid effort string", model.ClaudeModelOpus, model.ClaudeEffort("bogus"), false},
		{"unknown alias only allows default", model.ClaudeModelAlias("bogus"), model.ClaudeEffortDefault, true},
		{"unknown alias rejects high", model.ClaudeModelAlias("bogus"), model.ClaudeEffortHigh, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.ClaudeEffortAllowedForModel(tc.alias, tc.effort); got != tc.want {
				t.Errorf("ClaudeEffortAllowedForModel(%q, %q) = %v, want %v", tc.alias, tc.effort, got, tc.want)
			}
		})
	}
}
