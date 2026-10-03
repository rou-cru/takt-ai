package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// Button geometry: label padding on each side and the gap between buttons.
const (
	buttonPadding = 2
	buttonGap     = 2
)

// FooterActions renders the action row (VDS §3): only the focused button is
// filled; the others are text.secondary without fill, destructive ones keep
// danger.fg, and unavailable ones keep the disabled pair even when focused.
// Unavailable reasons go on their own line under the row, never inside it.
func FooterActions(actions []FooterAction, cursor int, focused bool) string {
	if !focused {
		cursor = -1
	}
	parts := make([]string, len(actions))
	var reasons []string
	for index, action := range actions {
		parts[index] = buttonStyle(action, index == cursor).Render(buttonLabel(action.Label, index == cursor))
		if action.Unavailable != "" {
			reasons = append(reasons, TextUnavailableIntro+action.Unavailable)
		}
	}
	row := strings.Join(parts, strings.Repeat(" ", buttonGap))
	if len(reasons) == 0 {
		return row
	}
	return row + "\n" + theme.Caption.Render(strings.Join(reasons, " · "))
}

func buttonStyle(action FooterAction, focused bool) lipgloss.Style {
	switch {
	case action.Unavailable != "":
		return theme.ButtonDisabled
	case action.Danger && focused:
		return theme.ButtonDangerFocus
	case action.Danger:
		return theme.ButtonDanger
	case focused:
		return theme.ButtonFocus
	}
	return theme.Button
}

// buttonLabel pads the label so a button keeps its width whether or not it is
// filled. Monochrome has no fill, so brackets mark the focused button.
func buttonLabel(label string, focused bool) string {
	pad := strings.Repeat(" ", buttonPadding)
	if focused && theme.Mono() {
		return "[" + pad[1:] + label + pad[1:] + "]"
	}
	return pad + label + pad
}
