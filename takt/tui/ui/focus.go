package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// Section marks which screen region holds keyboard focus so lists and footers stay independent.
type Section int

// Focus sections are the closed set of screen regions.
const (
	// SectionBody focuses the list region so cursor keys stay inside the list.
	SectionBody Section = iota
	// SectionFooter focuses completion controls so Enter commits instead of moving.
	SectionFooter
)

// SwitchSection moves focus between regions.
func SwitchSection(current Section, keymap keys.KeyMap, message tea.Msg) (Section, bool) {
	if !keymap.NextSection.Matches(message) && !keymap.PrevSection.Matches(message) {
		return current, false
	}
	if current == SectionBody {
		return SectionFooter, true
	}
	return SectionBody, true
}

// FooterAction is one labeled completion control.
type FooterAction struct {
	Label       string
	Unavailable string
	Danger      bool
}

// Actions builds available actions from labels so callers skip the struct boilerplate.
func Actions(labels ...string) []FooterAction {
	actions := make([]FooterAction, len(labels))
	for index, label := range labels {
		actions[index] = FooterAction{Label: label}
	}
	return actions
}

// FooterActions renders the row of filled action buttons. The focused button
// is filled with focus.ring; unavailable and destructive choices keep their verb and reason.
func FooterActions(actions []FooterAction, cursor int, focused bool) string {
	if !focused {
		cursor = -1
	}
	parts := make([]string, len(actions))
	for index, action := range actions {
		label, style := action.Label, theme.Button
		if index == cursor {
			style = theme.ButtonFocus
		} else {
			switch {
			case action.Unavailable != "":
				style = theme.ButtonDisabled
			case action.Danger:
				style = theme.ButtonDanger
			}
		}
		if action.Unavailable != "" {
			label += TextUnavailableIntro + action.Unavailable
		}
		parts[index] = style.Render("  " + label + "  ")
	}
	return strings.Join(parts, "  ")
}
