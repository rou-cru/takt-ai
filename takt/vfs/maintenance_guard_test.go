package vfs_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// setCycle stores a maintenance state with an active cycle, or none when
// cycle is empty.
func setCycle(t *testing.T, f *vfs.FS, cycle, phase string) {
	t.Helper()
	raw := `{"version":1}`
	if cycle != "" {
		raw = fmt.Sprintf(`{"version":1,"cycle":{"plan":{"cycle_id":%q},"phase":%q}}`, cycle, phase)
	}
	if err := f.SetMaintenance([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceGuardBarrierLetsAdmittedUnitsFinish(t *testing.T) {
	f, _ := openFS(t)
	setCycle(t, f, "c1", "baseline")
	if err := f.GuardMaintenance("bind", "1", "", "", ""); err != nil {
		t.Fatalf("bind of an admitted unit during the barrier = %v; want allowed", err)
	}
	if err := f.GuardMaintenance("bind", "", "", "", ""); err == nil {
		t.Fatal("bind of new work during the barrier = nil; want refused")
	}
	setCycle(t, f, "", "")
	if err := f.GuardMaintenance("bind", "", "", "", ""); err != nil {
		t.Fatalf("bind after the barrier = %v; want allowed", err)
	}
}

func TestMaintenanceGuardRejectsCorruptState(t *testing.T) {
	f, _ := openFS(t)
	if err := f.SetMaintenance([]byte(`{"version":1,"cycle":"invalid"}`)); err != nil {
		t.Fatal(err)
	}
	if err := f.GuardMaintenance("op", "", "read", "a.go", "agent"); err == nil {
		t.Fatal("guard over corrupt maintenance state = nil; want fail-closed")
	}
}

func TestMaintenanceGuardLeavesOrdinaryWorkAlone(t *testing.T) {
	f, _ := openFS(t)
	key, err := f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "wu", AttemptID: "1", AgentID: "dev", Specialist: "dev"}, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	setCycle(t, f, "c1", "baseline")
	if err := f.GuardMaintenance("op", "", "patch", "a.go", key); err != nil {
		t.Fatalf("ordinary write during a cycle = %v; want allowed", err)
	}
}

func TestMaintenanceGuardCycleRules(t *testing.T) {
	f, _ := openFS(t)
	key, err := f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "c1", AttemptID: "1", AgentID: "simplify", Specialist: "simplify", CycleID: "c1"}, []string{"in.go"})
	if err != nil {
		t.Fatal(err)
	}
	setCycle(t, f, "c1", "collect")
	for command, continuation := range map[string]string{"verify": "gc_verdict", "consolidate": "gc_acceptance"} {
		if err := f.GuardMaintenance(command, "", "", "", key); err == nil || !strings.Contains(err.Error(), continuation) {
			t.Fatalf("%s through a collector = %v; want refused naming %s", command, err, continuation)
		}
	}
	for _, tc := range []struct {
		name, phase, action, path string
		ok                        bool
	}{
		{"read context", "collect", "read", "elsewhere.go", true},
		{"patch authorized", "collect", "patch", "in.go", true},
		{"patch outside scope", "collect", "patch", "out.go", false},
		{"inspect baseline", "baseline", "read", "in.go", true},
		{"mutation outside collect", "baseline", "patch", "in.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCycle(t, f, "c1", tc.phase)
			if err := f.GuardMaintenance("op", "", tc.action, tc.path, key); (err == nil) != tc.ok {
				t.Fatalf("guard = %v; want allowed %v", err, tc.ok)
			}
		})
	}
	setCycle(t, f, "other", "collect")
	if err := f.GuardMaintenance("op", "", "read", "in.go", key); err == nil {
		t.Fatal("collector of a foreign cycle = nil; want refused")
	}
	setCycle(t, f, "", "")
	if err := f.GuardMaintenance("op", "", "read", "in.go", key); err == nil {
		t.Fatal("collector of a closed cycle = nil; want refused")
	}
}
