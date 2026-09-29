package gc

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// TestBindGuardLetsRunningUnitsFinishDuringDrain proves the barrier halts new
// work only: a unit whose delegation is already in flight keeps binding, while
// a unit that was never admitted stays refused. Cycle claims through the
// ordinary bind surface stay refused: collector bindings are issued only by
// coordinator authorization.
func TestBindGuardLetsRunningUnitsFinishDuringDrain(t *testing.T) {
	c := &Coordinator{Draining: true}
	if err := guardVFSBind(c, "bind", vfs.Identity{WorkUnitID: "dev-a", AttemptID: "1"}); err != nil {
		t.Fatalf("in-flight unit refused during drain: %v", err)
	}
	if err := guardVFSBind(c, "bind", vfs.Identity{WorkUnitID: "dev-b"}); err == nil {
		t.Fatal("a unit that was never admitted bound during drain")
	}
	if err := guardVFSBind(c, "bind", vfs.Identity{Specialist: CollectorSpecialistID, AttemptID: "1", CycleID: "c1", MandateClass: "dead-code"}); err == nil {
		t.Fatal("cycle-claiming bind accepted without coordinator authorization")
	}
	if err := guardVFSBind(&Coordinator{}, "bind", vfs.Identity{WorkUnitID: "dev-b"}); err != nil {
		t.Fatalf("bind refused with no barrier: %v", err)
	}
}

// TestDrainStillAdmitsVerification proves a drain waiting on staged deltas
// admits the verifier that clears them, refuses everything else, and refuses
// even the verifier once the cycle itself is in flight.
func TestDrainStillAdmitsVerification(t *testing.T) {
	draining := &Coordinator{Draining: true}
	if draining.holdsAdmission("verify") {
		t.Fatal("drain refused the verifier that clears staged deltas")
	}
	if !draining.holdsAdmission("dev") {
		t.Fatal("drain admitted new implementation work")
	}
	if !(&Coordinator{Cycle: &Cycle{}}).holdsAdmission("verify") {
		t.Fatal("cycle in flight admitted work")
	}
	if (&Coordinator{}).holdsAdmission("dev") {
		t.Fatal("no barrier refused work")
	}
}
