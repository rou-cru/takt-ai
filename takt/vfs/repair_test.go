package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Approval is about one failed effect; it never substitutes for a new operation's guards.
func TestManifestRepairRevalidatesAndRetainsEvidence(t *testing.T) {
	for _, diverged := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "preserve divergent files"}[diverged], func(t *testing.T) {
			f, root, state := durable(t)
			path := filepath.Join(root, "a")
			if err := os.WriteFile(path, []byte("base"), 0600); err != nil {
				t.Fatal(err)
			}
			key := bind(t, f, "author", "unit", "dev", "a")
			delta := apply(t, f, applyCase{key, "write", 0, OpPatch, "a", "candidate"})
			gate(t, f, key, delta)
			f.failpoint = func(point string) error {
				if point == "after/a" {
					return errors.New("power loss")
				}
				return nil
			}
			if err := f.ConsolidateCheckpoint(key, "partial", delta.Revision, false); err == nil {
				t.Fatal("flush unexpectedly completed")
			}
			f.failpoint = nil
			if diverged {
				if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			repair, err := f.RecoveryRepair()
			if err != nil || repair == nil || repair.Preserve != diverged {
				t.Fatalf("repair %+v %v", repair, err)
			}
			approval := RepairApproval{Kind: repair.Kind, ID: repair.ID, State: repair.State, Preserve: repair.Preserve}
			if err := os.WriteFile(path, []byte("changed again"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := f.RepairRecovery(approval); err == nil {
				t.Fatal("stale approval repaired newer physical state")
			}
			content := "candidate"
			if diverged {
				content = "external"
			}
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			f, err = Open(root, state)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := f.RepairRecovery(approval); err != nil {
				t.Fatal(err)
			}
			want := "base"
			if diverged {
				want = "external"
			}
			got, _ := os.ReadFile(path)
			if string(got) != want {
				t.Fatalf("physical %q", got)
			}
			if view := f.InspectDelta(key); view.Hash != delta.DeltaHash || len(view.Files) != 1 {
				t.Fatalf("staged work lost: %+v", view)
			}
			if err := f.RepairRecovery(approval); err == nil {
				t.Fatal("obsolete approval reused")
			}
			if diverged {
				if len(f.interrupted) != 1 {
					t.Fatal("manifest evidence not retained")
				}
				if err := f.ConsolidateCheckpoint(key, "retry", delta.Revision, false); !errors.Is(err, ErrBaseChanged) {
					t.Fatalf("base guard bypassed: %v", err)
				}
				apply(t, f, applyCase{key, "later-write", delta.Revision, OpPatch, "a", "later staged work"})
				for _, evidence := range f.interrupted {
					if string(evidence.Deltas[key].Files["a"]) != "candidate" {
						t.Fatal("later staged work rewrote archived manifest evidence")
					}
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := Open(root, state)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := reopened.Close(); err != nil {
						t.Error(err)
					}
				}()
				if len(reopened.interrupted) != 1 {
					t.Fatal("evidence lost on restart")
				}
			} else if err := f.ConsolidateCheckpoint(key, "retry", delta.Revision, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCyclePreservationRetainsDeltaWithoutOwnershipOrAcceptance(t *testing.T) {
	f, root, state := durable(t)
	path := filepath.Join(root, "a")
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	key := bind(t, f, "author", "unit", "dev", "a")
	delta := apply(t, f, applyCase{key, "write", 0, OpPatch, "a", "staged"})
	identity := f.bindings[key]
	identity.CycleID = "cycle"
	f.bindings[key] = identity
	f.cycles["cycle"] = &cycleSnapshot{Agent: key, Items: map[string]recoveryItem{"a": {Path: "a", Before: baseFile{Present: true, Content: []byte("base"), Mode: 0600}, After: baseFile{Present: true, Content: []byte("candidate"), Mode: 0600}}}}
	repair, err := f.CycleRepair("cycle", "blocked-state")
	if err != nil {
		t.Fatal(err)
	}
	a := RepairApproval{Kind: "cycle", ID: repair.ID, State: repair.State, Preserve: true}
	if err := f.PreserveInterruptedCycle("cycle", "new-state", a); err == nil {
		t.Fatal("cycle state changed but approval accepted")
	}
	if err := f.PreserveInterruptedCycle("cycle", "blocked-state", a); err != nil {
		t.Fatal(err)
	}
	if err := f.DropCycleStaging("cycle"); err != nil {
		t.Fatal(err)
	}
	if len(f.owners) != 0 || len(f.bindings) != 0 {
		t.Fatal("active ownership retained")
	}
	e := f.interrupted["cycle/cycle"]
	if e.Cycle == nil || e.Cycle.Completed || e.Deltas[key].Revision != delta.Revision || string(e.Deltas[key].Files["a"]) != "staged" {
		t.Fatalf("evidence lost or accepted: %+v", e)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err = Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if !f.CycleInterrupted("cycle") {
		t.Fatal("interrupted closure lost")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "external" {
		t.Fatal("physical files overwritten")
	}
}
