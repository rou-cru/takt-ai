package ui_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func TestMoveCursor(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		count  int
		delta  int
		want   int
	}{
		{"zero count stays put", 0, 0, 1, 0},
		{"moves forward within range", 1, 5, 1, 2},
		{"moves backward within range", 1, 5, -1, 0},
		{"wraps past end forward", 4, 5, 1, 0},
		{"wraps past start backward", 0, 5, -1, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ui.MoveCursor(tc.cursor, tc.count, tc.delta); got != tc.want {
				t.Errorf("MoveCursor(%d, %d, %d) = %d, want %d", tc.cursor, tc.count, tc.delta, got, tc.want)
			}
		})
	}
}

func TestNudge(t *testing.T) {
	km := keys.Default()

	cursor := 1
	if !ui.Nudge(&cursor, 5, km, tea.KeyPressMsg{Code: 'j', Text: "j"}) {
		t.Fatal("Nudge should match the Down binding")
	}
	if cursor != 2 {
		t.Errorf("cursor after down = %d, want 2", cursor)
	}

	cursor = 1
	if !ui.Nudge(&cursor, 5, km, tea.KeyPressMsg{Code: 'k', Text: "k"}) {
		t.Fatal("Nudge should match the Up binding")
	}
	if cursor != 0 {
		t.Errorf("cursor after up = %d, want 0", cursor)
	}

	cursor = 1
	if ui.Nudge(&cursor, 5, km, tea.KeyPressMsg{Code: 'x', Text: "x"}) {
		t.Error("Nudge should not match an unbound key")
	}
	if cursor != 1 {
		t.Errorf("cursor should stay unchanged on no match, got %d", cursor)
	}
}

func TestNudgeHorizontal(t *testing.T) {
	km := keys.Default()

	cursor := 1
	if !ui.NudgeHorizontal(&cursor, 5, km, tea.KeyPressMsg{Code: 'l', Text: "l"}) {
		t.Fatal("NudgeHorizontal should match the Right binding")
	}
	if cursor != 2 {
		t.Errorf("cursor after right = %d, want 2", cursor)
	}

	cursor = 1
	if !ui.NudgeHorizontal(&cursor, 5, km, tea.KeyPressMsg{Code: 'h', Text: "h"}) {
		t.Fatal("NudgeHorizontal should match the Left binding")
	}
	if cursor != 0 {
		t.Errorf("cursor after left = %d, want 0", cursor)
	}

	cursor = 1
	if ui.NudgeHorizontal(&cursor, 5, km, tea.KeyPressMsg{Code: 'j', Text: "j"}) {
		t.Error("NudgeHorizontal should not match Down")
	}
}

func TestScroll(t *testing.T) {
	got, ok := ui.Scroll(0, 30, "pgdown")
	if !ok {
		t.Fatal("Scroll(pgdown) should report ok")
	}
	if got <= 0 {
		t.Errorf("Scroll(pgdown) = %d, want > 0", got)
	}

	up, ok := ui.Scroll(got, 30, "pgup")
	if !ok {
		t.Fatal("Scroll(pgup) should report ok")
	}
	if up != 0 {
		t.Errorf("Scroll(pgup) back to start = %d, want 0", up)
	}

	floor, ok := ui.Scroll(0, 30, "pgup")
	if !ok {
		t.Fatal("Scroll(pgup) at zero should still report ok")
	}
	if floor != 0 {
		t.Errorf("Scroll(pgup) at zero = %d, want floored at 0", floor)
	}

	unchanged, ok := ui.Scroll(5, 30, "unknown")
	if ok {
		t.Error("Scroll with an unknown key should not report ok")
	}
	if unchanged != 5 {
		t.Errorf("Scroll with unknown key should leave scroll unchanged, got %d", unchanged)
	}
}

func TestBack(t *testing.T) {
	msg := ui.Back()
	if _, ok := msg.(ui.BackMsg); !ok {
		t.Errorf("Back() = %T, want ui.BackMsg", msg)
	}
}
