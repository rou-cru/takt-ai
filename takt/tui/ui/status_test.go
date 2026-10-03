package ui_test

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func TestStatusContainsStateAndMessage(t *testing.T) {
	tests := []struct {
		name  string
		state ui.State
	}{
		{"success", ui.StateSuccess},
		{"partial", ui.StatePartial},
		{"failed", ui.StateFailed},
		{"warning", ui.StateWarning},
		{"pending", ui.StatePending},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ui.Status(tc.state, "did a thing")
			if !strings.Contains(got, string(tc.state)) {
				t.Errorf("Status(%q, ...) = %q, want it to contain the state name", tc.state, got)
			}
			if !strings.Contains(got, "did a thing") {
				t.Errorf("Status(%q, ...) = %q, want it to contain the message", tc.state, got)
			}
		})
	}
}

func TestBusyListsPhasesAndTheCurrentOne(t *testing.T) {
	got := ansi.Strip(ui.Busy("Installation", "*", ui.Progress{Done: []string{"Checking OpenCode connection"}, Message: "Applying installation files", Current: "a.md", Completed: 1, Total: 2}))
	for _, want := range []string{theme.Icon.Done + " Checking OpenCode connection", "* Applying installation files", "50%", "a.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("Busy() = %q, want it to contain %q", got, want)
		}
	}
	if first := ansi.Strip(ui.Busy("Installation", "*", ui.Progress{})); !strings.Contains(first, "* Installation") {
		t.Errorf("Busy() before any phase = %q, want the operation as the current phase", first)
	}
}

func TestBusyFooterOffersCancelUntilRequested(t *testing.T) {
	if got := ansi.Strip(ui.BusyFooter(false)); !strings.Contains(got, ui.TextActionCancel) || strings.Contains(got, ui.TextUnavailableIntro) {
		t.Errorf("BusyFooter(false) = %q, want an available Cancel", got)
	}
	if got := ansi.Strip(ui.BusyFooter(true)); !strings.Contains(got, ui.TextUnavailableIntro+ui.TextCancelRequested) {
		t.Errorf("BusyFooter(true) = %q, want Cancel unavailable while the phase finishes", got)
	}
}
