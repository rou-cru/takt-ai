// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package gc_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/obs"
)

// newRateTestBus opens a workspace-local store and a bus attached to it, so
// gc.EvaluateMandateReversionRate has real persisted events to read back, the
// same store/bus wiring the rest of the session uses.
func newRateTestBus(t *testing.T) (*obs.Store, *obs.Bus, *obs.Clock) {
	t.Helper()
	store, err := obs.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clock := obs.NewClock()
	bus := obs.NewBus("session-rate-test", clock)
	bus.AttachStore(store)
	return store, bus, clock
}

// seedRegisteredWork appends n content-free tool-activity events, standing
// in for "units of registered work" (the PR-MNT-9/PR-MNT-11 denominator).
func seedRegisteredWork(t *testing.T, bus *obs.Bus, clock *obs.Clock, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		env := obs.NewEnvelope(clock, obs.PlaneHarness, "agent-a")
		env.EventClass = obs.EventToolActivity
		env.Correlations = obs.CorrelationIDs{SessionID: "session-rate-test", WorkUnitID: "wu-1"}
		if err := bus.Publish(env); err != nil {
			t.Fatal(err)
		}
	}
}

// seedReedits attributes n reversions to mandate, via the same helper N3
// wired for PR-MNT-31.
func seedReedits(t *testing.T, bus *obs.Bus, clock *obs.Clock, mandate gc.MandateClass, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := obs.PublishCycleReeditEvent(bus, clock, "agent-a", "cycle-1", string(mandate), "hash-1", "wu-1"); err != nil {
			t.Fatal(err)
		}
	}
}

func eligibleFixture(mandate gc.MandateClass) gc.Finding {
	return gc.Finding{ID: "f1", Mandate: mandate, Tool: "vet", ToolVersion: "1", Rule: "r1", Evidence: "e1"}
}

// confirmedFixture is the confirmed investigation that authorizes eligibleFixture.
var confirmedFixture = []gc.Investigation{{FindingID: "f1", Outcome: "confirmed", Evidence: "traced the call site"}}

func TestEvaluateMandateReversionRate_BelowThresholdDoesNotDemote(t *testing.T) {
	store, bus, clock := newRateTestBus(t)
	seedRegisteredWork(t, bus, clock, 20)             // denominator = 20
	seedReedits(t, bus, clock, gc.MandateDeadCode, 2) // 2/20 = 10%, below the 25% threshold

	d, err := gc.EvaluateMandateReversionRate(store, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateDeadCode, AfterID: 0, BeforeID: 0, WorkUnitID: "wu-1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Demoted {
		t.Fatalf("did not expect demotion: %+v", d)
	}
	if d.Reversions != 2 || d.RegisteredWork != 22 {
		t.Fatalf("wrong raw counts: %+v", d)
	}
	if !gc.AuthorizedFinding(eligibleFixture(gc.MandateDeadCode), confirmedFixture, nil) {
		t.Fatal("a confirmed finding must stay authorized when its mandate is not demoted")
	}
}

func TestEvaluateMandateReversionRate_AboveThresholdDemotesAndPropagates(t *testing.T) {
	store, bus, clock := newRateTestBus(t)
	seedRegisteredWork(t, bus, clock, 10)                // denominator = 10 registered-work events...
	seedReedits(t, bus, clock, gc.MandateDuplication, 5) // ...plus 5 reedits = 15 registered, 5/15 = 33%, above 25%

	d, err := gc.EvaluateMandateReversionRate(store, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateDuplication, AfterID: 0, BeforeID: 0, WorkUnitID: "wu-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Demoted {
		t.Fatalf("expected demotion: %+v", d)
	}
	if d.Reversions != 5 || d.RegisteredWork != 15 {
		t.Fatalf("wrong raw counts: %+v", d)
	}

}

func TestEvaluateMandateReversionRate_SmallDenominatorNeverDemotes(t *testing.T) {
	store, bus, clock := newRateTestBus(t)
	// Only 2 registered-work events total, both reedits: 100% rate, but far
	// below the policy's min_registered_work, so it must not fire.
	seedReedits(t, bus, clock, gc.MandateDocumentation, 2)

	d, err := gc.EvaluateMandateReversionRate(store, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateDocumentation, AfterID: 0, BeforeID: 0, WorkUnitID: "wu-1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Demoted {
		t.Fatalf("must not demote on a denominator too small to mean anything: %+v", d)
	}
}

func TestEvaluateMandateReversionRate_PublishesControlEffectEvent(t *testing.T) {
	store, bus, clock := newRateTestBus(t)
	seedRegisteredWork(t, bus, clock, 10)
	seedReedits(t, bus, clock, gc.MandateComplexity, 5)

	if _, err := gc.EvaluateMandateReversionRate(store, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateComplexity, AfterID: 0, BeforeID: 0, WorkUnitID: "wu-1"}); err != nil {
		t.Fatal(err)
	}

	events, err := store.Events("session-rate-test", obs.EventControlContain, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one control-contain effect event, got %d", len(events))
	}
}
