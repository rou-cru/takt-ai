package dispatch_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

const (
	recoverySession   = "s1"
	recoveryObjective = "obj"
)

// recoveryUnits are the units of the recovery scope the tests abandon.
var recoveryUnits = []string{"a", "b"}

func openRecoveryFS(t *testing.T) *vfs.FS {
	t.Helper()
	f, err := vfs.Open(t.TempDir(), filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("vfs.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func admitUnit(t *testing.T, h *history.History, p dispatch.AdmissionPolicy, unit, from string) error {
	t.Helper()
	return dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Event: unit, Session: recoverySession, Agent: "dev", Dispatch: from})
}

func mustAdmit(t *testing.T, h *history.History, p dispatch.AdmissionPolicy, unit, from string) {
	t.Helper()
	if err := admitUnit(t, h, p, unit, from); err != nil {
		t.Fatalf("Admit(%s) error = %v", unit, err)
	}
}

func mustFinish(t *testing.T, h *history.History, unit string) {
	t.Helper()
	if err := dispatch.Finish(h, "", unit, recoverySession); err != nil {
		t.Fatalf("Finish(%s) error = %v", unit, err)
	}
}

// stagedUnits reports which units hold staged work in the filesystem.
func stagedUnits(f *vfs.FS) map[string]bool {
	staged := map[string]bool{}
	for _, claim := range f.OwnershipClaims(recoverySession) {
		if claim.Staged {
			staged[claim.WorkUnitID] = true
		}
	}
	return staged
}

// abandonedScope runs both units once with staged work, declares a recovery
// over them with a single attempt, spends it without the result, and returns
// the state after the harness abandoned the scope.
func abandonedScope(t *testing.T) (*history.History, dispatch.AdmissionPolicy, *vfs.FS) {
	t.Helper()
	h, p, f := newHistory(t), loadPolicy(t), openRecoveryFS(t)
	for _, unit := range recoveryUnits {
		mustAdmit(t, h, p, unit, "first-"+unit)
		key, err := f.Bind(vfs.Identity{SessionID: recoverySession, WorkUnitID: unit, AttemptID: history.FirstAttempt, AgentID: vfs.AgentID("agent-" + unit), Specialist: "dev", InvariantsHash: "h"}, []string{unit + ".go"})
		if err != nil {
			t.Fatalf("Bind(%s) error = %v", unit, err)
		}
		if _, err := f.Apply(vfs.Operation{Key: key, CallID: "create-" + unit, Action: vfs.OpCreate, Path: unit + ".go", Content: []byte("x")}); err != nil {
			t.Fatalf("Apply(%s) error = %v", unit, err)
		}
		mustFinish(t, h, unit)
	}
	declaration := dispatch.RecoveryDeclaration{Objective: recoveryObjective, Result: "both units deliver", Point: "before-retry", Scope: recoveryUnits, Actions: 10, Attempts: 1}
	if err := dispatch.DeclareRecovery(h, p, "", "recover", recoverySession, declaration); err != nil {
		t.Fatalf("DeclareRecovery() error = %v", err)
	}
	mustAdmit(t, h, p, "a", "retry-a")
	mustFinish(t, h, "a")
	if err := dispatch.Account(h, nil); err != nil {
		t.Fatalf("Account() error = %v", err)
	}
	if recovery := h.Project().Budgets(recoverySession).Recoveries[recoveryObjective]; !recovery.Backtracked {
		t.Fatalf("the spent attempt left the recovery %+v, want it abandoned", recovery)
	}
	return h, p, f
}

// namesAll reports whether the error names every tool the agent continues with.
func namesAll(err error, tools ...string) bool {
	if err == nil {
		return false
	}
	for _, tool := range tools {
		if !strings.Contains(err.Error(), tool) {
			return false
		}
	}
	return true
}

func TestAbandonedScopeGuidesToTheUsersDecision(t *testing.T) {
	h, p, _ := abandonedScope(t)
	exit := []string{"dispatch_exception", "dispatch_restore", history.BoundRecoveryAttempts}
	if err := admitUnit(t, h, p, "b", "retry-b"); !namesAll(err, exit...) {
		t.Errorf("Admit() in an abandoned scope error = %v, want it to name %v", err, exit)
	}
	declaration := dispatch.RecoveryDeclaration{Objective: "other", Result: "r", Point: "p", Scope: recoveryUnits, Actions: 1, Attempts: 1}
	if err := dispatch.DeclareRecovery(h, p, "", "recover", recoverySession, declaration); !namesAll(err, exit...) {
		t.Errorf("DeclareRecovery() over an abandoned scope error = %v, want it to name %v", err, exit)
	}
	if err := dispatch.CloseRecovery(h, "", "recover", recoverySession, recoveryObjective, "", false); !namesAll(err, exit...) {
		t.Errorf("CloseRecovery() of an abandoned recovery error = %v, want it to name %v", err, exit)
	}
}

func TestExceptionLiftsAnAbandonedScopeAndKeepsItsStagedWork(t *testing.T) {
	h, p, f := abandonedScope(t)
	if err := dispatch.Except(h, "", "", recoverySession, history.BoundRecoveryActions, recoveryObjective, 1); !namesAll(err, history.BoundRecoveryAttempts) {
		t.Fatalf("Except() on the bound that did not abandon the scope error = %v, want it to name %s", err, history.BoundRecoveryAttempts)
	}
	if err := dispatch.Except(h, "", "", recoverySession, history.BoundRecoveryAttempts, "", 1); err == nil {
		t.Fatal("Except() on a recovery bound without its objective error = nil, want an error")
	}
	if err := dispatch.Except(h, "", "", recoverySession, history.BoundRecoveryAttempts, recoveryObjective, 1); err != nil {
		t.Fatalf("Except() on the bound that abandoned the scope error = %v", err)
	}
	if recovery := h.Project().Budgets(recoverySession).Recoveries[recoveryObjective]; !recovery.Open || recovery.Backtracked {
		t.Fatalf("the exception left the recovery %+v, want it open again", recovery)
	}
	if err := admitUnit(t, h, p, "b", "retry-b"); err != nil {
		t.Fatalf("Admit() after the exception error = %v", err)
	}
	if staged := stagedUnits(f); !staged["a"] || !staged["b"] {
		t.Errorf("staged work after the exception = %v, want both units kept", staged)
	}
}

func TestRestoreRevertsTheStagedWorkOfTheWholeScope(t *testing.T) {
	h, p, f := abandonedScope(t)
	if err := dispatch.Restore(h, f, "", recoverySession, recoveryObjective); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if staged := stagedUnits(f); len(staged) != 0 {
		t.Errorf("staged work after Restore = %v, want none", staged)
	}
	if !h.Project().Budgets(recoverySession).Recoveries[recoveryObjective].Restored {
		t.Error("Restore() did not record the restoration")
	}
	if err := dispatch.Restore(h, f, "", recoverySession, recoveryObjective); err == nil {
		t.Error("Restore() of an already restored recovery error = nil, want an error")
	}
	declaration := dispatch.RecoveryDeclaration{Objective: "again", Result: "both units deliver", Point: "after-restore", Scope: recoveryUnits, Actions: 10, Attempts: 2}
	if err := dispatch.DeclareRecovery(h, p, "", "recover", recoverySession, declaration); err != nil {
		t.Errorf("DeclareRecovery() after the restoration error = %v", err)
	}
}

func TestRestoreNeedsNoStagedWork(t *testing.T) {
	h, p := newHistory(t), loadPolicy(t)
	for _, unit := range recoveryUnits {
		mustAdmit(t, h, p, unit, "first-"+unit)
		mustFinish(t, h, unit)
	}
	declaration := dispatch.RecoveryDeclaration{Objective: recoveryObjective, Result: "r", Point: "p", Scope: recoveryUnits, Actions: 10, Attempts: 1}
	if err := dispatch.DeclareRecovery(h, p, "", "recover", recoverySession, declaration); err != nil {
		t.Fatalf("DeclareRecovery() error = %v", err)
	}
	mustAdmit(t, h, p, "a", "retry-a")
	mustFinish(t, h, "a")
	if err := dispatch.Account(h, nil); err != nil {
		t.Fatalf("Account() error = %v", err)
	}
	if err := dispatch.Restore(h, openRecoveryFS(t), "", recoverySession, recoveryObjective); err != nil {
		t.Errorf("Restore() of a scope with nothing staged error = %v", err)
	}
}

func TestRestoreWaitsForWorkThatCanStillAct(t *testing.T) {
	h, p, f := newHistory(t), loadPolicy(t), openRecoveryFS(t)
	mustAdmit(t, h, p, "a", "first-a")
	mustFinish(t, h, "a")
	declaration := dispatch.RecoveryDeclaration{Objective: recoveryObjective, Result: "r", Point: "p", Scope: recoveryUnits, Actions: 1, Attempts: 2}
	if err := dispatch.DeclareRecovery(h, p, "", "recover", recoverySession, declaration); err != nil {
		t.Fatalf("DeclareRecovery() error = %v", err)
	}
	mustAdmit(t, h, p, "a", "retry-a")
	// One governed call spends the action budget while the retry still runs.
	if err := dispatch.Account(h, []vfs.JournalEntry{{Seq: 0, CallID: "call-1", WorkUnitID: "a", SessionID: recoverySession}}); err != nil {
		t.Fatalf("Account() error = %v", err)
	}
	if err := dispatch.Restore(h, f, "", recoverySession, recoveryObjective); !namesAll(err, "dispatch_restore") {
		t.Fatalf("Restore() while the scope's work still runs error = %v, want it to say when to call again", err)
	}
	mustFinish(t, h, "a")
	if err := dispatch.Restore(h, f, "", recoverySession, recoveryObjective); err != nil {
		t.Errorf("Restore() once the work ended error = %v", err)
	}
}

func TestExceptionRaisesTheConcurrencyCeiling(t *testing.T) {
	h, p := newHistory(t), loadPolicy(t)
	// A committed plan covers the units, so only the ceiling is at stake.
	plan := []dispatch.PlanUnit{{Unit: "over", Contract: "c"}}
	for i := 0; i < p.Concurrency.Specialists; i++ {
		plan = append(plan, dispatch.PlanUnit{Unit: "unit-" + string(rune('a'+i)), Contract: "c"})
	}
	if err := dispatch.Commit(h, "", recoverySession, "v1", plan); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	for i := 0; i < p.Concurrency.Specialists; i++ {
		mustAdmit(t, h, p, "unit-"+string(rune('a'+i)), "d")
	}
	if err := admitUnit(t, h, p, "over", "d"); !namesAll(err, "dispatch_exception", history.BoundConcurrency) {
		t.Fatalf("Admit() past the ceiling error = %v, want it to name the exception that raises it", err)
	}
	if err := dispatch.Except(h, "", "", recoverySession, history.BoundConcurrency, "", 1); err != nil {
		t.Fatalf("Except() error = %v", err)
	}
	if err := admitUnit(t, h, p, "over", "d"); err != nil {
		t.Errorf("Admit() after the exception error = %v", err)
	}
}

func TestRestoreReleasesAllAuthorReservationsDurably(t *testing.T) {
	for _, scenario := range []string{"active", "pending", "mixed"} {
		t.Run(scenario, func(t *testing.T) {
			h, _, original := abandonedScope(t)
			_ = original.Close()
			workspace, state := t.TempDir(), filepath.Join(t.TempDir(), "state")
			f, err := vfs.Open(workspace, state)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			for i, unit := range recoveryUnits {
				identity := vfs.Identity{SessionID: recoverySession, WorkUnitID: unit, AgentID: vfs.AgentID(unit), Specialist: "dev"}
				bind := f.Bind
				if scenario == "pending" || scenario == "mixed" && i == 1 {
					bind = f.AssignScope
				}
				key, err := bind(identity, []string{unit + ".go"})
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "mixed" && i == 0 {
					if _, err := f.Apply(vfs.Operation{Key: key, CallID: "create", Action: vfs.OpCreate, Path: unit + ".go", Content: []byte("x")}); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, identity := range []vfs.Identity{
				{SessionID: recoverySession, WorkUnitID: "outside", AgentID: "outside", Specialist: "dev"},
				{SessionID: "other", WorkUnitID: "a", AgentID: "other", Specialist: "dev"},
			} {
				key, err := f.Bind(identity, []string{string(identity.AgentID) + ".go"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.Apply(vfs.Operation{Key: key, CallID: string(identity.AgentID), Action: vfs.OpCreate, Path: string(identity.AgentID) + ".go", Content: []byte("keep")}); err != nil {
					t.Fatal(err)
				}
			}
			if err := dispatch.Restore(h, f, "", recoverySession, recoveryObjective); err != nil {
				t.Fatal(err)
			}
			check := func() {
				claims := f.OwnershipClaims(recoverySession)
				if len(claims) != 2 {
					t.Fatalf("claims after restore: %+v", claims)
				}
				for _, claim := range claims {
					if !claim.Staged {
						t.Fatalf("unrelated delta lost: %+v", claim)
					}
				}
			}
			check()
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			f, err = vfs.Open(workspace, state)
			if err != nil {
				t.Fatal(err)
			}
			check()
			for _, unit := range recoveryUnits {
				if _, err := f.AssignScope(vfs.Identity{SessionID: recoverySession, WorkUnitID: "replacement-" + unit, AgentID: "replacement", Specialist: "dev"}, []string{unit + ".go"}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
