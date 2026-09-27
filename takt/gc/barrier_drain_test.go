package gc_test

import (
	"github.com/rou-cru/takt-ai/takt/gc"
	"testing"
)

func TestBarrierRequiresOrdinaryDrain(t *testing.T) {
	for _, tc := range []struct {
		name            string
		active, pending int
		reason          string
	}{
		{"unfinished dispatch outside scope", 1, 0, gc.ReasonActiveUnits},
		{"idle session with pending delta", 0, 1, gc.ReasonPendingDeltas},
		{"invalid active count fails closed", -1, 0, gc.ReasonActiveUnits},
		{"invalid pending count fails closed", 0, -1, gc.ReasonPendingDeltas},
		{"drained", 0, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := gc.Barrier(gc.BarrierInput{Decision: gc.Decision{Outcome: gc.OutcomeRun}, ActiveUnits: tc.active, PendingOrdinaryDeltas: tc.pending})
			if got.Reason != tc.reason || got.Proceed != (tc.reason == "") {
				t.Fatalf("verdict = %+v", got)
			}
			if tc.reason != "" && got.Deferrals != 1 {
				t.Fatalf("missing deferral: %+v", got)
			}
		})
	}
}
