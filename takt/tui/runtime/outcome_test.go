package runtime_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

func TestActionResultCancelled(t *testing.T) {
	tests := []struct {
		name    string
		outcome lifecycle.Outcome
		want    bool
	}{
		{"cancelled partial is cancelled", lifecycle.OutcomeCancelledPartial, true},
		{"cancelled nothing applied is cancelled", lifecycle.OutcomeCancelledNothingApplied, true},
		{"empty outcome is not cancelled", lifecycle.Outcome(""), false},
		{"other outcome is not cancelled", lifecycle.Outcome("something-else"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := runtime.ActionResult{Outcome: tc.outcome}
			if got := result.Cancelled(); got != tc.want {
				t.Errorf("Cancelled() with outcome %q = %v, want %v", tc.outcome, got, tc.want)
			}
		})
	}
}

func TestActionResultPartial(t *testing.T) {
	tests := []struct {
		name    string
		outcome lifecycle.Outcome
		want    bool
	}{
		{"cancelled partial is partial", lifecycle.OutcomeCancelledPartial, true},
		{"cancelled nothing applied is not partial", lifecycle.OutcomeCancelledNothingApplied, false},
		{"empty outcome is not partial", lifecycle.Outcome(""), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := runtime.ActionResult{Outcome: tc.outcome}
			if got := result.Partial(); got != tc.want {
				t.Errorf("Partial() with outcome %q = %v, want %v", tc.outcome, got, tc.want)
			}
		})
	}
}
