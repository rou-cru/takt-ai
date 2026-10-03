package runtime_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

// Review counts skills, not the files each skill deploys.
func TestPreviewSummaryCountsSkillsNotFiles(t *testing.T) {
	plan, err := runtime.Adapter{}.PreviewPlan(runtime.PreviewRequest{Action: runtime.ActionInstall, RootDir: t.TempDir(), Components: []string{"context7"}})
	if err != nil {
		t.Fatalf("PreviewPlan() error = %v", err)
	}
	packages, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Skills != len(packages.Skills) {
		t.Fatalf("Summary.Skills = %d, want the %d catalog skills", plan.Summary.Skills, len(packages.Skills))
	}
	if len(plan.Summary.Agents) == 0 || len(plan.Summary.MCPServers) != 3 || len(plan.Summary.Integrations) == 0 {
		t.Fatalf("Summary = %+v, want agents, three MCP servers and the integrations", plan.Summary)
	}
}
