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
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// priorRoot leaves an earlier root's adopted claim holding staged work over
// kept.go and gone.go: kept.go created from base "original" and gone.go
// staged for deletion, with the store reopened so the claim survives its
// session as prior work.
func priorRoot(t *testing.T) (*vfs.FS, string, string, vfs.AgentID) {
	t.Helper()
	f, root, state := newStore(t)
	for path, content := range map[string]string{"kept.go": "original", "gone.go": "to remove"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), diskFileMode); err != nil {
			t.Fatal(err)
		}
	}
	c, key := delegate(t, f, state, testSession, "dev", "prior", "kept.go", "gone.go")
	write(t, f, c, "prior-write", "kept.go", "kept staged")
	apply(t, f, applyCase{key, "prior-delete", 1, vfs.OpDelete, "gone.go", ""})
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened, root, state, key
}

// assertNoRollbackJournal fails when any journal entry rolled work back.
func assertNoRollbackJournal(t *testing.T, f *vfs.FS) {
	t.Helper()
	for _, e := range f.JournalPage("", "", -1, vfs.MaxJournalPageSize) {
		if e.Operation == vfs.OpRollback {
			t.Fatalf("journal %d rolled %s back", e.Seq, e.Path)
		}
	}
}
func TestFreshCrossRootOverlapIsRefusedAndMovesNothing(t *testing.T) {
	f, root, _, prior := priorRoot(t)
	// The earlier root's disk moved on after it captured its base.
	if err := os.WriteFile(filepath.Join(root, "kept.go"), []byte("rewritten on disk"), diskFileMode); err != nil {
		t.Fatal(err)
	}
	key, err := f.AssignScope(vfs.Identity{SessionID: "fresh-root", WorkUnitID: "fresh", AgentID: "fresh", Specialist: "dev"}, []string{"kept.go", "gone.go", "free.go"})
	refused(t, "AssignScope() over another root's staged claim", err, vfs.ErrCollision)
	var collision *vfs.CollisionError
	if !errors.As(err, &collision) || collision.OwnerSession != testSession || collision.OwnerUnit != "prior" {
		t.Fatalf("AssignScope() = %v; want a CollisionError naming the owner session %s and unit prior", err, testSession)
	}
	for _, want := range []string{"claim_assign", "author_key " + string(prior), "vfs_consolidate", "vfs_discard"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q does not name %q", err, want)
		}
	}
	if key != "" {
		t.Fatalf("refused request returned key %q", key)
	}
	if content, ok := stagedContent(f, prior, "kept.go"); !ok || content != "kept staged" {
		t.Fatalf("prior kept.go = %q, %v; want its staged content untouched", content, ok)
	}
	if base, ok := f.StagedBase(prior, "kept.go"); !ok || string(base.Content) != "original" {
		t.Fatalf("prior base = %q, %v; want the base it captured", string(base.Content), ok)
	}
	if got := stagedCount(f, prior); got != 2 {
		t.Fatalf("prior claim stages %d files; want kept.go and the gone.go tombstone", got)
	}
	for path, want := range map[string]vfs.AgentID{"kept.go": prior, "gone.go": prior, "free.go": ""} {
		if ownerOf(f, path) != want {
			t.Fatalf("ownerOf(%s) = %q; want %q", path, ownerOf(f, path), want)
		}
	}
	for path, want := range map[string]string{"kept.go": "rewritten on disk", "gone.go": "to remove"} {
		if got, ok := diskContent(t, root, path); !ok || got != want {
			t.Fatalf("refusal wrote the workspace: %s = %q, %v", path, got, ok)
		}
	}
	assertNoRollbackJournal(t, f)
}

func TestExplicitReassignmentHandsOverOnlyTheRequestedPaths(t *testing.T) {
	f, root, state := newStore(t)
	for path, content := range map[string]string{"kept.go": "original", "gone.go": "to remove"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), diskFileMode); err != nil {
			t.Fatal(err)
		}
	}
	c, key := delegate(t, f, state, testSession, "dev", "u", "kept.go", "gone.go")
	write(t, f, c, "w", "kept.go", "kept staged")
	apply(t, f, applyCase{key, "d", 1, vfs.OpDelete, "gone.go", ""})
	next, err := f.ReassignScope(ident("fix", "fix", "fix"), key, []string{"kept.go"})
	if err != nil {
		t.Fatal(err)
	}
	if next == key {
		t.Fatal("partial hand-over kept one key for split work")
	}
	if content, ok := stagedContent(f, next, "kept.go"); !ok || content != "kept staged" {
		t.Fatalf("handed-over kept.go = %q, %v", content, ok)
	}
	if base, ok := f.StagedBase(next, "kept.go"); !ok || string(base.Content) != "original" {
		t.Fatalf("handed-over base = %q, %v; want the captured base", string(base.Content), ok)
	}
	if got := stagedCount(f, next); got != 1 {
		t.Fatalf("handed-over delta holds %d files; want kept.go only", got)
	}
	if file, ok := f.InspectDelta(key).Files["gone.go"]; !ok || file != nil {
		t.Fatalf("gone.go stayed as %v, %v; want its tombstone with the old claim", file, ok)
	}
	if ownerOf(f, "kept.go") != next || ownerOf(f, "gone.go") != key {
		t.Fatalf("owners = %q, %q; want kept.go handed over and gone.go kept", ownerOf(f, "kept.go"), ownerOf(f, "gone.go"))
	}
	if got, ok := diskContent(t, root, "gone.go"); !ok || got != "to remove" {
		t.Fatalf("hand-over wrote the workspace: %q, %v", got, ok)
	}
	assertNoRollbackJournal(t, f)
}

func TestMixedRequestsRefuseWholeAndKeepAllWork(t *testing.T) {
	f, _, state := newStore(t)
	pc, peer := delegate(t, f, state, testSession, "dev", "peer", "held.go")
	write(t, f, pc, "w", "held.go", "peer staged")
	for name, tc := range map[string]struct {
		identity vfs.Identity
		scope    []string
		want     error
	}{
		"same-root collision": {ident("dev", "mixed", "dev"), []string{"free.go", "held.go"}, vfs.ErrCollision},
		"invalid path":        {vfs.Identity{SessionID: "free-root", WorkUnitID: "free-unit", AgentID: "free", Specialist: "dev"}, []string{"free.go", escapingPath}, vfs.ErrInvalidPath},
	} {
		t.Run(name, func(t *testing.T) {
			key, err := f.AssignScope(tc.identity, tc.scope)
			refused(t, "AssignScope()", err, tc.want)
			if key != "" {
				t.Fatalf("refused request returned key %q", key)
			}
			if ownerOf(f, "free.go") != "" {
				t.Fatal("the free half of a refused request was claimed")
			}
			if ownerOf(f, "held.go") != peer {
				t.Fatalf("ownerOf(held.go) = %q; want the peer's claim kept", ownerOf(f, "held.go"))
			}
			if content, ok := stagedContent(f, peer, "held.go"); !ok || content != "peer staged" {
				t.Fatalf("peer staging = %q, %v; want it intact", content, ok)
			}
			assertNoRollbackJournal(t, f)
		})
	}
}
func TestReservationConflictsRefuseTheWholeRequest(t *testing.T) {
	f, _, state := newStore(t)
	reader, reading := delegate(t, f, state, testSession, "test", "reading", "tests.go")
	write(t, f, reader, "w", "tests.go", "reader staged")
	if _, err := f.PrepareShell(reader, "graph", "codegraph explore src", state); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ContinueShell(reader, "graph", "start"); err != nil {
		t.Fatal(err)
	}
	// The request defers whole while the inspection reserves its paths.
	key, err := f.AssignScope(ident("dev", "blocked", "dev"), []string{"src/new.go", "free.go"})
	if err == nil || !strings.Contains(err.Error(), "retry claim_assign") {
		t.Fatalf("claim under a running inspection = %v, %q; want it deferred whole", err, key)
	}
	if ownerOf(f, "src/new.go") != "" || ownerOf(f, "free.go") != "" {
		t.Fatal("the deferred request left claims behind")
	}
	if ownerOf(f, "tests.go") != reading {
		t.Fatalf("ownerOf(tests.go) = %q; want the reader's claim kept", ownerOf(f, "tests.go"))
	}
	if content, ok := stagedContent(f, reading, "tests.go"); !ok || content != "reader staged" {
		t.Fatalf("reader staging = %q, %v; want it intact", content, ok)
	}
	if _, err := f.CompleteShell(reader, "graph", state); err != nil {
		t.Fatal(err)
	}
	key, err = f.AssignScope(ident("dev", "blocked", "dev"), []string{"src/new.go", "free.go"})
	if err != nil {
		t.Fatalf("claim after the inspection completed = %v", err)
	}
	if ownerOf(f, "src/new.go") != key || ownerOf(f, "free.go") != key {
		t.Fatal("the retried request did not claim both its paths")
	}
	assertNoRollbackJournal(t, f)
}

func TestAnOldVerdictAuthorizesNoTransferredWork(t *testing.T) {
	f, root, state := newStore(t)
	c, author := delegate(t, f, state, testSession, "dev", "u", "kept.go", "spare.go")
	write(t, f, c, "w", "kept.go", "kept staged")
	write(t, f, c, "w2", "spare.go", "spare staged")
	verifier, _ := judge(t, f, state, testSession, "verify", "u/verify", author)
	readAs(t, f, verifier, "r1", "kept.go", "")
	readAs(t, f, verifier, "r2", "spare.go", "")
	if err := f.VerifyAs(verifier, "v", author, true, "sound"); err != nil {
		t.Fatal(err)
	}
	rev, hash, staged := f.StagedState(author)
	if !staged {
		t.Fatal("rig: the author stages no work")
	}
	approved := vfs.StagedState{Revision: rev, DeltaHash: hash}
	next, err := f.ReassignScope(ident("fix", "fix", "fix"), author, []string{"kept.go"})
	if err != nil {
		t.Fatal(err)
	}
	if next == author {
		t.Fatal("reassignment kept one key for the split work")
	}
	// The passing verdict stays journal history...
	verdicts := 0
	for _, e := range f.JournalPage("", "", -1, vfs.MaxJournalPageSize) {
		if e.Operation == vfs.OperationType("verdict") {
			verdicts++
		}
	}
	if verdicts != 1 {
		t.Fatalf("journal holds %d verdict entries; want the one history entry", verdicts)
	}
	// ...while its acceptance no longer matches any work it did not cover.
	_, err = f.ConsolidateAs(orchestrator(testSession), next, "", &approved)
	refused(t, "consolidation accepted for the verdict's state", err, vfs.ErrStaleRevision)
	if _, err := f.ConsolidateAs(orchestrator(testSession), next, "", nil); err != nil {
		t.Fatalf("fresh consolidation of the transferred work = %v", err)
	}
	if got, ok := diskContent(t, root, "kept.go"); !ok || got != "kept staged" {
		t.Fatalf("kept.go = %q, %v; want the transferred work consolidated", got, ok)
	}
}
