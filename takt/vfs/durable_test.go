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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func durable(t *testing.T) (*FS, string, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	f, e := Open(root, state)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, root, state
}
func bind(t *testing.T, f *FS, agent, unit, specialist string, scope ...string) AgentID {
	t.Helper()
	identity := Identity{SessionID: "s", WorkUnitID: unit, AttemptID: "1", AgentID: AgentID(agent), Specialist: specialist, InvariantsHash: "frozen-invariants"}
	grants, _, _ := instanceVFSCapabilities(specialist)
	if slices.Contains(grants, model.VFSCapabilityVerify) {
		// A gate judges the unit's author through its key; a unit with no
		// author yet gets one so the verifier has staged work to link to.
		var author AgentID
		for _, owner := range f.owners {
			if existing := f.bindings[owner]; existing.WorkUnitID == unit && existing.AgentID != identity.AgentID {
				author = owner
			}
		}
		if author == "" {
			var err error
			if author, err = f.Bind(Identity{SessionID: identity.SessionID, WorkUnitID: unit, AttemptID: identity.AttemptID, AgentID: AgentID("author-" + agent), Specialist: "dev"}, []string{"author-" + unit + ".go"}); err != nil {
				t.Fatal(err)
			}
		}
		key, err := f.AssignVerifier(identity, author)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	key, e := f.Bind(identity, scope)
	if e != nil {
		t.Fatal(e)
	}
	return key
}

// applyCase is one operation apply exercises against the test fixture: the
// binding key, its call identity and expected revision, the action, and the
// target path and content.
type applyCase struct {
	Key     AgentID
	CallID  string
	Rev     uint64
	Action  OperationType
	Path    string
	Content string
}

func apply(t *testing.T, f *FS, c applyCase) OperationResult {
	t.Helper()
	r, e := f.Apply(Operation{c.Key, c.CallID, c.Rev, c.Action, c.Path, []byte(c.Content)})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

// readForTest exercises the same locked read path Apply(OpRead) uses,
// without the Bind/identity bookkeeping — for fault-injection assertions
// that only care about the readyLocked/finishLocked gate.
func readForTest(f *FS, agent AgentID, path string) (data []byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return nil, err
	}
	defer func() {
		f.finishLocked(&err)
		if err != nil {
			data = nil
		}
	}()
	return f.readLockedAs(agent, path, false)
}

func stagedCount(f *FS, key AgentID) int {
	d, ok := f.staged[key]
	if !ok {
		return 0
	}
	return len(d.files)
}

func hasVerdict(f *FS, key AgentID) bool {
	d, ok := f.staged[key]
	return ok && d.verdict != nil
}

func gate(t *testing.T, f *FS, key AgentID, r OperationResult) {
	t.Helper()
	id := f.bindings[key]
	v := bind(t, f, "independent", id.WorkUnitID, "verify")
	if e := f.Verify(v, key, "gate", r.Revision, r.DeltaHash, true, ""); e != nil {
		t.Fatal(e)
	}
}

func TestDurableReopenAndContentFreeJournal(t *testing.T) {
	f, root, state := durable(t)
	key := bind(t, f, "author", "unit", "dev", "dir/new.txt")
	r := apply(t, f, applyCase{key, "create", 0, OpCreate, "dir/new.txt", "secret-content"})
	if _, e := os.Stat(filepath.Join(root, "dir")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("staging touched workspace")
	}
	if e := f.Close(); e != nil {
		t.Fatal(e)
	}
	f, e := Open(root, state)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close filesystem: %v", err)
		}
	})
	read := apply(t, f, applyCase{key, "read", r.Revision, OpRead, "dir/new.txt", ""})
	if string(read.Content) != "secret-content" {
		t.Fatal("lost staging")
	}
	page := f.JournalPage("s", "unit", -1, 1)
	if len(page) != 1 || page[0].Seq != 0 || page[0].Agent != "author" || page[0].Role != "execution" || page[0].CallID != "create" {
		t.Fatalf("journal %+v", page)
	}
	if strings.Contains(fmt.Sprint(page), "secret-content") || strings.Contains(fmt.Sprint(page), string(key)) {
		t.Fatal("evidence leaked data or binding")
	}
	gate(t, f, key, read)
	if e = f.ConsolidateCheckpoint(key, "milestone", r.Revision); e != nil {
		t.Fatal(e)
	}
	content, e := os.ReadFile(filepath.Join(root, "dir/new.txt"))
	if e != nil || string(content) != "secret-content" {
		t.Fatalf("materialize %s %v", content, e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	f, e = Open(root, state)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close filesystem: %v", err)
		}
	})
	if stagedCount(f, key) != 0 {
		t.Fatal("completed delta resurrected")
	}
}

func TestPrelaunchClaimPersistsConflictIsTypedAndReleaseNeverStrandsStagedWork(t *testing.T) {
	f, root, state := durable(t)
	identity := Identity{SessionID: "root-session", WorkUnitID: "unit-a", AgentID: "dev-a", Specialist: "dev"}
	key, err := f.AssignScope(identity, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	claims := f.OwnershipClaims("root-session")
	if len(claims) != 1 || !claims[0].Pending || claims[0].Active || claims[0].TargetInstance != "dev" || claims[0].RootSessionID != "root-session" {
		t.Fatalf("prelaunch claims = %+v", claims)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err = Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	claims = f.OwnershipClaims("another-session")
	if len(claims) != 1 || claims[0].Active || !claims[0].Pending || claims[0].Key != key {
		t.Fatalf("persisted prior-session claim = %+v", claims)
	}

	_, err = f.AssignScope(Identity{SessionID: "root-session", WorkUnitID: "unit-b", AgentID: "fix-b", Specialist: "fix"}, []string{"a.go"})
	var collision *CollisionError
	if !errors.Is(err, ErrCollision) || !errors.As(err, &collision) {
		t.Fatalf("conflict = %v, want typed ErrCollision", err)
	}
	if collision.RequestedPath != "a.go" || collision.TargetAgent != "fix-b" || collision.TargetInstance != "fix" ||
		collision.OwnerAgent != "dev-a" || collision.OwnerSession != "root-session" || collision.OwnerUnit != "unit-a" {
		t.Fatalf("conflict evidence = %+v", collision)
	}
	if len(f.collisions) != 0 {
		t.Fatalf("refused prelaunch assignment poisoned unresolved work: %+v", f.collisions)
	}
	if _, err = f.AssignScope(Identity{SessionID: "root-session", WorkUnitID: "read-only", AgentID: "verify", Specialist: "verify"}, []string{"readonly.go"}); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("role-capable but explicitly read-only target assignment = %v, want scope denial", err)
	}

	adopted, err := f.Bind(identity, nil)
	if err != nil || adopted != key {
		t.Fatalf("adopt prelaunch claim = %q, %v; want %q", adopted, err, key)
	}
	created := apply(t, f, applyCase{key, "write", 0, OpCreate, "a.go", "staged"})
	verifier, err := f.AssignVerifier(Identity{SessionID: "root-session", WorkUnitID: "unit-a", AttemptID: "1", AgentID: "verify", Specialist: "verify"}, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.ReadAs(Operation{Key: verifier, CallID: "verify-read", ExpectedRevision: created.Revision, Action: OpRead, Path: "a.go"}, key); err != nil {
		t.Fatalf("authorized staged-view read before release: %v", err)
	}
	if err = f.Verify(verifier, key, "accept", created.Revision, created.DeltaHash, true, ""); err != nil {
		t.Fatal(err)
	}
	if err = f.RevokeOwnership(key); !errors.Is(err, ErrStagedWork) {
		t.Fatalf("release of staged work = %v, want ErrStagedWork", err)
	}
	if delta := f.InspectDelta(key); delta.Revision != created.Revision || delta.Hash != created.DeltaHash {
		t.Fatalf("refused release changed the staged delta: %+v", delta)
	}
	if f.owners["a.go"] != key {
		t.Fatal("refused release dropped ownership of the staged path")
	}
	read, err := f.ReadAs(Operation{Key: verifier, CallID: "recovery-read", ExpectedRevision: created.Revision, Action: OpRead, Path: "a.go"}, key)
	if err != nil || string(read.Content) != "staged" {
		t.Fatalf("authorized staged-view read after refused release = %q, %v", read.Content, err)
	}
	if err = f.ConsolidateCheckpoint(key, "deliver", created.Revision); err != nil {
		t.Fatalf("consolidation after refused release = %v", err)
	}
	if claims = f.OwnershipClaims("root-session"); len(claims) != 1 || claims[0].AgentID != "verify" {
		t.Fatalf("consolidated claim still listed: %+v", claims)
	}
}

func TestWorkspaceReadRequiresOwnership(t *testing.T) {
	f, root, _ := durable(t)
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("physical"), 0600); err != nil {
		t.Fatal(err)
	}
	key := bind(t, f, "reader", "unit", "dev", "owned.txt")
	if _, err := f.Apply(Operation{Key: key, CallID: "outside", Action: OpRead, Path: "outside.txt"}); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("unclaimed workspace read = %v, want scope denial", err)
	}
	if _, err := f.Apply(Operation{Key: key, CallID: "owned", Action: OpRead, Path: "owned.txt"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner read of absent scoped path = %v, want not-exist", err)
	}
}

func TestCoreAuthorizationUsesExactInstanceCapabilitiesNotRole(t *testing.T) {
	f, _, _ := durable(t)
	author, err := f.Bind(Identity{SessionID: "s", WorkUnitID: "verify-unit", AgentID: "author", Specialist: "dev"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := f.AssignVerifier(Identity{SessionID: "s", WorkUnitID: "verify-unit", AttemptID: "1", AgentID: "verify", Specialist: "verify"}, author)
	if err != nil {
		t.Fatalf("explicit bind grant was rejected for verifier role: %v", err)
	}
	// Discard is the orchestrator's grant: it authorizes rollback of any
	// staged work, whichever instance the binding belongs to.
	if _, err = f.Apply(Operation{Key: verifier, CallID: "discard-empty", Action: OpRollback}); err != nil {
		t.Fatalf("orchestrator discard grant was not applied: %v", err)
	}
	if _, err = f.Bind(Identity{SessionID: "s", WorkUnitID: "no-grant", AgentID: "pm", Specialist: "pm"}, nil); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("instance with no VFS grant bound by role alone: %v", err)
	}
}

func TestWorkspaceLockAndPrivateStore(t *testing.T) {
	f, root, _ := durable(t)
	if _, e := Open(root, filepath.Join(t.TempDir(), "another")); e == nil {
		t.Fatal("second coordinator acquired workspace")
	}
	_ = f.Close()
	if _, e := Open(root, filepath.Join(root, "state")); e == nil {
		t.Fatal("state allowed in workspace")
	}
	if _, e := os.Stat(filepath.Join(root, "state")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("rejected setup mutated workspace")
	}
}

func TestGateRevisionIdentityAndReplay(t *testing.T) {
	f, _, _ := durable(t)
	key := bind(t, f, "author", "unit", "dev", "a")
	r := apply(t, f, applyCase{key, "create", 0, OpCreate, "a", "one"})
	if _, e := f.Apply(Operation{key, "create", r.Revision, OpPatch, "a", []byte("two")}); !errors.Is(e, ErrDuplicateCall) {
		t.Fatal(e)
	}
	if _, e := f.Apply(Operation{key, "stale", 0, OpPatch, "a", []byte("two")}); !errors.Is(e, ErrStaleRevision) {
		t.Fatal(e)
	}
	if _, e := f.Apply(Operation{key, "scope", r.Revision, OpCreate, "not-owned", nil}); !errors.Is(e, ErrScopeDenied) {
		t.Fatal(e)
	}
	bad := bind(t, f, "not-verifier", "unit", "dev")
	if e := f.Verify(bad, key, "bad", r.Revision, r.DeltaHash, true, ""); !errors.Is(e, ErrScopeDenied) {
		t.Fatal(e)
	}
	self := bind(t, f, "author", "unit-self", "verify")
	// Same author in the same attempt is deliberately prevented at binding.
	if f.Verify(self, key, "self", r.Revision, r.DeltaHash, true, "") == nil {
		t.Fatal("wrong unit verified")
	}
	v := bind(t, f, "verifier", "unit", "verify")
	if e := f.Verify(v, key, "reject-empty", r.Revision, r.DeltaHash, false, ""); !errors.Is(e, ErrInvalidVerdict) {
		t.Fatal(e)
	}
	if e := f.Verify(v, key, "reject", r.Revision, r.DeltaHash, false, "finding"); e != nil {
		t.Fatal(e)
	}
	if stagedCount(f, key) != 1 {
		t.Fatal("rejection discarded delta")
	}
	if e := f.Verify(v, key, "pass", r.Revision, r.DeltaHash, true, ""); e != nil {
		t.Fatal(e)
	}
	next := apply(t, f, applyCase{key, "edit", r.Revision, OpPatch, "a", "two"})
	if hasVerdict(f, key) {
		t.Fatal("edit retained gate")
	}
	if e := f.ConsolidateCheckpoint(key, "checkpoint", next.Revision); e != nil {
		t.Fatal(e)
	}
}

func TestPathAliasesRejected(t *testing.T) {
	f, root, _ := durable(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if e := os.WriteFile(outside, []byte("safe"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"../outside", "/absolute", "a/../b", "a//b", "a\\b", ".git/config", "newdir/.git/config", "a/b/.takt-vfs-x"} {
		if _, e := f.Bind(Identity{SessionID: "s", WorkUnitID: p, AttemptID: "1", AgentID: "a", Specialist: "dev", InvariantsHash: "h"}, []string{p}); e == nil {
			t.Fatalf("accepted %s", p)
		}
	}
	if e := os.Symlink(outside, filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(outside, filepath.Join(root, "hard")); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "Case"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"link", "hard", "case"} {
		if e := f.validatePath(p); !errors.Is(e, ErrInvalidPath) {
			t.Fatalf("%s: %v", p, e)
		}
	}
}

func TestEveryFlushBoundaryRecoversAfterReopen(t *testing.T) {
	for _, point := range []string{"prepared", "before/a", "after/a", "before/b", "after/b", "verified"} {
		t.Run(point, func(t *testing.T) {
			f, root, state := durable(t)
			if e := os.WriteFile(filepath.Join(root, "a"), []byte("base"), 0751); e != nil {
				t.Fatal(e)
			}
			key := bind(t, f, "author", "unit", "dev", "a", "b")
			r := apply(t, f, applyCase{key, "a", 0, OpPatch, "a", "new"})
			r = apply(t, f, applyCase{key, "b", r.Revision, OpCreate, "b", ""})
			gate(t, f, key, r)
			f.failpoint = func(p string) error {
				if p == point {
					return errors.New("injected I/O failure")
				}
				return nil
			}
			if f.ConsolidateCheckpoint(key, "checkpoint", r.Revision) == nil {
				t.Fatal("missing injected failure")
			}
			if _, e := readForTest(f, key, "a"); !errors.Is(e, ErrRecoveryRequired) {
				t.Fatalf("work continued during recovery: %v", e)
			}
			_ = f.Close()
			f, e := Open(root, state)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				if err := f.Close(); err != nil {
					t.Errorf("close filesystem: %v", err)
				}
			})
			if e = f.Recover(); e != nil {
				t.Fatal(e)
			}
			a, e := os.ReadFile(filepath.Join(root, "a"))
			if e != nil || string(a) != "base" {
				t.Fatalf("restore %s %v", a, e)
			}
			info, e := os.Stat(filepath.Join(root, "a"))
			if e != nil || info.Mode().Perm() != 0751 {
				t.Fatal("lost executable metadata")
			}
			if _, e = os.Stat(filepath.Join(root, "b")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("creation not reverted")
			}
			if stagedCount(f, key) != 2 {
				t.Fatal("lost staged work")
			}
		})
	}
}

func TestRecoveryNeverOverwritesExternalChange(t *testing.T) {
	f, root, _ := durable(t)
	key := bind(t, f, "author", "unit", "dev", "a")
	r := apply(t, f, applyCase{key, "create", 0, OpCreate, "a", "new"})
	gate(t, f, key, r)
	f.failpoint = func(p string) error {
		if p == "after/a" {
			return errors.New("stop")
		}
		return nil
	}
	if f.ConsolidateCheckpoint(key, "checkpoint", r.Revision) == nil {
		t.Fatal("expected failure")
	}
	f.failpoint = nil
	if e := os.WriteFile(filepath.Join(root, "a"), []byte("external"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := f.Recover(); !errors.Is(e, ErrBaseChanged) {
		t.Fatal(e)
	}
	if f.recovery == nil {
		t.Fatal("deleted backups after unconfirmed restoration")
	}
	data, _ := os.ReadFile(filepath.Join(root, "a"))
	if string(data) != "external" {
		t.Fatal("overwrote user work")
	}
}

func TestPersistenceFailureDoesNotPublishSuccess(t *testing.T) {
	f, _, state := durable(t)
	key := bind(t, f, "author", "unit", "dev", "a")
	count := 0
	f.OnJournal(func(JournalEntry) { count++ })
	// SQLITE_FULL via an aborting trigger exercises the transaction failure
	// boundary deterministically without filling the developer's disk.
	if _, e := f.db.Exec(`CREATE TRIGGER fail_state BEFORE UPDATE ON state BEGIN SELECT RAISE(ABORT,'disk full'); END;`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Apply(Operation{key, "write", 0, OpCreate, "a", []byte("x")}); !errors.Is(e, ErrStoreFailed) {
		t.Fatal(e)
	}
	if count != 0 || len(f.JournalPage("", "", -1, 10)) != 0 {
		t.Fatal("published uncommitted journal")
	}
	if _, e := readForTest(f, key, "a"); !errors.Is(e, ErrStoreFailed) {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(state, "vfs.sqlite")); e != nil {
		t.Fatal(e)
	}
}

func TestConcurrentUnitCorrelation(t *testing.T) {
	f, _, _ := durable(t)
	var wg sync.WaitGroup
	for i := range 4 {
		unit := fmt.Sprint(i)
		key := bind(t, f, "same-agent", unit, "dev", unit)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := f.Apply(Operation{key, "call-" + unit, 0, OpCreate, unit, []byte(unit)}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	for _, e := range f.JournalPage("s", "", -1, 100) {
		if e.WorkUnitID != e.Path || e.CallID != "call-"+e.Path || e.Agent != "same-agent" {
			t.Fatalf("cross-unit correlation %+v", e)
		}
	}
}

func TestAbruptProcessRecovery(t *testing.T) {
	if os.Getenv("TAKT_VFS_CRASH_HELPER") == "1" {
		f, e := Open(os.Getenv("TAKT_VFS_ROOT"), os.Getenv("TAKT_VFS_STATE"))
		if e != nil {
			panic(e)
		}
		key := bind(t, f, "author", "unit", "dev", "a")
		r := apply(t, f, applyCase{key, "create", 0, OpCreate, "a", "new"})
		gate(t, f, key, r)
		f.failpoint = func(p string) error {
			if p == "after/a" {
				os.Exit(73)
			}
			return nil
		}
		_ = f.ConsolidateCheckpoint(key, "checkpoint", r.Revision)
		os.Exit(74)
	}
	if testing.Short() {
		t.Skip("subprocess crash integration")
	}
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	cmd := exec.Command(os.Args[0], "-test.run=^TestAbruptProcessRecovery$")
	cmd.Env = append(os.Environ(), "TAKT_VFS_CRASH_HELPER=1", "TAKT_VFS_ROOT="+root, "TAKT_VFS_STATE="+state)
	e := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != 73 {
		t.Fatalf("helper %v", e)
	}
	f, e := Open(root, state)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close filesystem: %v", err)
		}
	})
	if e = f.Recover(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "a")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("abrupt flush not recovered")
	}
}

func TestVerifierInspectsAuthorProjectionWithoutMutationRights(t *testing.T) {
	f, _, _ := durable(t)
	key := bind(t, f, "author", "unit", "dev", "a")
	r := apply(t, f, applyCase{key, "create", 0, OpCreate, "a", "staged"})
	verifier := bind(t, f, "judge", "unit", "verify")
	view, err := f.apply(Operation{Key: verifier, CallID: "inspect", ExpectedRevision: r.Revision, Action: OpRead, Path: "a"}, key)
	if err != nil || string(view.Content) != "staged" || view.DeltaHash != r.DeltaHash {
		t.Fatalf("view %+v %v", view, err)
	}
	if _, err = f.Apply(Operation{verifier, "write", 0, OpCreate, "a", []byte("bad")}); !errors.Is(err, ErrScopeDenied) {
		t.Fatal(err)
	}
	other := bind(t, f, "other-judge", "other-unit", "verify")
	if _, err = f.apply(Operation{Key: other, CallID: "wrong-unit", ExpectedRevision: r.Revision, Action: OpRead, Path: "a"}, key); !errors.Is(err, ErrIdentity) {
		t.Fatal(err)
	}
}

func TestRecoveryRetainsManifestOnRestorationFailure(t *testing.T) {
	f, _, state := durable(t)
	key := bind(t, f, "author", "unit", "dev", "new/child/a")
	r := apply(t, f, applyCase{key, "create", 0, OpCreate, "new/child/a", "staged"})
	gate(t, f, key, r)
	f.failpoint = func(p string) error {
		if p == "after/new/child/a" {
			return errors.New("flush failure")
		}
		return nil
	}
	if f.ConsolidateCheckpoint(key, "c", r.Revision) == nil {
		t.Fatal("no failure")
	}
	f.failpoint = func(p string) error {
		if p == "restore-before/new/child/a" {
			return errors.New("restore failure")
		}
		return nil
	}
	if f.Recover() == nil {
		t.Fatal("no restoration failure")
	}
	if f.recovery == nil {
		t.Fatal("discarded recovery manifest")
	}
	root := f.rootDir
	_ = f.Close()
	f, e := Open(root, state)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close filesystem: %v", err)
		}
	})
	if e = f.Recover(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "new")); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("left directories: %v", e)
	}
}

func FuzzCanonicalPaths(f *testing.F) {
	for _, p := range []string{"file", "nested/file", "../escape", "/absolute", "a//b", "a/../b", ".git/config", "a\\b"} {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, p string) {
		fs, e := newFS(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if err := fs.Close(); err != nil {
				t.Errorf("close filesystem: %v", err)
			}
		})
		if e = fs.validatePath(p); e == nil {
			if !filepath.IsLocal(p) || filepath.ToSlash(filepath.Clean(p)) != p {
				t.Fatalf("accepted non-canonical path %q", p)
			}
		}
	})
}

func TestBindOnlyForImplementationAndGateRoles(t *testing.T) {
	f, _, _ := durable(t)
	for _, specialist := range []string{"pm", "architect", "product-designer", "analyst", "spec", "tpm"} {
		_, err := f.Bind(Identity{SessionID: "s", WorkUnitID: "u-" + specialist, AttemptID: "1", AgentID: AgentID(specialist), Specialist: specialist, InvariantsHash: "h"}, nil)
		if !errors.Is(err, ErrScopeDenied) {
			t.Errorf("Bind(%s, empty scope) = %v, want ErrScopeDenied", specialist, err)
		}
	}
	for _, specialist := range []string{"verify", "judge-a", "judge-b"} {
		bind(t, f, specialist, "unit-"+specialist, specialist)
	}
}
