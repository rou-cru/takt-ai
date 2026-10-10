package dispatch_test

import (
	"reflect"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
)

func TestAdmitSucceedsWithinBudget(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "unit-1", Session: "session-1", Agent: "some-agent", Dispatch: ""}); e != nil {
		t.Fatalf("expected admission to succeed, got %v", e)
	}
	entries := h.Entries()
	if len(entries) != 1 || entries[0].Kind != history.KindAdmitted {
		t.Fatalf("expected one admitted entry, got %+v", entries)
	}
}

func TestActivityLifecycleUsesSeparateIdentityAndProjection(t *testing.T) {
	h := newHistory(t)
	id := "direct-activity-31c8"
	if err := dispatch.StartActivity(h, "root", id, history.NodeKindOrchestrator); err != nil {
		t.Fatalf("start activity: %v", err)
	}
	if err := dispatch.StartActivity(h, "root", id, history.NodeKindOrchestrator); err != nil {
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
	if err := dispatch.FinishActivity(h, "root", id, history.NodeKindOrchestrator, history.OutcomeCompleted); err != nil {
		t.Fatalf("finish activity: %v", err)
	}
	if err := dispatch.FinishActivity(h, "root", id, history.NodeKindOrchestrator, history.OutcomeCompleted); err != nil {
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
	h := newHistory(t)
	p := loadPolicy(t)
	for i := 0; i < p.Concurrency.Specialists; i++ {
		unit := "unit-" + string(rune('a'+i))
		if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: unit, Session: "session-1", Agent: "agent", Dispatch: ""}); e != nil {
			t.Fatalf("expected admission %d to succeed, got %v", i, e)
		}
	}
	if dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "unit-over-ceiling", Session: "session-1", Agent: "agent", Dispatch: ""}) == nil {
		t.Fatal("expected admission past the concurrency ceiling to be denied")
	}
}

func TestCommitRejectsPrerequisiteCycle(t *testing.T) {
	h := newHistory(t)
	units := []dispatch.PlanUnit{
		{Unit: "a", Contract: "c", Prerequisites: []string{"b"}},
		{Unit: "b", Contract: "c", Prerequisites: []string{"a"}},
	}
	if dispatch.Commit(h, "", "session-1", "v1", units) == nil {
		t.Fatal("expected a prerequisite cycle to be rejected")
	}
}

func TestCommitAcceptsValidPlan(t *testing.T) {
	h := newHistory(t)
	units := []dispatch.PlanUnit{
		{Unit: "a", Contract: "c"},
		{Unit: "b", Contract: "c", Prerequisites: []string{"a"}},
	}
	if e := dispatch.Commit(h, "", "session-1", "v1", units); e != nil {
		t.Fatalf("expected a valid plan to commit, got %v", e)
	}
	if len(h.Entries()) != 2 {
		t.Fatalf("expected 2 planned entries, got %d", len(h.Entries()))
	}
}

func TestReviseTacticalAddsUnconnectedUnit(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Commit(h, "", "session-1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := dispatch.Revise(h, "", "session-1", "v1", "v2", []dispatch.PlanUnit{{Unit: "d", Contract: "c"}}, nil); e != nil {
		t.Fatalf("expected a tactical revision to succeed, got %v", e)
	}
	entries := h.Entries()
	last := entries[len(entries)-2]
	if last.Kind != history.KindRevised || last.Classification != history.ClassificationTactical {
		t.Fatalf("expected a tactical KindRevised entry, got %+v", last)
	}
	if p := h.Project(); p.Units[history.UnitKey("session-1", "d")].State != history.StatePlanned {
		t.Fatalf("expected d to be planned, got %+v", p.Units[history.UnitKey("session-1", "d")])
	}
}

func TestReviseStrategicReconnectsRetainedUnit(t *testing.T) {
	h := newHistory(t)
	units := []dispatch.PlanUnit{
		{Unit: "a", Contract: "c"},
		{Unit: "c", Contract: "c"},
		{Unit: "b", Contract: "c", Prerequisites: []string{"a"}},
	}
	if e := dispatch.Commit(h, "", "session-1", "v1", units); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := dispatch.Revise(h, "", "session-1", "v1", "v2", []dispatch.PlanUnit{{Unit: "b", Contract: "c", Prerequisites: []string{"c"}}}, nil); e != nil {
		t.Fatalf("expected a strategic revision to succeed, got %v", e)
	}
	entries := h.Entries()
	revised := entries[len(entries)-2]
	if revised.Kind != history.KindRevised || revised.Classification != history.ClassificationStrategic {
		t.Fatalf("expected a strategic KindRevised entry, got %+v", revised)
	}
	if p := h.Project(); !reflect.DeepEqual(p.Units[history.UnitKey("session-1", "b")].Prerequisites, []string{"c"}) {
		t.Fatalf("expected b's prerequisites reconnected to c, got %+v", p.Units[history.UnitKey("session-1", "b")])
	}
}

func TestReviseRejectsStaleBaseVersion(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Commit(h, "", "session-1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if dispatch.Revise(h, "", "session-1", "stale", "v2", []dispatch.PlanUnit{{Unit: "d", Contract: "c"}}, nil) == nil {
		t.Fatal("expected a stale base version to be rejected")
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindInvalidRevision || last.Reason == "" {
		t.Fatalf("expected a recorded KindInvalidRevision with a reason, got %+v", last)
	}
	p := h.Project()
	if _, known := p.Units[history.UnitKey("session-1", "d")]; known {
		t.Fatal("stale revision must not add the declared unit")
	}
	if p.Units[history.UnitKey("session-1", "a")].State != history.StatePlanned {
		t.Fatalf("expected the last valid plan unchanged, got %+v", p.Units[history.UnitKey("session-1", "a")])
	}
}

func TestReviseRejectsCycle(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Commit(h, "", "session-1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	adds := []dispatch.PlanUnit{
		{Unit: "x", Contract: "c", Prerequisites: []string{"y"}},
		{Unit: "y", Contract: "c", Prerequisites: []string{"x"}},
	}
	if dispatch.Revise(h, "", "session-1", "v1", "v2", adds, nil) == nil {
		t.Fatal("expected a cyclic revision to be rejected")
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindInvalidRevision {
		t.Fatalf("expected a recorded KindInvalidRevision, got %+v", last)
	}
	p := h.Project()
	if _, known := p.Units[history.UnitKey("session-1", "x")]; known {
		t.Fatal("cyclic revision must not add its declared units")
	}
	if got := p.Budgets("session-1").PlanVersion; got != "v1" {
		t.Fatalf("tracked plan version %q; want unchanged v1", got)
	}
}

func TestReviseRejectsAlteringAnAdmittedUnit(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if e := dispatch.Commit(h, "", "session-1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "session-1", Agent: "agent", Dispatch: ""}); e != nil {
		t.Fatalf("admit: %v", e)
	}
	if dispatch.Revise(h, "", "session-1", "v1", "v2", []dispatch.PlanUnit{{Unit: "a", Contract: "c2"}}, nil) == nil {
		t.Fatal("expected a revision altering an admitted unit's contract to be rejected")
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindInvalidRevision {
		t.Fatalf("expected a recorded KindInvalidRevision, got %+v", last)
	}
	if got := h.Project().Units[history.UnitKey("session-1", "a")].Contract; got != "c" {
		t.Fatalf("admitted unit's contract changed to %q; want unchanged c", got)
	}
}

func TestReviseWithdrawalWithoutReconnectionPreservesReference(t *testing.T) {
	h := newHistory(t)
	units := []dispatch.PlanUnit{
		{Unit: "a", Contract: "c"},
		{Unit: "b", Contract: "c", Prerequisites: []string{"a"}},
	}
	if e := dispatch.Commit(h, "", "session-1", "v1", units); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := dispatch.Revise(h, "", "session-1", "v1", "v2", nil, []string{"a"}); e != nil {
		t.Fatalf("expected withdrawal without reconnection to succeed, got %v", e)
	}
	p := h.Project()
	if p.Units[history.UnitKey("session-1", "a")].State != history.StateWithdrawn {
		t.Fatalf("expected a withdrawn, got %+v", p.Units[history.UnitKey("session-1", "a")])
	}
	if !reflect.DeepEqual(p.Units[history.UnitKey("session-1", "b")].Prerequisites, []string{"a"}) {
		t.Fatalf("expected b's unresolved reference to a preserved, got %+v", p.Units[history.UnitKey("session-1", "b")])
	}
	if p.Units[history.UnitKey("session-1", "b")].State != history.StatePlanned {
		t.Fatalf("expected b to remain planned, got %+v", p.Units[history.UnitKey("session-1", "b")])
	}
}

func TestPlanVersionTracksCommitThenRevisions(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Commit(h, "", "session-1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if got := h.Project().Budgets("session-1").PlanVersion; got != "v1" {
		t.Fatalf("plan version %q after commit; want v1", got)
	}
	if e := dispatch.Revise(h, "", "session-1", "v1", "v2", []dispatch.PlanUnit{{Unit: "d", Contract: "c"}}, nil); e != nil {
		t.Fatalf("revise to v2: %v", e)
	}
	if got := h.Project().Budgets("session-1").PlanVersion; got != "v2" {
		t.Fatalf("plan version %q after first revision; want v2", got)
	}
	if e := dispatch.Revise(h, "", "session-1", "v2", "v3", []dispatch.PlanUnit{{Unit: "e", Contract: "c"}}, nil); e != nil {
		t.Fatalf("revise to v3: %v", e)
	}
	if got := h.Project().Budgets("session-1").PlanVersion; got != "v3" {
		t.Fatalf("plan version %q after second revision; want v3", got)
	}
}

// settleWithoutResult admits a unit and ends its attempt without a result.
func settleWithoutResult(t *testing.T, h *history.History, p dispatch.AdmissionPolicy, unit string) {
	t.Helper()
	if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: unit, Session: "session-1", Agent: "agent", Dispatch: "d-" + unit}); e != nil {
		t.Fatalf("admit %s: %v", unit, e)
	}
	if e := dispatch.Record(h, "", unit, "session-1", history.KindCancelRequested); e != nil {
		t.Fatalf("cancel %s: %v", unit, e)
	}
	if e := dispatch.Finish(h, "", unit, "session-1"); e != nil {
		t.Fatalf("finish %s: %v", unit, e)
	}
}

func TestContestAllowanceIsExhaustible(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	for i := 0; i <= p.Budgets.Contests; i++ {
		settleWithoutResult(t, h, p, "unit-"+string(rune('a'+i)))
	}
	for i := 0; i < p.Budgets.Contests; i++ {
		unit := "unit-" + string(rune('a'+i))
		if e := dispatch.Contest(h, p, "", unit, "session-1", history.FirstAttempt); e != nil {
			t.Fatalf("expected contest %d to be admitted, got %v", i, e)
		}
	}
	over := "unit-" + string(rune('a'+p.Budgets.Contests))
	if dispatch.Contest(h, p, "", over, "session-1", history.FirstAttempt) == nil {
		t.Fatal("expected the contest allowance to be exhausted")
	}
}

// A completed attempt has nothing to contest: the request is refused and
// recorded, and it spends none of the allowance.
func TestContestRefusesAttemptThatCompleted(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "impl", Session: "session-1", Agent: "dev", Dispatch: "d-1"}); e != nil {
		t.Fatalf("admit: %v", e)
	}
	if e := dispatch.Finish(h, "", "impl", "session-1"); e != nil {
		t.Fatalf("finish: %v", e)
	}
	if dispatch.Contest(h, p, "", "impl", "session-1", history.FirstAttempt) == nil {
		t.Fatal("expected the contest of a completed attempt to be refused")
	}
	last := h.Entries()[len(h.Entries())-1]
	if last.Kind != history.KindDenied || last.Cause != dispatch.CauseNoFailure {
		t.Fatalf("expected a recorded denial for a missing failure, got %+v", last)
	}
	if got := len(h.Project().Budgets("session-1").Contests); got != 0 {
		t.Fatalf("a refused contest spent %d of the allowance", got)
	}
}

// A delegation named after a committed unit is covered: past the uncovered
// bound it is still admitted, and it adds nothing to the uncovered count.
func TestAdmitPlannedUnitIsCoveredPastUnplannedBound(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	for i := 0; i < p.Budgets.UnplannedUnits; i++ {
		unit := "loose-" + string(rune('a'+i))
		if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: unit, Session: "session-1", Agent: "agent", Dispatch: "d-" + unit}); e != nil {
			t.Fatalf("admit %s: %v", unit, e)
		}
		if e := dispatch.Finish(h, "", unit, "session-1"); e != nil {
			t.Fatalf("finish %s: %v", unit, e)
		}
	}
	if dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "uncovered", Session: "session-1", Agent: "agent", Dispatch: "d-uncovered"}) == nil {
		t.Fatal("expected uncovered work past the bound to be denied")
	}
	if e := dispatch.Commit(h, "", "session-1", "v1", []dispatch.PlanUnit{{Unit: "verify-feature", Contract: "judge the delta"}}); e != nil {
		t.Fatalf("commit: %v", e)
	}
	if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "verify-feature", Session: "session-1", Agent: "verify", Dispatch: "d-verify"}); e != nil {
		t.Fatalf("expected the planned unit to be admitted, got %v", e)
	}
	if got := h.Project().Budgets("session-1").Unplanned; got != p.Budgets.UnplannedUnits {
		t.Fatalf("unplanned count = %d, want %d", got, p.Budgets.UnplannedUnits)
	}
}

// Delegating a settled unit again retries it: the same unit, its next
// attempt, and no new uncovered unit (PR-DAG-MUT-8, PR-HAR-19).
func TestAdmitRetriesSettledUnitAsNextAttempt(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "impl", Session: "session-1", Agent: "dev", Dispatch: "d-1"}); e != nil {
		t.Fatalf("first admit: %v", e)
	}
	if e := dispatch.Finish(h, "", "impl", "session-1"); e != nil {
		t.Fatalf("finish: %v", e)
	}
	if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "impl", Session: "session-1", Agent: "dev", Dispatch: "d-2"}); e != nil {
		t.Fatalf("retry admit: %v", e)
	}
	projection := h.Project()
	u := projection.Units[history.UnitKey("session-1", "impl")]
	if u.State != history.StateInFlight || u.AttemptID != "2" || u.Dispatch != "d-2" || u.Launched {
		t.Fatalf("retry projection = %+v, want attempt 2 in flight from d-2", u)
	}
	if got := projection.Budgets("session-1").Unplanned; got != 1 {
		t.Fatalf("unplanned count = %d, want 1", got)
	}
	if e := dispatch.Finish(h, "", "impl", "session-1"); e != nil {
		t.Fatalf("finish retry: %v", e)
	}
	if last := h.Entries()[len(h.Entries())-1]; last.AttemptID != "2" {
		t.Fatalf("termination recorded against attempt %q, want 2", last.AttemptID)
	}
}

// A second delegation of a unit whose attempt is still in flight would
// duplicate execution, so it is denied and recorded (PR-HAR-17).
func TestAdmitDeniesUnitAlreadyInFlight(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if e := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "impl", Session: "session-1", Agent: "dev", Dispatch: "d-1"}); e != nil {
		t.Fatalf("first admit: %v", e)
	}
	if dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "impl", Session: "session-1", Agent: "dev", Dispatch: "d-2"}) == nil {
		t.Fatal("expected a duplicate delegation of an in-flight unit to be denied")
	}
	last := h.Entries()[len(h.Entries())-1]
	if last.Kind != history.KindDenied || last.Cause != dispatch.CauseRepetition {
		t.Fatalf("expected a recorded repetition denial, got %+v", last)
	}
	if u := h.Project().Units[history.UnitKey("session-1", "impl")]; u.Dispatch != "d-1" || u.AttemptID != history.FirstAttempt {
		t.Fatalf("the running attempt changed: %+v", u)
	}
}

func TestDeclareCommitsFirstThenOnlyRevisesAgainstTheStandingVersion(t *testing.T) {
	h := newHistory(t)
	if dispatch.Declare(h, "", "session-1", "v1", "v0", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}, nil) == nil {
		t.Fatal("a revision with no standing plan must be refused")
	}
	if e := dispatch.Declare(h, "", "session-1", "v1", "", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}, nil); e != nil {
		t.Fatalf("first declaration must commit: %v", e)
	}
	before := len(h.Entries())
	if dispatch.Declare(h, "", "session-1", "v2", "", []dispatch.PlanUnit{{Unit: "d", Contract: "c"}}, nil) == nil {
		t.Fatal("a second declaration without its base version must be refused")
	}
	if len(h.Entries()) != before {
		t.Fatal("a refused silent rewrite must record nothing")
	}
	if dispatch.Declare(h, "", "session-1", "v2", "stale", []dispatch.PlanUnit{{Unit: "d", Contract: "c"}}, nil) == nil {
		t.Fatal("a stale base version must be refused")
	}
	if last := h.Entries()[len(h.Entries())-1]; last.Kind != history.KindInvalidRevision {
		t.Fatalf("a stale revision is recorded as invalid, got %+v", last)
	}
	if e := dispatch.Declare(h, "", "session-1", "v2", "v1", []dispatch.PlanUnit{{Unit: "d", Contract: "c"}}, []string{"a"}); e != nil {
		t.Fatalf("a revision against the standing version must apply: %v", e)
	}
	p := h.Project()
	if p.Budgets("session-1").PlanVersion != "v2" || p.Units[history.UnitKey("session-1", "d")].State != history.StatePlanned {
		t.Fatalf("revision not applied: version %q, d %+v", p.Budgets("session-1").PlanVersion, p.Units[history.UnitKey("session-1", "d")])
	}
}
