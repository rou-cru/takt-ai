// Package keys defines the global keyboard grammar for TUI flows.
package keys

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// Binding is an action and its equivalent key names.
type Binding struct {
	Keys []string
}

// Matches reports whether msg activates this binding.
func (b Binding) Matches(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false
	}
	return slices.Contains(b.Keys, key.String())
}

// KeyMap is the full interaction grammar.
type KeyMap struct {
	Up, Down, Left, Right, Confirm, Toggle, NextSection, PrevSection, Back Binding
}

// Default returns the shared navigation bindings.
func Default() KeyMap {
	return KeyMap{
		Up:          Binding{Keys: []string{"up", "k"}},
		Down:        Binding{Keys: []string{"down", "j"}},
		Left:        Binding{Keys: []string{"left", "h"}},
		Right:       Binding{Keys: []string{"right", "l"}},
		Confirm:     Binding{Keys: []string{"enter"}},
		Toggle:      Binding{Keys: []string{"space"}},
		NextSection: Binding{Keys: []string{"tab"}},
		PrevSection: Binding{Keys: []string{"shift+tab"}},
		Back:        Binding{Keys: []string{"esc"}},
	}
}
