package gc

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// TestBindGuardLetsRunningUnitsFinishDuringCycle proves a cycle in flight halts new
// work only: a unit whose delegation is already in flight keeps binding, while
// a unit that was never admitted stays refused. Cycle claims through the
// ordinary bind surface stay refused: collector bindings are issued only by
// coordinator authorization.
func TestBindGuardLetsRunningUnitsFinishDuringCycle(t *testing.T) {
	c := &Coordinator{Cycle: &Cycle{}}
	if err := guardVFSBind(c, "bind", vfs.Identity{WorkUnitID: "dev-a", AttemptID: "1"}); err != nil {
		t.Fatalf("in-flight unit refused during a cycle: %v", err)
	}
	if err := guardVFSBind(c, "bind", vfs.Identity{WorkUnitID: "dev-b"}); err == nil {
		t.Fatal("a unit that was never admitted bound during a cycle")
	}
	if err := guardVFSBind(c, "bind", vfs.Identity{Specialist: CollectorSpecialistID, AttemptID: "1", CycleID: "c1", MandateClass: "dead-code"}); err == nil {
		t.Fatal("cycle-claiming bind accepted without coordinator authorization")
	}
	if err := guardVFSBind(&Coordinator{}, "bind", vfs.Identity{WorkUnitID: "dev-b"}); err != nil {
		t.Fatalf("bind refused with no barrier: %v", err)
	}
}
