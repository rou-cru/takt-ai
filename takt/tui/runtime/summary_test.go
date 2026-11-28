package runtime_test

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

// Review counts active skills, not the files each skill deploys.
func TestPreviewSummaryCountsSkillsNotFiles(t *testing.T) {
	packages, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	coreSkills := 0
	for _, skill := range packages.Skills {
		if catalog.SkillActive(skill.Component, nil) {
			coreSkills++
		}
	}

	plan, err := runtime.Adapter{}.PreviewPlan(runtime.PreviewRequest{Action: runtime.ActionInstall, RootDir: t.TempDir(), Components: []string{"context7"}})
	if err != nil {
		t.Fatalf("PreviewPlan() error = %v", err)
	}
	if plan.Summary.Skills != coreSkills {
		t.Fatalf("Summary.Skills = %d, want the %d core catalog skills", plan.Summary.Skills, coreSkills)
	}
	if len(plan.Summary.Agents) == 0 || !slices.Equal(plan.Summary.MCPServers, []string{"codegraph", "context7", memory.ServerKey}) || len(plan.Summary.Integrations) == 0 {
		t.Fatalf("Summary = %+v, want agents, the codegraph, context7 and memory MCP servers, and the integrations", plan.Summary)
	}
	if slices.Contains(plan.Summary.Integrations, "Archify") {
		t.Fatalf("Summary.Integrations = %v, Archify must not appear when deselected", plan.Summary.Integrations)
	}

	withArchify, err := runtime.Adapter{}.PreviewPlan(runtime.PreviewRequest{Action: runtime.ActionInstall, RootDir: t.TempDir(), Components: []string{string(model.ComponentContext7), string(model.ComponentArchify)}})
	if err != nil {
		t.Fatalf("PreviewPlan(archify) error = %v", err)
	}
	if want := len(packages.Skills) + 1; withArchify.Summary.Skills != want {
		t.Fatalf("Summary.Skills with archify = %d, want %d", withArchify.Summary.Skills, want)
	}
	if !slices.Contains(withArchify.Summary.Integrations, "Archify") {
		t.Fatalf("Summary.Integrations with archify = %v, want Archify", withArchify.Summary.Integrations)
	}
}
