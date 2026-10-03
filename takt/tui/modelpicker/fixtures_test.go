package modelpicker

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func TestFixtureAssignments(t *testing.T) {
	testutil.RequireFixtures(t, func(width, height int) string {
		p := New()
		p.Height = height
		p.Preload(map[string]model.ModelAssignment{"takt": {Model: "openai/gpt-6-luna"}, "analyst": {Model: "openai/gpt-6-luna"}})
		p.overrides["dev"] = model.ModelAssignment{Model: "opencode-go/hy4-preview"}
		p.cursor = 2
		frame := p.Frame()
		frame.Header, frame.Width, frame.Height = "Assign models · Agents", width, height
		return ui.Shell(frame)
	})
}
