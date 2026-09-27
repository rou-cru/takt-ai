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

package gc

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// newRateTestBus opens a workspace-local store and a bus attached to it, so
// EvaluateMandateReversionRate has real persisted events to read back, the
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
func seedReedits(t *testing.T, bus *obs.Bus, clock *obs.Clock, mandate MandateClass, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := obs.PublishCycleReeditEvent(bus, clock, "agent-a", "cycle-1", string(mandate), "hash-1", "wu-1"); err != nil {
			t.Fatal(err)
		}
	}
}

func eligibleFixture(mandate MandateClass) Finding {
	return Finding{Mandate: mandate, Tool: "vet", ToolVersion: "1", Rule: "r1", Evidence: "e1"}
}

func TestEvaluateMandateReversionRate_BelowThresholdDoesNotDemote(t *testing.T) {
	resetDemotedMandates(t)
	store, bus, clock := newRateTestBus(t)
	seedRegisteredWork(t, bus, clock, 20)          // denominator = 20
	seedReedits(t, bus, clock, MandateDeadCode, 2) // 2/20 = 10%, below the 25% threshold

	d, err := EvaluateMandateReversionRate(store, bus, clock, "agent-a", "session-rate-test", MandateDeadCode, 0, 0, "wu-1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Demoted {
		t.Fatalf("did not expect demotion: %+v", d)
	}
	if d.Reversions != 2 || d.RegisteredWork != 22 {
		t.Fatalf("wrong raw counts: %+v", d)
	}
	if !eligibleFinding(eligibleFixture(MandateDeadCode)) {
		t.Fatal("finding must stay eligible when the mandate is not demoted")
	}
}

func TestEvaluateMandateReversionRate_AboveThresholdDemotesAndPropagates(t *testing.T) {
	resetDemotedMandates(t)
	store, bus, clock := newRateTestBus(t)
	seedRegisteredWork(t, bus, clock, 10)             // denominator = 10 registered-work events...
	seedReedits(t, bus, clock, MandateDuplication, 5) // ...plus 5 reedits = 15 registered, 5/15 = 33%, above 25%

	d, err := EvaluateMandateReversionRate(store, bus, clock, "agent-a", "session-rate-test", MandateDuplication, 0, 0, "wu-1")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Demoted {
		t.Fatalf("expected demotion: %+v", d)
	}
	if d.Reversions != 5 || d.RegisteredWork != 15 {
		t.Fatalf("wrong raw counts: %+v", d)
	}

	// Subsequent findings of the demoted mandate class must read as ProposalOnly.
	if eligibleFinding(eligibleFixture(MandateDuplication)) {
		t.Fatal("demoted mandate class must not stay eligible")
	}
	// Unrelated mandate classes are unaffected.
	if !eligibleFinding(eligibleFixture(MandateComplexity)) {
		t.Fatal("demotion must not leak to other mandate classes")
	}
}

func TestEvaluateMandateReversionRate_SmallDenominatorNeverDemotes(t *testing.T) {
	resetDemotedMandates(t)
	store, bus, clock := newRateTestBus(t)
	// Only 2 registered-work events total, both reedits: 100% rate, but far
	// below minRegisteredWorkForRateDecision, so it must not fire.
	seedReedits(t, bus, clock, MandateDocumentation, 2)

	d, err := EvaluateMandateReversionRate(store, bus, clock, "agent-a", "session-rate-test", MandateDocumentation, 0, 0, "wu-1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Demoted {
		t.Fatalf("must not demote on a denominator too small to mean anything: %+v", d)
	}
}

func TestEvaluateMandateReversionRate_PublishesControlEffectEvent(t *testing.T) {
	resetDemotedMandates(t)
	store, bus, clock := newRateTestBus(t)
	seedRegisteredWork(t, bus, clock, 10)
	seedReedits(t, bus, clock, MandateComplexity, 5)

	if _, err := EvaluateMandateReversionRate(store, bus, clock, "agent-a", "session-rate-test", MandateComplexity, 0, 0, "wu-1"); err != nil {
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

// resetDemotedMandates clears the package-level demotion state between tests
// so one test's decision cannot leak into another's.
func resetDemotedMandates(t *testing.T) {
	t.Helper()
	demotedMu.Lock()
	demotedMandates = map[MandateClass]bool{}
	demotedMu.Unlock()
}
