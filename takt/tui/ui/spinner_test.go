package ui_test

import (
	"os"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func TestSpinnerDisabledAnimation(t *testing.T) {
	t.Setenv("TAKT_NO_ANIMATION", "1")
	defer os.Unsetenv("TAKT_NO_ANIMATION")

	s := ui.NewSpinner()
	if cmd := s.Tick(); cmd != nil {
		t.Errorf("Tick() with animation disabled = %v, want nil", cmd)
	}
	if view := s.View(); view != "*" {
		t.Errorf("View() with animation disabled = %q, want %q", view, "*")
	}
	updated, cmd := s.Update(nil)
	if cmd != nil {
		t.Errorf("Update() with animation disabled returned a non-nil cmd: %v", cmd)
	}
	if updated.View() != "*" {
		t.Errorf("Update() with animation disabled changed the view to %q", updated.View())
	}
}

func TestSpinnerEnabledAnimation(t *testing.T) {
	t.Setenv("TAKT_NO_ANIMATION", "")

	s := ui.NewSpinner()
	if view := s.View(); view == "" {
		t.Error("View() with animation enabled should not be empty")
	}
	if cmd := s.Tick(); cmd == nil {
		t.Error("Tick() with animation enabled should return a non-nil cmd")
	}
	// An unrelated message type should be ignored without side effects.
	if _, cmd := s.Update(struct{}{}); cmd != nil {
		t.Errorf("Update() with an unrelated message returned a non-nil cmd: %v", cmd)
	}
}
