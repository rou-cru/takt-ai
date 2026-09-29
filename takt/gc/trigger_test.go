package gc

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
)

func testPolicy(t *testing.T) TriggerPolicy {
	t.Helper()
	p, err := LoadTriggerPolicy()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestDecide covers cadence, proportionality, skip, abort and the user path through counters + policy only.
func TestDecide(t *testing.T) {
	p := testPolicy(t)
	u, m := p.Cadence.UnitsPerCycle, p.Cadence.MutationsPerCycle
	cases := []struct {
		name       string
		in         TriggerInput
		wantOut    Outcome
		wantTrig   TriggerKind
		wantReason string
	}{
		{"below both shares", TriggerInput{UnitsDispatched: 1, Mutations: 1}, OutcomeIdle, TriggerCadence, ""},
		{"units alone fill budget", TriggerInput{UnitsDispatched: u, Mutations: 1}, OutcomeRun, TriggerCadence, ""},
		{"mutations alone fill budget", TriggerInput{Mutations: m}, OutcomeRun, TriggerCadence, ""},
		{"half of each is proportional", TriggerInput{UnitsDispatched: u / 2, Mutations: m / 2}, OutcomeRun, TriggerCadence, ""},
		{"just under half of each stays idle", TriggerInput{UnitsDispatched: u/2 - 1, Mutations: m/2 - 1}, OutcomeIdle, TriggerCadence, ""},
		{"due but cycle in flight", TriggerInput{Mutations: m, CycleInFlight: true}, OutcomeSkip, TriggerCadence, ReasonInFlight},
		{"due by units but no mutations", TriggerInput{UnitsDispatched: u}, OutcomeSkip, TriggerCadence, ReasonNoDelta},
		{"deferral limit aborts", TriggerInput{Mutations: m, Deferrals: p.Cadence.MaxDeferrals}, OutcomeAbort, TriggerCadence, ReasonDeferralLimit},
		{"user request runs below cadence", TriggerInput{Mutations: 1, UserRequested: true}, OutcomeRun, TriggerUser, ""},
		{"user request with cycle in flight skips", TriggerInput{Mutations: 1, UserRequested: true, CycleInFlight: true}, OutcomeSkip, TriggerUser, ReasonInFlight},
		{"user request with empty delta skips", TriggerInput{UserRequested: true}, OutcomeSkip, TriggerUser, ReasonNoDelta},
		{"user request past deferral limit aborts", TriggerInput{Mutations: 1, UserRequested: true, Deferrals: p.Cadence.MaxDeferrals}, OutcomeAbort, TriggerUser, ReasonDeferralLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Decide(c.in, p)
			if d.Outcome != c.wantOut || d.Trigger != c.wantTrig || d.Reason != c.wantReason {
				t.Fatalf("Decide(%+v)=%+v, want outcome=%s trigger=%s reason=%q", c.in, d, c.wantOut, c.wantTrig, c.wantReason)
			}
			if d.Units != c.in.UnitsDispatched || d.Mutations != c.in.Mutations || d.PolicyRef != TriggerPolicyRef {
				t.Fatalf("record fields not echoed: %+v", d)
			}
		})
	}
}

// TestDecideFollowsPolicy proves parameters come from policy data: doubling the budget doubles the pace needed.
func TestDecideFollowsPolicy(t *testing.T) {
	p := testPolicy(t)
	in := TriggerInput{UnitsDispatched: p.Cadence.UnitsPerCycle, Mutations: 1}
	if Decide(in, p).Outcome != OutcomeRun {
		t.Fatal("expected run at base policy")
	}
	p.Cadence.UnitsPerCycle *= 2
	if Decide(in, p).Outcome != OutcomeIdle {
		t.Fatal("expected idle once policy doubles the unit budget")
	}
}

// TestControlRecord verifies skipped and aborted cycles are recorded and idle ticks are not.
func TestControlRecord(t *testing.T) {
	p := testPolicy(t)
	m := p.Cadence.MutationsPerCycle
	if _, ok := Decide(TriggerInput{}, p).ControlRecord("harness"); ok {
		t.Fatal("idle must not be recorded")
	}
	var got []obs.ControlRecord
	for _, in := range []TriggerInput{
		{Mutations: m},                      // run
		{Mutations: m, CycleInFlight: true}, // skip
		{Mutations: m, Deferrals: 99},       // abort
	} {
		rec, ok := Decide(in, p).ControlRecord("harness")
		if !ok {
			t.Fatalf("decision for %+v not recorded", in)
		}
		got = append(got, rec)
	}
	if len(got) != 3 {
		t.Fatalf("recorded %d actions, want 3", len(got))
	}
	if !strings.Contains(got[1].TriggeringCondition, ReasonInFlight) || got[1].ActionClass != obs.ActionObserve {
		t.Fatalf("skip record wrong: %+v", got[1])
	}
	if !strings.Contains(got[2].TriggeringCondition, ReasonDeferralLimit) || got[2].ActionClass != obs.ActionEscalate {
		t.Fatalf("abort record wrong: %+v", got[2])
	}
}

// TestParseTriggerPolicy rejects broken policy data.
func TestParseTriggerPolicy(t *testing.T) {
	for name, y := range map[string]string{
		"bad version": "version: 2\ncadence: {units_per_cycle: 1, mutations_per_cycle: 1, max_deferrals: 1}",
		"zero param":  "version: 1\ncadence: {units_per_cycle: 0, mutations_per_cycle: 1, max_deferrals: 1}",
		"unknown key": "version: 1\ncadence: {units_per_cycle: 1, mutations_per_cycle: 1, max_deferrals: 1, error_rate: 1}",
	} {
		if _, err := parseTriggerPolicy([]byte(y)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
