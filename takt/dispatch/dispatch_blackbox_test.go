package dispatch_test

import (
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func newHistory(t *testing.T) *history.History {
	t.Helper()
	h, err := history.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("history.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Fatalf("h.Close() error = %v", err)
		}
	})
	return h
}

func loadPolicy(t *testing.T) dispatch.AdmissionPolicy {
	t.Helper()
	p, err := dispatch.LoadAdmissionPolicy()
	if err != nil {
		t.Fatalf("LoadAdmissionPolicy() error = %v", err)
	}
	return p
}

func TestRecordAppendsRawKind(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.Record(h, "", "a", "s1", history.KindSuspended); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindSuspended || last.WorkUnitID != "a" {
		t.Errorf("Record() last entry = %+v, want Kind=%q WorkUnitID=a", last, history.KindSuspended)
	}
}

func TestLaunchIsIdempotentOnceObserved(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.Launch(h, "", "a", "s1"); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !h.Project().Units[history.UnitKey("s1", "a")].Launched {
		t.Fatal("Launch() did not mark the unit launched")
	}
	before := len(h.Entries())
	if err := dispatch.Launch(h, "", "a", "s1"); err != nil {
		t.Fatalf("second Launch() error = %v, want nil (idempotent)", err)
	}
	if len(h.Entries()) != before {
		t.Errorf("second Launch() appended %d new entries, want 0 (idempotent)", len(h.Entries())-before)
	}
}

func TestLaunchRequiresAdmittedUnit(t *testing.T) {
	h := newHistory(t)
	if err := dispatch.Launch(h, "", "never-admitted", "s1"); err == nil {
		t.Fatal("Launch() on a never-admitted unit error = nil, want an error")
	}
}

func TestReconcileRunningRecordsLaunch(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.Record(h, "", "a", "s1", history.KindUncertain); err != nil {
		t.Fatalf("Record(KindUncertain) error = %v", err)
	}
	if err := dispatch.Reconcile(h, "", "a", "s1", true); err != nil {
		t.Fatalf("Reconcile(running=true) error = %v", err)
	}
	unit := h.Project().Units[history.UnitKey("s1", "a")]
	if !unit.Launched || unit.Flight != history.FlightRunning {
		t.Errorf("Reconcile(running=true) left unit = %+v, want launched and running", unit)
	}
}

func TestReconcileNotRunningFinishesAsInterrupted(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.Record(h, "", "a", "s1", history.KindUncertain); err != nil {
		t.Fatalf("Record(KindUncertain) error = %v", err)
	}
	if err := dispatch.Reconcile(h, "", "a", "s1", false); err != nil {
		t.Fatalf("Reconcile(running=false) error = %v", err)
	}
	unit := h.Project().Units[history.UnitKey("s1", "a")]
	if unit.State != history.StateSettled || unit.Outcome != history.OutcomeInterrupted {
		t.Errorf("Reconcile(running=false) left unit = %+v, want settled/interrupted", unit)
	}
	if err := dispatch.Record(h, "", "a", "s1", history.KindUncertain); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Reconcile(h, "", "a", "s1", false); err != nil {
		t.Fatalf("repeated recovery of settled delegation = %v", err)
	}
	// Another root's unit of the same name is not this one: nothing to do,
	// and this session's unit is untouched.
	recorded := len(h.Entries())
	if err := dispatch.Reconcile(h, "", "a", "other", false); err != nil {
		t.Fatalf("another session's Reconcile() of a name it never admitted = %v, want nothing to do", err)
	}
	if len(h.Entries()) != recorded {
		t.Fatal("another session's reconcile recorded against this session's unit")
	}
}

func TestReconcileNotRunningHasNothingToDoForUnitsNotInFlight(t *testing.T) {
	h := newHistory(t)
	if err := dispatch.Commit(h, "", "s1", "v1", []dispatch.PlanUnit{{Unit: "planned", Contract: "c"}}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	for name, event := range map[string]string{"unknown": "never-admitted", "planned": "planned"} {
		if err := dispatch.Reconcile(h, "", event, "s1", false); err != nil {
			t.Errorf("%s unit: Reconcile(running=false) = %v, want nothing to do", name, err)
		}
	}
	recorded := len(h.Entries())
	if err := dispatch.Reconcile(h, "", "planned", "other", false); err != nil || len(h.Entries()) != recorded {
		t.Errorf("another session's Reconcile() of a same-named planned unit = %v, recorded %d entries; want nothing done", err, len(h.Entries())-recorded)
	}
	if err := dispatch.Reconcile(h, "", "never-admitted", "s1", true); err == nil {
		t.Error("Reconcile(running=true) of a unit that was never admitted = nil, want an error")
	}
}

func TestReconcileRequiresUncertainFlight(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.Reconcile(h, "", "a", "s1", true); err == nil {
		t.Fatal("Reconcile() on a unit that is not uncertain error = nil, want an error")
	}
}

func TestWithdrawStillPlannedUnit(t *testing.T) {
	h := newHistory(t)
	if err := dispatch.Commit(h, "", "s1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := dispatch.Withdraw(h, "", "a", "s1"); err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if h.Project().Units[history.UnitKey("s1", "a")].State != history.StateWithdrawn {
		t.Errorf("Withdraw() left state = %v, want withdrawn", h.Project().Units[history.UnitKey("s1", "a")].State)
	}
}

func TestWithdrawRejectsAdmittedUnit(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Commit(h, "", "s1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.Withdraw(h, "", "a", "s1"); err == nil {
		t.Fatal("Withdraw() on an admitted unit error = nil, want an error")
	}
}

func recoveryDeclaration(objective string) dispatch.RecoveryDeclaration {
	return dispatch.RecoveryDeclaration{Objective: objective, Result: "the fix works", Point: "before-edit", Scope: []string{"a"}, Actions: 2, Attempts: 2}
}

func TestDeclareRecoveryAndCloseRecovery(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Commit(h, "", "s1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}

	if err := dispatch.DeclareRecovery(h, p, "", "recover-a", "s1", recoveryDeclaration("obj1")); err != nil {
		t.Fatalf("DeclareRecovery() error = %v", err)
	}
	if !h.Project().Budgets("s1").Recoveries["obj1"].Open {
		t.Fatal("DeclareRecovery() did not open the recovery")
	}

	before := len(h.Entries())
	if err := dispatch.DeclareRecovery(h, p, "", "recover-a", "s1", recoveryDeclaration("obj1")); err != nil {
		t.Fatalf("redeclaring an already-open recovery error = %v, want nil", err)
	}
	if len(h.Entries()) != before {
		t.Errorf("redeclaring an already-open recovery appended entries, want none")
	}

	if err := dispatch.CloseRecovery(h, "", "recover-a", "s1", "obj1", "", false); err != nil {
		t.Fatalf("CloseRecovery(demonstrated=false) error = %v", err)
	}
	closed := h.Project().Budgets("s1").Recoveries["obj1"]
	if closed.Open || closed.Failures != 1 {
		t.Errorf("CloseRecovery(demonstrated=false) = %+v, want closed with Failures=1", closed)
	}

	if err := dispatch.CloseRecovery(h, "", "recover-a", "s1", "obj1", "", false); err == nil {
		t.Fatal("CloseRecovery() on an already-closed objective error = nil, want an error")
	}
}

func TestDeclareRecoveryRejectsIncompleteDeclaration(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	incomplete := dispatch.RecoveryDeclaration{Objective: "obj1"} // missing Result, Point, Scope, budgets
	if err := dispatch.DeclareRecovery(h, p, "", "recover-a", "s1", incomplete); err == nil {
		t.Fatal("DeclareRecovery() with an incomplete declaration error = nil, want an error")
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindDenied {
		t.Errorf("DeclareRecovery() with an incomplete declaration last entry = %+v, want a denial", last)
	}
}

func TestCloseRecoveryDemonstratedRequiresEvidence(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Commit(h, "", "s1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.DeclareRecovery(h, p, "", "recover-a", "s1", recoveryDeclaration("obj1")); err != nil {
		t.Fatalf("DeclareRecovery() error = %v", err)
	}
	if err := dispatch.CloseRecovery(h, "", "recover-a", "s1", "obj1", "", true); err == nil {
		t.Fatal("CloseRecovery(demonstrated=true, evidence=\"\") error = nil, want an error")
	}
	if err := dispatch.CloseRecovery(h, "", "recover-a", "s1", "obj1", "saw it pass", true); err != nil {
		t.Fatalf("CloseRecovery(demonstrated=true) with evidence error = %v", err)
	}
	if got := h.Project().Budgets("s1").Recoveries["obj1"].Failures; got != 0 {
		t.Errorf("CloseRecovery(demonstrated=true) Failures = %d, want 0 (streak cleared)", got)
	}
}

func TestAccountTracksScopedActionsAndForcesBacktrackAtExhaustion(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Commit(h, "", "s1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	declaration := recoveryDeclaration("obj1")
	if err := dispatch.DeclareRecovery(h, p, "", "recover-a", "s1", declaration); err != nil {
		t.Fatalf("DeclareRecovery() error = %v", err)
	}

	entries := []vfs.JournalEntry{
		{Seq: 0, CallID: "call-1", WorkUnitID: "a", SessionID: "s1"},
	}
	if err := dispatch.Account(h, entries); err != nil {
		t.Fatalf("Account() error = %v", err)
	}
	recovery := h.Project().Budgets("s1").Recoveries["obj1"]
	if !recovery.Open {
		t.Fatalf("Account() closed the recovery after only 1 of %d actions: %+v", declaration.Actions, recovery)
	}
	if recovery.Used != 1 {
		t.Fatalf("Account() Used = %d, want 1", recovery.Used)
	}

	// A second scoped call exhausts the 1-action budget and forces backtracking.
	entries = append(entries, vfs.JournalEntry{Seq: 1, CallID: "call-2", WorkUnitID: "a", SessionID: "s1"})
	if err := dispatch.Account(h, entries); err != nil {
		t.Fatalf("second Account() error = %v", err)
	}
	recovery = h.Project().Budgets("s1").Recoveries["obj1"]
	if recovery.Open || !recovery.Backtracked {
		t.Errorf("Account() at action exhaustion left recovery = %+v, want closed and backtracked", recovery)
	}
}

func TestAccountWithNoOpenRecoveriesIsNoop(t *testing.T) {
	h := newHistory(t)
	if err := dispatch.Account(h, nil); err != nil {
		t.Errorf("Account(nil) with no sessions error = %v, want nil", err)
	}
}

func TestExceptRecordsAllowance(t *testing.T) {
	h := newHistory(t)
	if err := dispatch.Except(h, "", "grant-1", "s1", history.BoundConcurrency, "", 3); err != nil {
		t.Fatalf("Except() error = %v", err)
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindException || last.Bound != history.BoundConcurrency || last.Allowance != 3 {
		t.Errorf("Except() last entry = %+v, want KindException Bound=%q Allowance=3", last, history.BoundConcurrency)
	}
	if got := h.Project().Budgets("s1").Allowance[history.BoundConcurrency]; got != 3 {
		t.Errorf("Allowance[%q] = %d, want 3", history.BoundConcurrency, got)
	}
}

// Two root sessions name their work alike: each plans, admits, retries,
// finishes, reconciles and withdraws only its own unit.
func TestSameUnitNameInTwoRootsNeverCrosses(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	unit := func(session string) history.Unit {
		u, _ := h.Project().Unit(session, "impl")
		return u
	}
	for _, session := range []string{"root-a", "root-b"} {
		if err := dispatch.Commit(h, "", session, "v1", []dispatch.PlanUnit{{Unit: "impl", Contract: "contract of " + session}}); err != nil {
			t.Fatalf("Commit(%s) = %v", session, err)
		}
	}
	if a, b := unit("root-a"), unit("root-b"); a.Contract != "contract of root-a" || b.Contract != "contract of root-b" {
		t.Fatalf("plans crossed: a %+v, b %+v", a, b)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "impl", Session: "root-a", Agent: "dev", Dispatch: "a:1"}); err != nil {
		t.Fatalf("Admit(root-a) = %v", err)
	}
	// root-a's unit in flight never denies root-b's own unit as a repetition.
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "impl", Session: "root-b", Agent: "dev", Dispatch: "b:1"}); err != nil {
		t.Fatalf("Admit(root-b) while root-a's unit is in flight = %v", err)
	}
	if b := unit("root-b"); b.AttemptID != history.FirstAttempt || b.Dispatch != "b:1" {
		t.Fatalf("root-b's admission continued root-a's unit: %+v", b)
	}
	// root-b's restart reconciling its own delegation never interrupts root-a's work.
	if err := dispatch.Record(h, "", "impl", "root-b", history.KindUncertain); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Reconcile(h, "", "impl", "root-b", false); err != nil {
		t.Fatal(err)
	}
	if a := unit("root-a"); a.State != history.StateInFlight || a.Flight == history.FlightUncertain {
		t.Fatalf("root-b's reconcile reached root-a's unit: %+v", a)
	}
	if err := dispatch.Finish(h, "", "impl", "root-a"); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "impl", Session: "root-a", Agent: "dev", Dispatch: "a:2"}); err != nil {
		t.Fatal(err)
	}
	if a, b := unit("root-a"), unit("root-b"); a.AttemptID != "2" || b.AttemptID != history.FirstAttempt {
		t.Fatalf("attempts shared across roots: a %+v, b %+v", a, b)
	}
	// Neither root can withdraw the other's still-planned unit.
	if err := dispatch.Commit(h, "", "root-c", "v1", []dispatch.PlanUnit{{Unit: "later", Contract: "c"}}); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Withdraw(h, "", "later", "root-a"); err == nil {
		t.Fatal("root-a withdrew root-c's planned unit")
	}
	if u, _ := h.Project().Unit("root-c", "later"); u.State != history.StatePlanned {
		t.Fatalf("root-c's planned unit changed: %+v", u)
	}
}

// A revision sees only its own session's plan.
func TestRevisionSeesOnlyItsOwnPlan(t *testing.T) {
	h := newHistory(t)
	if err := dispatch.Commit(h, "", "root-a", "v1", []dispatch.PlanUnit{{Unit: "a1", Contract: "c"}}); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Commit(h, "", "root-b", "v1", []dispatch.PlanUnit{{Unit: "b1", Contract: "c"}}); err != nil {
		t.Fatal(err)
	}
	// root-b cannot depend on, nor withdraw, root-a's unit.
	if err := dispatch.Revise(h, "", "root-b", "v1", "v2", []dispatch.PlanUnit{{Unit: "b2", Contract: "c", Prerequisites: []string{"a1"}}}, nil); err == nil {
		t.Fatal("root-b's revision took root-a's unit as a prerequisite")
	}
	if err := dispatch.Declare(h, "", "root-b", "v3", "v1", nil, []string{"a1"}); err == nil {
		t.Fatal("root-b's revision withdrew root-a's unit")
	}
	if u, _ := h.Project().Unit("root-a", "a1"); u.State != history.StatePlanned {
		t.Fatalf("root-a's plan changed: %+v", u)
	}
}
