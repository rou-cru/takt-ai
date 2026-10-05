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

package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// TestVerdictBoundToInvariantSetVersion: a pass applies only to the invariant
// set it was judged against, so editing a governing document after the verdict
// requires new verification before the evidence governs (PR-VFS-CSL-7).
func TestVerdictBoundToInvariantSetVersion(t *testing.T) {
	f, root, _ := durable(t)
	set := InvariantSet{directives(t, root, "verify before you consolidate")}

	author, err := f.Bind(Identity{SessionID: "s", WorkUnitID: "u", AgentID: "dev", Specialist: "dev", Invariants: set}, []string{"app.go"})
	if err != nil {
		t.Fatal(err)
	}
	// The verifier declares nothing: it inherits the attempt's applicable set,
	// so it cannot be handed a different one.
	verifier, err := f.AssignVerifier(Identity{SessionID: "s", WorkUnitID: "u", AgentID: "judge", Specialist: "verify"}, author)
	if err != nil {
		t.Fatal(err)
	}
	judged, _ := f.BindingIdentity(verifier)
	authored, _ := f.BindingIdentity(author)
	if judged.InvariantsHash != authored.InvariantsHash || !strings.HasPrefix(judged.InvariantsHash, invariantScheme+":") {
		t.Fatalf("verifier invariants %q; want the author's %q", judged.InvariantsHash, authored.InvariantsHash)
	}

	staged := apply(t, f, applyCase{author, "c1", 0, OpCreate, "app.go", "package main"})
	if err = f.Verify(verifier, author, "v1", staged.Revision, staged.DeltaHash, true, ""); err != nil {
		t.Fatal(err)
	}

	directives(t, root, "verify before you consolidate, and never on a Friday")
	if err = f.ConsolidateCheckpoint(author, "cp", staged.Revision); !errors.Is(err, ErrInvalidVerdict) {
		t.Fatalf("consolidation under a changed invariant set = %v; want ErrInvalidVerdict", err)
	}
	if _, err = os.Stat(filepath.Join(root, "app.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ungoverned delta reached disk")
	}

	// The same evidence governs again once its set reads as it did when judged.
	directives(t, root, "verify before you consolidate")
	if err = f.ConsolidateCheckpoint(author, "cp", staged.Revision); err != nil {
		t.Fatalf("consolidation under the judged invariant set: %v", err)
	}
}

// TestAttemptIdentitySurvivesRestart: the attempt identity is the harness's and
// is durable, so a restart neither restarts the count nor reuses an identity for
// different work (PR-DAG-REP-3, PR-HAR-22).
func TestAttemptIdentitySurvivesRestart(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	f, err := Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := Identity{SessionID: "s", WorkUnitID: "u", AgentID: "dev", Specialist: "dev", Invariants: InvariantSet{directives(t, root, "one attempt at a time")}}
	first, err := f.Bind(dispatch, []string{"app.go"})
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := f.BindingIdentity(first); id.AttemptID != "1" {
		t.Fatalf("first attempt = %q; want 1", id.AttemptID)
	}
	apply(t, f, applyCase{first, "c1", 0, OpCreate, "app.go", "package main"})
	apply(t, f, applyCase{first, "c2", 1, OpRollback, "", ""})
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	f, err = Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	retry, err := f.Bind(dispatch, []string{"app.go"})
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := f.BindingIdentity(retry); id.AttemptID != "2" {
		t.Fatalf("attempt after restart = %q; want 2", id.AttemptID)
	}
}

// TestBindJoinsAdmittedAttempt: delegated work carries the attempt its
// admission issued, and the verifier judging it joins that same attempt, so a
// verdict names the unit and attempt the history recorded (PR-VFS-CSL-7).
func TestBindJoinsAdmittedAttempt(t *testing.T) {
	f, root, _ := durable(t)
	set := InvariantSet{directives(t, root, "retries continue the unit")}
	author, err := f.Bind(Identity{SessionID: "s", WorkUnitID: "impl", AttemptID: "3", AgentID: "dev", Specialist: "dev", Invariants: set}, []string{"app.go"})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := f.AssignVerifier(Identity{SessionID: "s", WorkUnitID: "impl", AttemptID: "3", AgentID: "judge", Specialist: "verify"}, author)
	if err != nil {
		t.Fatal(err)
	}
	authored, _ := f.BindingIdentity(author)
	judged, _ := f.BindingIdentity(verifier)
	if authored.AttemptID != "3" || judged.AttemptID != "3" {
		t.Fatalf("attempts author=%q verifier=%q; want the admitted 3 for both", authored.AttemptID, judged.AttemptID)
	}
}

// TestVerifierReadsAuthorsStagedView: the gate reads the exact staged delta its
// verdict covers, through the author's view, and nothing else can.
func TestVerifierReadsAuthorsStagedView(t *testing.T) {
	f, root, _ := durable(t)
	set := InvariantSet{directives(t, root, "judge what was staged")}
	author, err := f.Bind(Identity{SessionID: "s", WorkUnitID: "u", AgentID: "dev", Specialist: "dev", Invariants: set}, []string{"app.go"})
	if err != nil {
		t.Fatal(err)
	}
	staged := apply(t, f, applyCase{author, "c1", 0, OpCreate, "app.go", "package staged"})
	verifier, err := f.AssignVerifier(Identity{SessionID: "s", WorkUnitID: "u", AgentID: "judge", Specialist: "verify"}, author)
	if err != nil {
		t.Fatal(err)
	}
	read, err := f.ReadAs(Operation{Key: verifier, CallID: "r1", ExpectedRevision: staged.Revision, Action: OpRead, Path: "app.go"}, author)
	if err != nil || string(read.Content) != "package staged" {
		t.Fatalf("verifier read = %q, %v; want the author's staged content", read.Content, err)
	}
	if _, err := f.ReadAs(Operation{Key: verifier, CallID: "w1", ExpectedRevision: staged.Revision, Action: OpCreate, Path: "app.go", Content: []byte("x")}, author); err == nil {
		t.Fatal("a verifier must not write through the author's view")
	}
}

// TestVerifierUnitJudgesSeveralAuthors: one verifier unit holds a gate per
// author of the round it judges and adopts each by its own author key.
func TestVerifierUnitJudgesSeveralAuthors(t *testing.T) {
	f, root, _ := durable(t)
	set := InvariantSet{directives(t, root, "judge the round together")}
	var authors [2]AgentID
	for i, unit := range []string{"a", "b"} {
		key, err := f.Bind(Identity{SessionID: "s", WorkUnitID: unit, AttemptID: "1", AgentID: AgentID("dev-" + unit), Specialist: "dev", Invariants: set}, []string{unit + ".go"})
		if err != nil {
			t.Fatal(err)
		}
		authors[i] = key
	}
	gate := Identity{SessionID: "s", WorkUnitID: "round-verify", AttemptID: "1", AgentID: "verify", Specialist: "verify", Invariants: set}
	for _, author := range authors {
		if _, err := f.AssignVerifier(gate, author); err != nil {
			t.Fatalf("assign gate for %s: %v", author, err)
		}
	}
	if _, err := f.AssignVerifier(gate, authors[0]); !errors.Is(err, ErrIdentity) {
		t.Fatalf("second gate for the same author = %v; want ErrIdentity", err)
	}
	for _, author := range authors {
		bound := gate
		bound.GateAuthorKey = author
		verifier, err := f.Bind(bound, nil)
		if err != nil {
			t.Fatalf("adopt gate for %s: %v", author, err)
		}
		if id, _ := f.BindingIdentity(verifier); id.GateAuthorKey != author {
			t.Fatalf("adopted gate judges %q; want %q", id.GateAuthorKey, author)
		}
	}
}
