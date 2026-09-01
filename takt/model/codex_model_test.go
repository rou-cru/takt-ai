package model_test

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

// TestCodexAvailableModels_Contents checks the available model IDs.
func TestCodexAvailableModels_Contents(t *testing.T) {
	models := model.CodexAvailableModels()
	want := []string{"openai/gpt-5.6-sol", "openai/gpt-5.6-terra", "openai/gpt-5.6-luna"}
	if len(models) != len(want) {
		t.Fatalf("len = %d, want %d", len(models), len(want))
	}
	for i, w := range want {
		if models[i] != w {
			t.Errorf("[%d] = %q, want %q", i, models[i], w)
		}
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
			got := model.FilterCodexModels(tc.query)
			if !slices.Equal(got, tc.want) {
				t.Errorf("FilterCodexModels(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// TestCodexAvailableModels_ReturnsCopy verifies that mutating the returned
// slice does not affect subsequent calls, i.e. CodexAvailableModels does not
// leak its internal backing array.
func TestCodexAvailableModels_ReturnsCopy(t *testing.T) {
	first := model.CodexAvailableModels()
	if len(first) == 0 {
		t.Fatal("CodexAvailableModels() returned no models")
	}
	first[0] = "mutated"

	second := model.CodexAvailableModels()
	if second[0] == "mutated" {
		t.Error("mutating the returned slice affected a subsequent call; CodexAvailableModels leaks internal state")
	}
	if second[0] != model.CodexModelSol {
		t.Errorf("second[0] = %q, want %q", second[0], model.CodexModelSol)
	}
}
