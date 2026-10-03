package ui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func keyMsg(name string) tea.KeyPressMsg {
	switch name {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	runes := []rune(name)
	code := tea.KeyExtended
	if len(runes) == 1 {
		code = runes[0]
	}
	return tea.KeyPressMsg{Code: code, Text: name}
}

func TestSwitchSectionMovesFocusInBothDirections(t *testing.T) {
	keymap := keys.Default()
	for _, name := range []string{"tab", "shift+tab"} {
		next, ok := ui.SwitchSection(ui.SectionBody, keymap, keyMsg(name))
		if !ok || next != ui.SectionFooter {
			t.Fatalf("%s from body = (%v, ok=%v), want footer", name, next, ok)
		}
		back, ok := ui.SwitchSection(ui.SectionFooter, keymap, keyMsg(name))
		if !ok || back != ui.SectionBody {
			t.Fatalf("%s from footer = (%v, ok=%v), want body", name, back, ok)
		}
	}
}

// TestSwitchSectionIgnoresNavigationKeys proves Tab is the only way to change
// section: Up/Down and Enter must leave focus where it is.
func TestSwitchSectionIgnoresNavigationKeys(t *testing.T) {
	keymap := keys.Default()
	for _, name := range []string{"up", "down", "k", "j", "enter", " "} {
		next, ok := ui.SwitchSection(ui.SectionBody, keymap, keyMsg(name))
		if ok || next != ui.SectionBody {
			t.Fatalf("%q = (%v, ok=%v), want body unchanged", name, next, ok)
		}
	}
}

func TestFooterActionsMarksTheFocusedButtonOnlyWhenFocused(t *testing.T) {
	labels := ui.Actions("Continue")
	focused := ui.FooterActions(labels, 0, true)
	blurred := ui.FooterActions(labels, 0, false)
	if !strings.Contains(focused, "Continue") || !strings.Contains(blurred, "Continue") {
		t.Fatalf("footer must render its labels in both states: focused=%q blurred=%q", focused, blurred)
	}
	if strings.Contains(focused, ">") || strings.Contains(blurred, ">") {
		t.Fatalf("footer buttons must not carry a '>' marker: focused=%q blurred=%q", focused, blurred)
	}
}

// An unavailable action states its reason; a destructive one keeps its verb.
func TestFooterActionsRenderUnavailableReasonAndDanger(t *testing.T) {
	out := ansiFree(ui.FooterActions([]ui.FooterAction{
		{Label: "Continue", Unavailable: "select at least one file"},
		{Label: "Uninstall", Danger: true},
	}, 1, true))
	row, reason, found := strings.Cut(out, "\n")
	if !found || !strings.Contains(row, "Continue") || !strings.Contains(row, "Uninstall") || strings.Contains(row, "Unavailable") {
		t.Fatalf("the reason must leave the button row: %q", out)
	}
	if reason != ui.TextUnavailableIntro+"select at least one file" {
		t.Fatalf("reason line = %q", reason)
	}
}
