package install

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// fixtureRoot is never created: fixtures must not depend on the machine.
const fixtureRoot = "/takt-fixture-home"

func fixtureModel(width, height int, step Step) Model {
	m := New(fixtureRoot)
	agents := []runtime.AgentModel{{Name: "takt", Role: model.RoleOrchestrator, Model: "openai/gpt-6-luna"}}
	for _, name := range []string{"analyst", "architect", "dev", "fix", "pm", "product-designer", "simplify", "spec", "tpm", "verify"} {
		agents = append(agents, runtime.AgentModel{Name: name, Role: model.RoleExecution, Model: "openai/gpt-6-luna"})
	}
	agents = append(agents, runtime.AgentModel{Name: "judge-a", Role: model.RoleVerification}, runtime.AgentModel{Name: "judge-b", Role: model.RoleVerification})
	m.plan = runtime.InstallPlan{
		Add:    make([]string, 137),
		Modify: []string{".config/opencode/opencode.json"},
		Summary: runtime.InstallSummary{
			Agents:       agents,
			Skills:       27,
			MCPServers:   []string{"codegraph", "context7", "engram"},
			Integrations: []string{"DAG panel", "memory", "VFS", "sandbox"},
			Configs:      []runtime.ConfigChange{{Path: ".config/opencode/opencode.json", Merged: true}},
		},
	}
	m.step, m.cursor = step, m.cursorFor(step)
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return next.(Model)
}

func TestFixtureReview(t *testing.T) {
	testutil.RequireFixtures(t, func(width, height int) string {
		return fixtureModel(width, height, StepReview).View().Content
	})
}

func TestFixtureResult(t *testing.T) {
	testutil.RequireFixtures(t, func(width, height int) string {
		m := fixtureModel(width, height, StepResult)
		m.outcome = runtime.ActionResultMsg{Result: runtime.ActionResult{
			Changed: []string{".config/opencode/opencode.json"},
			Verify: &verify.Report{Checks: []verify.CheckResult{
				{ID: "mcp:opencode:engram", State: verify.Verified, Explanation: "OpenCode API reports the MCP server connected."},
				{ID: "skills:opencode:takt", State: verify.NotVerifiable, Explanation: "Cannot query native OpenCode skills."},
			}},
		}}
		return m.View().Content
	})
}
