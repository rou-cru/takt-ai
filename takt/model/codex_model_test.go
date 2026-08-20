package model_test

import (
	"fmt"
	"slices"
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

func TestResolveCodexSubAgentAssignment_AcceptsProviderModelID(t *testing.T) {
	const customModel = "openai/custom-model"
	modelID, effort, err := model.ResolveCodexSubAgentAssignment(model.SubAgentTaktDev, map[string]model.ModelAssignment{
		model.SubAgentTaktDev: {Model: customModel, Effort: "custom-effort"},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if modelID != customModel || effort != "custom-effort" {
		t.Errorf("got (%q, %q), want (%q, %q)", modelID, effort, customModel, "custom-effort")
	}
}

func TestResolveCodexSubAgentAssignment_PartialOverridePreservesDefaults(t *testing.T) {
	assignments := map[string]model.ModelAssignment{
		model.SubAgentTaktPM: {Model: model.CodexModelTerra, Effort: "medium"},
	}
	modelID, effort, err := model.ResolveCodexSubAgentAssignment(model.SubAgentTaktDev, assignments)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if modelID != model.CodexModelLuna || effort != "high" {
		t.Errorf("got (%q, %q), want (%q, %q)", modelID, effort, model.CodexModelLuna, "high")
	}
}

func TestResolveCodexSubAgentAssignment_ExplicitDefaultFallback(t *testing.T) {
	assignments := map[string]model.ModelAssignment{
		model.SubAgentDefault: {Model: model.CodexModelSol, Effort: "high"},
	}
	modelID, effort, err := model.ResolveCodexSubAgentAssignment("unknown", assignments)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if modelID != model.CodexModelSol || effort != "high" {
		t.Errorf("got (%q, %q), want (%q, %q)", modelID, effort, model.CodexModelSol, "high")
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

func TestRenderCodexSubAgentAssignments_PartialOverride(t *testing.T) {
	assignments := map[string]model.ModelAssignment{
		"takt-pm": {Model: model.CodexModelTerra, Effort: "medium"},
	}
	out, err := model.RenderCodexSubAgentAssignments(assignments)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	for _, wantRow := range []string{
		"| `takt-pm` | `openai/gpt-5.6-terra` | `medium` |",
		"| `takt-dev` | `openai/gpt-5.6-luna` | `high` |",
	} {
		if !strings.Contains(out, wantRow) {
			t.Errorf("row %q not found; output:\n%s", wantRow, out)
		}
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

// TestResolveCodexSubAgentAssignment_UnrelatedOverridePreservesPreset verifies
// that a partial override does not remove canonical assignments.
func TestResolveCodexSubAgentAssignment_UnrelatedOverridePreservesPreset(t *testing.T) {
	assignments := map[string]model.ModelAssignment{
		"some-other-sub-agent": {Model: model.CodexModelSol, Effort: "high"},
	}
	modelID, effort, err := model.ResolveCodexSubAgentAssignment("takt-dev", assignments)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if modelID != model.CodexModelLuna || effort != "high" {
		t.Errorf("got (%q, %q), want (%q, %q)", modelID, effort, model.CodexModelLuna, "high")
	}
}

// TestResolveCodexSubAgentAssignment_ErrorWhenAssignmentHasEmptyModel
// verifies that an assignment present in the map but with an empty Model
// field is treated as missing.
func TestResolveCodexSubAgentAssignment_ErrorWhenAssignmentHasEmptyModel(t *testing.T) {
	assignments := map[string]model.ModelAssignment{
		"takt-dev": {Model: "", Effort: "high"},
	}
	_, _, err := model.ResolveCodexSubAgentAssignment("takt-dev", assignments)
	if got, want := fmt.Sprint(err), `codex sub-agent "takt-dev" has no model assignment`; got != want {
		t.Errorf("error = %q, want %q", got, want)
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
