package keys_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
)

func TestDefaultNavigationMatchesArrowsAndVim(t *testing.T) {
	km := keys.Default()
	for _, test := range []struct {
		name string
		msg  tea.KeyMsg
		key  keys.Binding
	}{
		{"up", tea.KeyMsg{Type: tea.KeyUp}, km.Up},
		{"vim up", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}, km.Up},
		{"down", tea.KeyMsg{Type: tea.KeyDown}, km.Down},
		{"vim down", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, km.Down},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !test.key.Matches(test.msg) {
				t.Fatal("binding did not match")
			}
		})
	}
}
