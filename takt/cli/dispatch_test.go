package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	protocol "github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// gcEntries reads the recorded execution history of a harness state directory,
// the way an independent consumer would.
func gcEntries(t *testing.T, state string) []history.Entry {
	t.Helper()
	h, e := history.Open(state)
	if e != nil {
		t.Fatalf("open history: %v", e)
	}
	defer func() {
		if e := h.Close(); e != nil {
			t.Fatalf("close history: %v", e)
		}
	}()
	return h.Entries()
}

// gcProjection replays that record without a clock or external state.
func gcProjection(t *testing.T, state string) history.Projection {
	t.Helper()
	return history.Project(gcEntries(t, state))
}

// gcCount counts recorded entries of one kind, so a duplicated request that
// consumed twice is visible.
func gcCount(t *testing.T, state string, kind history.Kind) int {
	t.Helper()
	n := 0
	for _, e := range gcEntries(t, state) {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func dispatchHarness(t *testing.T) (string, string, func(coordinationRequest) (gc.Coordinator, error)) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	call := func(r coordinationRequest) (gc.Coordinator, error) {
		var out, errout bytes.Buffer
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		e := runDispatch([]string{"--workspace", root, "--state", state, "--request", string(b)}, &out, &errout)
		c, load := gc.LoadCoordinator(state)
		if load != nil {
			return gc.Coordinator{}, load
		}
		return *c, e
	}
	return root, state, call
}

// TestDispatchAdmitsOrdinaryWorkTheSameWayGCCoordinateDoes proves the new,
// properly named surface reaches the same execution history and the same
// admission protocol as `gc coordinate admit` — it is the same mechanism
// under a name that does not claim to belong to GC.
func TestDispatchAdmitsOrdinaryWorkTheSameWayGCCoordinateDoes(t *testing.T) {
	_, state, call := dispatchHarness(t)
	c, e := call(coordinationRequest{Action: "admit", Event: "unit-1", Session: "root", Agent: "dev"})
	if e != nil || c.Units != 1 {
		t.Fatalf("admit: %+v %v", c, e)
	}
	if n := gcCount(t, state, history.KindAdmitted); n != 1 {
		t.Fatalf("expected 1 admitted entry in the execution history, got %d", n)
	}
}

func TestDispatchRecordsDirectActivitiesOutsideWorkUnits(t *testing.T) {
	_, state, call := dispatchHarness(t)
	id := "direct-activity-4a91"
	if _, err := call(coordinationRequest{Action: "activity_start", Session: "root", ActivityID: id, NodeKind: history.NodeKindOrchestrator}); err != nil {
		t.Fatalf("activity_start: %v", err)
	}
	p := gcProjection(t, state)
	if len(p.Units) != 0 || len(p.Activities) != 1 || p.Activities[id].State != history.StateInFlight {
		t.Fatalf("direct activity was not projected separately: %+v", p)
	}
	if _, err := call(coordinationRequest{Action: "activity_finish", Session: "root", ActivityID: id, NodeKind: history.NodeKindOrchestrator, Outcome: string(history.OutcomeCompleted)}); err != nil {
		t.Fatalf("activity_finish: %v", err)
	}
	if a := gcProjection(t, state).Activities[id]; a.State != history.StateSettled || a.Outcome != history.OutcomeCompleted {
		t.Fatalf("activity not settled: %+v", a)
	}
	for _, entry := range gcEntries(t, state) {
		if entry.ActivityID == id && (entry.WorkUnitID != "" || entry.AttemptID != "") {
			t.Fatalf("activity used a work identity: %+v", entry)
		}
	}
	if _, err := call(coordinationRequest{Action: "activity_start", Session: "root", ActivityID: "bad-maintenance", NodeKind: history.NodeKindMaintenance}); err == nil {
		t.Fatal("dispatch CLI accepted maintenance activity owned by the harness")
	}
}

// TestDispatchRefusesMaintenanceCycleActions confirms the new surface only
// carries ordinary crew dispatch, not GC's own cycle phases.
func TestDispatchRefusesMaintenanceCycleActions(t *testing.T) {
	_, _, call := dispatchHarness(t)
	if _, e := call(coordinationRequest{Action: "baseline", Session: "root"}); e == nil {
		t.Fatal("expected a maintenance-cycle action to be refused on the dispatch surface")
	}
}

// TestDispatchSwitchesTheInterlocutorStackTheSameWayGCCoordinateDoes proves
// switch reaches the dispatch surface, and thus the same execution history
// gc coordinate would produce: the interlocutor stack is ordinary crew
// dispatch, not a maintenance-cycle action, so it belongs in the allowlist
// (unlike "baseline" above, which the previous test proves stays refused).
func TestDispatchSwitchesTheInterlocutorStackTheSameWayGCCoordinateDoes(t *testing.T) {
	_, state, call := dispatchHarness(t)
	if _, e := call(coordinationRequest{Action: "switch", Session: "root", Child: "child-1", Agent: "pm", Artifact: "PRD.md"}); e != nil {
		t.Fatalf("switch via dispatch: %v", e)
	}
	if n := gcCount(t, state, history.KindInterlocutorSwitched); n != 1 {
		t.Fatalf("expected 1 interlocutor_switched entry in the execution history, got %d", n)
	}
}

func TestDispatchAdmissionPersistenceAndReplay(t *testing.T) {
	_, state, call := dispatchHarness(t)
	inFlight := func() int { return gcProjection(t, state).InFlight() }
	r := coordinationRequest{Action: "admit", Event: "dispatch-1", Session: "root", Agent: "dev"}
	c, e := call(r)
	if e != nil || c.Units != 1 || inFlight() != 1 {
		t.Fatalf("first: %+v %v in flight %d", c, e, inFlight())
	}
	c, e = call(r)
	if e != nil || c.Units != 1 || inFlight() != 1 {
		t.Fatalf("replay: %+v %v in flight %d", c, e, inFlight())
	}
	if u := gcProjection(t, state).Units["dispatch-1"]; u.State != history.StateInFlight || u.Flight != history.FlightPendingLaunch || u.Launched {
		t.Fatalf("reservation reported as execution: %+v", u)
	}
	r.Agent = "simplify"
	r.Event = "cleanup-ordinary"
	if c, e = call(r); e != nil {
		t.Fatalf("ordinary simplify refused: %v", e)
	}
	if _, e = call(coordinationRequest{Action: "finish", Event: "cleanup-ordinary", Session: "root"}); e != nil {
		t.Fatalf("finish ordinary simplify: %v", e)
	}
	c, e = call(coordinationRequest{Action: "finish", Event: "dispatch-1", Session: "root"})
	if e != nil || inFlight() != 0 {
		t.Fatalf("finish %+v %v in flight %d", c, e, inFlight())
	}
	if u := gcProjection(t, state).Units["dispatch-1"]; u.State != history.StateSettled || u.Outcome != history.OutcomeCompleted {
		t.Fatalf("termination not settled: %+v", u)
	}
	c, e = call(coordinationRequest{Action: "finish", Event: "dispatch-1", Session: "root"})
	if e != nil || c.Units != 2 {
		t.Fatalf("finish replay %+v %v", c, e)
	}
}

// TestDispatchAdmissionCeiling drives the real IPC path the plugin uses, so
// the ceiling fails loudly if admission stops consulting the projection.
func TestDispatchAdmissionCeiling(t *testing.T) {
	_, state, call := dispatchHarness(t)
	p, e := protocol.LoadAdmissionPolicy()
	if e != nil {
		t.Fatal(e)
	}
	admit := func(event string) error {
		_, err := call(coordinationRequest{Action: "admit", Event: event, Session: "root", Agent: "dev"})
		return err
	}
	for i := 1; i < p.Concurrency.Specialists; i++ {
		if e := admit(fmt.Sprintf("dispatch-%d", i)); e != nil {
			t.Fatalf("admission %d denied: %v", i, e)
		}
	}
	// Two requests compete for the last slot: one reserves it, the other is denied.
	if e := admit("contender-a"); e != nil {
		t.Fatalf("contender a denied: %v", e)
	}
	denial := admit("contender-b")
	if denial == nil || !strings.Contains(denial.Error(), "ceiling") {
		t.Fatalf("fifth admission not visibly denied: %v", denial)
	}
	before := gcProjection(t, state)
	if before.InFlight() != p.Concurrency.Specialists {
		t.Fatalf("in flight %d; want %d", before.InFlight(), p.Concurrency.Specialists)
	}
	if _, ok := before.Units["contender-b"]; ok {
		t.Fatal("denied request created a work unit")
	}
	// The denial stays visible in the record even though no work state changed.
	h, e := history.Open(state)
	if e != nil {
		t.Fatal(e)
	}
	entries := h.Entries()
	if e := h.Close(); e != nil {
		t.Fatal(e)
	}
	denied := 0
	for i, entry := range entries {
		if entry.Seq != i {
			t.Fatalf("entry %d out of order: %+v", i, entry)
		}
		if entry.Kind == history.KindDenied && entry.WorkUnitID == "contender-b" {
			denied++
			if entry.Author != history.AuthorHarness || entry.PolicyRef != gc.AdmissionPolicyRef {
				t.Fatalf("denial not attributed: %+v", entry)
			}
		}
	}
	if denied != 1 {
		t.Fatalf("denials recorded: %d", denied)
	}
	// Replaying the same prefix derives the same projection, without a clock.
	if !reflect.DeepEqual(history.Project(entries), before) {
		t.Fatal("replay of the same prefix derived a different projection")
	}
	// Effective termination releases the slot. The four admissions also reached
	// the unplanned bound, so the denied work enters once a commitment covers
	// it: a plan enables the work it covers (PR-HAR-19, PR-DAG-MUT-1).
	if _, e := call(coordinationRequest{Action: "finish", Event: "contender-a", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	if e := admit("contender-b"); e == nil || !strings.Contains(e.Error(), "unplanned") {
		t.Fatalf("uncovered work admitted past the bound: %v", e)
	}
	if _, e := call(coordinationRequest{Action: "commit", Session: "root", Version: "v1",
		Plan: []gc.PlanUnit{{Unit: "contender-b", Contract: "finish the contested work"}}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := admit("contender-b"); e != nil {
		t.Fatalf("covered work denied: %v", e)
	}
	if u := gcProjection(t, state).Units["contender-b"]; u.Contract == "" || u.State != history.StateInFlight {
		t.Fatalf("commitment coverage lost: %+v", u)
	}
	// The commitment did not erase the accumulated unplanned count.
	if n := gcProjection(t, state).Budgets("root").Unplanned; n != p.Budgets.UnplannedUnits {
		t.Fatalf("unplanned count %d; want %d", n, p.Budgets.UnplannedUnits)
	}
}

// TestDispatchAdmissionRecoversFromOrphanedBarrierHold reproduces a real
// deadlock: a session leaves the workspace mid-resolution (an in-flight unit
// never finished, or a VFS delta staged and never verified/consolidated —
// exactly what an interrupted session leaves behind, with no liveness signal
// anywhere in the harness), then a second session insists on delegating new
// work the way an orchestrator does. Admission must recover within
// MaxDeferrals+1 attempts, never hang forever.
func TestDispatchAdmissionRecoversFromOrphanedBarrierHold(t *testing.T) {
	p, e := gc.LoadTriggerPolicy()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name           string
		orphanInFlight bool
		orphanDelta    bool
	}{
		{"orphaned in-flight unit", true, false},
		{"orphaned pending delta", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Ordinary admission goes through `dispatch`; GC trigger
			// bookkeeping stays under `gc coordinate`. Both share the same
			// root/state so the barrier recovery is exercised end to end.
			root := t.TempDir()
			state := filepath.Join(t.TempDir(), "state")
			dispatchCall := func(r coordinationRequest) (gc.Coordinator, error) {
				var out, errout bytes.Buffer
				b, err := json.Marshal(r)
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				e := runDispatch([]string{"--workspace", root, "--state", state, "--request", string(b)}, &out, &errout)
				c, load := gc.LoadCoordinator(state)
				if load != nil {
					return gc.Coordinator{}, load
				}
				return *c, e
			}
			gcCall := func(r coordinationRequest) (gc.Coordinator, error) {
				var out, errout bytes.Buffer
				b, err := json.Marshal(r)
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				e := runGC([]string{"coordinate", "--workspace", root, "--state", state, "--request", string(b)}, &out, &errout)
				c, load := gc.LoadCoordinator(state)
				if load != nil {
					return gc.Coordinator{}, load
				}
				return *c, e
			}

			if tc.orphanInFlight {
				if _, e := dispatchCall(coordinationRequest{Action: "admit", Event: "orphan-1", Session: "session-1", Agent: "dev"}); e != nil {
					t.Fatal(e)
				}
				// admit/finish are history-only: on their own they leave
				// Mutations==0, and Decide forces Skip regardless of
				// UserRequested when Mutations==0 (ReasonNoDelta). A resolved
				// ordinary mutation, unrelated to the orphan, is needed so
				// the barrier is actually exercised for ActiveUnits alone.
				fs, e := vfs.Open(root, state)
				if e != nil {
					t.Fatal(e)
				}
				id := vfs.Identity{SessionID: "session-1", WorkUnitID: "keepalive", AgentID: "dev", Specialist: "dev"}
				author, e := fs.Bind(id, []string{"keepalive.txt"})
				if e != nil {
					t.Fatal(e)
				}
				result, e := fs.Apply(vfs.Operation{Key: author, CallID: "write", Action: vfs.OpCreate, Path: "keepalive.txt", Content: []byte("ok\n")})
				if e != nil {
					t.Fatal(e)
				}
				id.AgentID, id.Specialist = "verify", "verify"
				verifier, e := fs.AssignVerifier(id, author)
				if e != nil {
					t.Fatal(e)
				}
				if e := fs.Verify(verifier, author, "verify", result.Revision, result.DeltaHash, true, ""); e != nil {
					t.Fatal(e)
				}
				if e := fs.ConsolidateCheckpoint(author, "keepalive", result.Revision); e != nil {
					t.Fatal(e)
				}
				if e := fs.Close(); e != nil {
					t.Fatal(e)
				}
			}
			if tc.orphanDelta {
				fs, e := vfs.Open(root, state)
				if e != nil {
					t.Fatal(e)
				}
				id := vfs.Identity{SessionID: "session-1", WorkUnitID: "orphan-delta", AgentID: "dev", Specialist: "dev"}
				author, e := fs.Bind(id, []string{"a.go"})
				if e != nil {
					t.Fatal(e)
				}
				if _, e = fs.Apply(vfs.Operation{Key: author, CallID: "write", Action: vfs.OpCreate, Path: "a.go", Content: []byte("package main\n")}); e != nil {
					t.Fatal(e)
				}
				if e := fs.Close(); e != nil {
					t.Fatal(e)
				}
			}

			// Makes a cycle due right away, without waiting out the cadence budget.
			if _, e := gcCall(coordinationRequest{Action: "request", Session: "session-1"}); e != nil {
				t.Fatal(e)
			}

			// Session 2 (the orchestrator): repeatedly tries to delegate
			// brand-new work — the exact "blocked from the first delegation
			// attempt" scenario from the real incident.
			var lastErr error
			for i := 0; i < p.Cadence.MaxDeferrals+1; i++ {
				_, lastErr = dispatchCall(coordinationRequest{Action: "admit", Event: "new-unit", Session: "session-2", Agent: "dev"})
				if lastErr == nil {
					break
				}
			}
			if lastErr != nil {
				t.Fatalf("admission still blocked after %d attempts: %v", p.Cadence.MaxDeferrals+1, lastErr)
			}
			if u := gcProjection(t, state).Units["new-unit"]; u.State != history.StateInFlight {
				t.Fatalf("admission did not actually proceed: %+v", u)
			}
		})
	}
}

// TestDispatchLaunchAndUncertainReconciliation drives the repetition and
// reconciliation guarantees of PR-HAR-17 over the real IPC path.
func TestDispatchLaunchAndUncertainReconciliation(t *testing.T) {
	_, state, call := dispatchHarness(t)
	do := func(action, event string, pass bool) error {
		_, e := call(coordinationRequest{Action: action, Event: event, Session: "root", Agent: "dev", Pass: pass})
		return e
	}
	if e := do("admit", "one", false); e != nil {
		t.Fatal(e)
	}
	// The same launch delivered twice yields one execution, not two.
	for range 2 {
		if e := do("launch", "one", false); e != nil {
			t.Fatalf("launch: %v", e)
		}
	}
	if n := gcCount(t, state, history.KindLaunched); n != 1 {
		t.Fatalf("launches recorded: %d", n)
	}
	// Communication fails after a possible launch: reconcile before any repeat,
	// and keep the reservation while the outcome is unknown.
	if e := do("admit", "two", false); e != nil {
		t.Fatal(e)
	}
	if e := do("uncertain", "two", false); e != nil {
		t.Fatal(e)
	}
	if e := do("launch", "two", false); e == nil || !strings.Contains(e.Error(), "reconciled") {
		t.Fatalf("uncertain launch repeated: %v", e)
	}
	if n := gcProjection(t, state).InFlight(); n != 2 {
		t.Fatalf("uncertainty released capacity: in flight %d", n)
	}
	// Reconciliation that finds execution running keeps the slot and the unit.
	if e := do("reconcile", "two", true); e != nil {
		t.Fatal(e)
	}
	if u := gcProjection(t, state).Units["two"]; !u.Launched || u.State != history.StateInFlight {
		t.Fatalf("reconciled launch: %+v", u)
	}
	// Reconciliation that establishes non-start is the effective termination.
	if e := do("admit", "three", false); e != nil {
		t.Fatal(e)
	}
	if e := do("uncertain", "three", false); e != nil {
		t.Fatal(e)
	}
	if e := do("reconcile", "three", false); e != nil {
		t.Fatal(e)
	}
	if u := gcProjection(t, state).Units["three"]; u.State != history.StateSettled || u.Outcome != history.OutcomeInterrupted || u.Launched {
		t.Fatalf("non-start: %+v", u)
	}
}

// TestDispatchCancellationRetainsCapacityAndControls covers PR-HAR-18: a
// request is not effective termination, and the controls that resolve it
// need no slot.
func TestDispatchCancellationRetainsCapacityAndControls(t *testing.T) {
	_, state, call := dispatchHarness(t)
	p, e := protocol.LoadAdmissionPolicy()
	if e != nil {
		t.Fatal(e)
	}
	do := func(action, event string) error {
		_, err := call(coordinationRequest{Action: action, Event: event, Session: "root", Agent: "dev"})
		return err
	}
	for i := range p.Concurrency.Specialists {
		if e := do("admit", fmt.Sprintf("unit-%d", i)); e != nil {
			t.Fatalf("admission %d: %v", i, e)
		}
	}
	if e := do("cancel", "unit-0"); e != nil {
		t.Fatal(e)
	}
	if e := do("suspend", "unit-1"); e != nil {
		t.Fatal(e)
	}
	before := gcProjection(t, state)
	if before.InFlight() != p.Concurrency.Specialists {
		t.Fatalf("cancellation or suspension released capacity: in flight %d", before.InFlight())
	}
	if u := before.Units["unit-0"]; u.Flight != history.FlightCancelling {
		t.Fatalf("cancellation pending lost: %+v", u)
	}
	// Termination and resolution controls stay available with the ceiling full
	// and admit no further execution specialist.
	if e := do("admit", "another"); e == nil || !strings.Contains(e.Error(), "ceiling") {
		t.Fatalf("admitted past a full ceiling: %v", e)
	}
	if _, e := call(coordinationRequest{Action: "exception", Event: "unit-0", Session: "root",
		Bound: history.BoundUnplanned, Allowance: 1}); e != nil {
		t.Fatalf("resolution control unavailable at the ceiling: %v", e)
	}
	if e := do("finish", "unit-0"); e != nil {
		t.Fatalf("termination control unavailable at the ceiling: %v", e)
	}
	after := gcProjection(t, state)
	if u := after.Units["unit-0"]; u.State != history.StateSettled || u.Outcome != history.OutcomeInterrupted {
		t.Fatalf("cancelled unit settled as: %+v", u)
	}
	if after.InFlight() != p.Concurrency.Specialists-1 {
		t.Fatalf("effective termination did not release: in flight %d", after.InFlight())
	}
}

// TestDispatchUnplannedDelegationBound covers PR-HAR-19: the fourth uncovered
// unit reaches the bound and the fifth is denied until a resolution is
// recorded.
func TestDispatchUnplannedDelegationBound(t *testing.T) {
	_, state, call := dispatchHarness(t)
	p, e := protocol.LoadAdmissionPolicy()
	if e != nil {
		t.Fatal(e)
	}
	do := func(action, event string) error {
		_, err := call(coordinationRequest{Action: action, Event: event, Session: "root", Agent: "dev"})
		return err
	}
	// Each unit is retired before the next so the ceiling never masks the bound.
	for i := range p.Budgets.UnplannedUnits {
		unit := fmt.Sprintf("loose-%d", i)
		if e := do("admit", unit); e != nil {
			t.Fatalf("unplanned admission %d: %v", i, e)
		}
		// A unit cancelled and never launched still counts against the bound.
		if i == 0 {
			if e := do("cancel", unit); e != nil {
				t.Fatal(e)
			}
		}
		if e := do("finish", unit); e != nil {
			t.Fatal(e)
		}
		// A retry of the same unit adds no unit.
		if e := do("admit", unit); e != nil {
			t.Fatal(e)
		}
	}
	if n := gcProjection(t, state).Budgets("root").Unplanned; n != p.Budgets.UnplannedUnits {
		t.Fatalf("unplanned count %d; want %d", n, p.Budgets.UnplannedUnits)
	}
	denied := func(unit string) {
		t.Helper()
		if e := do("admit", unit); e == nil || !strings.Contains(e.Error(), "unplanned") {
			t.Fatalf("%s admitted past the bound: %v", unit, e)
		}
	}
	denied("loose-5")
	// Stopping delegation and sending an escalation are recorded distinctly and
	// reopen nothing.
	for _, action := range []string{"stop", "escalate"} {
		if e := do(action, "loose-5"); e != nil {
			t.Fatalf("%s: %v", action, e)
		}
		denied("loose-5")
	}
	budgets := gcProjection(t, state).Budgets("root")
	if budgets.Stopped != 1 || budgets.Escalated != 1 {
		t.Fatalf("stop and escalation not distinguishable: %+v", budgets)
	}
	// A scoped exception with a finite allowance enables exactly that much.
	if _, e := call(coordinationRequest{Action: "exception", Event: "loose-5", Session: "root",
		Bound: history.BoundUnplanned, Allowance: 1}); e != nil {
		t.Fatal(e)
	}
	if e := do("admit", "loose-5"); e != nil {
		t.Fatalf("scoped exception did not enable the unit: %v", e)
	}
	denied("loose-6")
	// Consumption survives the restart the next invocation already is.
	if n := gcProjection(t, state).Budgets("root").Unplanned; n != p.Budgets.UnplannedUnits+1 {
		t.Fatalf("consumption reset: %d", n)
	}
}

// TestDispatchContestAllowance covers PR-HAR-20: three distinct contests
// consume the allowance, transport duplicates do not, and the fourth needs an
// exception.
func TestDispatchContestAllowance(t *testing.T) {
	_, state, call := dispatchHarness(t)
	p, e := protocol.LoadAdmissionPolicy()
	if e != nil {
		t.Fatal(e)
	}
	contest := func(unit string) error {
		_, err := call(coordinationRequest{Action: "contest", Event: unit, Session: "root"})
		return err
	}
	for i := range p.Budgets.Contests {
		if e := contest(fmt.Sprintf("failure-%d", i)); e != nil {
			t.Fatalf("contest %d denied: %v", i, e)
		}
	}
	// A transport duplicate returns the recorded disposition and consumes nothing.
	if e := contest("failure-0"); e != nil {
		t.Fatal(e)
	}
	if n := gcCount(t, state, history.KindContested); n != p.Budgets.Contests {
		t.Fatalf("contests recorded: %d; want %d", n, p.Budgets.Contests)
	}
	e = contest("failure-late")
	if e == nil || !strings.Contains(e.Error(), "contest allowance") {
		t.Fatalf("fourth contest not denied: %v", e)
	}
	// The denial stays visible without consuming the allowance.
	budgets := gcProjection(t, state).Budgets("root")
	if len(budgets.Contests) != p.Budgets.Contests {
		t.Fatalf("denied contest consumed: %+v", budgets.Contests)
	}
	if n := gcCount(t, state, history.KindDenied); n != 1 {
		t.Fatalf("denials recorded: %d", n)
	}
	if _, e := call(coordinationRequest{Action: "exception", Event: "failure-late", Session: "root",
		Bound: history.BoundContests, Allowance: 1}); e != nil {
		t.Fatal(e)
	}
	if e := contest("failure-late"); e != nil {
		t.Fatalf("scoped exception did not enable the contest: %v", e)
	}
	if contest("failure-later") == nil {
		t.Fatal("exception granted an open-ended allowance")
	}
}

// TestDispatchRecoveryFailureStreak covers PR-HAR-21: two consecutive failed
// recoveries of the same objective close autonomous recovery for it.
func TestDispatchRecoveryFailureStreak(t *testing.T) {
	_, state, call := dispatchHarness(t)
	p, e := protocol.LoadAdmissionPolicy()
	if e != nil {
		t.Fatal(e)
	}
	declare := func(unit, objective string) error {
		_, err := call(gcDeclaration(unit, objective, []string{unit}, 4, 1))
		return err
	}
	closeRecovery := func(unit, objective string, demonstrated bool) {
		t.Helper()
		if _, e := call(coordinationRequest{Action: "recovered", Event: unit, Session: "root",
			Objective: objective, Pass: demonstrated, Evidence: "gate evidence"}); e != nil {
			t.Fatalf("close recovery: %v", e)
		}
	}
	for i := range p.Budgets.RecoveryFailures {
		unit := fmt.Sprintf("attempt-%d", i)
		if e := declare(unit, "goal"); e != nil {
			t.Fatalf("recovery %d denied: %v", i, e)
		}
		closeRecovery(unit, "goal", false)
	}
	blocked := func(unit string) {
		t.Helper()
		if e := declare(unit, "goal"); e == nil || !strings.Contains(e.Error(), "escalation") {
			t.Fatalf("%s admitted past the streak: %v", unit, e)
		}
	}
	// Renaming the work does not reset the streak; the objective identity carries it.
	blocked("renamed-work")
	// Unrelated progress does not break it, and another objective is unaffected.
	for _, r := range []coordinationRequest{
		{Action: "admit", Event: "elsewhere", Session: "root", Agent: "dev"},
		{Action: "finish", Event: "elsewhere", Session: "root"},
	} {
		if _, e := call(r); e != nil {
			t.Fatal(e)
		}
	}
	blocked("after-progress")
	if e := declare("other-attempt", "other-goal"); e != nil {
		t.Fatalf("a different objective was blocked: %v", e)
	}
	// An enabling user decision scoped to the objective reopens exactly one.
	if _, e := call(coordinationRequest{Action: "exception", Event: "attempt-0", Session: "root",
		Bound: history.BoundRecovery, Objective: "goal", Allowance: 1}); e != nil {
		t.Fatal(e)
	}
	if e := declare("attempt-3", "goal"); e != nil {
		t.Fatalf("enabling decision ignored: %v", e)
	}
	// Only a demonstrated recovery breaks the streak.
	closeRecovery("attempt-3", "goal", true)
	if r := gcProjection(t, state).Budgets("root").Recoveries["goal"]; r.Failures != 0 || r.Open {
		t.Fatalf("demonstrated recovery did not break the streak: %+v", r)
	}
	if e := declare("attempt-4", "goal"); e != nil {
		t.Fatalf("recovery blocked after a demonstrated success: %v", e)
	}
}

// gcDeclaration is a complete recovery declaration: a binary expected result,
// both budgets, the objective, a recoverable point and the explicit scope.
func gcDeclaration(unit, objective string, scope []string, actions, attempts int) coordinationRequest {
	return coordinationRequest{
		Action: "recovery", Event: unit, Session: "root", Objective: objective,
		Result: "the failing gate passes", Point: "journal/0", Scope: scope,
		Actions: actions, Attempts: attempts,
	}
}

// gcActions records n tool executions against unit through the real VFS, which
// is the bus the plugin's tick reports; it returns the binding whose staged
// delta a restoration discards.
func gcActions(t *testing.T, root, state string, key vfs.AgentID, unit, label string, n int) vfs.AgentID {
	t.Helper()
	fs, e := vfs.Open(root, state)
	if e != nil {
		t.Fatalf("open vfs: %v", e)
	}
	defer func() {
		if e := fs.Close(); e != nil {
			t.Fatalf("close vfs: %v", e)
		}
	}()
	// An attempt stays open while its binding owns the file, so later actions of
	// the same unit reuse the key the first ones were recorded under.
	if key == "" {
		if key, e = fs.Bind(vfs.Identity{SessionID: "root", WorkUnitID: unit, AgentID: "dev", Specialist: "dev"}, []string{unit + ".txt"}); e != nil {
			t.Fatalf("bind %s: %v", unit, e)
		}
	}
	for i := range n {
		op := vfs.Operation{
			Key: key, CallID: fmt.Sprintf("%s-%s-%d", unit, label, i), Action: vfs.OpCreate,
			Path: unit + ".txt", Content: []byte(fmt.Sprintf("%s %d\n", label, i)),
			ExpectedRevision: fs.InspectDelta(key).Revision,
		}
		if _, e := fs.Apply(op); e != nil {
			t.Fatalf("action %d: %v", i, e)
		}
	}
	return key
}

// gcRecovery replays the record and returns one objective's derived accounting.
func gcRecovery(t *testing.T, state, objective string) history.Recovery {
	t.Helper()
	return gcProjection(t, state).Budgets("root").Recoveries[objective]
}

// TestDispatchRecoveryDeclarationRecordedWhenIncomplete covers PR-DAG-MUT-9:
// what the orchestrator did not declare is recorded as missing, and an
// incomplete declaration is no proof that bounded recovery occurred.
func TestDispatchRecoveryDeclarationRecordedWhenIncomplete(t *testing.T) {
	_, state, call := dispatchHarness(t)
	partial := gcDeclaration("unit", "goal", []string{"unit"}, 0, 2)
	partial.Point = ""
	if _, e := call(partial); e == nil || !strings.Contains(e.Error(), "recoverable point") {
		t.Fatalf("incomplete declaration accepted: %v", e)
	}
	if r := gcRecovery(t, state, "goal"); r.Open || r.Actions != 0 {
		t.Fatalf("incomplete declaration opened bounded recovery: %+v", r)
	}
	recorded := 0
	for _, e := range gcEntries(t, state) {
		if e.Kind != history.KindDenied {
			continue
		}
		recorded++
		// The record shows exactly what was declared and what was absent.
		if e.Objective != "goal" || e.Result == "" || e.Attempts != 2 || e.Point != "" || e.Actions != 0 {
			t.Fatalf("declaration not recorded as it arrived: %+v", e)
		}
	}
	if recorded != 1 {
		t.Fatalf("incomplete declaration recorded %d times", recorded)
	}
	// A declaration beyond the admissible ceiling is denied rather than trimmed.
	p, e := protocol.LoadAdmissionPolicy()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := call(gcDeclaration("unit", "goal", []string{"unit"}, p.Recovery.MaxActions+1, 1)); e == nil ||
		!strings.Contains(e.Error(), "ceiling") {
		t.Fatalf("unbounded action budget admitted: %v", e)
	}
	if _, e := call(gcDeclaration("unit", "goal", []string{"unit"}, 4, 1)); e != nil {
		t.Fatalf("complete declaration denied: %v", e)
	}
	if r := gcRecovery(t, state, "goal"); !r.Open || r.Actions != 4 || r.Attempts != 1 || len(r.Scope) != 1 || r.Result == "" || r.Point == "" {
		t.Fatalf("complete declaration not recorded: %+v", r)
	}
}

// TestDispatchRecoveryActionBudgetForcesBacktracking covers PR-HAR-6 and
// PR-OBS-PRG-2: every recorded action in the scope consumes allowance
// whichever attempt performs it, and exhaustion forces backtracking through
// the tick the plugin already sends. Abandonment, termination and
// restoration stay three facts.
func TestDispatchRecoveryActionBudgetForcesBacktracking(t *testing.T) {
	root, state, call := dispatchHarness(t)
	const budget = 3
	if _, e := call(gcDeclaration("declaring-unit", "goal", []string{"scope-unit", "next-attempt"}, budget, 2)); e != nil {
		t.Fatal(e)
	}
	if _, e := call(coordinationRequest{Action: "admit", Event: "scope-unit", Session: "root", Agent: "dev"}); e != nil {
		t.Fatal(e)
	}
	// The admission declares its scope membership rather than being inferred.
	admitted := gcEntries(t, state)
	if last := admitted[len(admitted)-1]; last.Kind != history.KindAdmitted || last.Objective != "goal" {
		t.Fatalf("attempt did not identify its scope: %+v", last)
	}
	// Work outside the scope consumes nothing, whatever its timing.
	gcActions(t, root, state, "", "elsewhere", "noise", 2)
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	if r := gcRecovery(t, state, "goal"); r.Used != 0 || !r.Open {
		t.Fatalf("unrelated work consumed allowance: %+v", r)
	}
	// A first attempt spends part of the allowance.
	scope := gcActions(t, root, state, "", "scope-unit", "first", budget-1)
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	if r := gcRecovery(t, state, "goal"); r.Used != budget-1 || !r.Open {
		t.Fatalf("consumption after the first attempt: %+v", r)
	}
	// A second attempt of the same scope keeps spending the same allowance.
	key := gcActions(t, root, state, scope, "scope-unit", "second", 1)
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	r := gcRecovery(t, state, "goal")
	if r.Used != budget || r.Open || !r.Backtracked || r.Restored {
		t.Fatalf("exhausted action budget did not force backtracking: %+v", r)
	}
	// No further attempt enters the abandoned scope.
	if _, e := call(coordinationRequest{Action: "admit", Event: "next-attempt", Session: "root", Agent: "dev"}); e == nil {
		t.Fatal("abandoned scope admitted another attempt")
	}
	// Restoration waits until the affected execution can no longer act.
	if _, e := call(coordinationRequest{Action: "restore", Session: "root", Objective: "goal", Key: string(key)}); e == nil ||
		!strings.Contains(e.Error(), "no longer act") {
		t.Fatalf("restoration claimed while the scope could still act: %v", e)
	}
	if _, e := call(coordinationRequest{Action: "finish", Event: "scope-unit", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	if u := gcProjection(t, state).Units["scope-unit"]; u.Outcome != history.OutcomeBacktracked {
		t.Fatalf("terminated scope work not backtracked: %+v", u)
	}
	if _, e := call(coordinationRequest{Action: "restore", Session: "root", Objective: "goal", Key: string(key)}); e != nil {
		t.Fatalf("restoration: %v", e)
	}
	if r := gcRecovery(t, state, "goal"); !r.Restored {
		t.Fatalf("restoration not confirmed: %+v", r)
	}
	// Restoration discarded only the scope's virtual state, and confirmed it
	// after the abandonment decision and the effective termination, never before.
	fs, e := vfs.Open(root, state)
	if e != nil {
		t.Fatal(e)
	}
	staged := fs.InspectDelta(key).Files
	if e := fs.Close(); e != nil {
		t.Fatal(e)
	}
	if len(staged) != 0 {
		t.Fatalf("scope deltas survived restoration: %v", staged)
	}
	order := []history.Kind{}
	for _, entry := range gcEntries(t, state) {
		switch entry.Kind {
		case history.KindRecoveryClosed, history.KindTerminated, history.KindRestored:
			order = append(order, entry.Kind)
		}
	}
	if !reflect.DeepEqual(order, []history.Kind{history.KindRecoveryClosed, history.KindTerminated, history.KindRestored}) {
		t.Fatalf("abandonment, termination and restoration are not three ordered facts: %v", order)
	}
	// A restoration is not repeatable, and missing evidence was not success.
	if _, e := call(coordinationRequest{Action: "restore", Session: "root", Objective: "goal", Key: string(key)}); e == nil {
		t.Fatal("restoration confirmed twice")
	}
	for _, entry := range gcEntries(t, state) {
		if entry.Kind == history.KindRecoveryClosed && (entry.Outcome != history.OutcomeBacktracked || entry.Evidence != "") {
			t.Fatalf("forced closure claimed a result: %+v", entry)
		}
	}
}

// TestDispatchRecoveryAttemptBudget covers PR-HAR-6's attempt axis: the last
// permitted attempt bars another admission but may finish and present
// evidence, and finishing without the declared result forces backtracking.
func TestDispatchRecoveryAttemptBudget(t *testing.T) {
	_, state, call := dispatchHarness(t)
	admit := func(unit string) error {
		_, e := call(coordinationRequest{Action: "admit", Event: unit, Session: "root", Agent: "dev"})
		return e
	}
	if _, e := call(gcDeclaration("declaring", "goal", []string{"last-attempt", "never"}, 40, 1)); e != nil {
		t.Fatal(e)
	}
	if e := admit("last-attempt"); e != nil {
		t.Fatal(e)
	}
	if e := admit("never"); e == nil || !strings.Contains(e.Error(), "attempt budget") {
		t.Fatalf("attempt admitted past the budget: %v", e)
	}
	// The admitted attempt finishes and presents evidence while allowance remains.
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	if r := gcRecovery(t, state, "goal"); !r.Open || r.Backtracked {
		t.Fatalf("last attempt cut short: %+v", r)
	}
	if _, e := call(coordinationRequest{Action: "recovered", Event: "declaring", Session: "root", Objective: "goal", Pass: true}); e == nil {
		t.Fatal("a demonstrated result was accepted without evidence")
	}
	if _, e := call(coordinationRequest{Action: "recovered", Event: "declaring", Session: "root",
		Objective: "goal", Pass: true, Evidence: "journal/7"}); e != nil {
		t.Fatal(e)
	}
	if r := gcRecovery(t, state, "goal"); r.Open || r.Backtracked || r.Failures != 0 {
		t.Fatalf("demonstrated recovery: %+v", r)
	}
	// A second recovery whose only attempt finishes without the declared result is
	// backtracked by the harness, with no renewed approval.
	if _, e := call(gcDeclaration("declaring", "other", []string{"only-attempt"}, 40, 1)); e != nil {
		t.Fatal(e)
	}
	if e := admit("only-attempt"); e != nil {
		t.Fatal(e)
	}
	if _, e := call(coordinationRequest{Action: "finish", Event: "only-attempt", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	r := gcRecovery(t, state, "other")
	if r.Open || !r.Backtracked || r.Failures != 1 {
		t.Fatalf("exhausted attempts did not force backtracking: %+v", r)
	}
}

// TestDispatchRecoveryConsumptionUnestablished covers PR-OBS-PRG-2: when the
// bus can no longer account for consumption already observed, the
// uncertainty is visible and budgeted admission waits for an enabling
// decision instead of receiving a fresh allowance.
func TestDispatchRecoveryConsumptionUnestablished(t *testing.T) {
	root, state, call := dispatchHarness(t)
	if _, e := call(gcDeclaration("declaring", "goal", []string{"scope-unit", "later"}, 8, 3)); e != nil {
		t.Fatal(e)
	}
	gcActions(t, root, state, "", "scope-unit", "before", 2)
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	// The bus store is lost while the recorded history stands.
	if e := os.Remove(filepath.Join(state, "vfs.sqlite")); e != nil {
		t.Fatal(e)
	}
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	r := gcRecovery(t, state, "goal")
	if !r.Unreconciled || r.Used != 2 {
		t.Fatalf("lost consumption not visible: %+v", r)
	}
	if _, e := call(coordinationRequest{Action: "admit", Event: "later", Session: "root", Agent: "dev"}); e == nil ||
		!strings.Contains(e.Error(), "unestablished") {
		t.Fatalf("budgeted admission granted on unknown consumption: %v", e)
	}
	// Reconciliation is a recorded decision, and it keeps what was consumed.
	if _, e := call(coordinationRequest{Action: "exception", Event: "declaring", Session: "root",
		Bound: history.BoundRecoveryActions, Objective: "goal", Allowance: 2}); e != nil {
		t.Fatal(e)
	}
	if _, e := call(coordinationRequest{Action: "admit", Event: "later", Session: "root", Agent: "dev"}); e != nil {
		t.Fatalf("reconciled recovery still withheld: %v", e)
	}
	if r := gcRecovery(t, state, "goal"); r.Unreconciled || r.Used != 2 {
		t.Fatalf("reconciliation erased consumption: %+v", r)
	}
}

// TestDispatchRecoveryConsumptionSurvivesRestart is the anti-fraud property
// of PR-OBS-PRG-2 and PR-HAR-22: a restart mid-recovery grants no fresh
// allowance, and redeclaring the same objective does not reset what was
// consumed.
func TestDispatchRecoveryConsumptionSurvivesRestart(t *testing.T) {
	root, state, call := dispatchHarness(t)
	const budget = 4
	if _, e := call(gcDeclaration("declaring", "goal", []string{"scope-unit"}, budget, 2)); e != nil {
		t.Fatal(e)
	}
	scope := gcActions(t, root, state, "", "scope-unit", "before", budget/2)
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	if r := gcRecovery(t, state, "goal"); r.Used != budget/2 {
		t.Fatalf("consumption before the restart: %+v", r)
	}
	// The runtime restarts: transient coordinator state is gone, the recorded
	// history is not.
	if e := os.Remove(filepath.Join(state, "gc-coordinator.json")); e != nil {
		t.Fatal(e)
	}
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	// Redeclaring the same objective mid-recovery yields the recorded
	// disposition; it is not a fresh budget.
	if _, e := call(gcDeclaration("declaring", "goal", []string{"scope-unit"}, budget, 2)); e != nil {
		t.Fatal(e)
	}
	r := gcRecovery(t, state, "goal")
	if r.Used != budget/2 || !r.Open || r.Actions != budget {
		t.Fatalf("restart or redeclaration granted fresh allowance: %+v", r)
	}
	// The remaining allowance is what was left, so exhaustion arrives at the same
	// recorded total.
	gcActions(t, root, state, scope, "scope-unit", "after", budget/2)
	if _, e := call(coordinationRequest{Action: "tick", Session: "root"}); e != nil {
		t.Fatal(e)
	}
	if r := gcRecovery(t, state, "goal"); r.Used != budget || !r.Backtracked {
		t.Fatalf("budget did not exhaust at the recorded total: %+v", r)
	}
}

// dispatchHarnessRaw is like dispatchHarness but exposes the raw JSON
// response body instead of reloading *gc.Coordinator: switch/handoff/
// abort_switch return a map (result, additional_context, extra_artifacts,
// memory), not the coordinator.
func dispatchHarnessRaw(t *testing.T) (string, string, func(coordinationRequest) (map[string]any, error)) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	call := func(r coordinationRequest) (map[string]any, error) {
		var out, errout bytes.Buffer
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		if e := runDispatch([]string{"--workspace", root, "--state", state, "--request", string(b)}, &out, &errout); e != nil {
			return nil, e
		}
		var resp map[string]any
		if out.Len() > 0 {
			if e := json.Unmarshal(out.Bytes(), &resp); e != nil {
				t.Fatalf("unmarshal response: %v", e)
			}
		}
		return resp, nil
	}
	return root, state, call
}

// seedMemoryIndex writes the memory session index `takt-ai memory record`
// keeps under a fresh HOME: which author recorded which entry in session.
func seedMemoryIndex(t *testing.T, session string, authors map[int64]string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	type entry struct {
		ID     int64  `json:"id"`
		Author string `json:"author"`
	}
	index := struct {
		Session string  `json:"session"`
		Entries []entry `json:"entries"`
	}{Session: session}
	for id, author := range authors {
		index.Entries = append(index.Entries, entry{ID: id, Author: author})
	}
	dir := filepath.Join(home, ".takt-ai", "memory", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, session+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDispatchInterlocutorHandoffArtifactGate requires existing result IDs,
// not an optional filesystem copy, before a standard handoff can finish.
func TestDispatchInterlocutorHandoffArtifactGate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health", "/observations/123":
			_, _ = w.Write([]byte(`{"id":123}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("ENGRAM_BASE_URL", server.URL)
	seedMemoryIndex(t, "root", map[int64]string{123: "pm", 124: "dev", 999: "pm"})
	root, _, call := dispatchHarnessRaw(t)
	if _, e := call(coordinationRequest{Action: "validate_results", Session: "root", Agent: "pm", ResultIDs: []int64{999}}); e == nil {
		t.Fatal("autonomous result validator accepted a nonexistent ID")
	}
	if _, e := call(coordinationRequest{Action: "validate_results", Session: "root", Agent: "pm", ResultIDs: []int64{124}}); e == nil || !strings.Contains(e.Error(), "not recorded by pm") {
		t.Fatalf("autonomous result validator accepted another author's entry: %v", e)
	}
	if _, e := call(coordinationRequest{Action: "validate_results", Session: "other", Agent: "pm", ResultIDs: []int64{123}}); e == nil {
		t.Fatal("autonomous result validator accepted an entry of another session")
	}
	if _, e := call(coordinationRequest{Action: "validate_results", Session: "root", Agent: "pm", ResultIDs: []int64{123}}); e != nil {
		t.Fatalf("autonomous result validator rejected an existing ID: %v", e)
	}
	const artifact = "PRD.md"
	if _, e := call(coordinationRequest{Action: "switch", Session: "root", Child: "child-1", Agent: "pm", Artifact: artifact}); e != nil {
		t.Fatalf("switch: %v", e)
	}
	if err := os.WriteFile(filepath.Join(root, artifact), []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	handoff := coordinationRequest{Action: "handoff", Session: "root", Child: "child-1", Agent: "pm", Result: "Standard"}
	if _, e := call(handoff); e == nil || !strings.Contains(e.Error(), "Engram ID") {
		t.Fatalf("handoff without a result ID was not denied: %v", e)
	}
	handoff.ResultIDs = []int64{999}
	if _, e := call(handoff); e == nil || !strings.Contains(e.Error(), "#999") {
		t.Fatalf("handoff with nonexistent ID was not denied: %v", e)
	}
	handoff.ResultIDs = []int64{124}
	if _, e := call(handoff); e == nil || !strings.Contains(e.Error(), "not recorded by pm") {
		t.Fatalf("handoff accepted another author's result: %v", e)
	}
	if err := os.Remove(filepath.Join(root, artifact)); err != nil {
		t.Fatal(err)
	}
	handoff.ResultIDs = []int64{123}
	resp, e := call(handoff)
	if e != nil {
		t.Fatalf("first handoff denied with a valid ID and no filesystem copy: %v", e)
	}
	if resp["artifact_verified"] != false {
		t.Fatalf("missing optional copy was not reported: %+v", resp)
	}
	if resp["result"] != "Standard" {
		t.Fatalf("handoff result missing: %+v", resp)
	}
	if ids, ok := resp["memory"].([]any); !ok || len(ids) != 1 || ids[0] != float64(123) {
		t.Fatalf("handoff response did not carry the specific result ID: %+v", resp)
	}
}

// TestDispatchInterlocutorAbortSwitchByUser covers IR-24: the user may end a
// temporary holder's turn without negotiation, after a prior switch.
func TestDispatchInterlocutorAbortSwitchByUser(t *testing.T) {
	seedMemoryIndex(t, "root", map[int64]string{41: "pm", 42: "takt"})
	_, _, call := dispatchHarnessRaw(t)
	if _, e := call(coordinationRequest{Action: "switch", Session: "root", Child: "child-2", Agent: "pm", Artifact: "PRD.md"}); e != nil {
		t.Fatalf("switch: %v", e)
	}
	resp, e := call(coordinationRequest{Action: "abort_switch", Session: "root", Child: "child-2", Evidence: "user cancelled the switch", Origin: "user"})
	if e != nil {
		t.Fatalf("abort_switch: %v", e)
	}
	if resp["result"] != "Aborted" {
		t.Fatalf("abort_switch result: %+v", resp)
	}
	// The envelope carries what the holder recorded, not the whole session's memory.
	if ids, ok := resp["memory"].([]any); !ok || len(ids) != 1 || ids[0] != float64(41) {
		t.Fatalf("abort_switch memory is not the holder's entries: %+v", resp)
	}
	// PR-HAR-24: the artifact is never required to abort, but its absence is
	// still verified and reported.
	if resp["artifact_verified"] != false {
		t.Fatalf("abort with no artifact on disk must report artifact_verified = false: %+v", resp)
	}
}

// Optional filesystem copies never gate a nonstandard handoff.
func TestDispatchInterlocutorHandoffOptionalCopy(t *testing.T) {
	for _, result := range []string{"EarlyHandoff", "TechFault", "Outraged"} {
		for _, present := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/present=%t", result, present), func(t *testing.T) {
				root, _, call := dispatchHarnessRaw(t)
				if present {
					if err := os.WriteFile(filepath.Join(root, "copy.md"), []byte("draft"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := call(coordinationRequest{Action: "switch", Session: "root", Child: "child", Agent: "pm", Artifact: "copy.md"}); err != nil {
					t.Fatal(err)
				}
				handoff := coordinationRequest{Action: "handoff", Session: "root", Child: "child", Agent: "pm", Result: result, ResultIDs: []int64{123}}
				if _, err := call(handoff); err == nil {
					t.Fatal("nonstandard handoff accepted an unrecorded result ID")
				}
				handoff.ResultIDs = nil
				resp, err := call(handoff)
				if err != nil {
					t.Fatalf("handoff without a result must succeed without a filesystem retry: %v", err)
				}
				if resp["artifact_verified"] != present || resp["result"] != result {
					t.Fatalf("unexpected handoff metadata: %+v", resp)
				}
			})
		}
	}
}

// scrubFakeEngramPath drops any PATH entry from the fake engram stub
// TestMain installs (os.MkdirTemp("", "takt-tui-engram-")), so a real-binary
// test in this package can force a genuine download instead of finding it.
func scrubFakeEngramPath(path string) string {
	entries := strings.Split(path, string(os.PathListSeparator))
	kept := entries[:0]
	for _, entry := range entries {
		if !strings.Contains(entry, "takt-tui-engram-") {
			kept = append(kept, entry)
		}
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// TestDispatchValidateResultsAgainstRealEngram is E2E-D: every other
// coverage of validate_results (TestDispatchInterlocutorHandoffArtifactGate
// above) fakes Engram with an httptest.Server. This one downloads the pinned
// release, runs a real `engram serve`, saves a real observation through its
// real HTTP API, and drives the real dispatch action against it — the one
// link the Wave 2 delivery rewrite depends on that no test exercised without
// a mock. It runs only inside the disposable test container
// (development/testing/test-containerized.sh): it never touches a host data directory.
func TestDispatchValidateResultsAgainstRealEngram(t *testing.T) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Skip("E2E-D runs only inside the test container")
	}
	t.Setenv("HOME", t.TempDir())
	// This package's TestMain (memory_test.go) puts a fake `engram` stub first
	// on PATH for every other test's sake; strip it here so Acquire's PATH
	// lookup can't shadow the real pinned release this test needs.
	t.Setenv("PATH", scrubFakeEngramPath(os.Getenv("PATH")))
	binary, err := engram.Acquire(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("acquire real engram: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	dataDir := t.TempDir()
	server := exec.Command(binary, "serve", strconv.Itoa(port))
	server.Env = append(os.Environ(), "ENGRAM_DATA_DIR="+dataDir)
	var serverOut bytes.Buffer
	server.Stdout = &serverOut
	server.Stderr = &serverOut
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- server.Wait() }()
	t.Cleanup(func() { _ = server.Process.Kill() })
	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.After(15 * time.Second)
waitLoop:
	for {
		select {
		case waitErr := <-exited:
			t.Fatalf("engram serve exited early (%v): %s", waitErr, serverOut.String())
		case <-deadline:
			t.Fatalf("engram serve never listened: %s", serverOut.String())
		case <-time.After(100 * time.Millisecond):
			if conn, dialErr := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); dialErr == nil {
				_ = conn.Close()
				break waitLoop
			}
		}
	}
	t.Setenv("ENGRAM_BASE_URL", baseURL)

	sessionBody, err := json.Marshal(map[string]any{
		"id": "e2e-d-session", "project": "fixture", "directory": "/tmp/e2e-d",
	})
	if err != nil {
		t.Fatal(err)
	}
	// An observation belongs to a session; the real server rejects a post
	// under a session id it hasn't seen yet.
	sessionResp, err := http.Post(baseURL+"/sessions", "application/json", bytes.NewReader(sessionBody))
	if err != nil {
		t.Fatalf("post real session: %v", err)
	}
	_ = sessionResp.Body.Close()

	body, err := json.Marshal(map[string]any{
		"session_id": "e2e-d-session",
		"type":       "decision",
		"title":      "real engram fixture",
		"content":    "written by TestDispatchValidateResultsAgainstRealEngram",
		"tool_name":  "test",
		"project":    "fixture",
		"scope":      "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(baseURL+"/observations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post real observation: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var created struct {
		ID int64 `json:"id"`
	}
	if e := json.NewDecoder(resp.Body).Decode(&created); e != nil || created.ID == 0 {
		t.Fatalf("real engram did not return an id: %v", e)
	}

	missing := created.ID + 1_000_000
	seedMemoryIndex(t, "root", map[int64]string{created.ID: "pm", missing: "pm"})
	_, _, call := dispatchHarnessRaw(t)
	if _, e := call(coordinationRequest{Action: "validate_results", Session: "root", Agent: "pm", ResultIDs: []int64{missing}}); e == nil {
		t.Fatal("real engram accepted a nonexistent ID")
	}
	if _, e := call(coordinationRequest{Action: "validate_results", Session: "root", Agent: "pm", ResultIDs: []int64{created.ID}}); e != nil {
		t.Fatalf("real engram rejected its own freshly-saved observation: %v", e)
	}
}
