package ui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// Spinner is one animated step marker owned by a flow's busy state.
type Spinner struct {
	inner spinner.Model
}

// NewSpinner returns a MiniDot spinner; inert when animation is disabled.
func NewSpinner() Spinner {
	return Spinner{inner: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
}

// Tick requests the next frame; nil when animation is disabled.
func (s Spinner) Tick() tea.Cmd {
	if !theme.Animation() {
		return nil
	}
	return s.inner.Tick
}

// Update advances the animation and chains the next frame.
func (s Spinner) Update(msg tea.Msg) (Spinner, tea.Cmd) {
	if !theme.Animation() {
		return s, nil
	}
	if _, ok := msg.(spinner.TickMsg); !ok {
		return s, nil
	}
	var cmd tea.Cmd
	s.inner, cmd = s.inner.Update(msg)
	return s, cmd
}

// View renders the current frame, or the ASCII baseline when disabled.
func (s Spinner) View() string {
	if !theme.Animation() {
		return "*"
	}
	return s.inner.View()
}
