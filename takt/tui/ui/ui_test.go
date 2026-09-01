package ui_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func TestWrapPreservesLinesAndDisplayWidth(t *testing.T) {
	if got, want := ui.Wrap("alpha beta\ngamma", 5), "alpha\nbeta\ngamma"; got != want {
		t.Fatalf("Wrap() = %q, want %q", got, want)
	}
	for _, line := range ui.WrapLine("日本語のテキスト", 6) {
		if lipgloss.Width(line) > 6 {
			t.Fatalf("line %q exceeds display width", line)
		}
	}
}

func TestListNavigationAndRendering(t *testing.T) {
	if got := ui.MoveCursor(0, 3, -1); got != 2 {
		t.Fatalf("MoveCursor() = %d, want 2", got)
	}
	out := ui.Options([]string{"first", "second"}, 1, 0)
	if strings.Count(out, theme.Icon.Cursor) != 1 || !strings.Contains(out, "second") {
		t.Fatalf("focused list output = %q", out)
	}
	if lo, hi := ui.WindowRange(20, 19, 7); lo != 13 || hi != 20 {
		t.Fatalf("WindowRange() = [%d, %d), want [13, 20)", lo, hi)
	}
}

func TestKeyBarAndShellRenderWithinWidth(t *testing.T) {
	keybar := ui.KeyBar(keys.Default(), nil, ui.KeyBarContext{}, 40)
	if !strings.Contains(keybar, "enter") || lipgloss.Width(keybar) > 40 {
		t.Fatalf("keybar = %q", keybar)
	}
	frame := ui.Frame{Body: "Select AI Agents", Keys: keys.Default(), Width: 80, Height: 16}
	out := ui.Shell(frame)
	if !strings.Contains(out, "Select AI Agents") || !strings.Contains(out, "╔") {
		t.Fatalf("shell output = %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > frame.Width {
			t.Fatalf("shell line exceeds frame width: %q", line)
		}
	}
}

func TestSpinnerCycles(t *testing.T) {
	if ui.Spinner(0) == "" || ui.Spinner(0) != ui.Spinner(ui.SpinnerFrameCount) {
		t.Fatal("spinner does not cycle")
	}
}
