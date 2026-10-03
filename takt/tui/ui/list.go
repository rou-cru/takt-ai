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
	// Description is an optional muted line under the label (PR-UX-16).
	Description string
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

// Choices renders a single selection whose options may carry a muted
// description on the line below (PR-UX-16); the value is the option under >.
func Choices(items []Item, cursor int, focused bool) string {
	if !focused {
		cursor = -1
	}
	indent := strings.Repeat(" ", lipgloss.Width(theme.Icon.Cursor))
	var out strings.Builder
	for index, item := range items {
		out.WriteString(markedRow("", item.Label, index == cursor, theme.Label))
		if item.Description != "" {
			out.WriteString(indent + theme.Caption.Render(item.Description) + "\n")
		}
	}
	return out.String()
}

// Selector renders one single-choice field on a screen that holds several:
// each field's chosen value stays visible (selection pair and • marker) while
// the cursor > moves elsewhere, so no decision is hidden by focus.
func Selector(options []string, cursor, chosen int, focused bool) string {
	if !focused {
		cursor = -1
	}
	var out strings.Builder
	for index, option := range options {
		marker, style := theme.Icon.Unchosen, theme.Label
		if index == chosen {
			marker, style = theme.Icon.Chosen, theme.Selected
		}
		out.WriteString(markedRow(theme.CheckMarker.Render(marker), option, index == cursor, style))
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

// markedList shows each row's [x]/[ ] in text.muted and a checked label in
// success.fg (PR-UX-16); focus stays in the gutter, never on the marker.
func markedList(items []Item, cursor int, on, off string) string {
	var out strings.Builder
	for index, item := range items {
		marker, label := off, theme.Label
		if item.Checked {
			marker, label = on, theme.Checked
		}
		markerCell := theme.CheckMarker.Render(marker) + " "
		out.WriteString(markedRow(markerCell, item.Label, index == cursor, label))
		if item.Description != "" {
			indent := strings.Repeat(" ", lipgloss.Width(theme.Icon.Cursor)+lipgloss.Width(markerCell))
			out.WriteString(indent + theme.Caption.Render(item.Description) + "\n")
		}
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
