package gc

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
)

func runDecision() Decision {
	return Decision{Outcome: OutcomeRun, Trigger: TriggerCadence, Units: 8, Mutations: 3, PolicyRef: TriggerPolicyRef}
}

// TestBarrier covers the barrier as a black box: holds, proceed, deferral bookkeeping.
func TestBarrier(t *testing.T) {
	cases := []struct {
		name          string
		in            BarrierInput
		wantProceed   bool
		wantReason    string
		wantDeferrals int
	}{
		{"active unit holds", BarrierInput{Decision: runDecision(), Deferrals: 1, ActiveUnits: 1}, false, ReasonActiveUnits, 2},
		{"pending delta holds", BarrierInput{Decision: runDecision(), PendingOrdinaryDeltas: 1}, false, ReasonPendingDeltas, 1},
		{"drained proceeds and resets deferrals", BarrierInput{Decision: runDecision(), Deferrals: 2}, true, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Barrier(c.in)
			if v.Proceed != c.wantProceed || v.Reason != c.wantReason || v.Deferrals != c.wantDeferrals {
				t.Fatalf("Barrier(%+v)=%+v", c.in, v)
			}
			rec, ok := v.ControlRecord("harness")
			if ok != !c.wantProceed {
				t.Fatalf("recorded=%v, want %v", ok, !c.wantProceed)
			}
			if ok {
				if err := rec.Validate(); err != nil {
					t.Fatal(err)
				}
				if rec.ActionClass != obs.ActionEscalate || rec.PolicyRef != BarrierPolicyRef || !strings.Contains(rec.TriggeringCondition, c.wantReason) {
					t.Fatalf("bad record: %+v", rec)
				}
			}
		})
	}
}

// TestBarrierPassesThroughNonRun proves the barrier never alters or records idle, skip and abort decisions.
func TestBarrierPassesThroughNonRun(t *testing.T) {
	for _, o := range []Outcome{OutcomeIdle, OutcomeSkip, OutcomeAbort} {
		d := runDecision()
		d.Outcome = o
		in := BarrierInput{Decision: d, Deferrals: 2, ActiveUnits: 1}
		v := Barrier(in)
		if v.Decision != d || v.Proceed || v.Held() || v.Deferrals != 2 {
			t.Fatalf("%s not passed through: %+v", o, v)
		}
		if _, ok := v.ControlRecord("harness"); ok {
			t.Fatalf("%s must not add a barrier record", o)
		}
	}
}

// TestBarrierStarvationReachesAbort proves the returned Deferrals feeds A9 so repeated holds end in an abort.
func TestBarrierStarvationReachesAbort(t *testing.T) {
	p := testPolicy(t)
	in := TriggerInput{Mutations: p.Cadence.MutationsPerCycle}
	for i := range p.Cadence.MaxDeferrals {
		v := Barrier(BarrierInput{Decision: Decide(in, p), Deferrals: in.Deferrals, ActiveUnits: 1})
		if !v.Held() {
			t.Fatalf("round %d: want hold, got %+v", i, v)
		}
		in.Deferrals = v.Deferrals
	}
	if d := Decide(in, p); d.Outcome != OutcomeAbort || d.Reason != ReasonDeferralLimit {
		t.Fatalf("want abort after %d holds, got %+v", p.Cadence.MaxDeferrals, d)
	}
}
