package keys_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
)

func TestDefaultNavigationMatchesArrowsAndVim(t *testing.T) {
	km := keys.Default()
	for _, test := range []struct {
		name string
		msg  tea.KeyPressMsg
		key  keys.Binding
	}{
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, km.Up},
		{"vim up", tea.KeyPressMsg{Code: 'k', Text: "k"}, km.Up},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, km.Down},
		{"vim down", tea.KeyPressMsg{Code: 'j', Text: "j"}, km.Down},
		{"toggle", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, km.Toggle},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !test.key.Matches(test.msg) {
				t.Fatal("binding did not match")
			}
		})
	}
}
