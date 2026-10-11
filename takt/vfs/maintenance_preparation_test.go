package vfs_test

import (
	"errors"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

func laneIdentity(agent, unit, attempt string) vfs.Identity {
	return vfs.Identity{SessionID: testSession, WorkUnitID: unit, AttemptID: attempt, AgentID: vfs.AgentID(agent), Specialist: agent, CycleID: "c1", MandateClass: "dead-code"}
}

func TestPrepareMaintenanceRequiresIssuedAttempts(t *testing.T) {
	f, _, _ := newStore(t)
	for name, tc := range map[string]struct{ collector, verifier string }{
		"collector lane not attached": {"", "1"},
		"verifier lane not attached":  {"1", ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := f.PrepareMaintenance(laneIdentity("simplify", "c1", tc.collector), laneIdentity("verify", "c1/verify", tc.verifier), []string{"a.go"})
			if !errors.Is(err, vfs.ErrIdentity) {
				t.Fatalf("PrepareMaintenance() = %v; want ErrIdentity", err)
			}
			if author, verifier := f.CycleKeys("c1"); author != "" || verifier != "" {
				t.Fatalf("refused preparation left keys %q/%q", author, verifier)
			}
		})
	}
}

func TestCycleKeysComeFromTheCoreBindings(t *testing.T) {
	f, _, _ := newStore(t)
	author, verifier, err := f.PrepareMaintenance(laneIdentity("simplify", "c1", "1"), laneIdentity("verify", "c1/verify", "1"), []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuthor, gotVerifier := f.CycleKeys("c1"); gotAuthor != author || gotVerifier != verifier {
		t.Fatalf("CycleKeys() = %q/%q; want %q/%q", gotAuthor, gotVerifier, author, verifier)
	}
	if err := f.DropCycleStaging("c1"); err != nil {
		t.Fatal(err)
	}
	if gotAuthor, gotVerifier := f.CycleKeys("c1"); gotAuthor != "" || gotVerifier != "" {
		t.Fatalf("CycleKeys() after the cycle dropped = %q/%q; want none", gotAuthor, gotVerifier)
	}
}

func TestDelegatedGateRefusesMaintenanceAuthor(t *testing.T) {
	f, _, _ := newStore(t)
	author, _, err := f.PrepareMaintenance(laneIdentity("simplify", "c1", "1"), laneIdentity("verify", "c1/verify", "1"), []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	judge := ident("verify", "review", "verify")
	if err := f.PrepareDelegation(judge, []vfs.AgentID{author}, false, nil); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("PrepareDelegation() over a collector = %v; want ErrIdentity", err)
	}
}
