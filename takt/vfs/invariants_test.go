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

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// directives writes the governing document the tests declare as the invariant
// set and returns its workspace path.
func directives(t *testing.T, root, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return "AGENTS.md"
}

// TestVerdictBoundToInvariantSetVersion: a verdict applies only to the invariant
// set it was judged against, so editing a governing document after it leaves
// the evidence stale: it no longer asks for acceptance (PR-VFS-CSL-7).
func TestVerdictBoundToInvariantSetVersion(t *testing.T) {
	f, root, _ := newStore(t)
	set := vfs.InvariantSet{directives(t, root, "verify before you consolidate")}

	author, err := f.Bind(vfs.Identity{SessionID: testSession, WorkUnitID: "u", AgentID: "dev", Specialist: "dev", Invariants: set}, []string{"app.go"})
	if err != nil {
		t.Fatal(err)
	}
	// The verifier declares nothing: it inherits the attempt's applicable set,
	// so it cannot be handed a different one.
	verifier, err := f.AssignVerifier(vfs.Identity{SessionID: testSession, WorkUnitID: "u", AgentID: "judge", Specialist: "verify"}, author)
	if err != nil {
		t.Fatal(err)
	}
	judged, _ := f.BindingIdentity(verifier)
	authored, _ := f.BindingIdentity(author)
	if judged.InvariantsHash != authored.InvariantsHash || judged.InvariantsHash == "" {
		t.Fatalf("verifier invariants %q; want the author's %q", judged.InvariantsHash, authored.InvariantsHash)
	}

	staged := apply(t, f, applyCase{author, "c1", 0, vfs.OpCreate, "app.go", "package main"})
	if err = f.Verify(verifier, author, "v1", staged.Revision, staged.DeltaHash, false, "breaks the directive"); err != nil {
		t.Fatal(err)
	}

	// Under the judged set the failing verdict is current and asks for acceptance.
	if err = f.ConsolidateCheckpoint(author, "cp", staged.Revision, false); !errors.Is(err, vfs.ErrFailingVerdict) {
		t.Fatalf("consolidation under the judged invariant set = %v; want ErrFailingVerdict", err)
	}
	if _, err = os.Stat(filepath.Join(root, "app.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused delta reached disk")
	}

	// Once the set changes the verdict no longer describes the governing text.
	directives(t, root, "verify before you consolidate, and never on a Friday")
	if err = f.ConsolidateCheckpoint(author, "cp", staged.Revision, false); err != nil {
		t.Fatalf("consolidation under a changed invariant set: %v", err)
	}
}

// TestAttemptIdentitySurvivesRestart: the attempt identity is the execution
// history's and is durable, so a restart neither restarts the count nor reuses
// an identity for different work (PR-DAG-REP-3, PR-HAR-22).
func TestAttemptIdentitySurvivesRestart(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	f, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	c, first := delegate(t, f, state, testSession, "dev", "u", "app.go")
	if id, _ := f.BindingIdentity(first); id.AttemptID != "1" {
		t.Fatalf("first attempt = %q; want 1", id.AttemptID)
	}
	write(t, f, c, "c1", "app.go", "package main")
	if err = f.DiscardAs(orchestrator(testSession), first); err != nil {
		t.Fatal(err)
	}
	settle(t, state, testSession, "u", "1")
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	f, err = vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	admit(t, state, testSession, "u", "2")
	_, retry := delegate(t, f, state, testSession, "dev", "u", "app.go")
	if id, _ := f.BindingIdentity(retry); id.AttemptID != "2" || retry == first {
		t.Fatalf("attempt after restart = %q (key %s); want 2 under a new key", id.AttemptID, retry)
	}
}

// TestBindJoinsAdmittedAttempt: delegated work carries the attempt its
// admission issued, and a verifier gate of the same unit adopts that same
// attempt, so a verdict names the unit and attempt the history recorded
// (PR-VFS-CSL-7).
func TestBindJoinsAdmittedAttempt(t *testing.T) {
	f, root, state := newStore(t)
	directives(t, root, "retries continue the unit")
	admit(t, state, testSession, "impl", "3")
	_, author := delegate(t, f, state, testSession, "dev", "impl", "app.go")
	admit(t, state, testSession, "impl/verify", "3", "verify")
	_, gates := judge(t, f, state, testSession, "verify", "impl/verify", author)
	authored, _ := f.BindingIdentity(author)
	judged, _ := f.BindingIdentity(gates[0].Key)
	if authored.AttemptID != "3" || judged.AttemptID != "3" {
		t.Fatalf("attempts author=%q verifier=%q; want the admitted 3 for both", authored.AttemptID, judged.AttemptID)
	}
}

// TestVerifierReadsAuthorsStagedView: the gate reads the exact staged delta its
// verdict covers, through the author's view, and cannot write through it.
func TestVerifierReadsAuthorsStagedView(t *testing.T) {
	f, _, state := newStore(t)
	dev, author := delegate(t, f, state, testSession, "dev", "u", "app.go")
	write(t, f, dev, "c1", "app.go", "package staged")
	gate, _ := judge(t, f, state, testSession, "verify", "u/verify", author)
	if read := readAs(t, f, gate, "r1", "app.go", ""); string(read.Content) != "package staged" {
		t.Fatalf("verifier read = %q; want the author's staged content", read.Content)
	}
	if _, err := f.Operate(gate, vfs.CallerOperation{CallID: "w1", Action: vfs.OpCreate, Path: "app.go", Content: []byte("x")}); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("verifier write = %v; want ErrIdentity", err)
	}
}

// TestVerifierUnitJudgesSeveralAuthors: one verifier unit holds a gate per
// author of the round it judges, and a later assignment over one author
// supersedes the earlier gate over it.
func TestVerifierUnitJudgesSeveralAuthors(t *testing.T) {
	f, _, state := newStore(t)
	var authors [2]vfs.AgentID
	for i, specialist := range []string{"dev", "fix"} {
		_, authors[i] = delegate(t, f, state, testSession, specialist, specialist, specialist+".go")
	}
	_, gates := judge(t, f, state, testSession, "verify", "round-verify", authors[0], authors[1])
	if len(gates) != 2 || gates[0].AuthorKey == gates[1].AuthorKey {
		t.Fatalf("gates = %+v; want one per author", gates)
	}
	_, again := judge(t, f, state, testSession, "verify", "round-verify", authors[0])
	var replaced vfs.AgentID
	for _, gate := range gates {
		if gate.AuthorKey == authors[0] {
			replaced = gate.Key
		}
	}
	if len(again) != 2 || slicesContainKey(again, replaced) {
		t.Fatalf("gates after reassigning %s = %+v; want the earlier gate %s superseded", authors[0], again, replaced)
	}
}

func slicesContainKey(gates []vfs.AdoptedGate, key vfs.AgentID) bool {
	for _, gate := range gates {
		if gate.Key == key {
			return true
		}
	}
	return false
}
