// Package keys defines the global keyboard grammar for TUI flows.
package keys

import tea "github.com/charmbracelet/bubbletea"

// Binding is a discoverable action and its equivalent key names.
type Binding struct {
	Keys        []string
	Key         string
	Description string
}

// Matches reports whether msg activates this binding.
func (b Binding) Matches(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	for _, candidate := range b.Keys {
		if key.String() == candidate {
			return true
		}
	}
	return false
}

// KeyMap is the full interaction grammar.
type KeyMap struct {
	Up, Down, Confirm, NextSection, Back, Help Binding
}

// Default returns the shared navigation bindings.
func Default() KeyMap {
	return KeyMap{
		Up:          Binding{Keys: []string{"up", "k"}, Key: "↑/k", Description: "up"},
		Down:        Binding{Keys: []string{"down", "j"}, Key: "↓/j", Description: "down"},
		Confirm:     Binding{Keys: []string{"enter"}, Key: "enter", Description: "confirm"},
		NextSection: Binding{Keys: []string{"tab"}, Key: "tab", Description: "next section"},
		Back:        Binding{Keys: []string{"esc"}, Key: "esc", Description: "back"},
		Help:        Binding{Keys: []string{"?"}, Key: "?", Description: "help"},
	}
}
