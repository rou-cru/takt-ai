package model_test

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

// renderCodexSubAgentAssignments renders a table for tests.
func renderCodexSubAgentAssignments(assignments map[string]model.ModelAssignment) string {
	out, err := model.RenderCodexSubAgentAssignments(assignments)
	if err != nil {
		return ""
	}
	return out
}

func TestCodexDefaultPreset_CoversAllSubAgents(t *testing.T) {
	preset := model.CodexDefaultPreset()
	wantSubAgents := []string{
		"takt-init", "takt-analyst", "takt-pm", "takt-spec", "takt-architect", "takt-product-designer", "takt-tpm",
		"takt-dev", "takt-verify",
		"takt-judge-a", "takt-judge-b", "takt-fix", "default",
	}
	if len(preset) != len(wantSubAgents) {
		t.Errorf("preset has %d entries, want %d", len(preset), len(wantSubAgents))
	}
	for _, sa := range wantSubAgents {
		a, ok := preset[sa]
		if !ok {
			t.Errorf("preset missing sub-agent %q", sa)
			continue
		}
		if a.Model == "" {
			t.Errorf("preset[%q] has empty model", sa)
		}
		if a.Effort == "" {
			t.Errorf("preset[%q] has empty effort", sa)
		}
	}
}

func TestCodexDefaultPreset_ModelDistribution(t *testing.T) {
	preset := model.CodexDefaultPreset()
	want := map[string]model.ModelAssignment{
		"takt-init":             {Model: model.CodexModelLuna, Effort: "low"},
		"takt-analyst":          {Model: model.CodexModelLuna, Effort: "medium"},
		"takt-pm":               {Model: model.CodexModelSol, Effort: "high"},
		"takt-spec":             {Model: model.CodexModelTerra, Effort: "medium"},
		"takt-architect":        {Model: model.CodexModelSol, Effort: "high"},
		"takt-product-designer": {Model: model.CodexModelSol, Effort: "high"},
		"takt-tpm":              {Model: model.CodexModelTerra, Effort: "medium"},
		"takt-dev":              {Model: model.CodexModelLuna, Effort: "high"},
		"takt-verify":           {Model: model.CodexModelLuna, Effort: "high"},
		"takt-judge-a":          {Model: model.CodexModelLuna, Effort: "high"},
		"takt-judge-b":          {Model: model.CodexModelLuna, Effort: "high"},
		"takt-fix":              {Model: model.CodexModelTerra, Effort: "medium"},
		"default":               {Model: model.CodexModelTerra, Effort: "medium"},
	}
	for subAgent, expected := range want {
		if preset[subAgent] != expected {
			t.Errorf("preset[%q] = %#v, want %#v", subAgent, preset[subAgent], expected)
		}
	}
}

func TestResolveCodexSubAgentAssignment_Known(t *testing.T) {
	modelID, effort, err := model.ResolveCodexSubAgentAssignment("takt-dev", nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if modelID != model.CodexModelLuna {
		t.Errorf("model = %q, want %s", modelID, model.CodexModelLuna)
	}
	if effort != "high" {
		t.Errorf("effort = %q, want high", effort)
	}
}

func TestResolveCodexSubAgentAssignment_UnknownFallsBackToDefault(t *testing.T) {
	modelID, effort, err := model.ResolveCodexSubAgentAssignment("not-a-sub-agent", nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	// Falls back to default → terra/medium
	if modelID != model.CodexModelTerra {
		t.Errorf("model = %q, want %s (default)", modelID, model.CodexModelTerra)
	}
	if effort != "medium" {
		t.Errorf("effort = %q, want medium (default)", effort)
	}
}

func TestResolveCodexSubAgentAssignment_CustomOverride(t *testing.T) {
	assignments := map[string]model.ModelAssignment{
		"takt-dev": {Model: model.CodexModelSol, Effort: "high"},
	}
	modelID, effort, err := model.ResolveCodexSubAgentAssignment("takt-dev", assignments)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if modelID != model.CodexModelSol {
		t.Errorf("model = %q, want %s", modelID, model.CodexModelSol)
	}
	if effort != "high" {
		t.Errorf("effort = %q, want high", effort)
	}
}

func TestRenderCodexSubAgentAssignments_Deterministic(t *testing.T) {
	out1 := renderCodexSubAgentAssignments(nil)
	out2 := renderCodexSubAgentAssignments(nil)
	if out1 != out2 {
		t.Error("not deterministic")
	}
}

func TestRenderCodexSubAgentAssignments_AllSubAgentsPresent(t *testing.T) {
	out := renderCodexSubAgentAssignments(nil)
	subAgents := []string{
		"takt-init", "takt-analyst", "takt-pm", "takt-spec", "takt-architect", "takt-product-designer", "takt-tpm",
		"takt-dev", "takt-verify",
		"takt-judge-a", "takt-judge-b", "takt-fix", "default",
	}
	for _, sa := range subAgents {
		if !strings.Contains(out, sa) {
			t.Errorf("missing sub-agent %q", sa)
		}
	}
}

func TestRenderCodexSubAgentAssignments_Header(t *testing.T) {
	out := renderCodexSubAgentAssignments(nil)
	if !strings.Contains(out, "Sub-Agent") {
		t.Error("missing Sub-Agent header")
	}
	if !strings.Contains(out, "Model") {
		t.Error("missing Model header")
	}
	if !strings.Contains(out, "reasoning_effort") {
		t.Error("missing reasoning_effort header")
	}
}

func TestRenderCodexSubAgentAssignments_CustomModel(t *testing.T) {
	assignments := model.CodexDefaultPreset()
	assignments["takt-pm"] = model.ModelAssignment{Model: model.CodexModelTerra, Effort: "medium"}
	out := renderCodexSubAgentAssignments(assignments)
	wantRow := "| `takt-pm` | `openai/gpt-5.6-terra` | `medium` |"
	if !strings.Contains(out, wantRow) {
		t.Errorf("custom model row not found; output:\n%s", out)
	}
}

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
		query    string
		wantAny  []string
		wantNone []string
	}{
		{"sol", []string{"openai/gpt-5.6-sol"}, []string{"openai/gpt-5.6-terra"}},
		{"TERRA", []string{"openai/gpt-5.6-terra"}, nil},
		{"zzz", nil, []string{"openai/gpt-5.6-sol"}},
		{"", []string{"openai/gpt-5.6-sol", "openai/gpt-5.6-terra", "openai/gpt-5.6-luna"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			result := model.FilterCodexModels(tc.query)
			rs := make(map[string]bool)
			for _, r := range result {
				rs[r] = true
			}
			for _, w := range tc.wantAny {
				if !rs[w] {
					t.Errorf("expected %q in %v", w, result)
				}
			}
			for _, nw := range tc.wantNone {
				if rs[nw] {
					t.Errorf("unexpected %q in %v", nw, result)
				}
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

// TestResolveCodexSubAgentAssignment_EmptyMapUsesDefaultPreset verifies that
// a non-nil but empty assignment map is treated the same as nil, falling
// back to the default preset.
func TestResolveCodexSubAgentAssignment_EmptyMapUsesDefaultPreset(t *testing.T) {
	modelID, effort, err := model.ResolveCodexSubAgentAssignment("takt-dev", map[string]model.ModelAssignment{})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if modelID != model.CodexModelLuna {
		t.Errorf("model = %q, want %s", modelID, model.CodexModelLuna)
	}
	if effort != "high" {
		t.Errorf("effort = %q, want high", effort)
	}
}

// TestResolveCodexSubAgentAssignment_ErrorWhenNoModel verifies that an error
// is returned when the sub-agent and the default fallback both lack a model
// assignment.
func TestResolveCodexSubAgentAssignment_ErrorWhenNoModel(t *testing.T) {
	assignments := map[string]model.ModelAssignment{
		"some-other-sub-agent": {Model: model.CodexModelSol, Effort: "high"},
	}
	modelID, effort, err := model.ResolveCodexSubAgentAssignment("takt-dev", assignments)
	if err == nil {
		t.Fatal("error = nil, want error when no model is assigned")
	}
	if modelID != "" || effort != "" {
		t.Errorf("got (%q, %q), want (\"\", \"\") on error", modelID, effort)
	}
}

// TestResolveCodexSubAgentAssignment_ErrorWhenAssignmentHasEmptyModel
// verifies that an assignment present in the map but with an empty Model
// field is treated as missing.
func TestResolveCodexSubAgentAssignment_ErrorWhenAssignmentHasEmptyModel(t *testing.T) {
	assignments := map[string]model.ModelAssignment{
		"takt-dev": {Model: "", Effort: "high"},
	}
	if _, _, err := model.ResolveCodexSubAgentAssignment("takt-dev", assignments); err == nil {
		t.Error("error = nil, want error when assignment has an empty model")
	}
}

// TestRenderCodexSubAgentAssignments_ErrorWhenNoModel verifies that
// rendering fails with an error when a sub-agent has no assignment and there
// is no usable default fallback.
func TestRenderCodexSubAgentAssignments_ErrorWhenNoModel(t *testing.T) {
	assignments := map[string]model.ModelAssignment{
		"default": {Model: "", Effort: ""},
	}
	out, err := model.RenderCodexSubAgentAssignments(assignments)
	if err == nil {
		t.Fatal("error = nil, want error when no sub-agent has a model")
	}
	if out != "" {
		t.Errorf("output = %q, want empty string on error", out)
	}
}
