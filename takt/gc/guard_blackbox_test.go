package gc_test

import (
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestGuardVFSAllowsOrdinaryWork(t *testing.T) {
	state := t.TempDir()
	fs, err := vfs.Open(t.TempDir(), filepath.Join(t.TempDir(), "vfs-state"))
	if err != nil {
		t.Fatalf("vfs.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })

	identity := vfs.Identity{SessionID: "s", WorkUnitID: "wu", AttemptID: "1", AgentID: "agent-a", Specialist: "dev", InvariantsHash: "h"}
	key, err := fs.Bind(identity, []string{"a.go"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	if err := gc.GuardVFS(state, "op", identity, "read", "a.go", key, fs); err != nil {
		t.Errorf("GuardVFS() for ordinary work error = %v, want nil", err)
	}
}

func TestGuardVFSDeniesCollectorBindOutsideCoordinator(t *testing.T) {
	state := t.TempDir()
	fs, err := vfs.Open(t.TempDir(), filepath.Join(t.TempDir(), "vfs-state"))
	if err != nil {
		t.Fatalf("vfs.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })

	identity := vfs.Identity{SessionID: "s", WorkUnitID: "wu", AttemptID: "1", AgentID: "agent-a", Specialist: "collector", InvariantsHash: "h", CycleID: "c1"}
	if err := gc.GuardVFS(state, "bind", identity, "", "", "agent-a", fs); err == nil {
		t.Fatal("GuardVFS() for a bind claiming a cycle outside coordinator authorization error = nil, want an error")
	}
}

func TestGuardVFSDeniesNewBindingsWhileBarrierHolds(t *testing.T) {
	state := t.TempDir()
	c := &gc.Coordinator{Version: 1, Cursor: -1, Cycle: &gc.Cycle{}}
	if err := gc.SaveCoordinator(state, c); err != nil {
		t.Fatalf("SaveCoordinator() error = %v", err)
	}
	fs, err := vfs.Open(t.TempDir(), filepath.Join(t.TempDir(), "vfs-state"))
	if err != nil {
		t.Fatalf("vfs.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })

	// AttemptID empty: a brand-new ordinary binding, not one already in flight.
	identity := vfs.Identity{SessionID: "s", WorkUnitID: "wu", AgentID: "agent-a", Specialist: "dev", InvariantsHash: "h"}
	if err := gc.GuardVFS(state, "bind", identity, "", "", "agent-a", fs); err == nil {
		t.Fatal("GuardVFS() for a new binding while the barrier holds error = nil, want an error")
	}
}
