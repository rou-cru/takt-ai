package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// KeyBarContext carries the focused action and optional footer reachability.
type KeyBarContext struct {
	EnterLabel string
	ShowTab    bool
}

// KeyBar renders discoverable bindings using the shared keymap.
func KeyBar(keymap keys.KeyMap, extra []keys.Binding, context KeyBarContext, width int) string {
	enter := keymap.Confirm
	if context.EnterLabel == "" {
		enter.Description = "select"
	} else {
		enter.Description = context.EnterLabel
	}
	bindings := []keys.Binding{keymap.Up, keymap.Down, enter}
	if context.ShowTab {
		bindings = append(bindings, keymap.NextSection)
	}
	bindings = append(bindings, keymap.Help)
	bindings = append(bindings, extra...)

	parts := make([]string, len(bindings))
	for index, binding := range bindings {
		parts[index] = theme.Label.Render(binding.Key) + " " + theme.Caption.Render(binding.Description)
	}
	bar := strings.Join(parts, "  ")
	if width > 0 {
		return ansi.Truncate(bar, width, "…")
	}
	return bar
}
