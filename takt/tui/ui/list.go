package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// Item is one selectable row.
type Item struct {
	Label   string
	Checked bool
}

// Options renders a list of labels with a focused row.
func Options(options []string, cursor, width int) string {
	return optionList(options, cursor, width, theme.Label)
}

func optionList(options []string, cursor, width int, base lipgloss.Style) string {
	var out strings.Builder
	for index, option := range options {
		out.WriteString(markedRow("", base, option, index == cursor, width, base))
	}
	return out.String()
}

// CheckList renders independently checked rows.
func CheckList(items []Item, cursor, width int) string {
	return markedList(items, cursor, width, theme.Icon.CheckboxOn, theme.Icon.CheckboxOff, theme.Success)
}

func markedList(items []Item, cursor, width int, on, off string, checkedColor color.Color) string {
	var out strings.Builder
	for index, item := range items {
		marker, markerStyle := off, theme.Label
		if item.Checked {
			marker, markerStyle = on, lipgloss.NewStyle().Foreground(checkedColor)
		}
		out.WriteString(markedRow(marker+" ", markerStyle, item.Label, index == cursor, width, theme.Label))
	}
	return out.String()
}

// WindowRange returns the [lo, hi) rows that fit and keep the cursor visible.
func WindowRange(count, cursor, height int) (lo, hi int) {
	if height <= 0 || count <= height {
		return 0, count
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= count {
		cursor = count - 1
	}
	lo = cursor - height/2
	if lo < 0 {
		lo = 0
	}
	if max := count - height; lo > max {
		lo = max
	}
	return lo, lo + height
}

func markedRow(marker string, markerStyle lipgloss.Style, label string, focused bool, width int, base lipgloss.Style) string {
	prefix := strings.Repeat(" ", lipgloss.Width(theme.Icon.Cursor))
	labelStyle := base
	if focused {
		prefix, labelStyle = theme.Icon.Cursor, theme.Display
	}
	line := theme.Caption.Render(prefix) + markerStyle.Render(marker) + labelStyle.Render(label)
	if width > 0 && lipgloss.Width(line) > width {
		line = ansi.Truncate(line, width, "…")
	}
	return line + "\n"
}
