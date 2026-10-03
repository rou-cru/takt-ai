package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

func TestFixtureHome(t *testing.T) {
	testutil.RequireFixtures(t, func(width, height int) string {
		next, _ := New("/takt-fixture-home").Update(tea.WindowSizeMsg{Width: width, Height: height})
		return next.(Model).View().Content
	})
}
