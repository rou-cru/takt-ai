package vfs_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

func openFS(t *testing.T) (*vfs.FS, string) {
	t.Helper()
	root := t.TempDir()
	f, err := vfs.Open(root, filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("vfs.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, root
}

func TestPendingOrdinaryDeltas(t *testing.T) {
	f, _ := openFS(t)
	if n := f.PendingOrdinaryDeltas(); n != 0 {
		t.Fatalf("PendingOrdinaryDeltas() on a fresh FS = %d, want 0", n)
	}
	if ids := f.PendingOrdinaryDeltaIdentities(); len(ids) != 0 {
		t.Fatalf("PendingOrdinaryDeltaIdentities() on a fresh FS = %v, want none", ids)
	}

	key, err := f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "wu-1", AttemptID: "1", AgentID: "agent-a", Specialist: "dev", InvariantsHash: "h"}, []string{"a.go"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if _, err := f.Apply(vfs.Operation{Key: key, CallID: "c1", Action: vfs.OpCreate, Path: "a.go", Content: []byte("x")}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if n := f.PendingOrdinaryDeltas(); n != 1 {
		t.Errorf("PendingOrdinaryDeltas() with one staged ordinary delta = %d, want 1", n)
	}
	ids := f.PendingOrdinaryDeltaIdentities()
	if len(ids) != 1 {
		t.Fatalf("PendingOrdinaryDeltaIdentities() = %v, want one identity", ids)
	}
}

func TestOnCollisionHandlerFires(t *testing.T) {
	f, _ := openFS(t)
	var collisions int
	f.OnCollision(func(vfs.CollisionEvent) { collisions++ })

	if _, err := f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "wu-1", AttemptID: "1", AgentID: "agent-a", Specialist: "dev", InvariantsHash: "h"}, []string{"owned.go"}); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if _, err := f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "wu-2", AttemptID: "1", AgentID: "agent-b", Specialist: "dev", InvariantsHash: "h"}, []string{"owned.go"}); !errors.Is(err, vfs.ErrCollision) {
		t.Fatalf("Bind() over an owned scope error = %v, want ErrCollision", err)
	}
	if collisions != 1 {
		t.Errorf("OnCollision handler fired %d times, want 1", collisions)
	}
}

func TestDropCycleStaging(t *testing.T) {
	f, _ := openFS(t)
	// Dropping a cycle that never staged anything is a valid no-op.
	if err := f.DropCycleStaging("no-such-cycle"); err != nil {
		t.Fatalf("DropCycleStaging() on an absent cycle error = %v, want nil", err)
	}
}

func TestJournalEntryRef(t *testing.T) {
	f, _ := openFS(t)
	key, err := f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "wu-1", AttemptID: "1", AgentID: "agent-a", Specialist: "dev", InvariantsHash: "h"}, []string{"a.go"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if _, err := f.Apply(vfs.Operation{Key: key, CallID: "c1", Action: vfs.OpCreate, Path: "a.go", Content: []byte("x")}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	journal := f.JournalPage("s", "wu-1", -1, 10)
	if len(journal) != 1 {
		t.Fatalf("JournalPage() = %d entries, want 1", len(journal))
	}
	if got, want := journal[0].Ref(), "journal/0"; got != want {
		t.Errorf("Ref() = %q, want %q", got, want)
	}
}
