package vfs_test

import (
	"errors"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestShellContinuationReadsLiveStagedState(t *testing.T) {
	for _, phase := range []string{"start", "evaluate"} {
		t.Run(phase, func(t *testing.T) {
			f, _, state := newStore(t)
			c, _ := delegate(t, f, state, testSession, "dev", "unit", "file.txt")
			if _, err := f.PrepareShell(c, "old", "printf x > file.txt", state); err != nil {
				t.Fatal(err)
			}
			if phase == "evaluate" {
				if _, err := f.ContinueShell(c, "old", "start"); err != nil {
					t.Fatal(err)
				}
			}
			write(t, f, c, "changed", "file.txt", "new staged work")
			if _, err := f.ContinueShell(c, "old", phase); err == nil {
				t.Fatal("stale shell preparation authorized execution")
			}
			if err := f.ReleaseShell(c, "old"); err != nil {
				t.Fatal(err)
			}
			if _, err := f.PrepareShell(c, "fresh", "printf x > file.txt", state); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{"start", "evaluate"} {
				if _, err := f.ContinueShell(c, "fresh", p); err != nil {
					t.Fatal(err)
				}
			}
			got := readAs(t, f, c, "read", "file.txt", "")
			if string(got.Content) != "new staged work" {
				t.Fatalf("retry discarded staged work: %q", got.Content)
			}
		})
	}
}

func TestShellContinuationIsSingleUseAcrossReopen(t *testing.T) {
	f, root, state := newStore(t)
	c, _ := delegate(t, f, state, testSession, "dev", "unit", "file.txt")
	if _, err := f.PrepareShell(c, "call", "printf x > file.txt", state); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ContinueShell(c, "call", "evaluate"); err == nil {
		t.Fatal("evaluation before host start")
	}
	if _, err := f.ContinueShell(c, "call", "start"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := f.ContinueShell(c, "call", "start"); !errors.Is(err, vfs.ErrDuplicateCall) {
		t.Fatalf("second start: %v", err)
	}
	if _, err := f.ContinueShell(c, "call", "evaluate"); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err = vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := f.ContinueShell(c, "call", "evaluate"); err == nil {
		t.Fatal("persisted evaluation was reusable")
	}
}

func TestEndedAttemptReleasesShellPreparation(t *testing.T) {
	f, root, state := newStore(t)
	c, _ := delegate(t, f, state, testSession, "dev", "unit", "file.txt")
	if _, err := f.PrepareShell(c, "orphan", "printf x > file.txt", state); err != nil {
		t.Fatal(err)
	}
	if err := f.CancelPreparedDelegation(c.SessionID, c.WorkUnitID, "1"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := f.ContinueShell(c, "orphan", "start"); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("preparation outlived its attempt: %v", err)
	}
}

func TestResumedChildRejectsPriorInvocationIdentity(t *testing.T) {
	f, _, state := newStore(t)
	c, key := delegate(t, f, state, testSession, "dev", "unit", "file.txt")
	c.Dispatch = testSession + "/unit/1"
	c.AttemptID = "1"
	plan, err := f.PrepareShell(c, "old", "printf x > file.txt", state)
	if err != nil {
		t.Fatal(err)
	}
	shellRun(t, plan, "0", writeProjected(t, "file.txt", "late shell"))
	settle(t, state, testSession, "unit", "1")
	if _, err := f.ReassignScope(vfs.Identity{SessionID: testSession, WorkUnitID: "unit", AgentID: "dev", Specialist: "dev"}, key, []string{"file.txt"}); err != nil {
		t.Fatal(err)
	}
	admit(t, state, testSession, "unit", "2")
	fresh := callerOf(testSession, "dev", "unit")
	if _, err := f.BindAs(fresh, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Operate(c, vfs.CallerOperation{CallID: "late", Action: vfs.OpCreate, Path: "file.txt", Content: []byte("late")}); err == nil {
		t.Fatal("late invocation wrote through a resumed child")
	}
	if _, err := f.ContinueShell(c, "old", "start"); err == nil {
		t.Fatal("prior attempt shell started")
	}
	if _, err := f.CompleteShell(fresh, "old", state); err == nil {
		t.Fatal("late native shell completion imported into a resumed attempt")
	}
	write(t, f, fresh, "current", "file.txt", "current")
}

func TestUnboundShellCompletionIsOwnerDecidedAndImportsNothing(t *testing.T) {
	f, _, state := newStore(t)
	c := vfs.Caller{SessionID: testSession, HostSession: "inspector", Agent: "analyst"}
	plan, err := f.PrepareShell(c, "inspect", "ls", state)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Capture {
		t.Fatal("unbound shell has a captured plan")
	}
	for _, phase := range []string{"start", "evaluate"} {
		if _, err = f.ContinueShell(c, "inspect", phase); err != nil {
			t.Fatal(err)
		}
	}
	before := lastEntry(t, f)
	if _, err = f.CompleteShell(c, "inspect", state); err != nil {
		t.Fatal(err)
	}
	if after := lastEntry(t, f); after.Seq != before.Seq {
		t.Fatalf("inspection staged an import: %+v", after)
	}
	if len(f.OwnershipClaims(testSession)) != 0 {
		t.Fatal("inspection acquired ownership")
	}
	if _, err = f.CompleteShell(c, "inspect", state); err == nil {
		t.Fatal("completion was reusable")
	}
}
