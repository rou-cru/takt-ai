package install_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/tui/install"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// fixtureRoot is never created: fixtures must not depend on the machine.
const fixtureRoot = "/takt-fixture-home"

func sized(m install.Model, width, height int) install.Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return next.(install.Model)
}

func TestFixtureReview(t *testing.T) {
	withoutOpenCode(t)
	testutil.RequireFixtures(t, func(width, height int) string {
		return sized(install.New(fixtureRoot), width, height).View().Content
	})
}

func TestFixtureResult(t *testing.T) {
	testutil.RequireFixtures(t, func(width, height int) string {
		next, cmd := install.New(fixtureRoot).Update(key("enter"))
		m := next.(install.Model)
		next, _ = m.Update(runtime.ActionResultMsg{Request: testutil.ActionRequest(t, cmd), Result: runtime.ActionResult{
			Changed: []string{".config/opencode/opencode.json"},
			Verify: &verify.Report{Checks: []verify.CheckResult{
				{ID: "mcp:opencode:takt_memory", State: verify.Verified, Explanation: "OpenCode API reports the MCP server connected."},
				{ID: "skills:opencode:takt", State: verify.NotVerifiable, Explanation: "Cannot query native OpenCode skills."},
			}},
		}})
		return sized(next.(install.Model), width, height).View().Content
	})
}
