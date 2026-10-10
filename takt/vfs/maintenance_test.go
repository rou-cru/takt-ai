// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package vfs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

const gcSpecialist = "simplify"

type gcRig struct {
	t     *testing.T
	f     *vfs.FS
	root  string
	state string
}

func newGCRig(t *testing.T) *gcRig {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{"a.txt": "old-a", "b.txt": "old-b"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(t.TempDir(), "private")
	f, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return &gcRig{t: t, f: f, root: root, state: state}
}

func (g *gcRig) bind(agent, specialist string, scope ...string) vfs.AgentID {
	g.t.Helper()
	identity := vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: vfs.AgentID(agent), Specialist: specialist, InvariantsHash: "inv", CycleID: "cycle-1", MandateClass: "dead-code"}
	if specialist == "verify" {
		var author vfs.AgentID
		for _, claim := range g.f.OwnershipClaims("s") {
			if claim.AgentID != identity.AgentID && len(claim.Scope) > 0 {
				author = claim.Key
				break
			}
		}
		key, err := g.f.AssignVerifier(identity, author)
		if err != nil {
			g.t.Fatal(err)
		}
		return key
	}
	key, err := g.f.Bind(identity, scope)
	if err != nil {
		g.t.Fatal(err)
	}
	return key
}

// stage has key rewrite a.txt, delete b.txt and create c.txt.
func (g *gcRig) stage(key vfs.AgentID) vfs.OperationResult {
	g.t.Helper()
	var r vfs.OperationResult
	var err error
	for i, op := range []vfs.Operation{
		{Action: vfs.OpPatch, Path: "a.txt", Content: []byte("new-a")},
		{Action: vfs.OpDelete, Path: "b.txt"},
		{Action: vfs.OpCreate, Path: "c.txt", Content: []byte("new-c")},
	} {
		op.Key, op.CallID, op.ExpectedRevision = key, "stage-"+string(key)+string(rune('0'+i)), r.Revision
		if r, err = g.f.Apply(op); err != nil {
			g.t.Fatal(err)
		}
	}
	return r
}

func (g *gcRig) disk(name string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(g.root, name))
	return string(b), err == nil
}

func (g *gcRig) verdict(author vfs.AgentID, r vfs.OperationResult, pass bool) error {
	verifier := g.bind("judge-"+string(author), "verify")
	finding := ""
	if !pass {
		finding = "behavior changed"
	}
	return g.f.Verify(verifier, author, "verdict-"+string(author), r.Revision, r.DeltaHash, pass, finding)
}

func (g *gcRig) journalOps(agent vfs.AgentID, op vfs.OperationType) (paths []string) {
	id, _ := g.f.BindingIdentity(agent)
	for _, e := range g.f.JournalPage("s", "u", -1, 500) {
		if e.Agent == id.AgentID && e.Operation == op {
			paths = append(paths, e.Path)
			if e.Role != model.RoleMaintenance && e.Role != model.RoleExecution || e.CycleID != "cycle-1" {
				g.t.Errorf("entry %s not stamped as its author: %+v", e.Ref(), e)
			}
		}
	}
	return paths
}

func TestMaintenanceBindsAndConsolidatesOnPass(t *testing.T) {
	g := newGCRig(t)
	key := g.bind("collector", gcSpecialist, "a.txt", "b.txt", "c.txt")
	r := g.stage(key)
	if err := g.f.ConsolidateCheckpoint(key, "cp", r.Revision, false); err == nil {
		t.Fatal("consolidated without a verdict")
	}
	if err := g.verdict(key, r, true); err != nil {
		t.Fatal(err)
	}
	if err := g.f.ConsolidateCheckpoint(key, "cp", r.Revision, false); err != nil {
		t.Fatal(err)
	}
	if a, _ := g.disk("a.txt"); a != "new-a" {
		t.Fatalf("a.txt = %q", a)
	}
	if _, ok := g.disk("b.txt"); ok {
		t.Fatal("b.txt survived consolidation")
	}
	if c, _ := g.disk("c.txt"); c != "new-c" {
		t.Fatalf("c.txt = %q", c)
	}
}

func TestMaintenanceFailingVerdictKeepsDeltaForCoordinatorDiscard(t *testing.T) {
	g := newGCRig(t)
	key := g.bind("collector", gcSpecialist, "a.txt", "b.txt", "c.txt")
	r := g.stage(key)
	if err := g.verdict(key, r, false); err != nil {
		t.Fatal(err)
	}
	// A failing gate records the rejection but retains the staged delta: the
	// GC coordinator owns the full-cycle discard, never the verdict itself.
	if got := g.journalOps(key, vfs.OpRollback); len(got) != 0 {
		t.Fatalf("rollback entries = %v; verdict must not discard", got)
	}
	verdicts := 0
	for _, e := range g.f.JournalPage("s", "u", -1, 500) {
		if e.Operation == "verdict" && e.Outcome == "rejected" {
			verdicts++
		}
	}
	if verdicts != 1 {
		t.Fatalf("rejected verdict entries = %d", verdicts)
	}
	seen, err := g.f.Apply(vfs.Operation{Key: key, CallID: "look", ExpectedRevision: r.Revision, Action: vfs.OpRead, Path: "a.txt"})
	if err != nil || string(seen.Content) != "new-a" {
		t.Fatalf("staged view = %q, %v; want retained delta", seen.Content, err)
	}
	if err = g.f.ConsolidateCheckpoint(key, "cp", seen.Revision, false); err == nil {
		t.Fatal("rejected delta consolidated")
	}
}

func TestFailingVerdictStillKeepsExecutionDelta(t *testing.T) {
	g := newGCRig(t)
	key := g.bind("author", "dev", "a.txt", "b.txt", "c.txt")
	r := g.stage(key)
	if err := g.verdict(key, r, false); err != nil {
		t.Fatal(err)
	}
	if got := g.journalOps(key, vfs.OpRollback); len(got) != 0 {
		t.Fatalf("execution delta discarded: %v", got)
	}
	seen, err := g.f.Apply(vfs.Operation{Key: key, CallID: "look", ExpectedRevision: r.Revision, Action: vfs.OpRead, Path: "a.txt"})
	if err != nil || string(seen.Content) != "new-a" {
		t.Fatalf("execution view = %q, %v", seen.Content, err)
	}
}

func TestMaintenanceCannotVerifyItself(t *testing.T) {
	g := newGCRig(t)
	key := g.bind("collector", gcSpecialist, "a.txt")
	r, err := g.f.Apply(vfs.Operation{Key: key, CallID: "w", Action: vfs.OpPatch, Path: "a.txt", Content: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	peer := g.bind("peer-collector", gcSpecialist)
	for _, judge := range []vfs.AgentID{key, peer} {
		if err = g.f.Verify(judge, key, "self-"+string(judge), r.Revision, r.DeltaHash, true, ""); !errors.Is(err, vfs.ErrScopeDenied) {
			t.Fatalf("collector verdict by %q = %v; want explicit-capability denial (collector holds no verify grant)", judge, err)
		}
	}
}

// A cycle judge delegated again replaces its own verdict: its withdrawn
// failure no longer gates the cycle's consolidation.
func TestMaintenanceRetriedJudgeGatesWithItsLatestVerdict(t *testing.T) {
	g := newGCRig(t)
	key := g.bind("collector", gcSpecialist, "a.txt", "b.txt", "c.txt")
	r := g.stage(key)
	if err := g.verdict(key, r, false); err != nil {
		t.Fatal(err)
	}
	retry := vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "2", AgentID: vfs.AgentID("judge-" + string(key)), Specialist: "verify", CycleID: "cycle-1", MandateClass: "dead-code"}
	gate, err := g.f.AssignVerifier(retry, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.f.ConsolidateCheckpoint(key, "cp", r.Revision, false); err == nil {
		t.Fatal("consolidated over the judge's failing verdict")
	}
	if err := g.f.Verify(gate, key, "verdict-retry", r.Revision, r.DeltaHash, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := g.f.ConsolidateCheckpoint(key, "cp", r.Revision, false); err != nil {
		t.Fatalf("ConsolidateCheckpoint() = %v; the judge's latest verdict passes", err)
	}
}
