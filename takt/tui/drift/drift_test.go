package drift_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/drift"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// Known gap: this suite does not yet drive every drift screen (select,
// review, result, report) black-box, through its visible actions only. Add
// that coverage before relying on this file alone to catch drift-screen
// regressions.

// TestDriftShowsNotInstalledGuardOnFreshRoot verifies that on a root with
// nothing installed, drift says plainly that nothing is installed instead of
// scanning an empty manifest and reporting no drift.
func TestDriftShowsNotInstalledGuardOnFreshRoot(t *testing.T) {
	root := t.TempDir()
	model := drift.New(root)

	if !strings.Contains(render(model), setup.NotInstalledMessage) {
		t.Fatalf("View() = %q, want the not-installed guard message", render(model))
	}
	if model.Init() != nil {
		t.Fatal("Init() should not scan when nothing is installed")
	}

	_, backCmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if backCmd == nil {
		t.Fatal("Escape produced no command")
	}
	if _, ok := backCmd().(ui.BackMsg); !ok {
		t.Fatal("Escape did not request BackMsg")
	}
}

// render returns the view at a wide size without styling, so assertions
// match copy rather than wrapping or ANSI sequences.
func render(m drift.Model) string {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 400, Height: 60})
	return ansi.Strip(next.View().Content)
}
