package model_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestClaudeModelAliasValid(t *testing.T) {
	tests := []struct {
		name  string
		input model.ClaudeModelAlias
		want  bool
	}{
		{name: "fable", input: model.ClaudeModelFable, want: true},
		{name: "opus", input: model.ClaudeModelOpus, want: true},
		{name: "sonnet", input: model.ClaudeModelSonnet, want: true},
		{name: "haiku", input: model.ClaudeModelHaiku, want: true},
		{name: "empty", input: "", want: false},
		{name: "unknown", input: "unknown", want: false},
		{name: "uppercase", input: "FABLE", want: false},
		{name: "full model id", input: "not-a-model-id", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("ClaudeModelAlias(%q).Valid() = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestClaudeModelAliasString(t *testing.T) {
	if got := model.ClaudeModelFable.String(); got != "fable" {
		t.Errorf("ClaudeModelFable.String() = %q, want fable", got)
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
		{name: "fable", alias: model.ClaudeModelFable, want: []model.ClaudeEffort{"", "low", "medium", "high", "xhigh", "max"}},
		{name: "opus", alias: model.ClaudeModelOpus, want: []model.ClaudeEffort{"", "low", "medium", "high", "xhigh", "max"}},
		{name: "sonnet", alias: model.ClaudeModelSonnet, want: []model.ClaudeEffort{"", "low", "medium", "high", "max"}},
		{name: "haiku", alias: model.ClaudeModelHaiku, want: []model.ClaudeEffort{""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := model.ClaudeEffortsForModel(tc.alias)
			if len(got) != len(tc.want) {
				t.Fatalf("ClaudeEffortsForModel(%q) len = %d, want %d", tc.alias, len(got), len(tc.want))
			}
			for index := range tc.want {
				if got[index] != tc.want[index] {
					t.Errorf("ClaudeEffortsForModel(%q)[%d] = %q, want %q", tc.alias, index, got[index], tc.want[index])
				}
			}
		})
	}
}

func TestClaudeEffortValid(t *testing.T) {
	tests := []struct {
		name  string
		input model.ClaudeEffort
		want  bool
	}{
		{name: "default", input: model.ClaudeEffortDefault, want: true},
		{name: "low", input: model.ClaudeEffortLow, want: true},
		{name: "medium", input: model.ClaudeEffortMedium, want: true},
		{name: "high", input: model.ClaudeEffortHigh, want: true},
		{name: "xhigh", input: model.ClaudeEffortXHigh, want: true},
		{name: "max", input: model.ClaudeEffortMax, want: true},
		{name: "unknown", input: "unknown", want: false},
		{name: "uppercase", input: "HIGH", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("ClaudeEffort(%q).Valid() = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestClaudeEffortAllowedForModel(t *testing.T) {
	tests := []struct {
		name   string
		alias  model.ClaudeModelAlias
		effort model.ClaudeEffort
		want   bool
	}{
		{name: "fable allows xhigh", alias: model.ClaudeModelFable, effort: model.ClaudeEffortXHigh, want: true},
		{name: "opus allows max", alias: model.ClaudeModelOpus, effort: model.ClaudeEffortMax, want: true},
		{name: "sonnet allows high", alias: model.ClaudeModelSonnet, effort: model.ClaudeEffortHigh, want: true},
		{name: "sonnet rejects xhigh", alias: model.ClaudeModelSonnet, effort: model.ClaudeEffortXHigh, want: false},
		{name: "haiku rejects low", alias: model.ClaudeModelHaiku, effort: model.ClaudeEffortLow, want: false},
		{name: "haiku allows default", alias: model.ClaudeModelHaiku, effort: model.ClaudeEffortDefault, want: true},
		{name: "unknown effort", alias: model.ClaudeModelOpus, effort: "unknown", want: false},
		{name: "unknown alias allows default", alias: "unknown", effort: model.ClaudeEffortDefault, want: true},
		{name: "unknown alias rejects high", alias: "unknown", effort: model.ClaudeEffortHigh, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.ClaudeEffortAllowedForModel(tc.alias, tc.effort); got != tc.want {
				t.Errorf("ClaudeEffortAllowedForModel(%q, %q) = %v, want %v", tc.alias, tc.effort, got, tc.want)
			}
		})
	}
}
