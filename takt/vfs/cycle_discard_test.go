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
	if err := g.f.ConsolidateCheckpoint(key, "cp-"+agent, r.Revision); err != nil {
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
	if err = g.f.ConsolidateCheckpoint(second, "cp-2", r.Revision); err != nil {
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

// TestConsolidatedByAttributesAfterCompletion checks that a completed cycle's
// paths stay attributable (PR-MNT-31): a caller about to stage a re-edit can
// still learn which cycle and mandate class last consolidated the path, even
// though the cycle itself can no longer be discarded.
func TestConsolidatedByAttributesAfterCompletion(t *testing.T) {
	g := newGCRig(t)
	consolidatedCycle(g, "collector")

	// Not yet completed: still attributable, still discardable.
	if id, class, ok := g.f.ConsolidatedBy("a.txt"); !ok || id != "cycle-1" || class != "dead-code" {
		t.Fatalf("ConsolidatedBy before completion = %q %q %v", id, class, ok)
	}

	if err := g.f.CompleteCycle("cycle-1"); err != nil {
		t.Fatal(err)
	}

	id, class, ok := g.f.ConsolidatedBy("a.txt")
	if !ok || id != "cycle-1" || class != "dead-code" {
		t.Fatalf("ConsolidatedBy after completion = %q %q %v, want cycle-1/dead-code/true", id, class, ok)
	}
	if _, _, ok = g.f.ConsolidatedBy("never-touched.txt"); ok {
		t.Error("ConsolidatedBy matched a path no cycle ever consolidated")
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
	if err = g.f.ConsolidateCheckpoint(key, "cp-dev", r.Revision); err != nil {
		t.Fatal(err)
	}
	if a, _ := g.disk("a.txt"); a != "dev-a" {
		t.Fatalf("a.txt = %q", a)
	}
	if _, err = g.f.DiscardCycle("cycle-1"); !errors.Is(err, vfs.ErrUnknownCycle) {
		t.Errorf("discard = %v, want ErrUnknownCycle", err)
	}
}
