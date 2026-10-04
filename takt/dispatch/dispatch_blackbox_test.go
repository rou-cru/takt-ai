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
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}, false); err != nil {
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
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}, false); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.Launch(h, "", "a", "s1"); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !h.Project().Units["a"].Launched {
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
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}, false); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.Record(h, "", "a", "s1", history.KindUncertain); err != nil {
		t.Fatalf("Record(KindUncertain) error = %v", err)
	}
	if err := dispatch.Reconcile(h, "", "a", "s1", true); err != nil {
		t.Fatalf("Reconcile(running=true) error = %v", err)
	}
	unit := h.Project().Units["a"]
	if !unit.Launched || unit.Flight != history.FlightRunning {
		t.Errorf("Reconcile(running=true) left unit = %+v, want launched and running", unit)
	}
}

func TestReconcileNotRunningFinishesAsInterrupted(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}, false); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if err := dispatch.Record(h, "", "a", "s1", history.KindUncertain); err != nil {
		t.Fatalf("Record(KindUncertain) error = %v", err)
	}
	if err := dispatch.Reconcile(h, "", "a", "s1", false); err != nil {
		t.Fatalf("Reconcile(running=false) error = %v", err)
	}
	unit := h.Project().Units["a"]
	if unit.State != history.StateSettled || unit.Outcome != history.OutcomeInterrupted {
		t.Errorf("Reconcile(running=false) left unit = %+v, want settled/interrupted", unit)
	}
	if err := dispatch.Record(h, "", "a", "s1", history.KindUncertain); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Reconcile(h, "", "a", "s1", false); err != nil {
		t.Fatalf("repeated recovery of settled delegation = %v", err)
	}
	if err := dispatch.Reconcile(h, "", "a", "other", false); err == nil {
		t.Fatal("unrelated session reconciled a settled delegation")
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
	if err := dispatch.Reconcile(h, "", "planned", "other", false); err == nil {
		t.Error("another session reconciled a planned unit")
	}
	if err := dispatch.Reconcile(h, "", "never-admitted", "s1", true); err == nil {
		t.Error("Reconcile(running=true) of a unit that was never admitted = nil, want an error")
	}
}

func TestReconcileRequiresUncertainFlight(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}, false); err != nil {
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
	if h.Project().Units["a"].State != history.StateWithdrawn {
		t.Errorf("Withdraw() left state = %v, want withdrawn", h.Project().Units["a"].State)
	}
}

func TestWithdrawRejectsAdmittedUnit(t *testing.T) {
	h := newHistory(t)
	p := loadPolicy(t)
	if err := dispatch.Commit(h, "", "s1", "v1", []dispatch.PlanUnit{{Unit: "a", Contract: "c"}}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}, false); err != nil {
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
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}, false); err != nil {
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
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}, false); err != nil {
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
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: "a", Session: "s1", Agent: "agent", Dispatch: "d1"}, false); err != nil {
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
