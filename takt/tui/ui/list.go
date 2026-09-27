package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// Item is one selectable row so checked state travels with its label.
type Item struct {
	Label   string
	Checked bool
}

// Options renders labels with a focus marker so the cursor survives focus changes.
func Options(options []string, cursor int, focused bool) string {
	if !focused {
		cursor = -1
	}
	return optionList(options, cursor, theme.Label)
}

// optionList renders plain labels through one focus path so lists and footers agree.
func optionList(options []string, cursor int, base lipgloss.Style) string {
	var out strings.Builder
	for index, option := range options {
		out.WriteString(markedRow("", option, index == cursor, base))
	}
	return out.String()
}

// CheckList renders independently checked rows so multi-select stays visible.
func CheckList(items []Item, cursor int, focused bool) string {
	if !focused {
		cursor = -1
	}
	return markedList(items, cursor, theme.Icon.CheckboxOn, theme.Icon.CheckboxOff)
}

// markedList marks checked rows as selected, never as success, so state stays unambiguous.
func markedList(items []Item, cursor int, on, off string) string {
	var out strings.Builder
	for index, item := range items {
		marker, markerStyle := off, theme.Label
		if item.Checked {
			marker, markerStyle = on, theme.Selected
		}
		out.WriteString(markedRow(markerStyle.Render(marker)+" ", item.Label, index == cursor, theme.Label))
	}
	return out.String()
}

// markedRow keeps focus in the gutter so labels keep their own meaning.
func markedRow(marker, label string, focused bool, base lipgloss.Style) string {
	prefix := strings.Repeat(" ", lipgloss.Width(theme.Icon.Cursor))
	if focused {
		prefix = theme.Focus.Render(theme.Icon.Cursor)
	}
	return prefix + marker + base.Render(label) + "\n"
}
