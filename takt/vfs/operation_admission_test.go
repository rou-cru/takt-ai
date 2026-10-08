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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

const (
	// testSession is the session every helper identity belongs to.
	testSession = "s"
	// escapingPath leaves the workspace, so no scope or invariant may name it.
	escapingPath = "../escape"
	// unknownInstance is a specialist name the catalog does not declare.
	unknownInstance = "no-such-specialist"
	// noGrantInstance is a catalog instance that declares an explicit empty grant list.
	noGrantInstance = "pm"
	// unsupportedAction is an operation type the dispatcher does not implement.
	unsupportedAction vfs.OperationType = "bogus"
	// outsideAttempt is an attempt identity no assignment was issued for.
	outsideAttempt = "9"
	// reassignCall and authorKeyInput are what a refused repeat claim tells the
	// agent to continue with.
	reassignCall   = "claim_assign"
	authorKeyInput = "author_key"
	// diskFileMode and diskDirMode are the modes of what the tests place in the workspace.
	diskFileMode os.FileMode = 0o644
	diskDirMode  os.FileMode = 0o755
	// privateStoreMode is the mode of the workspace-local store directory.
	privateStoreMode os.FileMode = 0o700
)

func ident(agent, unit, specialist string) vfs.Identity {
	return vfs.Identity{SessionID: testSession, WorkUnitID: unit, AgentID: vfs.AgentID(agent), Specialist: specialist}
}

// newStore opens a store over a fresh workspace and returns it with both directories.
func newStore(t *testing.T) (f *vfs.FS, root, state string) {
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

// bind opens attempt 1 of a unit for agent. A verifier is preassigned to the
// author of its unit, who is bound first when the unit has none.
func bind(t *testing.T, f *vfs.FS, agent, unit, specialist string, scope ...string) vfs.AgentID {
	t.Helper()
	identity := vfs.Identity{SessionID: testSession, WorkUnitID: unit, AttemptID: "1", AgentID: vfs.AgentID(agent), Specialist: specialist}
	if vfs.RequireVFSCapability(specialist, model.VFSCapabilityVerify) == nil {
		author := authorOf(f, unit, identity.AgentID)
		if author == "" {
			var err error
			author, err = f.Bind(vfs.Identity{SessionID: testSession, WorkUnitID: unit, AttemptID: "1", AgentID: vfs.AgentID("author-" + agent), Specialist: "dev"}, []string{"author-" + unit + ".go"})
			if err != nil {
				t.Fatal(err)
			}
		}
		key, err := f.AssignVerifier(identity, author)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	key, err := f.Bind(identity, scope)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// authorOf is the key of the claim another agent of unit holds paths under.
func authorOf(f *vfs.FS, unit string, judge vfs.AgentID) vfs.AgentID {
	for _, claim := range f.OwnershipClaims(testSession) {
		if claim.WorkUnitID == unit && claim.AgentID != judge && len(claim.Scope) > 0 {
			return claim.Key
		}
	}
	return ""
}

// applyCase is one operation: the binding key, its call identity and expected
// revision, the action, and the target path and content.
type applyCase struct {
	Key     vfs.AgentID
	CallID  string
	Rev     uint64
	Action  vfs.OperationType
	Path    string
	Content string
}

func apply(t *testing.T, f *vfs.FS, c applyCase) vfs.OperationResult {
	t.Helper()
	r, err := f.Apply(vfs.Operation{Key: c.Key, CallID: c.CallID, ExpectedRevision: c.Rev, Action: c.Action, Path: c.Path, Content: []byte(c.Content)})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func claimOf(f *vfs.FS, key vfs.AgentID) (vfs.OwnershipClaim, bool) {
	for _, claim := range f.OwnershipClaims(testSession) {
		if claim.Key == key {
			return claim, true
		}
	}
	return vfs.OwnershipClaim{}, false
}

// ownerOf is the key holding path, empty when nobody does.
func ownerOf(f *vfs.FS, path string) vfs.AgentID {
	for _, claim := range f.OwnershipClaims(testSession) {
		if slices.Contains(claim.Scope, path) {
			return claim.Key
		}
	}
	return ""
}

func scopeOf(f *vfs.FS, key vfs.AgentID) []string {
	claim, _ := claimOf(f, key)
	return claim.Scope
}

// gateOf is the claim of the verifier gate preassigned to author.
func gateOf(f *vfs.FS, author vfs.AgentID) (vfs.OwnershipClaim, bool) {
	for _, claim := range f.OwnershipClaims(testSession) {
		if claim.AuthorKey == author {
			return claim, true
		}
	}
	return vfs.OwnershipClaim{}, false
}

// stagedContent is the content key has staged for path; false when it holds
// nothing there or a deletion.
func stagedContent(f *vfs.FS, key vfs.AgentID, path string) (string, bool) {
	content := f.InspectDelta(key).Files[path]
	if content == nil {
		return "", false
	}
	return *content, true
}

// refused fails unless err is want. An identity refusal must also state its
// cause, so it never reads as the bare sentinel.
func refused(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s = %v; want %v", what, err, want)
		return
	}
	if want == vfs.ErrIdentity && err.Error() == vfs.ErrIdentity.Error() {
		t.Errorf("%s = %v; an identity refusal states its cause", what, err)
	}
}

// refusedNaming is refused for a denial that also names the calls the agent
// continues with.
func refusedNaming(t *testing.T, what string, err, want error, calls ...string) {
	t.Helper()
	refused(t, what, err, want)
	for _, call := range calls {
		if err == nil || !strings.Contains(err.Error(), call) {
			t.Errorf("%s = %v; the denial should name %q", what, err, call)
		}
	}
}

func reassign(t *testing.T, f *vfs.FS, identity vfs.Identity, key vfs.AgentID, scope ...string) {
	t.Helper()
	if err := f.ReassignScope(identity, key, scope); err != nil {
		t.Fatalf("ReassignScope() = %v", err)
	}
}

func TestBindRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		identity vfs.Identity
		scope    []string
		want     error
	}{
		"unknown specialist":            {ident("a", "u", unknownInstance), []string{"a.go"}, vfs.ErrIdentity},
		"verifier without preassign":    {ident("v", "u", "verify"), nil, vfs.ErrScopeDenied},
		"incomplete identity":           {vfs.Identity{SessionID: testSession, AgentID: "a", Specialist: "dev"}, []string{"a.go"}, vfs.ErrIdentity},
		"specialist without bind grant": {ident("a", "u", noGrantInstance), nil, vfs.ErrScopeDenied},
		"scope escapes workspace":       {ident("a", "u", "dev"), []string{escapingPath}, vfs.ErrInvalidPath},
		"invariant escapes workspace": {
			vfs.Identity{SessionID: testSession, WorkUnitID: "u", AgentID: "a", Specialist: "dev", Invariants: vfs.InvariantSet{escapingPath}},
			[]string{"a.go"}, vfs.ErrIdentity},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := newStore(t)
			_, err := f.Bind(tc.identity, tc.scope)
			refused(t, "Bind()", err, tc.want)
			if len(f.OwnershipClaims(testSession)) != 0 {
				t.Fatal("a refused bind left a claim behind")
			}
		})
	}
}

// Claiming again as the same agent of the same attempt is how an agent learns
// the key it already holds; a different scope is a reassignment, which the
// denial sends to the orchestrator.
func TestRepeatedClaimReturnsTheHeldKeyOrNamesTheReassignment(t *testing.T) {
	for name, claim := range map[string]func(*vfs.FS, []string) (vfs.AgentID, error){
		"Bind": func(f *vfs.FS, scope []string) (vfs.AgentID, error) { return f.Bind(ident("a", "u", "dev"), scope) },
		"AssignScope": func(f *vfs.FS, scope []string) (vfs.AgentID, error) {
			return f.AssignScope(ident("a", "u", "dev"), scope)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := newStore(t)
			key, err := claim(f, []string{"a.go"})
			if err != nil {
				t.Fatal(err)
			}
			if again, err := claim(f, []string{"a.go"}); err != nil || again != key {
				t.Fatalf("same scope again = %q, %v; want the held key %q", again, err, key)
			}
			_, err = claim(f, []string{"b.go"})
			refusedNaming(t, "another scope", err, vfs.ErrScopeDenied, reassignCall, authorKeyInput)
			if claims := f.OwnershipClaims(testSession); len(claims) != 1 || !slices.Equal(claims[0].Scope, []string{"a.go"}) {
				t.Fatalf("claims = %+v; want only the first scope", claims)
			}
			if owner := ownerOf(f, "b.go"); owner != "" {
				t.Fatalf("the refused claim took b.go for %q", owner)
			}
		})
	}
}

func TestAssignScopeRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		identity vfs.Identity
		scope    []string
		want     error
	}{
		"empty scope":         {ident("a", "u", "dev"), nil, vfs.ErrIdentity},
		"no specialist":       {ident("a", "u", ""), []string{"a.go"}, vfs.ErrIdentity},
		"incomplete identity": {vfs.Identity{SessionID: testSession, AgentID: "a", Specialist: "dev"}, []string{"a.go"}, vfs.ErrIdentity},
		"unknown specialist":  {ident("a", "u", unknownInstance), []string{"a.go"}, vfs.ErrIdentity},
		"verifier target":     {ident("v", "u", "verify"), []string{"a.go"}, vfs.ErrScopeDenied},
		"invalid path":        {ident("a", "u", "dev"), []string{escapingPath}, vfs.ErrInvalidPath},
		"case-folded claim":   {ident("b", "u2", "dev"), []string{"A.GO"}, vfs.ErrCollision},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := newStore(t)
			notified := 0
			f.OnCollision(func(vfs.CollisionEvent) { notified++ })
			if _, err := f.AssignScope(ident("holder", "held", "dev"), []string{"a.go"}); err != nil {
				t.Fatal(err)
			}
			_, err := f.AssignScope(tc.identity, tc.scope)
			refused(t, "AssignScope()", err, tc.want)
			if len(f.OwnershipClaims(testSession)) != 1 {
				t.Fatalf("claims = %+v; want only the holder's", f.OwnershipClaims(testSession))
			}
			if notified != 0 {
				t.Fatalf("a refused assignment notified %d collisions", notified)
			}
		})
	}
}

func TestAdoptionRefusesMismatchedAssignment(t *testing.T) {
	f, _, _ := newStore(t)
	key, err := f.AssignScope(ident("dev-a", "u", "dev"), []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		mutate func(*vfs.Identity)
		scope  []string
	}{
		"another specialist":  {func(i *vfs.Identity) { i.Specialist = "fix" }, nil},
		"another attempt":     {func(i *vfs.Identity) { i.AttemptID = outsideAttempt }, nil},
		"another invariants":  {func(i *vfs.Identity) { i.Invariants = vfs.InvariantSet{"spec.md"} }, nil},
		"another gate author": {func(i *vfs.Identity) { i.GateAuthorKey = "someone" }, nil},
		"another scope":       {func(*vfs.Identity) {}, []string{"other.go"}},
	} {
		identity := ident("dev-a", "u", "dev")
		tc.mutate(&identity)
		_, err := f.Bind(identity, tc.scope)
		refused(t, name+": Bind()", err, vfs.ErrScopeDenied)
	}
	if claims := f.OwnershipClaims(testSession); len(claims) != 1 || !claims[0].Pending {
		t.Fatalf("a refused adoption changed the assignment: %+v", claims)
	}
	// The exact assignment, with the scope it reserved restated, is adopted.
	if adopted, err := f.Bind(ident("dev-a", "u", "dev"), []string{"a.go"}); err != nil || adopted != key {
		t.Fatalf("adopt = %q, %v; want %q", adopted, err, key)
	}
	if claims := f.OwnershipClaims(testSession); len(claims) != 1 || claims[0].Pending || !claims[0].Active {
		t.Fatalf("adopted claim = %+v; want active, not pending", claims)
	}
}

// The orchestrator can hand a key to any catalog specialist or change what a
// gate judges after assigning it, so adoption checks the grants and links the
// assignment needs when its target arrives, and a refusal keeps the assignment.
func TestAdoptionChecksTheGrantsTheAssignmentNeeds(t *testing.T) {
	for name, specialist := range map[string]string{
		"target without bind grant":  noGrantInstance,
		"target without write grant": "verify",
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := newStore(t)
			key := bind(t, f, "target", "tu", "dev", "t.go")
			handed := ident("target", "tu", specialist)
			reassign(t, f, handed, key, "t.go")
			_, err := f.Bind(handed, nil)
			refused(t, "Bind()", err, vfs.ErrScopeDenied)
			if claim, _ := claimOf(f, key); !claim.Pending {
				t.Fatal("a refused adoption consumed the assignment")
			}
		})
	}

	const authorCycle = "author-cycle"
	for name, tc := range map[string]struct {
		change func(t *testing.T, f *vfs.FS, author vfs.AgentID)
		want   error
	}{
		"gate judging itself": {func(t *testing.T, f *vfs.FS, author vfs.AgentID) {
			reassign(t, f, ident("v", "elsewhere", "dev"), author, "auth.go")
		}, vfs.ErrIdentity},
		"gate whose author cannot write": {func(t *testing.T, f *vfs.FS, author vfs.AgentID) {
			reassign(t, f, ident("auth", "u", "verify"), author, "auth.go")
		}, vfs.ErrScopeDenied},
		"gate against another invariant set": {func(t *testing.T, f *vfs.FS, author vfs.AgentID) {
			changed := ident("auth", "u", "dev")
			changed.Invariants = vfs.InvariantSet{"invariants.md"}
			reassign(t, f, changed, author, "auth.go")
		}, vfs.ErrIdentity},
		"gate whose author is gone": {func(t *testing.T, f *vfs.FS, _ vfs.AgentID) {
			if err := f.DropCycleStaging(authorCycle); err != nil {
				t.Fatal(err)
			}
		}, vfs.ErrIdentity},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := newStore(t)
			author, err := f.Bind(vfs.Identity{SessionID: testSession, WorkUnitID: "u", AttemptID: "1", AgentID: "auth", Specialist: "dev", CycleID: authorCycle}, []string{"auth.go"})
			if err != nil {
				t.Fatal(err)
			}
			gate := ident("v", "u", "verify")
			gate.AttemptID = "1"
			if _, err := f.AssignVerifier(gate, author); err != nil {
				t.Fatal(err)
			}
			tc.change(t, f, author)
			gate.GateAuthorKey = author
			_, err = f.Bind(gate, nil)
			refused(t, "Bind()", err, tc.want)
			if claim, ok := gateOf(f, author); !ok || !claim.Pending {
				t.Fatalf("gate claim = %+v, %v; a refused adoption must keep the assignment", claim, ok)
			}
		})
	}
}

func TestAssignVerifierRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		identity func(author vfs.AgentID) (vfs.Identity, vfs.AgentID)
		want     error
	}{
		"incomplete identity": {func(a vfs.AgentID) (vfs.Identity, vfs.AgentID) {
			return vfs.Identity{SessionID: testSession, Specialist: "verify"}, a
		}, vfs.ErrIdentity},
		"no specialist": {func(a vfs.AgentID) (vfs.Identity, vfs.AgentID) { return ident("v", "u", ""), a }, vfs.ErrIdentity},
		"no author key": {func(vfs.AgentID) (vfs.Identity, vfs.AgentID) { return ident("v", "u", "verify"), "" }, vfs.ErrIdentity},
		"unknown specialist": {func(a vfs.AgentID) (vfs.Identity, vfs.AgentID) {
			return ident("v", "u", unknownInstance), a
		}, vfs.ErrIdentity},
		"instance cannot verify": {func(a vfs.AgentID) (vfs.Identity, vfs.AgentID) { return ident("v", "u", "dev"), a }, vfs.ErrScopeDenied},
		"author not bound": {func(vfs.AgentID) (vfs.Identity, vfs.AgentID) {
			return ident("v", "u", "verify"), "ghost"
		}, vfs.ErrIdentity},
		"author is the verifier": {func(a vfs.AgentID) (vfs.Identity, vfs.AgentID) {
			return ident("author", "u", "verify"), a
		}, vfs.ErrIdentity},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := newStore(t)
			author := bind(t, f, "author", "u", "dev", "a.go")
			identity, authorKey := tc.identity(author)
			_, err := f.AssignVerifier(identity, authorKey)
			refused(t, "AssignVerifier()", err, tc.want)
		})
	}
	t.Run("same verifier twice", func(t *testing.T) {
		f, _, _ := newStore(t)
		author := bind(t, f, "author", "u", "dev", "a.go")
		if _, err := f.AssignVerifier(ident("v", "u", "verify"), author); err != nil {
			t.Fatal(err)
		}
		_, err := f.AssignVerifier(ident("v", "u", "verify"), author)
		refused(t, "duplicate verifier", err, vfs.ErrIdentity)
	})
}

// One verifier unit judging several authors holds one gate per author; each
// gate must be adoptable under the single attempt its delegation is admitted as.
func TestEveryGateOfOneVerifierUnitAdoptsTheAdmittedAttempt(t *testing.T) {
	f, _, _ := newStore(t)
	authors := []vfs.AgentID{bind(t, f, "author1", "u1", "dev", "a.go"), bind(t, f, "author2", "u2", "dev", "b.go")}
	for _, author := range authors {
		if _, err := f.AssignVerifier(ident("v", "vu", "verify"), author); err != nil {
			t.Fatal(err)
		}
	}
	for _, author := range authors {
		gate := ident("v", "vu", "verify")
		gate.AttemptID, gate.GateAuthorKey = "1", author
		if _, err := f.Bind(gate, nil); err != nil {
			t.Fatalf("gate for %s: %v", author, err)
		}
	}
}

func TestRevokeOwnershipNeverLeavesAnOwnerlessDelta(t *testing.T) {
	f, _, _ := newStore(t)
	key := bind(t, f, "author", "u", "dev", "a.go", "b.go")
	if err := f.RevokeOwnership(key); err != nil {
		t.Fatalf("release without staged work = %v", err)
	}
	for _, path := range []string{"a.go", "b.go"} {
		if owner := ownerOf(f, path); owner != "" {
			t.Fatalf("release kept %s for %q", path, owner)
		}
	}
	key = bind(t, f, "retry", "u2", "dev", "c.go")
	apply(t, f, applyCase{key, "create", 0, vfs.OpCreate, "c.go", "work in progress"})
	if claims := f.OwnershipClaims(testSession); len(claims) != 1 || !claims[0].Staged {
		t.Fatalf("claims = %+v; want one marked staged", claims)
	}
	refused(t, "release with staged work", f.RevokeOwnership(key), vfs.ErrStagedWork)
	if content, _ := stagedContent(f, key, "c.go"); ownerOf(f, "c.go") != key || content != "work in progress" {
		t.Fatal("a refused release changed the claim or its delta")
	}
}

func TestRevokeOwnershipRefusesUnknownKey(t *testing.T) {
	f, _, _ := newStore(t)
	refused(t, "RevokeOwnership(ghost)", f.RevokeOwnership("ghost"), vfs.ErrIdentity)
}

func TestReassignScopeKeepsTheStagedDelta(t *testing.T) {
	f, _, _ := newStore(t)
	key := bind(t, f, "author", "u", "dev", "a.go", "spare.go")
	staged := apply(t, f, applyCase{key, "create", 0, vfs.OpCreate, "a.go", "partial"})
	peer := bind(t, f, "peer", "other", "dev", "p.go")
	retry := vfs.Identity{SessionID: testSession, WorkUnitID: "u", AgentID: "author", Specialist: "dev"}
	for name, tc := range map[string]struct {
		identity vfs.Identity
		key      vfs.AgentID
		scope    []string
		want     error
	}{
		"drops a staged path":    {retry, key, []string{"b.go"}, vfs.ErrScopeDenied},
		"collides on added path": {retry, key, []string{"a.go", "p.go"}, vfs.ErrCollision},
		"unknown specialist": {
			vfs.Identity{SessionID: testSession, WorkUnitID: "u", AgentID: "author", Specialist: "ghost"}, key, []string{"a.go"}, vfs.ErrIdentity},
		"unknown key": {retry, "ghost", []string{"a.go"}, vfs.ErrIdentity},
	} {
		refused(t, name+": ReassignScope()", f.ReassignScope(tc.identity, tc.key, tc.scope), tc.want)
	}
	if ownerOf(f, "spare.go") != key || ownerOf(f, "p.go") != peer {
		t.Fatal("a refused reassignment changed ownership")
	}
	reassign(t, f, retry, key, "a.go", "b.go")
	if held, _ := f.BindingIdentity(key); !held.Prelaunch || held.AttemptID != "2" {
		t.Fatalf("binding = %+v; want a prelaunch claim on attempt 2", held)
	}
	if got := scopeOf(f, key); !slices.Equal(got, []string{"a.go", "b.go"}) {
		t.Fatalf("scope = %v; want the new scope owned and the unused spare.go released", got)
	}
	view := f.InspectDelta(key)
	if content, _ := stagedContent(f, key, "a.go"); content != "partial" || view.Revision != staged.Revision {
		t.Fatalf("delta = %+v; want the staged content and revision kept", view)
	}
}

func TestReassignScopeClearsTheVerdictOfTheOldRevision(t *testing.T) {
	r := newVerdictRig(t)
	if err := r.f.Verify(r.verifier, r.author, "reject", r.staged.Revision, r.staged.DeltaHash, false, "needs work"); err != nil {
		t.Fatal(err)
	}
	if err := r.f.ConsolidateCheckpoint(r.author, "cp", r.staged.Revision); !errors.Is(err, vfs.ErrVerificationRequired) {
		t.Fatalf("ConsolidateCheckpoint() = %v; want the failing verdict to block it", err)
	}
	reassign(t, r.f, ident("author", "u", "dev"), r.author, "a.go")
	if err := r.f.ConsolidateCheckpoint(r.author, "cp", r.staged.Revision); err != nil {
		t.Fatalf("ConsolidateCheckpoint() after the reassignment = %v; want the old verdict cleared", err)
	}
}

func TestReassignScopeHandsTheWorkToAnotherSpecialist(t *testing.T) {
	f, _, _ := newStore(t)
	key := bind(t, f, "author", "u", "dev", "a.go")
	staged := apply(t, f, applyCase{key, "create", 0, vfs.OpCreate, "a.go", "partial"})
	fix := vfs.Identity{SessionID: "later", WorkUnitID: "fix-u", AgentID: "fixer", Specialist: "fix"}
	reassign(t, f, fix, key, "a.go", "b.go")
	held, _ := f.BindingIdentity(key)
	if held.SessionID != "later" || held.WorkUnitID != "fix-u" || held.AgentID != "fixer" || held.Specialist != "fix" || !held.Prelaunch {
		t.Fatalf("binding = %+v; want the work handed to fix as a prelaunch claim", held)
	}
	if content, _ := stagedContent(f, key, "a.go"); content != "partial" || f.InspectDelta(key).Revision != staged.Revision {
		t.Fatalf("delta = %+v; want the staged content and revision kept", f.InspectDelta(key))
	}
	adopted, err := f.Bind(fix, []string{"a.go", "b.go"})
	if err != nil || adopted != key {
		t.Fatalf("Bind() = %v, %v; want the handed-over key %v", adopted, err, key)
	}
	if revision, _, ok := f.StagedState(key); !ok || revision != staged.Revision {
		t.Fatalf("StagedState() = %d, %v; want the kept revision %d", revision, ok, staged.Revision)
	}
	// A verifier assigned after the handover judges the new owner's work in its session.
	gate := vfs.Identity{SessionID: "later", WorkUnitID: "verify-u", AttemptID: "1", AgentID: "verify", Specialist: "verify"}
	if _, err := f.AssignVerifier(gate, key); err != nil {
		t.Fatalf("AssignVerifier() after the handover = %v", err)
	}
}

func TestCatalogLookups(t *testing.T) {
	if role, err := vfs.SpecialistRole("dev"); err != nil || role != model.RoleExecution {
		t.Fatalf("SpecialistRole(dev) = %q, %v", role, err)
	}
	// The coordinator is not a dispatchable specialist, so it has no role here.
	_, err := vfs.SpecialistRole(vfs.OrchestratorInstance)
	refused(t, "SpecialistRole("+vfs.OrchestratorInstance+")", err, vfs.ErrIdentity)
	_, err = vfs.SpecialistRole(unknownInstance)
	refused(t, "SpecialistRole(unknown)", err, vfs.ErrIdentity)
	if err := vfs.RequireVFSCapability("dev", model.VFSCapabilityRead); err != nil {
		t.Fatalf("dev read grant: %v", err)
	}
	refused(t, "explicitly empty grant list", vfs.RequireVFSCapability(noGrantInstance, model.VFSCapabilityRead), vfs.ErrScopeDenied)
	refused(t, "unknown instance", vfs.RequireVFSCapability(unknownInstance, model.VFSCapabilityRead), vfs.ErrIdentity)
}

func TestUnknownKeyHasNoDelta(t *testing.T) {
	f, _, _ := newStore(t)
	idle := bind(t, f, "idle", "u", "dev", "a.go")
	_, emptyHash, _ := f.StagedState(idle)
	if revision, hash, staged := f.StagedState("ghost"); revision != 0 || staged || hash != emptyHash {
		t.Fatalf("StagedState(ghost) = %d, %q, %v; want nothing staged and the hash of an empty delta %q", revision, hash, staged, emptyHash)
	}
	if view := f.InspectDelta("ghost"); view.Revision != 0 || view.Hash != emptyHash || len(view.Files) != 0 {
		t.Fatalf("InspectDelta(ghost) = %+v; want an empty view", view)
	}
}

func TestApplyRefusals(t *testing.T) {
	f, _, _ := newStore(t)
	key := bind(t, f, "author", "u", "dev", "a.go")
	created := apply(t, f, applyCase{key, "create", 0, vfs.OpCreate, "a.go", "v1"})
	// The work of a unit is handed to a specialist that holds no VFS grants.
	handed := bind(t, f, "handed", "hu", "dev", "handed.go")
	reassign(t, f, ident("handed", "hu", noGrantInstance), handed, "handed.go")

	for name, tc := range map[string]struct {
		op    vfs.Operation
		want  error
		names []string
	}{
		"unknown key":          {vfs.Operation{Key: "ghost", CallID: "c1", Action: vfs.OpRead, Path: "a.go"}, vfs.ErrIdentity, []string{reassignCall}},
		"empty call id":        {vfs.Operation{Key: key, Action: vfs.OpRead, Path: "a.go", ExpectedRevision: created.Revision}, vfs.ErrIdentity, nil},
		"stale revision":       {vfs.Operation{Key: key, CallID: "c2", Action: vfs.OpRead, Path: "a.go", ExpectedRevision: created.Revision + 1}, vfs.ErrStaleRevision, nil},
		"replayed call":        {vfs.Operation{Key: key, CallID: "create", Action: vfs.OpRead, Path: "a.go", ExpectedRevision: created.Revision}, vfs.ErrDuplicateCall, nil},
		"read without grant":   {vfs.Operation{Key: handed, CallID: "c3", Action: vfs.OpRead, Path: "handed.go"}, vfs.ErrScopeDenied, nil},
		"write without grant":  {vfs.Operation{Key: handed, CallID: "c4", Action: vfs.OpCreate, Path: "handed.go"}, vfs.ErrScopeDenied, nil},
		"delete without grant": {vfs.Operation{Key: handed, CallID: "c5", Action: vfs.OpDelete, Path: "handed.go"}, vfs.ErrScopeDenied, nil},
		"path outside scope": {vfs.Operation{Key: key, CallID: "c6", Action: vfs.OpCreate, Path: "elsewhere.go", ExpectedRevision: created.Revision},
			vfs.ErrScopeDenied, []string{reassignCall, authorKeyInput}},
		"path escapes": {vfs.Operation{Key: key, CallID: "c7", Action: vfs.OpCreate, Path: escapingPath, ExpectedRevision: created.Revision}, vfs.ErrInvalidPath, nil},
	} {
		_, err := f.Apply(tc.op)
		refusedNaming(t, name+": Apply()", err, tc.want, tc.names...)
	}
	if _, err := f.Apply(vfs.Operation{Key: key, CallID: "c8", Action: unsupportedAction, ExpectedRevision: created.Revision}); err == nil {
		t.Error("an unsupported action was accepted")
	}
	denied := 0
	for _, e := range f.JournalPage("", "", -1, vfs.MaxJournalPageSize) {
		if e.Outcome == "denied" {
			denied++
		}
	}
	if denied == 0 {
		t.Fatal("refused operations left no denied journal entry")
	}
	if got := f.InspectDelta(key); got.Revision != created.Revision {
		t.Fatalf("refused operations moved the revision to %d", got.Revision)
	}
}

func TestReadAsRefusals(t *testing.T) {
	f, _, _ := newStore(t)
	author := bind(t, f, "author", "u", "dev", "a.go")
	created := apply(t, f, applyCase{author, "create", 0, vfs.OpCreate, "a.go", "staged"})
	verifier := bind(t, f, "judge", "u", "verify")
	stranger := bind(t, f, "stranger", "u2", "dev", "b.go")
	read := func(key vfs.AgentID, call string) vfs.Operation {
		return vfs.Operation{Key: key, CallID: call, ExpectedRevision: created.Revision, Action: vfs.OpRead, Path: "a.go"}
	}
	_, err := f.ReadAs(vfs.Operation{Key: verifier, CallID: "w", Action: vfs.OpCreate, Path: "a.go"}, author)
	refused(t, "ReadAs with a write action", err, vfs.ErrIdentity)
	_, err = f.ReadAs(read(verifier, "ghost-view"), "ghost")
	refused(t, "ReadAs of an unbound view", err, vfs.ErrIdentity)
	_, err = f.ReadAs(read(stranger, "no-verify"), author)
	refused(t, "ReadAs by an instance without verify", err, vfs.ErrScopeDenied)
	_, err = f.ReadAs(read(verifier, "unlinked"), stranger)
	refused(t, "ReadAs of an author the gate is not linked to", err, vfs.ErrIdentity)
	got, err := f.ReadAs(read(verifier, "linked"), author)
	if err != nil || string(got.Content) != "staged" {
		t.Errorf("ReadAs of the linked author = %q, %v; want the staged content", got.Content, err)
	}
}

func TestDeletionIsStagedAndReadsAsAbsent(t *testing.T) {
	f, root, _ := newStore(t)
	if err := os.WriteFile(filepath.Join(root, "d.txt"), []byte("on disk"), diskFileMode); err != nil {
		t.Fatal(err)
	}
	key := bind(t, f, "author", "u", "dev", "d.txt")
	read := apply(t, f, applyCase{key, "read", 0, vfs.OpRead, "d.txt", ""})
	if string(read.Content) != "on disk" {
		t.Fatalf("read = %q; want the base content", read.Content)
	}
	deleted := apply(t, f, applyCase{key, "delete", read.Revision, vfs.OpDelete, "d.txt", ""})
	if deleted.Revision != read.Revision+1 {
		t.Fatalf("delete revision = %d; want %d", deleted.Revision, read.Revision+1)
	}
	if _, err := f.Apply(vfs.Operation{Key: key, CallID: "again", ExpectedRevision: deleted.Revision, Action: vfs.OpRead, Path: "d.txt"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read of a staged deletion = %v; want not-exist", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "d.txt")); err != nil || string(data) != "on disk" {
		t.Fatalf("staging a deletion touched the disk: %q, %v", data, err)
	}
	var outcomes []string
	for _, e := range f.JournalPage("", "", -1, vfs.MaxJournalPageSize) {
		outcomes = append(outcomes, string(e.Operation)+":"+e.Outcome)
	}
	if !slices.Contains(outcomes, "read:not_found") {
		t.Fatalf("journal = %v; want the not_found read recorded", outcomes)
	}
}

// A refused foreign access, whether an operation or a claim, is observed and
// leaves nothing behind: the agent's own work stages and consolidates.
func TestRefusedForeignAccessNeverBlocksConsolidation(t *testing.T) {
	f, root, _ := newStore(t)
	var seen []vfs.CollisionEvent
	f.OnCollision(func(e vfs.CollisionEvent) { seen = append(seen, e) })
	bind(t, f, "owner", "u1", "dev", "a.go")
	intruder := bind(t, f, "intruder", "u2", "dev", "b.go")

	_, err := f.Apply(vfs.Operation{Key: intruder, CallID: "peek", Action: vfs.OpRead, Path: "a.go"})
	refused(t, "foreign read", err, vfs.ErrCollision)
	_, err = f.Apply(vfs.Operation{Key: intruder, CallID: "grab", Action: vfs.OpCreate, Path: "a.go", Content: []byte("x")})
	refused(t, "foreign write", err, vfs.ErrCollision)
	if len(seen) != 2 || seen[0].Path != "a.go" || seen[0].AttemptingAgent != "intruder" || seen[0].OwningAgent != "owner" {
		t.Fatalf("collision handler saw %+v", seen)
	}

	staged := apply(t, f, applyCase{intruder, "own", 0, vfs.OpCreate, "b.go", "ok"})
	_, err = f.Bind(ident("loser", "u2", "dev"), []string{"a.go"})
	refused(t, "Bind() over a foreign path", err, vfs.ErrCollision)
	if len(seen) != 3 {
		t.Fatalf("collision handler saw %d events; want the rejected bind observed too", len(seen))
	}

	if err := f.ConsolidateCheckpoint(intruder, "own", staged.Revision); err != nil {
		t.Fatalf("ConsolidateCheckpoint() after refused foreign access = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "b.go")); err != nil || string(data) != "ok" {
		t.Fatalf("consolidated file = %q, %v; want the intruder's own work", data, err)
	}
}

// verdictRig is an author with one staged file and a linked verifier gate.
type verdictRig struct {
	f        *vfs.FS
	root     string
	author   vfs.AgentID
	verifier vfs.AgentID
	staged   vfs.OperationResult
}

// newVerdictRig stages an author's work under the invariant documents given,
// which exist in the workspace.
func newVerdictRig(t *testing.T, invariants ...string) verdictRig {
	t.Helper()
	f, root, _ := newStore(t)
	for _, document := range invariants {
		if err := os.WriteFile(filepath.Join(root, document), []byte("the governing goal"), diskFileMode); err != nil {
			t.Fatal(err)
		}
	}
	author, err := f.Bind(vfs.Identity{SessionID: testSession, WorkUnitID: "u", AttemptID: "1", AgentID: "author", Specialist: "dev", Invariants: invariants}, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	staged := apply(t, f, applyCase{author, "create", 0, vfs.OpCreate, "a.go", "v1"})
	return verdictRig{f, root, author, bind(t, f, "judge", "u", "verify"), staged}
}

// unreadable replaces a regular workspace document with a directory, so
// reading it fails; restore puts the document back.
func unreadable(t *testing.T, root, name string) (restore func()) {
	t.Helper()
	path := filepath.Join(root, name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, diskDirMode); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, diskFileMode); err != nil {
			t.Fatal(err)
		}
	}
}

// A refused verdict is never attached: the failing verdicts below would block
// the consolidation that follows them if any had been.
func TestVerifyRefusals(t *testing.T) {
	r := newVerdictRig(t)
	rev, hash := r.staged.Revision, r.staged.DeltaHash
	const finding = "does not meet the contract"
	for name, tc := range map[string]struct {
		call func() error
		want error
	}{
		"empty call id":           {func() error { return r.f.Verify(r.verifier, r.author, "", rev, hash, false, finding) }, vfs.ErrIdentity},
		"unknown verifier":        {func() error { return r.f.Verify("ghost", r.author, "v1", rev, hash, false, finding) }, vfs.ErrIdentity},
		"unknown author":          {func() error { return r.f.Verify(r.verifier, "ghost", "v2", rev, hash, false, finding) }, vfs.ErrIdentity},
		"caller lacks verify":     {func() error { return r.f.Verify(r.author, r.author, "v3", rev, hash, false, finding) }, vfs.ErrScopeDenied},
		"stale revision":          {func() error { return r.f.Verify(r.verifier, r.author, "v4", rev+1, hash, false, finding) }, vfs.ErrInvalidVerdict},
		"wrong delta hash":        {func() error { return r.f.Verify(r.verifier, r.author, "v5", rev, "deadbeef", false, finding) }, vfs.ErrInvalidVerdict},
		"failure without finding": {func() error { return r.f.Verify(r.verifier, r.author, "v6", rev, hash, false, "  ") }, vfs.ErrInvalidVerdict},
	} {
		refused(t, name+": Verify()", tc.call(), tc.want)
	}
	if err := r.f.ConsolidateCheckpoint(r.author, "cp", rev); err != nil {
		t.Fatalf("ConsolidateCheckpoint() = %v; a refused verdict was attached to the delta", err)
	}
}

func TestVerifyRefusesSelfVerification(t *testing.T) {
	r := newVerdictRig(t)
	// The orchestrator hands the author's work to the agent holding its gate.
	reassign(t, r.f, ident("judge", "handover", "dev"), r.author, "a.go")
	err := r.f.Verify(r.verifier, r.author, "self", r.staged.Revision, r.staged.DeltaHash, true, "")
	refused(t, "Verify()", err, vfs.ErrSelfVerification)
}

// A verdict pins the invariant documents as they read at the moment it is
// recorded and again when its work consolidates.
func TestUnreadableInvariantDocumentsRefuseTheVerdictAndItsConsolidation(t *testing.T) {
	const document = "invariants.md"
	t.Run("verdict", func(t *testing.T) {
		r := newVerdictRig(t, document)
		restore := unreadable(t, r.root, document)
		err := r.f.Verify(r.verifier, r.author, "inv", r.staged.Revision, r.staged.DeltaHash, false, "does not meet the contract")
		refused(t, "Verify()", err, vfs.ErrIdentity)
		restore()
		if err := r.f.ConsolidateCheckpoint(r.author, "cp", r.staged.Revision); err != nil {
			t.Fatalf("ConsolidateCheckpoint() = %v; a verdict was attached although its invariants could not be pinned", err)
		}
	})
	t.Run("consolidation", func(t *testing.T) {
		r := newVerdictRig(t, document)
		if err := r.f.Verify(r.verifier, r.author, "pass", r.staged.Revision, r.staged.DeltaHash, true, ""); err != nil {
			t.Fatal(err)
		}
		restore := unreadable(t, r.root, document)
		refused(t, "ConsolidateCheckpoint()", r.f.ConsolidateCheckpoint(r.author, "cp", r.staged.Revision), vfs.ErrIdentity)
		restore()
		if err := r.f.ConsolidateCheckpoint(r.author, "cp", r.staged.Revision); err != nil {
			t.Fatalf("ConsolidateCheckpoint() once the documents are readable = %v", err)
		}
	})
}

func TestFailingVerdictBlocksConsolidationWithItsFinding(t *testing.T) {
	r := newVerdictRig(t)
	const finding = "missing edge case"
	if err := r.f.Verify(r.verifier, r.author, "reject", r.staged.Revision, r.staged.DeltaHash, false, finding); err != nil {
		t.Fatal(err)
	}
	err := r.f.ConsolidateCheckpoint(r.author, "cp", r.staged.Revision)
	// The finding is how the orchestrator learns what to correct.
	if !errors.Is(err, vfs.ErrVerificationRequired) || !strings.Contains(err.Error(), finding) {
		t.Fatalf("ConsolidateCheckpoint() = %v; want ErrVerificationRequired carrying %q", err, finding)
	}
	for _, e := range r.f.JournalPage("", "", -1, vfs.MaxJournalPageSize) {
		entry, marshalErr := json.Marshal(e)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if strings.Contains(string(entry), finding) {
			t.Fatalf("the finding leaked into the content-free journal: %s", entry)
		}
	}
}

func TestConsolidateCheckpointRefusals(t *testing.T) {
	r := newVerdictRig(t)
	rev := r.staged.Revision
	for name, tc := range map[string]struct {
		key        vfs.AgentID
		checkpoint string
		revision   uint64
		want       error
	}{
		"unknown key":           {"ghost", "cp", rev, vfs.ErrIdentity},
		"stale revision":        {r.author, "cp", rev + 1, vfs.ErrStaleRevision},
		"binding without delta": {r.verifier, "cp", 0, vfs.ErrNothingStaged},
	} {
		refused(t, name+": ConsolidateCheckpoint()", r.f.ConsolidateCheckpoint(tc.key, tc.checkpoint, tc.revision), tc.want)
	}
}

// Without a gate, ordinary delegated work consolidates on its ownership and
// revision alone; the claim is released and a blank label is accepted.
func TestConsolidateCheckpointWithoutAGate(t *testing.T) {
	r := newVerdictRig(t)
	if err := r.f.ConsolidateCheckpoint(r.author, "  ", r.staged.Revision); err != nil {
		t.Fatalf("authorized consolidation without a gate and with a blank label = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(r.root, "a.go")); err != nil || string(data) != "v1" {
		t.Fatalf("consolidated file = %q, %v", data, err)
	}
	if claim, ok := claimOf(r.f, r.author); ok && len(claim.Scope) != 0 {
		t.Fatal("consolidation retained the author's ownership")
	}
	refused(t, "consolidating again", r.f.ConsolidateCheckpoint(r.author, "again", r.staged.Revision), vfs.ErrNothingStaged)
}

func TestAssignScopeAcrossRoots(t *testing.T) {
	// crossRoot leaves "old-unit" holding staged work and an unused path in an
	// earlier root, next to a peer in the same root and a claim in the new one,
	// then reopens the store from disk.
	type rig struct {
		f               *vfs.FS
		old, peer, same vfs.AgentID
		next            vfs.Identity
	}
	crossRoot := func(t *testing.T) rig {
		f, root, state := newStore(t)
		old := bind(t, f, "old", "old-unit", "dev", "index.html", "extra.html")
		apply(t, f, applyCase{old, "create", 0, vfs.OpCreate, "index.html", "previous"})
		peer := bind(t, f, "peer", "peer-unit", "dev", "peer.html")
		same, err := f.AssignScope(vfs.Identity{SessionID: "new-root", WorkUnitID: "same-unit", AgentID: "same", Specialist: "dev"}, []string{"same.html"})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := vfs.Open(root, state)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = reopened.Close() })
		return rig{reopened, old, peer, same, vfs.Identity{SessionID: "new-root", WorkUnitID: "new-unit", AgentID: "new", Specialist: "dev"}}
	}

	t.Run("replace", func(t *testing.T) {
		r := crossRoot(t)
		key, err := r.f.AssignScope(r.next, []string{"index.html"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, staged := r.f.StagedState(r.old); ownerOf(r.f, "index.html") != key || staged {
			t.Fatal("prior claim not replaced")
		}
		if owner := ownerOf(r.f, "extra.html"); owner != "" {
			t.Fatalf("partial old claim retained by %q", owner)
		}
		if ownerOf(r.f, "peer.html") != r.peer || ownerOf(r.f, "same.html") != r.same {
			t.Fatal("unrelated claim lost")
		}
		for _, claim := range r.f.OwnershipClaims("new-root") {
			if claim.Key == r.old {
				t.Fatal("discarded claim still pending")
			}
		}
	})

	for name, tc := range map[string]struct {
		extra string
		want  error
	}{
		"invalid path":        {escapingPath, vfs.ErrInvalidPath},
		"same root collision": {"same.html", vfs.ErrCollision},
	} {
		t.Run(name, func(t *testing.T) {
			r := crossRoot(t)
			_, err := r.f.AssignScope(r.next, []string{"index.html", tc.extra})
			refused(t, "AssignScope()", err, tc.want)
			if _, _, staged := r.f.StagedState(r.old); ownerOf(r.f, "index.html") != r.old || !staged || ownerOf(r.f, "extra.html") != r.old {
				t.Fatal("refusal discarded old claim")
			}
		})
	}

	t.Run("continue", func(t *testing.T) {
		r := crossRoot(t)
		reassign(t, r.f, r.next, r.old, "index.html")
		if content, _ := stagedContent(r.f, r.old, "index.html"); content != "previous" {
			t.Fatal("continuation lost staging")
		}
	})
}

// TestWorkspaceLocalStoreIsPrivate checks that the workspace-local store is created
// private (0700), cannot be claimed as workspace content, and imports no claims from
// a store kept elsewhere.
func TestWorkspaceLocalStoreIsPrivate(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	global := filepath.Join(home, ".local", "share", "takt-ai", "vfs", "old-store")
	previous, err := vfs.Open(root, global)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := previous.AssignScope(ident("old", "old", "dev"), []string{"index.html"}); err != nil {
		t.Fatal(err)
	}
	if err := previous.Close(); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, ".takt-ai", "vfs")
	f, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, path := range []string{".takt-ai/vfs/state.db", ".takt-ai/other", ".TAKT-AI/vfs/new"} {
		_, err := f.AssignScope(ident("a", "u", "dev"), []string{path})
		refused(t, "private path "+path, err, vfs.ErrInvalidPath)
	}
	info, err := os.Stat(state)
	if err != nil || info.Mode().Perm() != privateStoreMode {
		t.Fatalf("private store: %v %v", info, err)
	}
	if len(f.OwnershipClaims(testSession)) != 0 {
		t.Fatal("new local store imported claims")
	}
}

// An admitted retry must adopt its own gate, even when the previous launch
// ended before adopting its assignment.
func TestVerifierRetryReplacesUnadoptedGate(t *testing.T) {
	f, _, _ := newStore(t)
	author := bind(t, f, "dev", "writer", "dev", "a.go")
	identity := vfs.Identity{SessionID: testSession, WorkUnitID: "review", AttemptID: "1", AgentID: "verify", Specialist: "verify"}
	old, err := f.AssignVerifier(identity, author)
	if err != nil {
		t.Fatal(err)
	}
	identity.AttemptID = "2"
	fresh, err := f.AssignVerifier(identity, author)
	if err != nil {
		t.Fatal(err)
	}
	if previous, _ := f.BindingIdentity(old); previous.Prelaunch {
		t.Fatal("previous gate remained pending")
	}
	identity.GateAuthorKey = author
	adopted, err := f.Bind(identity, nil)
	if err != nil || adopted != fresh {
		t.Fatalf("retry adopted %q, %v; want %q", adopted, err, fresh)
	}
}

// An Engram invariant pins itself: its version needs no workspace document, and
// changes with the entries declared and their precedence order.
func TestEngramInvariantsVersion(t *testing.T) {
	versionOf := func(set ...int64) string {
		t.Helper()
		f, _, _ := newStore(t)
		set2 := make(vfs.InvariantSet, 0, len(set))
		for _, id := range set {
			set2 = append(set2, vfs.EngramInvariant(id))
		}
		key, err := f.Bind(vfs.Identity{SessionID: testSession, WorkUnitID: "u", AttemptID: "1", AgentID: "author", Specialist: "dev", Invariants: set2}, []string{"a.go"})
		if err != nil {
			t.Fatal(err)
		}
		identity, _ := f.BindingIdentity(key)
		return identity.InvariantsHash
	}
	base := versionOf(7, 8)
	if again := versionOf(7, 8); again != base {
		t.Errorf("the same entries gave versions %q and %q", base, again)
	}
	for name, other := range map[string]string{
		"another entry": versionOf(7, 9),
		"reordered":     versionOf(8, 7),
		"fewer entries": versionOf(7),
		"none":          versionOf(),
	} {
		if other == base {
			t.Errorf("%s kept version %q", name, base)
		}
	}
}

// An invariant that names no Engram entry is refused at binding instead of
// being pinned as if it were one.
func TestMalformedEngramInvariantIsRefused(t *testing.T) {
	for _, invariant := range []string{"engram:", "engram:abc", "engram:0", "engram:-3"} {
		f, _, _ := newStore(t)
		_, err := f.Bind(vfs.Identity{SessionID: testSession, WorkUnitID: "u", AttemptID: "1", AgentID: "author", Specialist: "dev", Invariants: vfs.InvariantSet{invariant}}, []string{"a.go"})
		if !errors.Is(err, vfs.ErrIdentity) {
			t.Errorf("Bind with %q error = %v, want ErrIdentity", invariant, err)
		}
	}
}

// A gate in a unit of its own, declaring nothing, is judged against the
// invariants its author was issued, so its verdict governs the author's work.
func TestVerifierGateInheritsTheAuthorsEngramInvariants(t *testing.T) {
	f, _, _ := newStore(t)
	set := vfs.InvariantSet{vfs.EngramInvariant(7), vfs.EngramInvariant(8)}
	author, err := f.Bind(vfs.Identity{SessionID: testSession, WorkUnitID: "impl", AttemptID: "1", AgentID: "author", Specialist: "dev", Invariants: set}, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	staged := apply(t, f, applyCase{author, "create", 0, vfs.OpCreate, "a.go", "v1"})
	gate, err := f.AssignVerifier(ident("judge", "verify-impl", "verify"), author)
	if err != nil {
		t.Fatalf("AssignVerifier() = %v; a gate declaring no invariants must take its author's", err)
	}
	judged, _ := f.BindingIdentity(gate)
	authored, _ := f.BindingIdentity(author)
	if judged.InvariantsHash != authored.InvariantsHash {
		t.Fatalf("gate version %q; want the author's %q", judged.InvariantsHash, authored.InvariantsHash)
	}
	if err := f.Verify(gate, author, "v1", staged.Revision, staged.DeltaHash, true, ""); err != nil {
		t.Fatalf("Verify() = %v", err)
	}
	if err := f.ConsolidateCheckpoint(author, "cp", staged.Revision); err != nil {
		t.Fatalf("ConsolidateCheckpoint() = %v; the verdict was judged against the author's invariants", err)
	}
}
