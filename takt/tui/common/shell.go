package common

import (
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// Shell renders the standard TUI frame. Callers pass the full extra bindings
// they want shown, in the order they want them shown.
func Shell(keymap keys.KeyMap, enterLabel, body string, extra ...keys.Binding) string {
	return ui.Shell(ui.Frame{Body: body, Keys: keymap, Extra: extra, EnterLabel: enterLabel})
}
