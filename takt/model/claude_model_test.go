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
