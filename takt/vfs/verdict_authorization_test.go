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
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// verdictSession is the root session every verdict binding runs in.
const verdictSession = "rq34-session"

// verdictStore opens a private store over a fresh workspace.
func verdictStore(t *testing.T) (f *vfs.FS, root, state string) {
	t.Helper()
	root = t.TempDir()
	state = filepath.Join(t.TempDir(), "private")
	f, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, root, state
}

// verdictBind binds an ordinary author over scope and returns its key.
func verdictBind(t *testing.T, f *vfs.FS, unit, attempt, agent string, invariants vfs.InvariantSet, scope ...string) vfs.AgentID {
	t.Helper()
	key, err := f.Bind(vfs.Identity{
		SessionID:  verdictSession,
		WorkUnitID: unit,
		AttemptID:  attempt,
		AgentID:    vfs.AgentID(agent),
		Specialist: "dev",
		Invariants: invariants,
	}, scope)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// verdictMaintainer binds a maintenance-cycle author whose consolidation a
// current passing verdict gates.
func verdictMaintainer(t *testing.T, f *vfs.FS, unit, attempt, agent string, scope ...string) vfs.AgentID {
	t.Helper()
	key, err := f.Bind(vfs.Identity{
		SessionID:    verdictSession,
		WorkUnitID:   unit,
		AttemptID:    attempt,
		AgentID:      vfs.AgentID(agent),
		Specialist:   "simplify",
		CycleID:      "cycle-34",
		MandateClass: "dead-code",
	}, scope)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// verdictGate assigns a verifier gate over author and returns its key.
func verdictGate(t *testing.T, f *vfs.FS, unit, attempt, agent string, author vfs.AgentID) vfs.AgentID {
	t.Helper()
	key, err := f.AssignVerifier(vfs.Identity{
		SessionID:  verdictSession,
		WorkUnitID: unit,
		AttemptID:  attempt,
		AgentID:    vfs.AgentID(agent),
		Specialist: "verify",
	}, author)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// verdictCreate stages a new file under key.
func verdictCreate(t *testing.T, f *vfs.FS, key vfs.AgentID, call, path, content string) vfs.OperationResult {
	t.Helper()
	r, err := f.Apply(vfs.Operation{Key: key, CallID: call, Action: vfs.OpCreate, Path: path, Content: []byte(content)})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// verdictPatch restages path with new content at the expected revision.
func verdictPatch(t *testing.T, f *vfs.FS, key vfs.AgentID, call string, expected uint64, path, content string) vfs.OperationResult {
	t.Helper()
	r, err := f.Apply(vfs.Operation{Key: key, CallID: call, ExpectedRevision: expected, Action: vfs.OpPatch, Path: path, Content: []byte(content)})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// verdictJudge is the gate's read of its author's staged view.
func verdictJudge(t *testing.T, f *vfs.FS, gate, author vfs.AgentID) vfs.StagedView {
	t.Helper()
	view, err := f.JudgeDelta(gate, author)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

// verdictReopen closes f's store and reopens it over the same workspace.
func verdictReopen(t *testing.T, f *vfs.FS, root, state string) *vfs.FS {
	t.Helper()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

// verdictReplaceState rewrites the persisted state record after test surgery.
func verdictReplaceState(t *testing.T, f *vfs.FS, s vfs.StoredState) {
	t.Helper()
	blob, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ExecState("UPDATE state SET data = x'" + hex.EncodeToString(blob) + "' WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
}

// A verdict is journaled under the verifier's own identity and, naming the
// evaluated unit and attempt, still authorizes the author's consolidation
// after the store is reopened.
func TestVerdictAuthorizesConsolidationAfterReopen(t *testing.T) {
	f, root, state := verdictStore(t)
	author := verdictBind(t, f, "rq34-author-unit", "3", "rq34-author",
		vfs.InvariantSet{vfs.EngramInvariant(15187), vfs.EngramInvariant(15192)}, "rq34.txt")
	gate := verdictGate(t, f, "rq34-author-unit", "1", "rq34-judge", author)
	r := verdictCreate(t, f, author, "rq34-create", "rq34.txt", "rq34-content")
	view := verdictJudge(t, f, gate, author)
	if view.Revision != r.Revision || view.Hash != r.DeltaHash {
		t.Fatalf("judged view %+v does not cover the staged delta %+v", view, r)
	}
	if err := f.Verify(gate, author, "rq34-verdict", r.Revision, r.DeltaHash, true, "clean"); err != nil {
		t.Fatal(err)
	}
	gateID, ok := f.BindingIdentity(gate)
	if !ok {
		t.Fatalf("gate binding %q missing", gate)
	}
	var verdictEntry vfs.JournalEntry
	for _, e := range f.JournalPage(verdictSession, "rq34-author-unit", -1, 50) {
		if e.Operation == "verdict" {
			verdictEntry = e
		}
	}
	if verdictEntry.Outcome != "passed" || verdictEntry.Agent != gateID.AgentID {
		t.Fatalf("verdict journal entry = %+v", verdictEntry)
	}
	reopened := verdictReopen(t, f, root, state)
	if err := reopened.ConsolidateCheckpoint(author, "rq34-checkpoint", r.Revision, false); err != nil {
		t.Fatalf("consolidation after reopen = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "rq34.txt"))
	if err != nil || string(data) != "rq34-content" {
		t.Fatalf("consolidated bytes = %q, %v", data, err)
	}
}

// verdictPrelaunched stages content under an assigned, not yet adopted,
// binding judged by a gate's verdict; adopt moves the binding to attempt 2.
func verdictPrelaunched(t *testing.T, f *vfs.FS, identity vfs.Identity, pass bool, finding string) (author vfs.AgentID, r vfs.OperationResult, adopt func()) {
	t.Helper()
	author, err := f.AssignScope(identity, []string{"rq34.txt"})
	if err != nil {
		t.Fatal(err)
	}
	r = verdictCreate(t, f, author, "rq34-create", "rq34.txt", "rq34-content")
	gate := verdictGate(t, f, identity.WorkUnitID, "1", "rq34-judge", author)
	verdictJudge(t, f, gate, author)
	if err := f.Verify(gate, author, "rq34-verdict", r.Revision, r.DeltaHash, pass, finding); err != nil {
		t.Fatal(err)
	}
	return author, r, func() {
		identity.AttemptID = "2"
		if adopted, err := f.Bind(identity, nil); err != nil || adopted != author {
			t.Fatalf("adoption = %q, %v; want %q", adopted, err, author)
		}
	}
}

// A failing verdict refuses its own attempt's consolidation until the user
// accepts it, and stops refusing once the binding moves to another attempt.
func TestFailingVerdictRefusesItsAttemptUntilAcceptedOrAdopted(t *testing.T) {
	identity := vfs.Identity{SessionID: verdictSession, WorkUnitID: "rq34-author-unit", AgentID: "rq34-author", Specialist: "dev"}
	for name, accept := range map[string]bool{"accepted": true, "adopted": false} {
		t.Run(name, func(t *testing.T) {
			f, root, _ := verdictStore(t)
			author, r, adopt := verdictPrelaunched(t, f, identity, false, "behavior changed")
			if err := f.ConsolidateCheckpoint(author, "rq34-checkpoint", r.Revision, false); !errors.Is(err, vfs.ErrFailingVerdict) {
				t.Fatalf("current failing verdict = %v", err)
			}
			if !accept {
				adopt()
			}
			if err := f.ConsolidateCheckpoint(author, "rq34-checkpoint", r.Revision, accept); err != nil {
				t.Fatalf("consolidation = %v", err)
			}
			data, err := os.ReadFile(filepath.Join(root, "rq34.txt"))
			if err != nil || string(data) != "rq34-content" {
				t.Fatalf("consolidated bytes = %q, %v", data, err)
			}
		})
	}
}

// A maintenance cycle's binding never moves to another attempt, so its
// passing verdict keeps describing the attempt that consolidates.
func TestMaintenanceBindingRefusesAnotherAttempt(t *testing.T) {
	f, root, _ := verdictStore(t)
	identity := vfs.Identity{SessionID: verdictSession, WorkUnitID: "rq34-author-unit", AgentID: "rq34-author", Specialist: "simplify", CycleID: "cycle-34", MandateClass: "dead-code"}
	author, r, _ := verdictPrelaunched(t, f, identity, true, "clean")
	identity.AttemptID = "2"
	if adopted, err := f.Bind(identity, nil); !errors.Is(err, vfs.ErrScopeDenied) {
		t.Fatalf("adoption by another attempt = %q, %v", adopted, err)
	}
	if err := f.ConsolidateCheckpoint(author, "rq34-checkpoint", r.Revision, false); err != nil {
		t.Fatalf("consolidation over its own verdict = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "rq34.txt"))
	if err != nil || string(data) != "rq34-content" {
		t.Fatalf("consolidated bytes = %q, %v", data, err)
	}
}

// The maintenance cycle keeps gating on verdicts that still describe
// the staged work, and a current passing verdict keeps authorizing it.
func TestMaintenanceVerdictRulesKeepAuthorizing(t *testing.T) {
	f, root, _ := verdictStore(t)
	author := verdictMaintainer(t, f, "rq34-author-unit", "1", "rq34-author", "rq34.txt")
	r := verdictCreate(t, f, author, "rq34-create", "rq34.txt", "rq34-content")
	if err := f.ConsolidateCheckpoint(author, "rq34-checkpoint", r.Revision, false); !errors.Is(err, vfs.ErrVerificationRequired) {
		t.Fatalf("cycle consolidation without a verdict = %v", err)
	}
	gate := verdictGate(t, f, "rq34-author-unit", "1", "rq34-judge", author)
	verdictJudge(t, f, gate, author)
	if err := f.Verify(gate, author, "rq34-verdict", r.Revision, r.DeltaHash, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.ConsolidateCheckpoint(author, "rq34-checkpoint", r.Revision, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "rq34.txt"))
	if err != nil || string(data) != "rq34-content" {
		t.Fatalf("consolidated bytes = %q, %v", data, err)
	}
}

// A legacy verdict from a store without verdict identity never
// authorizes a current merge.
func TestLegacyVerdictNeverAuthorizes(t *testing.T) {
	f, root, state := verdictStore(t)
	author := verdictMaintainer(t, f, "rq34-author-unit", "1", "rq34-author", "rq34.txt")
	r := verdictCreate(t, f, author, "rq34-create", "rq34.txt", "rq34-content")
	gate := verdictGate(t, f, "rq34-author-unit", "1", "rq34-judge", author)
	verdictJudge(t, f, gate, author)
	if err := f.Verify(gate, author, "rq34-verdict", r.Revision, r.DeltaHash, true, "clean"); err != nil {
		t.Fatal(err)
	}
	// A store written before verdict identity kept verdicts without their
	// unit and attempt: strip both to reconstruct one.
	s, err := f.SavedState()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range s.Deltas[author].Verdicts {
		v.WorkUnitID, v.AttemptID = "", ""
	}
	verdictReplaceState(t, f, s)
	reopened := verdictReopen(t, f, root, state)
	err = reopened.ConsolidateCheckpoint(author, "rq34-checkpoint", r.Revision, false)
	if !errors.Is(err, vfs.ErrInvalidVerdict) {
		t.Fatalf("consolidation on a legacy verdict = %v", err)
	}
	if n := len(reopened.InspectDelta(author).Files); n != 1 {
		t.Fatalf("refused consolidation lost staged work: %d files", n)
	}
}

// Restaging, even identical bytes at a new revision, discards the earlier
// revision's verdict: a maintenance cycle's consolidation asks for a new one.
func TestVerdictForAnotherRevisionDoesNotAuthorizeIdenticalBytes(t *testing.T) {
	f, _, _ := verdictStore(t)
	author := verdictMaintainer(t, f, "rq34-author-unit", "1", "rq34-author", "rq34.txt")
	gate := verdictGate(t, f, "rq34-author-unit", "1", "rq34-judge", author)
	r := verdictCreate(t, f, author, "rq34-create", "rq34.txt", "rq34-content")
	verdictJudge(t, f, gate, author)
	if err := f.Verify(gate, author, "rq34-verdict", r.Revision, r.DeltaHash, true, "clean"); err != nil {
		t.Fatal(err)
	}
	// Restaging the identical bytes changes only the revision.
	next := verdictPatch(t, f, author, "rq34-restage", r.Revision, "rq34.txt", "rq34-content")
	if next.DeltaHash != r.DeltaHash || next.Revision == r.Revision {
		t.Fatalf("restage = %+v, want identical bytes at a new revision", next)
	}
	if err := f.ConsolidateCheckpoint(author, "rq34-checkpoint", next.Revision, false); !errors.Is(err, vfs.ErrVerificationRequired) {
		t.Fatalf("consolidation over another revision's verdict = %v", err)
	}
	if n := len(f.InspectDelta(author).Files); n != 1 {
		t.Fatalf("refused consolidation lost staged work: %d files", n)
	}
}
