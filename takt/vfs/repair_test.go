package vfs_test

import (
	"errors"
	"github.com/rou-cru/takt-ai/takt/vfs"
	"os"
	"path/filepath"
	"testing"
)

// Approval is about one failed effect; it never substitutes for a new operation's guards.
func TestManifestRepairRevalidatesAndRetainsEvidence(t *testing.T) {
	for _, diverged := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "preserve divergent files"}[diverged], func(t *testing.T) {
			f, root, state := newStore(t)
			path := filepath.Join(root, "a")
			if err := os.WriteFile(path, []byte("base"), 0600); err != nil {
				t.Fatal(err)
			}
			key := bind(t, f, "author", "unit", "dev", "a")
			delta := apply(t, f, applyCase{key, "write", 0, vfs.OpPatch, "a", "candidate"})
			gate(t, f, key, delta)
			f.SetFailpoint(func(point string) error {
				if point == "after/a" {
					return errors.New("power loss")
				}
				return nil
			})
			if err := f.ConsolidateCheckpoint(key, "partial", delta.Revision, false); err == nil {
				t.Fatal("flush unexpectedly completed")
			}
			f.SetFailpoint(nil)
			if diverged {
				if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			repair, err := f.RecoveryRepair()
			if err != nil || repair == nil || repair.Preserve != diverged {
				t.Fatalf("repair %+v %v", repair, err)
			}
			approval := vfs.RepairApproval{Kind: repair.Kind, ID: repair.ID, State: repair.State, Preserve: repair.Preserve}
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
			f, err = vfs.Open(root, state)
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
				if len(f.Interrupted()) != 1 {
					t.Fatal("manifest evidence not retained")
				}
				if err := f.ConsolidateCheckpoint(key, "retry", delta.Revision, false); !errors.Is(err, vfs.ErrBaseChanged) {
					t.Fatalf("base guard bypassed: %v", err)
				}
				apply(t, f, applyCase{key, "later-write", delta.Revision, vfs.OpPatch, "a", "later staged work"})
				for _, evidence := range f.Interrupted() {
					if string(evidence.Deltas[key].Files["a"]) != "candidate" {
						t.Fatal("later staged work rewrote archived manifest evidence")
					}
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := vfs.Open(root, state)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := reopened.Close(); err != nil {
						t.Error(err)
					}
				}()
				if len(reopened.Interrupted()) != 1 {
					t.Fatal("evidence lost on restart")
				}
			} else if err := f.ConsolidateCheckpoint(key, "retry", delta.Revision, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCyclePreservationRetainsDeltaWithoutOwnershipOrAcceptance(t *testing.T) {
	f, root, state := newStore(t)
	path := filepath.Join(root, "a")
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	key := bind(t, f, "author", "unit", "dev", "a")
	delta := apply(t, f, applyCase{key, "write", 0, vfs.OpPatch, "a", "staged"})
	f.SeedConsolidatedCycle("cycle", key, "a", []byte("base"), []byte("candidate"), 0600)
	repair, err := f.CycleRepair("cycle", "blocked-state")
	if err != nil {
		t.Fatal(err)
	}
	a := vfs.RepairApproval{Kind: "cycle", ID: repair.ID, State: repair.State, Preserve: true}
	if err := f.PreserveInterruptedCycle("cycle", "new-state", a); err == nil {
		t.Fatal("cycle state changed but approval accepted")
	}
	if err := f.PreserveInterruptedCycle("cycle", "blocked-state", a); err != nil {
		t.Fatal(err)
	}
	if err := f.DropCycleStaging("cycle"); err != nil {
		t.Fatal(err)
	}
	if owners, bindings := f.Held(); owners != 0 || bindings != 0 {
		t.Fatal("active ownership retained")
	}
	e := f.Interrupted()["cycle/cycle"]
	if e.Cycle == nil || e.Cycle.Completed || e.Deltas[key].Revision != delta.Revision || string(e.Deltas[key].Files["a"]) != "staged" {
		t.Fatalf("evidence lost or accepted: %+v", e)
	}
	if err := f.Close(); err != nil {
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
	if !f.CycleInterrupted("cycle") {
		t.Fatal("interrupted closure lost")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "external" {
		t.Fatal("physical files overwritten")
	}
}
