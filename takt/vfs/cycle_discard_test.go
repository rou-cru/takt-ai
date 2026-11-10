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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// consolidatedCycle stages the rig's delta under agent and consolidates it, so
// a.txt is rewritten, b.txt deleted and c.txt created on disk.
func consolidatedCycle(g *gcRig, agent string) vfs.AgentID {
	g.t.Helper()
	key := g.bind(agent, gcSpecialist, "a.txt", "b.txt", "c.txt")
	r := g.stage(key)
	if err := g.verdict(key, r, true); err != nil {
		g.t.Fatal(err)
	}
	if err := g.f.ConsolidateCheckpoint(key, "cp-"+agent, r.Revision, false); err != nil {
		g.t.Fatal(err)
	}
	return key
}

// assertPreCycle fails unless the workspace holds exactly what it held before
// the cycle ran: the rewrite undone, the deletion undone, the creation gone.
func assertPreCycle(g *gcRig) {
	g.t.Helper()
	if a, ok := g.disk("a.txt"); !ok || a != "old-a" {
		g.t.Errorf("a.txt = %q (present %v), want %q restored", a, ok, "old-a")
	}
	if b, ok := g.disk("b.txt"); !ok || b != "old-b" {
		g.t.Errorf("b.txt = %q (present %v), want %q brought back", b, ok, "old-b")
	}
	if _, ok := g.disk("c.txt"); ok {
		g.t.Error("c.txt survived the discard; the cycle created it")
	}
}

func TestDiscardCycleRestoresPreCycleState(t *testing.T) {
	g := newGCRig(t)
	consolidatedCycle(g, "collector")

	restored, err := g.f.DiscardCycle("cycle-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.txt", "b.txt", "c.txt"}; !slices.Equal(restored, want) {
		t.Errorf("restored = %v, want %v", restored, want)
	}
	assertPreCycle(g)

	// The reversion is visible and attributed, so it can feed the problem rate
	// (PR-MNT-9) and the cycle's mandate class (PR-MNT-31).
	var discarded []string
	for _, e := range g.f.JournalPage("s", "u", -1, 500) {
		if e.Outcome == "discarded" {
			discarded = append(discarded, e.Path)
			if e.CycleID != "cycle-1" || e.MandateClass != "dead-code" {
				t.Errorf("entry %s lost its cycle attribution: %+v", e.Ref(), e)
			}
		}
	}
	if want := []string{"a.txt", "b.txt", "c.txt"}; !slices.Equal(discarded, want) {
		t.Errorf("journaled discards = %v, want %v", discarded, want)
	}
	// The cycle is resolved: it cannot be discarded twice.
	if _, err = g.f.DiscardCycle("cycle-1"); !errors.Is(err, vfs.ErrUnknownCycle) {
		t.Errorf("second discard = %v, want ErrUnknownCycle", err)
	}
}

// A cycle consolidates more than one delta (PR-MNT-25), and discarding it means
// going back to before the first, not before the last.
func TestDiscardCycleSpansEveryDeltaOfTheCycle(t *testing.T) {
	g := newGCRig(t)
	consolidatedCycle(g, "collector")

	second := g.bind("collector-2", gcSpecialist, "a.txt")
	r, err := g.f.Apply(vfs.Operation{Key: second, CallID: "second", Action: vfs.OpPatch, Path: "a.txt", Content: []byte("newer-a")})
	if err != nil {
		t.Fatal(err)
	}
	if err = g.verdict(second, r, true); err != nil {
		t.Fatal(err)
	}
	if err = g.f.ConsolidateCheckpoint(second, "cp-2", r.Revision, false); err != nil {
		t.Fatal(err)
	}
	if a, _ := g.disk("a.txt"); a != "newer-a" {
		t.Fatalf("a.txt = %q before discard", a)
	}
	if _, err = g.f.DiscardCycle("cycle-1"); err != nil {
		t.Fatal(err)
	}
	assertPreCycle(g)
}

func TestDiscardCycleRefusesWhenWorkspaceDiverged(t *testing.T) {
	g := newGCRig(t)
	consolidatedCycle(g, "collector")

	// Something wrote after consolidation; restoring would clobber it.
	if err := os.WriteFile(filepath.Join(g.root, "a.txt"), []byte("someone-else"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := g.f.DiscardCycle("cycle-1")
	if !errors.Is(err, vfs.ErrCycleDiverged) {
		t.Fatalf("discard = %v, want ErrCycleDiverged", err)
	}
	if a, _ := g.disk("a.txt"); a != "someone-else" {
		t.Errorf("a.txt = %q, a refused restore must touch nothing", a)
	}
	if _, ok := g.disk("c.txt"); !ok {
		t.Error("c.txt removed by a refused restore")
	}
	// Refusing leaves the cycle discardable once the divergence is resolved.
	if err = os.WriteFile(filepath.Join(g.root, "a.txt"), []byte("new-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = g.f.DiscardCycle("cycle-1"); err != nil {
		t.Fatal(err)
	}
	assertPreCycle(g)
}

func TestCompleteCycleReleasesRetainedState(t *testing.T) {
	g := newGCRig(t)
	consolidatedCycle(g, "collector")

	if err := g.f.CompleteCycle("cycle-1"); err != nil {
		t.Fatal(err)
	}
	if err := g.f.CompleteCycle("cycle-1"); err != nil {
		t.Errorf("completing twice = %v, want idempotent", err)
	}
	if _, err := g.f.DiscardCycle("cycle-1"); !errors.Is(err, vfs.ErrUnknownCycle) {
		t.Errorf("discard after completion = %v, want ErrUnknownCycle", err)
	}
	if a, _ := g.disk("a.txt"); a != "new-a" {
		t.Errorf("a.txt = %q, completing must not touch the workspace", a)
	}
}

// stageReedit stages one ordinary re-edit of path under a fresh agent, then
// consolidates it: consolidation frees the path's claim, so the next re-edit
// can bind it fresh. Returns the single journal entry the apply wrote;
// consolidation journals its own flush beside it.
func stageReedit(g *gcRig, unit string, agent vfs.AgentID, path string, op vfs.OperationType) vfs.JournalEntry {
	g.t.Helper()
	key, err := g.f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: unit, AttemptID: "1", AgentID: agent, Specialist: "dev", InvariantsHash: "inv"}, []string{path})
	if err != nil {
		g.t.Fatal(err)
	}
	r, err := g.f.Apply(vfs.Operation{Key: key, CallID: unit, Action: op, Path: path, Content: []byte("reedit-" + path)})
	if err != nil {
		g.t.Fatal(err)
	}
	if err := g.verdict(key, r, true); err != nil {
		g.t.Fatal(err)
	}
	if err := g.f.ConsolidateCheckpoint(key, "cp-"+unit, r.Revision, false); err != nil {
		g.t.Fatal(err)
	}
	var applied []vfs.JournalEntry
	for _, e := range g.f.JournalPage("s", unit, -1, 500) {
		if e.Operation == op {
			applied = append(applied, e)
		}
	}
	if len(applied) != 1 {
		g.t.Fatalf("journal for %s = %d %s entries, want the one re-edit", unit, len(applied), op)
	}
	return applied[0]
}

// TestReeditAttributionSurvivesCompletion checks that a completed cycle's
// paths stay attributed (PR-MNT-31): a later re-edit's journal entry still
// names the cycle and mandate class that last consolidated the path, even
// though the cycle itself can no longer be discarded.
func TestReeditAttributionSurvivesCompletion(t *testing.T) {
	g := newGCRig(t)
	consolidatedCycle(g, "collector")

	// Not yet completed: a re-edit is already attributed, and the cycle is
	// still discardable.
	if e := stageReedit(g, "reedit-open", "reditor-open", "a.txt", vfs.OpPatch); e.ReeditOfCycleID != "cycle-1" || e.ReeditOfMandateClass != "dead-code" {
		t.Fatalf("re-edit before completion = %+v, want reedit of cycle-1/dead-code", e)
	}

	if err := g.f.CompleteCycle("cycle-1"); err != nil {
		t.Fatal(err)
	}

	if e := stageReedit(g, "reedit-closed", "reditor-closed", "a.txt", vfs.OpPatch); e.ReeditOfCycleID != "cycle-1" || e.ReeditOfMandateClass != "dead-code" {
		t.Fatalf("re-edit after completion = %+v, want reedit of cycle-1/dead-code", e)
	}
	if e := stageReedit(g, "reedit-foreign", "reditor-foreign", "never-touched.txt", vfs.OpCreate); e.ReeditOfCycleID != "" || e.ReeditOfMandateClass != "" {
		t.Errorf("a path no cycle ever consolidated got attributed: %+v", e)
	}

	// Completion still blocks discard.
	if _, err := g.f.DiscardCycle("cycle-1"); !errors.Is(err, vfs.ErrUnknownCycle) {
		t.Errorf("discard after completion = %v, want ErrUnknownCycle", err)
	}
}

func TestDiscardCycleSurvivesReopen(t *testing.T) {
	g := newGCRig(t)
	consolidatedCycle(g, "collector")
	if err := g.f.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := vfs.Open(g.root, g.state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	g.f = reopened

	if _, err = reopened.DiscardCycle("cycle-1"); err != nil {
		t.Fatal(err)
	}
	assertPreCycle(g)
}

// Ordinary work carries no cycle, so nothing is retained and consolidation
// keeps behaving exactly as before.
func TestWorkOutsideACycleRetainsNothing(t *testing.T) {
	g := newGCRig(t)
	key, err := g.f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: "dev", Specialist: "dev", InvariantsHash: "inv"}, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := g.f.Apply(vfs.Operation{Key: key, CallID: "dev-1", Action: vfs.OpPatch, Path: "a.txt", Content: []byte("dev-a")})
	if err != nil {
		t.Fatal(err)
	}
	if err = g.verdict(key, r, true); err != nil {
		t.Fatal(err)
	}
	if err = g.f.ConsolidateCheckpoint(key, "cp-dev", r.Revision, false); err != nil {
		t.Fatal(err)
	}
	if a, _ := g.disk("a.txt"); a != "dev-a" {
		t.Fatalf("a.txt = %q", a)
	}
	if _, err = g.f.DiscardCycle("cycle-1"); !errors.Is(err, vfs.ErrUnknownCycle) {
		t.Errorf("discard = %v, want ErrUnknownCycle", err)
	}
}

// A path two cycles consolidated is traced to the latest one, every time:
// each re-edit's journal entry names the cycle that last consolidated the
// path, open or completed, before or after a reopen.
func TestReeditAttributionNamesTheLatestCycle(t *testing.T) {
	g := newGCRig(t)
	consolidatedCycle(g, "collector")
	author := vfs.Identity{SessionID: "s", WorkUnitID: "u2", AttemptID: "1", AgentID: "collector-2", Specialist: gcSpecialist, CycleID: "cycle-2", MandateClass: "complexity"}
	key, err := g.f.Bind(author, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := g.f.Apply(vfs.Operation{Key: key, CallID: "second", Action: vfs.OpPatch, Path: "a.txt", Content: []byte("newer-a")})
	if err != nil {
		t.Fatal(err)
	}
	judge := vfs.Identity{SessionID: "s", WorkUnitID: "u2/verify", AttemptID: "1", AgentID: "judge-2", Specialist: "verify", CycleID: "cycle-2", MandateClass: "complexity"}
	gate, err := g.f.AssignVerifier(judge, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.f.Verify(gate, key, "verdict-2", r.Revision, r.DeltaHash, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := g.f.ConsolidateCheckpoint(key, "cp-2", r.Revision, false); err != nil {
		t.Fatal(err)
	}

	// Both cycles open: a re-edit of a.txt names the latest one, cycle-2; a
	// re-edit of b.txt names cycle-1, the only one that touched it.
	if e := stageReedit(g, "reedit-open-a", "reditor-a", "a.txt", vfs.OpPatch); e.ReeditOfCycleID != "cycle-2" || e.ReeditOfMandateClass != "complexity" {
		t.Fatalf("re-edit of a.txt = %+v, want reedit of the latest cycle-2/complexity", e)
	}
	if e := stageReedit(g, "reedit-open-b", "reditor-b", "b.txt", vfs.OpCreate); e.ReeditOfCycleID != "cycle-1" || e.ReeditOfMandateClass != "dead-code" {
		t.Fatalf("re-edit of b.txt = %+v, want reedit of cycle-1/dead-code", e)
	}

	// Completed and reopened: the latest cycle still wins, every time.
	if err := g.f.CompleteCycle("cycle-1"); err != nil {
		t.Fatal(err)
	}
	if err := g.f.CompleteCycle("cycle-2"); err != nil {
		t.Fatal(err)
	}
	if err := g.f.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := vfs.Open(g.root, g.state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	g.f = reopened

	late, err := g.f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "u3", AttemptID: "1", AgentID: "reditor-late", Specialist: "dev", InvariantsHash: "inv"}, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	var res vfs.OperationResult
	for i := range 20 {
		if res, err = g.f.Apply(vfs.Operation{Key: late, CallID: fmt.Sprintf("reedit-%d", i), Action: vfs.OpPatch, Path: "a.txt", Content: []byte("late-a"), ExpectedRevision: res.Revision}); err != nil {
			t.Fatal(err)
		}
	}
	page := g.f.JournalPage("s", "u3", -1, 500)
	if len(page) != 20 {
		t.Fatalf("re-edit journal = %d entries, want 20", len(page))
	}
	for _, e := range page {
		if e.ReeditOfCycleID != "cycle-2" || e.ReeditOfMandateClass != "complexity" {
			t.Fatalf("%s attributed to %q/%q, want the latest cycle-2/complexity every time", e.Ref(), e.ReeditOfCycleID, e.ReeditOfMandateClass)
		}
	}
}
