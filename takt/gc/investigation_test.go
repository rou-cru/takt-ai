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
	"fmt"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// newRateTestBus retains measurements in the session's durable event store.
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

func registeredWork(mandate gc.MandateClass, n int) []vfs.JournalEntry {
	var entries []vfs.JournalEntry
	for i := 0; i < n; i++ {
		entries = append(entries, vfs.JournalEntry{Seq: i + 1, SessionID: "session-rate-test", Operation: vfs.OpFlush, CycleID: "cycle-1", MandateClass: string(mandate), Path: fmt.Sprintf("file-%d.go", i)})
	}
	return entries
}

func withReedits(entries []vfs.JournalEntry, mandate gc.MandateClass, n int) []vfs.JournalEntry {
	for i := 0; i < n; i++ {
		entries = append(entries, vfs.JournalEntry{Seq: len(entries) + 1, SessionID: "session-rate-test", Operation: vfs.OpPatch, ReeditOfCycleID: "cycle-1", ReeditOfMandateClass: string(mandate), Path: fmt.Sprintf("file-%d.go", i)})
	}
	return entries
}

func eligibleFixture(mandate gc.MandateClass) gc.Finding {
	return gc.Finding{ID: "f1", Mandate: mandate, Tool: "vet", ToolVersion: "1", Rule: "r1", Evidence: "e1"}
}

// confirmedFixture is the confirmed investigation that authorizes eligibleFixture.
var confirmedFixture = []gc.Investigation{{FindingID: "f1", Outcome: "confirmed", Evidence: "traced the call site"}}

func TestEvaluateMandateReversionRate_BelowThresholdDoesNotDemote(t *testing.T) {
	_, bus, clock := newRateTestBus(t)
	entries := withReedits(registeredWork(gc.MandateDeadCode, 20), gc.MandateDeadCode, 2)

	d, err := gc.EvaluateMandateReversionRate(entries, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateDeadCode, WorkUnitID: "wu-1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Demoted {
		t.Fatalf("did not expect demotion: %+v", d)
	}
	if d.Reversions != 2 || d.RegisteredWork != 20 {
		t.Fatalf("wrong raw counts: %+v", d)
	}
	if !gc.AuthorizedFinding(eligibleFixture(gc.MandateDeadCode), confirmedFixture, nil) {
		t.Fatal("a confirmed finding must stay authorized when its mandate is not demoted")
	}
}

func TestEvaluateMandateReversionRate_AboveThresholdDemotesAndPropagates(t *testing.T) {
	_, bus, clock := newRateTestBus(t)
	entries := withReedits(registeredWork(gc.MandateDuplication, 10), gc.MandateDuplication, 5)

	d, err := gc.EvaluateMandateReversionRate(entries, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateDuplication, WorkUnitID: "wu-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Demoted {
		t.Fatalf("expected demotion: %+v", d)
	}
	if d.Reversions != 5 || d.RegisteredWork != 10 {
		t.Fatalf("wrong raw counts: %+v", d)
	}

}

func TestEvaluateMandateReversionRate_SmallDenominatorNeverDemotes(t *testing.T) {
	_, bus, clock := newRateTestBus(t)
	entries := withReedits(registeredWork(gc.MandateDocumentation, 2), gc.MandateDocumentation, 2)

	d, err := gc.EvaluateMandateReversionRate(entries, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateDocumentation, WorkUnitID: "wu-1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Demoted {
		t.Fatalf("must not demote on a denominator too small to mean anything: %+v", d)
	}
}

func TestEvaluateMandateReversionRate_PublishesControlEffectEvent(t *testing.T) {
	store, bus, clock := newRateTestBus(t)
	entries := withReedits(registeredWork(gc.MandateComplexity, 10), gc.MandateComplexity, 5)

	if _, err := gc.EvaluateMandateReversionRate(entries, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateComplexity, WorkUnitID: "wu-1"}); err != nil {
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

func TestEvaluateMandateReversionRate_AlwaysPublishesRawCountsAndDenominator(t *testing.T) {
	store, bus, clock := newRateTestBus(t)
	entries := withReedits(registeredWork(gc.MandateDeadCode, 20), gc.MandateDeadCode, 2)
	// Events that are not work leave the denominator alone.
	if err := obs.PublishCollisionEvent(bus, clock, "agent-a", "agent-b", "a.go", "wu-1"); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := gc.EvaluateMandateReversionRate(entries, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateDeadCode, WorkUnitID: "wu-1"}); err != nil {
			t.Fatal(err)
		}
	}
	rates, err := store.Events("session-rate-test", obs.EventProblemRate, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rates) != 2 {
		t.Fatalf("%d problem_rate events for two evaluations below the threshold, want both published", len(rates))
	}
	// The same events under the same policy reproduce the same record.
	for _, r := range rates {
		a := r.Envelope.Attributes
		if a["signal"] != "cycle_reedit:"+string(gc.MandateDeadCode) || a["count"] != float64(2) || a["denominator"] != float64(20) || a["rate"] != 0.1 {
			t.Fatalf("problem_rate attributes = %+v, want signal, count 2, denominator 20, rate 0.1", a)
		}
		if _, windowed := a["window_seconds"]; windowed {
			t.Fatal("a time window travels with the rate")
		}
	}
	if contain, _ := store.Events("session-rate-test", obs.EventControlContain, 0, 0, 0); len(contain) != 0 {
		t.Fatalf("%d CONTAIN effects below the threshold", len(contain))
	}
}

func TestEvaluateMandateReversionRate_NoRegisteredWorkPublishesNoRate(t *testing.T) {
	store, bus, clock := newRateTestBus(t)
	if _, err := gc.EvaluateMandateReversionRate(nil, bus, clock, gc.MandateReversionRateInput{Agent: "agent-a", SessionID: "session-rate-test", Mandate: gc.MandateComplexity, WorkUnitID: "wu-1"}); err != nil {
		t.Fatal(err)
	}
	rates, err := store.Events("session-rate-test", obs.EventProblemRate, 0, 0, 0)
	if err != nil || len(rates) != 0 {
		t.Fatalf("rates = %+v err = %v, want no measurement without work", rates, err)
	}
}

func TestMandateRateIgnoresUnrelatedWorkAndPreservesMeasurementDuringInactivity(t *testing.T) {
	store, bus, clock := newRateTestBus(t)
	in := gc.MandateReversionRateInput{Agent: "harness", SessionID: bus.SessionID(), Mandate: gc.MandateDeadCode}
	entries := withReedits(registeredWork(in.Mandate, 20), in.Mandate, 2)
	first, err := gc.EvaluateMandateReversionRate(entries, bus, clock, in)
	if err != nil || first.Rate != 0.1 {
		t.Fatalf("initial measurement: %+v %v", first, err)
	}
	for _, e := range registeredWork(gc.MandateComplexity, 20) {
		e.Seq = len(entries)
		entries = append(entries, e)
	}
	for _, e := range entries[:20] {
		e.Seq, e.SessionID = len(entries), "another-session"
		entries = append(entries, e)
	}
	// Repeated edits and reads of one attributed path do not add damage.
	entries = withReedits(entries, in.Mandate, 2)
	read := entries[len(entries)-1]
	read.Seq, read.Operation = len(entries), vfs.OpRead
	entries = append(entries, read)
	again, err := gc.EvaluateMandateReversionRate(entries, bus, clock, in)
	if err != nil || again != first {
		t.Fatalf("unrelated work changed measurement: %+v -> %+v, %v", first, again, err)
	}
	if empty, err := gc.EvaluateMandateReversionRate(nil, bus, clock, in); err != nil || empty.RegisteredWork != 0 || empty.Demoted {
		t.Fatalf("no registered work: %+v %v", empty, err)
	}
	rates, err := store.Events(bus.SessionID(), obs.EventProblemRate, 0, 0, 0)
	if err != nil || len(rates) != 2 || rates[1].Envelope.Attributes["rate"] != 0.1 {
		t.Fatalf("inactivity replaced the last valid measurement: %+v %v", rates, err)
	}
}

func TestMandateRateUsesDurableConsolidationAndReeditJournal(t *testing.T) {
	for _, discarded := range []bool{false, true} {
		t.Run(fmt.Sprintf("discarded=%v", discarded), func(t *testing.T) {
			_, bus, clock := newRateTestBus(t)
			root, state := t.TempDir(), filepath.Join(t.TempDir(), "state")
			f, err := vfs.Open(root, state)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = f.Close() })
			identity := vfs.Identity{SessionID: bus.SessionID(), WorkUnitID: "collect", AttemptID: "1", AgentID: "collector", Specialist: "dev", CycleID: "cycle-1", MandateClass: string(gc.MandateDeadCode)}
			author, err := f.Bind(identity, []string{"a.txt"})
			if err != nil {
				t.Fatal(err)
			}
			r, err := f.Apply(vfs.Operation{Key: author, CallID: "create", Action: vfs.OpCreate, Path: "a.txt", Content: []byte("collected")})
			if err != nil {
				t.Fatal(err)
			}
			judge, err := f.AssignVerifier(vfs.Identity{SessionID: bus.SessionID(), WorkUnitID: "review", AttemptID: "1", AgentID: "judge", Specialist: "verify"}, author)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Verify(judge, author, "verdict", r.Revision, r.DeltaHash, true, ""); err != nil {
				t.Fatal(err)
			}
			measure := func(work, reversions int64) {
				t.Helper()
				d, err := gc.EvaluateMandateReversionRate(f.JournalPage(bus.SessionID(), "", -1, vfs.MaxJournalPageSize), bus, clock, gc.MandateReversionRateInput{Agent: "harness", SessionID: bus.SessionID(), Mandate: gc.MandateDeadCode})
				if err != nil || d.RegisteredWork != work || d.Reversions != reversions {
					t.Fatalf("durable VFS attribution: %+v %v, want work=%d reversions=%d", d, err, work, reversions)
				}
			}
			// A verified staged delta is not registered consolidated work.
			measure(0, 0)
			if err := f.ConsolidateCheckpoint(author, "cp", r.Revision, false); err != nil {
				t.Fatal(err)
			}
			measure(1, 0)
			if discarded {
				if _, err := f.DiscardCycle(identity.CycleID); err != nil {
					t.Fatal(err)
				}
			} else if err := f.CompleteCycle(identity.CycleID); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			f, err = vfs.Open(root, state)
			if err != nil {
				t.Fatal(err)
			}
			cycle, mandate, attributed := f.ConsolidatedBy("a.txt")
			if attributed == discarded || (!discarded && (cycle != identity.CycleID || mandate != identity.MandateClass)) {
				t.Fatalf("attribution after reopen: %q %q %v", cycle, mandate, attributed)
			}
			identity.CycleID, identity.MandateClass, identity.WorkUnitID = "", "", "ordinary"
			author, err = f.Bind(identity, []string{"a.txt"})
			if err != nil {
				t.Fatal(err)
			}
			action := vfs.OpPatch
			if discarded {
				action = vfs.OpCreate
			}
			if _, err = f.Apply(vfs.Operation{Key: author, CallID: "reedit", Action: action, Path: "a.txt", Content: []byte("corrected")}); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			f, err = vfs.Open(root, state)
			if err != nil {
				t.Fatal(err)
			}
			if discarded {
				// Discard remains historical work, but no longer owns later edits.
				measure(1, 0)
			} else {
				measure(1, 1)
			}
		})
	}
}
