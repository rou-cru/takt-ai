package ui_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func TestSelectorDescribedShowsOnlyTheFocusedDescriptionAndKeepsItsSize(t *testing.T) {
	items := []ui.Item{
		{Label: "One", Description: "first"},
		{Label: "Two", Description: "second, and clearly the longest"},
		{Label: "Three", Description: "third"},
	}
	width, height := -1, -1
	for cursor := range items {
		got := ui.SelectorDescribed(items, cursor, 0)
		plain := ansi.Strip(got)
		for index, item := range items {
			if shown := strings.Contains(plain, item.Description); shown != (index == cursor) {
				t.Errorf("cursor %d: description %q shown = %v, want %v:\n%s", cursor, item.Description, shown, index == cursor, plain)
			}
			if !strings.Contains(plain, item.Label) {
				t.Errorf("cursor %d: label %q missing:\n%s", cursor, item.Label, plain)
			}
		}
		if !strings.Contains(plain, theme.Icon.Chosen+"One") {
			t.Errorf("cursor %d: chosen marker is not on the chosen item:\n%s", cursor, plain)
		}
		if width >= 0 && (lipgloss.Width(got) != width || lipgloss.Height(got) != height) {
			t.Errorf("cursor %d: size = %dx%d, want %dx%d so the block never shifts", cursor, lipgloss.Width(got), lipgloss.Height(got), width, height)
		}
		width, height = lipgloss.Width(got), lipgloss.Height(got)
	}
}
