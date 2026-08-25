package model_test

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestCodexAvailableModels_Contents(t *testing.T) {
	got := model.CodexAvailableModels()
	want := []string{model.CodexModelSol, model.CodexModelTerra, model.CodexModelLuna}
	if !slices.Equal(got, want) {
		t.Errorf("CodexAvailableModels() = %v, want %v", got, want)
	}
}

func TestFilterCodexModels(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "case-insensitive match", query: "TERRA", want: []string{model.CodexModelTerra}},
		{name: "no matches", query: "zzz", want: []string{}},
		{name: "blank query preserves order", query: "", want: []string{model.CodexModelSol, model.CodexModelTerra, model.CodexModelLuna}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.FilterCodexModels(tc.query); !slices.Equal(got, tc.want) {
				t.Errorf("FilterCodexModels(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestCodexAvailableModels_ReturnsCopy(t *testing.T) {
	first := model.CodexAvailableModels()
	first[0] = "mutated"
	second := model.CodexAvailableModels()
	if second[0] != model.CodexModelSol {
		t.Errorf("second[0] = %q, want %q", second[0], model.CodexModelSol)
	}
}
