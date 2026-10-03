package ui_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// Every shared primitive in one paneled screen: states, checklist with a
// description, single selection, progress, and the five button states.
func TestFixtureShellPrimitives(t *testing.T) {
	testutil.RequireFixtures(t, func(width, height int) string {
		body := strings.Join([]string{
			ui.Status(ui.StateSuccess, "Configuration applied."),
			ui.Status(ui.StateWarning, "This file has external changes."),
			ui.Status(ui.StateFailed, "The change could not be applied."),
			"",
			ui.CheckList([]ui.Item{
				{Label: "Context7", Checked: true, Description: "Up-to-date library docs for agents"},
				{Label: "Optional tool"},
			}, 1, true),
			ui.Options([]string{"Keep my version", "Restore Takt version"}, 0, false),
			ui.Busy("Installation", "*", ui.Progress{Done: []string{"Checking OpenCode connection"}, Message: "Applying installation files", Current: ".config/opencode/opencode.json", Completed: 2, Total: 3}),
		}, "\n")
		footer := ui.FooterActions([]ui.FooterAction{
			{Label: "Back"},
			{Label: "Install"},
			{Label: "Remove", Danger: true},
			{Label: "Apply changes", Unavailable: "no changes to apply"},
		}, 1, true)
		return ui.Shell(ui.Frame{Header: "Install · Review", Body: body, Footer: footer, Width: width, Height: height})
	})
}

// A focused destructive action keeps the danger pair.
func TestFixtureFocusedDangerButton(t *testing.T) {
	testutil.RequireFixtures(t, func(width, height int) string {
		footer := ui.FooterActions([]ui.FooterAction{{Label: "Uninstall", Danger: true}, {Label: "Back"}}, 0, true)
		return ui.Shell(ui.Frame{Header: "Uninstall · Review", Body: "Remove: 139 files installed by Takt", Footer: footer, Width: width, Height: height})
	})
}

// An overflowing panel keeps its width and shows the visible range.
func TestFixtureShellOverflow(t *testing.T) {
	testutil.RequireFixtures(t, func(width, height int) string {
		var rows []string
		for index := range 60 {
			rows = append(rows, ".config/opencode/takt/skills/takt-memory-architect/references/adr-"+strings.Repeat("x", index%9)+".md")
		}
		footer := ui.FooterActions(ui.Actions("Back to menu"), 0, true)
		return ui.Shell(ui.Frame{Header: "Uninstall · Review", Body: strings.Join(rows, "\n"), Footer: footer, Width: width, Height: height, Scroll: 10})
	})
}

func TestFixtureShellTooSmall(t *testing.T) {
	theme.SetMode(theme.ModeColor)
	golden.RequireEqual(t, ui.Shell(ui.Frame{Header: "Install", Body: "Body", Width: 50, Height: 16}))
}
