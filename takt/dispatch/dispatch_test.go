package dispatch

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

func openHistory(t *testing.T) *history.History {
	t.Helper()
	h, e := history.Open(filepath.Join(t.TempDir(), "state"))
	if e != nil {
		t.Fatalf("open history: %v", e)
	}
	t.Cleanup(func() {
		if e := h.Close(); e != nil {
			t.Fatalf("close history: %v", e)
		}
	})
	return h
}

func TestAdmitHeldDeniesRegardlessOfBudget(t *testing.T) {
	h := openHistory(t)
	p, e := LoadAdmissionPolicy()
	if e != nil {
		t.Fatalf("load policy: %v", e)
	}
	if e := Admit(h, p, "", "unit-1", "session-1", "some-agent", "", true); e == nil {
		t.Fatal("expected the admission barrier to deny admission")
	}
	entries := h.Entries()
	if len(entries) != 1 || entries[0].Kind != history.KindDenied || entries[0].Cause != CauseHeld {
		t.Fatalf("expected one denied entry with cause %q, got %+v", CauseHeld, entries)
	}
}

func TestAdmitSucceedsWithinBudget(t *testing.T) {
	h := openHistory(t)
	p, e := LoadAdmissionPolicy()
	if e != nil {
		t.Fatalf("load policy: %v", e)
	}
	if e := Admit(h, p, "", "unit-1", "session-1", "some-agent", "", false); e != nil {
		t.Fatalf("expected admission to succeed, got %v", e)
	}
	entries := h.Entries()
	if len(entries) != 1 || entries[0].Kind != history.KindAdmitted {
		t.Fatalf("expected one admitted entry, got %+v", entries)
	}
}

func TestActivityLifecycleUsesSeparateIdentityAndProjection(t *testing.T) {
	h := openHistory(t)
	id := "direct-activity-31c8"
	if err := StartActivity(h, "root", id, history.NodeKindOrchestrator); err != nil {
		t.Fatalf("start activity: %v", err)
	}
	if err := StartActivity(h, "root", id, history.NodeKindOrchestrator); err != nil {
		t.Fatalf("repeated start was not idempotent: %v", err)
	}
	p := h.Project()
	if len(p.Units) != 0 || len(p.Activities) != 1 || p.Activities[id].State != history.StateInFlight {
		t.Fatalf("activity was not separate from units: %+v", p)
	}
	entry := h.Entries()[0]
	if entry.ActivityID != id || entry.WorkUnitID != "" || entry.AttemptID != "" {
		t.Fatalf("activity record identities %+v; work identity must remain empty", entry)
	}
	if err := FinishActivity(h, "root", id, history.NodeKindOrchestrator, history.OutcomeCompleted); err != nil {
		t.Fatalf("finish activity: %v", err)
	}
	if err := FinishActivity(h, "root", id, history.NodeKindOrchestrator, history.OutcomeCompleted); err != nil {
		t.Fatalf("repeated finish was not idempotent: %v", err)
	}
	if activity := h.Project().Activities[id]; activity.State != history.StateSettled || activity.Outcome != history.OutcomeCompleted {
		t.Fatalf("finished activity %+v", activity)
	}
	for _, e := range h.Entries() {
		if e.WorkUnitID != "" || e.AttemptID != "" {
			t.Fatalf("activity entry fabricated work identity: %+v", e)
		}
	}
}

func TestAdmitDeniesPastConcurrencyCeiling(t *testing.T) {
	h := openHistory(t)
	p, e := LoadAdmissionPolicy()
	if e != nil {
		t.Fatalf("load policy: %v", e)
	}
	for i := 0; i < p.Concurrency.Specialists; i++ {
		unit := "unit-" + string(rune('a'+i))
		if e := Admit(h, p, "", unit, "session-1", "agent", "", false); e != nil {
			t.Fatalf("expected admission %d to succeed, got %v", i, e)
		}
	}
	if e := Admit(h, p, "", "unit-over-ceiling", "session-1", "agent", "", false); e == nil {
		t.Fatal("expected admission past the concurrency ceiling to be denied")
	}
}

func TestCommitRejectsPrerequisiteCycle(t *testing.T) {
	h := openHistory(t)
	units := []PlanUnit{
		{Unit: "a", Contract: "c", Prerequisites: []string{"b"}},
		{Unit: "b", Contract: "c", Prerequisites: []string{"a"}},
	}
	if e := Commit(h, "", "session-1", "v1", units); e == nil {
		t.Fatal("expected a prerequisite cycle to be rejected")
	}
}

func TestCommitAcceptsValidPlan(t *testing.T) {
	h := openHistory(t)
	units := []PlanUnit{
		{Unit: "a", Contract: "c"},
		{Unit: "b", Contract: "c", Prerequisites: []string{"a"}},
	}
	if e := Commit(h, "", "session-1", "v1", units); e != nil {
		t.Fatalf("expected a valid plan to commit, got %v", e)
	}
	if len(h.Entries()) != 2 {
		t.Fatalf("expected 2 planned entries, got %d", len(h.Entries()))
	}
}

func TestReviseTacticalAddsUnconnectedUnit(t *testing.T) {
	h := openHistory(t)
	if e := Commit(h, "", "session-1", "v1", []PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := Revise(h, "", "session-1", "v1", "v2", []PlanUnit{{Unit: "d", Contract: "c"}}, nil); e != nil {
		t.Fatalf("expected a tactical revision to succeed, got %v", e)
	}
	entries := h.Entries()
	last := entries[len(entries)-2]
	if last.Kind != history.KindRevised || last.Classification != history.ClassificationTactical {
		t.Fatalf("expected a tactical KindRevised entry, got %+v", last)
	}
	if p := h.Project(); p.Units["d"].State != history.StatePlanned {
		t.Fatalf("expected d to be planned, got %+v", p.Units["d"])
	}
}

func TestReviseStrategicReconnectsRetainedUnit(t *testing.T) {
	h := openHistory(t)
	units := []PlanUnit{
		{Unit: "a", Contract: "c"},
		{Unit: "c", Contract: "c"},
		{Unit: "b", Contract: "c", Prerequisites: []string{"a"}},
	}
	if e := Commit(h, "", "session-1", "v1", units); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := Revise(h, "", "session-1", "v1", "v2", []PlanUnit{{Unit: "b", Contract: "c", Prerequisites: []string{"c"}}}, nil); e != nil {
		t.Fatalf("expected a strategic revision to succeed, got %v", e)
	}
	entries := h.Entries()
	revised := entries[len(entries)-2]
	if revised.Kind != history.KindRevised || revised.Classification != history.ClassificationStrategic {
		t.Fatalf("expected a strategic KindRevised entry, got %+v", revised)
	}
	if p := h.Project(); !reflect.DeepEqual(p.Units["b"].Prerequisites, []string{"c"}) {
		t.Fatalf("expected b's prerequisites reconnected to c, got %+v", p.Units["b"])
	}
}

func TestReviseRejectsStaleBaseVersion(t *testing.T) {
	h := openHistory(t)
	if e := Commit(h, "", "session-1", "v1", []PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := Revise(h, "", "session-1", "stale", "v2", []PlanUnit{{Unit: "d", Contract: "c"}}, nil); e == nil {
		t.Fatal("expected a stale base version to be rejected")
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindInvalidRevision || last.Reason == "" {
		t.Fatalf("expected a recorded KindInvalidRevision with a reason, got %+v", last)
	}
	p := h.Project()
	if _, known := p.Units["d"]; known {
		t.Fatal("stale revision must not add the declared unit")
	}
	if p.Units["a"].State != history.StatePlanned {
		t.Fatalf("expected the last valid plan unchanged, got %+v", p.Units["a"])
	}
}

func TestReviseRejectsCycle(t *testing.T) {
	h := openHistory(t)
	if e := Commit(h, "", "session-1", "v1", []PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	adds := []PlanUnit{
		{Unit: "x", Contract: "c", Prerequisites: []string{"y"}},
		{Unit: "y", Contract: "c", Prerequisites: []string{"x"}},
	}
	if e := Revise(h, "", "session-1", "v1", "v2", adds, nil); e == nil {
		t.Fatal("expected a cyclic revision to be rejected")
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindInvalidRevision {
		t.Fatalf("expected a recorded KindInvalidRevision, got %+v", last)
	}
	p := h.Project()
	if _, known := p.Units["x"]; known {
		t.Fatal("cyclic revision must not add its declared units")
	}
	if got := p.Budgets("session-1").PlanVersion; got != "v1" {
		t.Fatalf("tracked plan version %q; want unchanged v1", got)
	}
}

func TestReviseRejectsAlteringAnAdmittedUnit(t *testing.T) {
	h := openHistory(t)
	p, e := LoadAdmissionPolicy()
	if e != nil {
		t.Fatalf("load policy: %v", e)
	}
	if e := Commit(h, "", "session-1", "v1", []PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := Admit(h, p, "", "a", "session-1", "agent", "", false); e != nil {
		t.Fatalf("admit: %v", e)
	}
	if e := Revise(h, "", "session-1", "v1", "v2", []PlanUnit{{Unit: "a", Contract: "c2"}}, nil); e == nil {
		t.Fatal("expected a revision altering an admitted unit's contract to be rejected")
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindInvalidRevision {
		t.Fatalf("expected a recorded KindInvalidRevision, got %+v", last)
	}
	if got := h.Project().Units["a"].Contract; got != "c" {
		t.Fatalf("admitted unit's contract changed to %q; want unchanged c", got)
	}
}

func TestReviseWithdrawalWithoutReconnectionPreservesReference(t *testing.T) {
	h := openHistory(t)
	units := []PlanUnit{
		{Unit: "a", Contract: "c"},
		{Unit: "b", Contract: "c", Prerequisites: []string{"a"}},
	}
	if e := Commit(h, "", "session-1", "v1", units); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := Revise(h, "", "session-1", "v1", "v2", nil, []string{"a"}); e != nil {
		t.Fatalf("expected withdrawal without reconnection to succeed, got %v", e)
	}
	p := h.Project()
	if p.Units["a"].State != history.StateWithdrawn {
		t.Fatalf("expected a withdrawn, got %+v", p.Units["a"])
	}
	if !reflect.DeepEqual(p.Units["b"].Prerequisites, []string{"a"}) {
		t.Fatalf("expected b's unresolved reference to a preserved, got %+v", p.Units["b"])
	}
	if p.Units["b"].State != history.StatePlanned {
		t.Fatalf("expected b to remain planned, got %+v", p.Units["b"])
	}
}

func TestPlanVersionTracksCommitThenRevisions(t *testing.T) {
	h := openHistory(t)
	if e := Commit(h, "", "session-1", "v1", []PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if got := h.Project().Budgets("session-1").PlanVersion; got != "v1" {
		t.Fatalf("plan version %q after commit; want v1", got)
	}
	if e := Revise(h, "", "session-1", "v1", "v2", []PlanUnit{{Unit: "d", Contract: "c"}}, nil); e != nil {
		t.Fatalf("revise to v2: %v", e)
	}
	if got := h.Project().Budgets("session-1").PlanVersion; got != "v2" {
		t.Fatalf("plan version %q after first revision; want v2", got)
	}
	if e := Revise(h, "", "session-1", "v2", "v3", []PlanUnit{{Unit: "e", Contract: "c"}}, nil); e != nil {
		t.Fatalf("revise to v3: %v", e)
	}
	if got := h.Project().Budgets("session-1").PlanVersion; got != "v3" {
		t.Fatalf("plan version %q after second revision; want v3", got)
	}
}

func TestContestAllowanceIsExhaustible(t *testing.T) {
	h := openHistory(t)
	p, e := LoadAdmissionPolicy()
	if e != nil {
		t.Fatalf("load policy: %v", e)
	}
	for i := 0; i < p.Budgets.Contests; i++ {
		unit := "unit-" + string(rune('a'+i))
		if e := Contest(h, p, "", unit, "session-1", history.FirstAttempt); e != nil {
			t.Fatalf("expected contest %d to be admitted, got %v", i, e)
		}
	}
	if e := Contest(h, p, "", "unit-over-ceiling", "session-1", history.FirstAttempt); e == nil {
		t.Fatal("expected the contest allowance to be exhausted")
	}
}

// A delegation named after a committed unit is covered: past the uncovered
// bound it is still admitted, and it adds nothing to the uncovered count.
func TestAdmitPlannedUnitIsCoveredPastUnplannedBound(t *testing.T) {
	h := openHistory(t)
	p, e := LoadAdmissionPolicy()
	if e != nil {
		t.Fatalf("load policy: %v", e)
	}
	for i := 0; i < p.Budgets.UnplannedUnits; i++ {
		unit := "loose-" + string(rune('a'+i))
		if e := Admit(h, p, "", unit, "session-1", "agent", "d-"+unit, false); e != nil {
			t.Fatalf("admit %s: %v", unit, e)
		}
		if e := Finish(h, "", unit, "session-1"); e != nil {
			t.Fatalf("finish %s: %v", unit, e)
		}
	}
	if e := Admit(h, p, "", "uncovered", "session-1", "agent", "d-uncovered", false); e == nil {
		t.Fatal("expected uncovered work past the bound to be denied")
	}
	if e := Commit(h, "", "session-1", "v1", []PlanUnit{{Unit: "verify-feature", Contract: "judge the delta"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := Admit(h, p, "", "verify-feature", "session-1", "verify", "d-verify", false); e != nil {
		t.Fatalf("expected the planned unit to be admitted, got %v", e)
	}
	if got := h.Project().Budgets("session-1").Unplanned; got != p.Budgets.UnplannedUnits {
		t.Fatalf("unplanned count = %d, want %d", got, p.Budgets.UnplannedUnits)
	}
}

// Delegating a settled unit again retries it: the same unit, its next
// attempt, and no new uncovered unit (PR-DAG-MUT-8, PR-HAR-19).
func TestAdmitRetriesSettledUnitAsNextAttempt(t *testing.T) {
	h := openHistory(t)
	p, e := LoadAdmissionPolicy()
	if e != nil {
		t.Fatalf("load policy: %v", e)
	}
	if e := Admit(h, p, "", "impl", "session-1", "dev", "d-1", false); e != nil {
		t.Fatalf("first admit: %v", e)
	}
	if e := Finish(h, "", "impl", "session-1"); e != nil {
		t.Fatalf("finish: %v", e)
	}
	if e := Admit(h, p, "", "impl", "session-1", "dev", "d-2", false); e != nil {
		t.Fatalf("retry admit: %v", e)
	}
	projection := h.Project()
	u := projection.Units["impl"]
	if u.State != history.StateInFlight || u.AttemptID != "2" || u.Dispatch != "d-2" || u.Launched {
		t.Fatalf("retry projection = %+v, want attempt 2 in flight from d-2", u)
	}
	if got := projection.Budgets("session-1").Unplanned; got != 1 {
		t.Fatalf("unplanned count = %d, want 1", got)
	}
	if e := Finish(h, "", "impl", "session-1"); e != nil {
		t.Fatalf("finish retry: %v", e)
	}
	if last := h.Entries()[len(h.Entries())-1]; last.AttemptID != "2" {
		t.Fatalf("termination recorded against attempt %q, want 2", last.AttemptID)
	}
}

// A second delegation of a unit whose attempt is still in flight would
// duplicate execution, so it is denied and recorded (PR-HAR-17).
func TestAdmitDeniesUnitAlreadyInFlight(t *testing.T) {
	h := openHistory(t)
	p, e := LoadAdmissionPolicy()
	if e != nil {
		t.Fatalf("load policy: %v", e)
	}
	if e := Admit(h, p, "", "impl", "session-1", "dev", "d-1", false); e != nil {
		t.Fatalf("first admit: %v", e)
	}
	if e := Admit(h, p, "", "impl", "session-1", "dev", "d-2", false); e == nil {
		t.Fatal("expected a duplicate delegation of an in-flight unit to be denied")
	}
	last := h.Entries()[len(h.Entries())-1]
	if last.Kind != history.KindDenied || last.Cause != CauseRepetition {
		t.Fatalf("expected a recorded repetition denial, got %+v", last)
	}
	if u := h.Project().Units["impl"]; u.Dispatch != "d-1" || u.AttemptID != history.FirstAttempt {
		t.Fatalf("the running attempt changed: %+v", u)
	}
}
