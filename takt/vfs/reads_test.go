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
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// reader assigns unit a claim over scope with reads, admits it and binds it.
func reader(t *testing.T, f *vfs.FS, state, unit string, reads []string, scope ...string) (vfs.AgentID, error) {
	t.Helper()
	identity := vfs.Identity{SessionID: testSession, WorkUnitID: unit, AgentID: "dev", Specialist: "dev", Reads: reads}
	if _, err := f.AssignScope(identity, scope); err != nil {
		return "", err
	}
	ensureAdmitted(t, f, state, testSession, unit, "dev")
	bound, err := f.BindAs(callerOf(testSession, "dev", unit), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return bound.Key, nil
}

func readOf(f *vfs.FS, key vfs.AgentID, call, path string) (string, error) {
	res, err := f.Apply(vfs.Operation{Key: key, CallID: call, Action: vfs.OpRead, Path: path})
	return string(res.Content), err
}

// Work no one runs any more holds a mutex nobody owns in practice: stating
// reads lifts it for the reader, who sees the staged work, and the claim stays.
func TestReadsLiftTheMutexOfWorkNoOneRuns(t *testing.T) {
	f, _, state := newStore(t)
	_, author := delegate(t, f, state, testSession, "dev", "code", "a.go")
	apply(t, f, applyCase{author, "w", 0, vfs.OpCreate, "a.go", "package a"})
	settle(t, state, testSession, "code", history.FirstAttempt)

	docs, err := reader(t, f, state, "docs", []string{vfs.ReadAll}, "doc.md")
	if err != nil {
		t.Fatalf("reads over orphan work = %v; want admitted", err)
	}
	if got, err := readOf(f, docs, "r", "a.go"); err != nil || got != "package a" {
		t.Fatalf("lifted read = %q, %v; want the orphan's staged content", got, err)
	}
	if claims := f.OwnershipClaims(testSession); len(claims) != 2 {
		t.Fatalf("claims = %d; want the orphan's claim kept beside the reader's", len(claims))
	}
}

// Work in flight keeps its mutex: an explicit read over it is refused to the
// orchestrator, and free reads everything that is not in flight.
func TestReadsRefuseWorkInFlightAndFreeSkipsIt(t *testing.T) {
	f, _, state := newStore(t)
	delegate(t, f, state, testSession, "dev", "code", "a.go")

	for _, reads := range [][]string{{vfs.ReadAll}, {"a.go"}, {"*.go"}} {
		_, err := f.AssignScope(vfs.Identity{SessionID: testSession, WorkUnitID: "docs", AgentID: "dev", Specialist: "dev", Reads: reads}, []string{"doc.md"})
		refusedNaming(t, "reads over work in flight", err, vfs.ErrCollision, "wait", "narrow", vfs.ReadFree)
	}
	docs, err := reader(t, f, state, "docs", []string{vfs.ReadFree}, "doc.md")
	if err != nil {
		t.Fatalf("free reads = %v; want admitted beside work in flight", err)
	}
	_, err = readOf(f, docs, "r", "a.go")
	refused(t, "free read of a path in flight", err, vfs.ErrCollision)
}

// The same mutex holds the other way: nothing claims what a reader in flight
// covers until it delivers.
func TestReaderInFlightExcludesNewClaims(t *testing.T) {
	f, _, state := newStore(t)
	if _, err := reader(t, f, state, "docs", []string{"src/**"}, "doc.md"); err != nil {
		t.Fatal(err)
	}
	_, err := f.AssignScope(vfs.Identity{SessionID: testSession, WorkUnitID: "code", AgentID: "dev", Specialist: "dev"}, []string{"src/a.go"})
	refusedNaming(t, "claim over what a reader covers", err, vfs.ErrCollision, "wait", "narrow")
	if _, err := f.AssignScope(vfs.Identity{SessionID: testSession, WorkUnitID: "other", AgentID: "dev", Specialist: "dev"}, []string{"lib/b.go"}); err != nil {
		t.Fatalf("claim outside the reads = %v; want admitted", err)
	}
	settle(t, state, testSession, "docs", history.FirstAttempt)
	if _, err := f.AssignScope(vfs.Identity{SessionID: testSession, WorkUnitID: "code", AgentID: "dev", Specialist: "dev"}, []string{"src/a.go"}); err != nil {
		t.Fatalf("claim after the reader delivered = %v; want admitted", err)
	}
}

func TestReadsRejectUnknownSelectors(t *testing.T) {
	for _, bad := range []string{"", "/abs", "../x", "*.", "*.a/b", "dir//"} {
		if err := vfs.ValidateReads([]string{bad}); err == nil {
			t.Errorf("ValidateReads(%q) = nil; want a refusal", bad)
		}
	}
	for _, good := range []string{"all", "free", "src/", "src/**", "*.md", "a/b.go"} {
		if err := vfs.ValidateReads([]string{good}); err != nil {
			t.Errorf("ValidateReads(%q) = %v", good, err)
		}
	}
}
