package ui_test

import (
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

func TestBusyShowsCancelHintByDefault(t *testing.T) {
	got := ui.Busy("Installing", false, "1/3")
	if !strings.Contains(got, ui.TextCancelHint) {
		t.Errorf("Busy() without cancel requested = %q, want it to contain the cancel hint", got)
	}
	if strings.Contains(got, ui.TextCancelRequested) {
		t.Errorf("Busy() without cancel requested = %q, should not mention cancellation already requested", got)
	}
	if !strings.Contains(got, "1/3") {
		t.Errorf("Busy() = %q, want it to contain the marker", got)
	}
	if !strings.Contains(got, "Installing"+ui.TextInProgressSuffix) {
		t.Errorf("Busy() = %q, want it to contain the operation and in-progress suffix", got)
	}
}

func TestBusyShowsCancelRequested(t *testing.T) {
	got := ui.Busy("Installing", true, "1/3")
	if !strings.Contains(got, ui.TextCancelRequested) {
		t.Errorf("Busy() with cancel requested = %q, want it to contain the cancel-requested notice", got)
	}
}
