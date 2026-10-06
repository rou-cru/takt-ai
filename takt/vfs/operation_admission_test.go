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

	"github.com/rou-cru/takt-ai/takt/model"
)

const (
	// escapingPath leaves the workspace, so no scope or invariant may name it.
	escapingPath = "../escape"
	// unknownInstance is a specialist name the catalog does not declare.
	unknownInstance = "no-such-specialist"
	// noGrantInstance is a catalog instance that declares an explicit empty grant list.
	noGrantInstance = "judge-a"
	// unsupportedAction is an operation type the dispatcher does not implement.
	unsupportedAction OperationType = "bogus"
	// outsideAttempt is an attempt identity no assignment was issued for.
	outsideAttempt = "9"
)

func id(agent, unit, specialist string) Identity {
	return Identity{SessionID: "s", WorkUnitID: unit, AgentID: AgentID(agent), Specialist: specialist}
}

func TestBindRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		identity Identity
		scope    []string
		want     error
	}{
		"unknown specialist":            {id("a", "u", unknownInstance), []string{"a.go"}, ErrIdentity},
		"verifier without preassign":    {id("v", "u", "verify"), nil, ErrScopeDenied},
		"incomplete identity":           {Identity{SessionID: "s", AgentID: "a", Specialist: "dev"}, []string{"a.go"}, ErrIdentity},
		"specialist without bind grant": {id("a", "u", noGrantInstance), nil, ErrScopeDenied},
		"scope escapes workspace":       {id("a", "u", "dev"), []string{escapingPath}, ErrInvalidPath},
		"invariant escapes workspace": {
			Identity{SessionID: "s", WorkUnitID: "u", AgentID: "a", Specialist: "dev", Invariants: InvariantSet{escapingPath}},
			[]string{"a.go"}, ErrIdentity},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := durable(t)
			if _, err := f.Bind(tc.identity, tc.scope); !errors.Is(err, tc.want) {
				t.Fatalf("Bind() = %v; want %v", err, tc.want)
			}
			if len(f.OwnershipClaims("s")) != 0 {
				t.Fatal("a refused bind left a claim behind")
			}
		})
	}
}

func TestBindRefusesTheSameAgentTwiceInOneAttempt(t *testing.T) {
	f, _, _ := durable(t)
	if _, err := f.Bind(id("a", "u", "dev"), []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Bind(id("a", "u", "dev"), []string{"b.go"}); !errors.Is(err, ErrIdentity) {
		t.Fatalf("second bind of the same agent = %v; want ErrIdentity", err)
	}
	if _, owned := f.owners["b.go"]; owned {
		t.Fatal("the refused bind still took its scope")
	}
}

func TestAssignScopeRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		identity Identity
		scope    []string
		want     error
	}{
		"empty scope":         {id("a", "u", "dev"), nil, ErrIdentity},
		"no specialist":       {id("a", "u", ""), []string{"a.go"}, ErrIdentity},
		"incomplete identity": {Identity{SessionID: "s", AgentID: "a", Specialist: "dev"}, []string{"a.go"}, ErrIdentity},
		"unknown specialist":  {id("a", "u", unknownInstance), []string{"a.go"}, ErrIdentity},
		"verifier target":     {id("v", "u", "verify"), []string{"a.go"}, ErrScopeDenied},
		"invalid path":        {id("a", "u", "dev"), []string{escapingPath}, ErrInvalidPath},
		"case-folded claim":   {id("b", "u2", "dev"), []string{"A.GO"}, ErrCollision},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := durable(t)
			if _, err := f.AssignScope(id("holder", "held", "dev"), []string{"a.go"}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.AssignScope(tc.identity, tc.scope); !errors.Is(err, tc.want) {
				t.Fatalf("AssignScope() = %v; want %v", err, tc.want)
			}
			if len(f.OwnershipClaims("s")) != 1 {
				t.Fatalf("claims = %+v; want only the holder's", f.OwnershipClaims("s"))
			}
			if len(f.collisions) != 0 {
				t.Fatalf("a refused assignment recorded collisions: %+v", f.collisions)
			}
		})
	}
}

func TestAssignScopeRefusesDuplicateIdentity(t *testing.T) {
	f, _, _ := durable(t)
	if _, err := f.AssignScope(id("a", "u", "dev"), []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.AssignScope(id("a", "u", "dev"), []string{"b.go"}); !errors.Is(err, ErrIdentity) {
		t.Fatalf("duplicate assignment = %v; want ErrIdentity", err)
	}
}

func TestAdoptionRefusesMismatchedAssignment(t *testing.T) {
	f, _, _ := durable(t)
	key, err := f.AssignScope(id("dev-a", "u", "dev"), []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		mutate func(*Identity)
		scope  []string
	}{
		"another specialist":  {func(i *Identity) { i.Specialist = "fix" }, nil},
		"another attempt":     {func(i *Identity) { i.AttemptID = outsideAttempt }, nil},
		"another invariants":  {func(i *Identity) { i.Invariants = InvariantSet{"spec.md"} }, nil},
		"another gate author": {func(i *Identity) { i.GateAuthorKey = "someone" }, nil},
		"another scope":       {func(*Identity) {}, []string{"other.go"}},
	} {
		identity := id("dev-a", "u", "dev")
		tc.mutate(&identity)
		if _, err := f.Bind(identity, tc.scope); !errors.Is(err, ErrScopeDenied) {
			t.Errorf("%s: Bind() = %v; want ErrScopeDenied", name, err)
		}
	}
	if claims := f.OwnershipClaims("s"); len(claims) != 1 || !claims[0].Pending {
		t.Fatalf("a refused adoption changed the assignment: %+v", claims)
	}
	// The exact assignment, with the scope it reserved restated, is adopted.
	if adopted, err := f.Bind(id("dev-a", "u", "dev"), []string{"a.go"}); err != nil || adopted != key {
		t.Fatalf("adopt = %q, %v; want %q", adopted, err, key)
	}
	if claims := f.OwnershipClaims("s"); len(claims) != 1 || claims[0].Pending || !claims[0].Active {
		t.Fatalf("adopted claim = %+v; want active, not pending", claims)
	}
}

func TestAdoptionChecksTheGrantsTheAssignmentNeeds(t *testing.T) {
	author := func(t *testing.T, f *FS) AgentID { return bind(t, f, "auth", "u", "dev", "auth.go") }
	forgedAuthor := func(agent, specialist string, hash string) Identity {
		return Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: AgentID(agent), Specialist: specialist, InvariantsHash: hash}
	}
	for name, tc := range map[string]struct {
		assigned func(t *testing.T, f *FS) Identity
		want     error
	}{
		"target without bind grant": {
			func(*testing.T, *FS) Identity { return id("v", "u", "spec") }, ErrScopeDenied},
		"gate on an instance without verify": {
			func(t *testing.T, f *FS) Identity {
				i := id("v", "u", "dev")
				i.GateAuthorKey = author(t, f)
				return i
			}, ErrScopeDenied},
		"gate whose author is gone": {
			func(*testing.T, *FS) Identity {
				i := id("v", "u", "verify")
				i.GateAuthorKey = "missing-author"
				return i
			}, ErrIdentity},
		"gate judging itself": {
			func(t *testing.T, f *FS) Identity {
				key := AgentID("self-author")
				f.bindings[key] = forgedAuthor("v", "dev", "")
				i := id("v", "u", "verify")
				i.GateAuthorKey = key
				return i
			}, ErrIdentity},
		"gate whose author cannot write": {
			func(t *testing.T, f *FS) Identity {
				key := AgentID("readonly-author")
				f.bindings[key] = forgedAuthor("ro", "verify", "")
				i := id("v", "u", "verify")
				i.GateAuthorKey = key
				return i
			}, ErrScopeDenied},
		"gate against another invariant set": {
			func(t *testing.T, f *FS) Identity {
				i := id("v", "u", "verify")
				i.GateAuthorKey = author(t, f)
				i.InvariantsHash = "other-invariants"
				return i
			}, ErrIdentity},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := durable(t)
			assigned := tc.assigned(t, f)
			assigned.Prelaunch = true
			f.bindings["assignment"] = assigned
			assigned.Prelaunch = false
			if _, err := f.Bind(assigned, nil); !errors.Is(err, tc.want) {
				t.Fatalf("Bind() = %v; want %v", err, tc.want)
			}
			if !f.bindings["assignment"].Prelaunch {
				t.Fatal("a refused adoption consumed the assignment")
			}
		})
	}
}

func TestAssignVerifierRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		identity func(author AgentID) (Identity, AgentID)
		want     error
	}{
		"incomplete identity":    {func(a AgentID) (Identity, AgentID) { return Identity{SessionID: "s", Specialist: "verify"}, a }, ErrIdentity},
		"no specialist":          {func(a AgentID) (Identity, AgentID) { return id("v", "u", ""), a }, ErrIdentity},
		"no author key":          {func(AgentID) (Identity, AgentID) { return id("v", "u", "verify"), "" }, ErrIdentity},
		"unknown specialist":     {func(a AgentID) (Identity, AgentID) { return id("v", "u", unknownInstance), a }, ErrIdentity},
		"instance cannot verify": {func(a AgentID) (Identity, AgentID) { return id("v", "u", "dev"), a }, ErrScopeDenied},
		"author not bound":       {func(AgentID) (Identity, AgentID) { return id("v", "u", "verify"), "ghost" }, ErrIdentity},
		"author is the verifier": {func(a AgentID) (Identity, AgentID) { return id("author", "u", "verify"), a }, ErrIdentity},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := durable(t)
			author := bind(t, f, "author", "u", "dev", "a.go")
			identity, authorKey := tc.identity(author)
			if _, err := f.AssignVerifier(identity, authorKey); !errors.Is(err, tc.want) {
				t.Fatalf("AssignVerifier() = %v; want %v", err, tc.want)
			}
		})
	}
	t.Run("same verifier twice", func(t *testing.T) {
		f, _, _ := durable(t)
		author := bind(t, f, "author", "u", "dev", "a.go")
		if _, err := f.AssignVerifier(id("v", "u", "verify"), author); err != nil {
			t.Fatal(err)
		}
		if _, err := f.AssignVerifier(id("v", "u", "verify"), author); !errors.Is(err, ErrIdentity) {
			t.Fatalf("duplicate verifier = %v; want ErrIdentity", err)
		}
	})
}

// One verifier unit judging several authors holds one gate per author; each
// gate must be adoptable under the single attempt its delegation is admitted as.
func TestEveryGateOfOneVerifierUnitAdoptsTheAdmittedAttempt(t *testing.T) {
	f, _, _ := durable(t)
	authors := []AgentID{bind(t, f, "author1", "u1", "dev", "a.go"), bind(t, f, "author2", "u2", "dev", "b.go")}
	for _, author := range authors {
		if _, err := f.AssignVerifier(id("v", "vu", "verify"), author); err != nil {
			t.Fatal(err)
		}
	}
	for _, author := range authors {
		gate := id("v", "vu", "verify")
		gate.AttemptID, gate.GateAuthorKey = "1", author
		if _, err := f.Bind(gate, nil); err != nil {
			t.Fatalf("gate for %s: %v", author, err)
		}
	}
}

func TestRevokeOwnershipNeverLeavesAnOwnerlessDelta(t *testing.T) {
	f, _, _ := durable(t)
	key := bind(t, f, "author", "u", "dev", "a.go", "b.go")
	if err := f.RevokeOwnership(key); err != nil {
		t.Fatalf("release without staged work = %v", err)
	}
	if _, owned := f.owners["a.go"]; owned {
		t.Fatal("release kept the paths")
	}
	key = bind(t, f, "retry", "u2", "dev", "c.go")
	apply(t, f, applyCase{key, "create", 0, OpCreate, "c.go", "work in progress"})
	if claims := f.OwnershipClaims("s"); len(claims) != 1 || !claims[0].Staged {
		t.Fatalf("claims = %+v; want one marked staged", claims)
	}
	if err := f.RevokeOwnership(key); !errors.Is(err, ErrStagedWork) {
		t.Fatalf("release with staged work = %v; want ErrStagedWork", err)
	}
	if f.owners["c.go"] != key || string(f.staged[key].files["c.go"]) != "work in progress" {
		t.Fatal("a refused release changed the claim or its delta")
	}
}

func TestReassignScopeKeepsTheStagedDelta(t *testing.T) {
	f, _, _ := durable(t)
	key := bind(t, f, "author", "u", "dev", "a.go", "spare.go")
	staged := apply(t, f, applyCase{key, "create", 0, OpCreate, "a.go", "partial"})
	peer := bind(t, f, "peer", "other", "dev", "p.go")
	retry := Identity{SessionID: "s", WorkUnitID: "u", AgentID: "author", Specialist: "dev"}
	for name, tc := range map[string]struct {
		identity Identity
		key      AgentID
		scope    []string
		want     error
	}{
		"drops a staged path":    {retry, key, []string{"b.go"}, ErrScopeDenied},
		"collides on added path": {retry, key, []string{"a.go", "p.go"}, ErrCollision},
		"another unit's key":     {Identity{SessionID: "s", WorkUnitID: "other", AgentID: "author", Specialist: "dev"}, key, []string{"a.go"}, ErrIdentity},
		"unknown key":            {retry, "ghost", []string{"a.go"}, ErrIdentity},
	} {
		if err := f.ReassignScope(tc.identity, tc.key, tc.scope); !errors.Is(err, tc.want) {
			t.Errorf("%s: ReassignScope() = %v; want %v", name, err, tc.want)
		}
	}
	if f.owners["spare.go"] != key || f.owners["p.go"] != peer {
		t.Fatal("a refused reassignment changed ownership")
	}
	if err := f.ReassignScope(retry, key, []string{"a.go", "b.go"}); err != nil {
		t.Fatalf("ReassignScope() = %v", err)
	}
	held := f.bindings[key]
	if !held.Prelaunch || held.AttemptID != "2" {
		t.Fatalf("binding = %+v; want a prelaunch claim on attempt 2", held)
	}
	if f.owners["a.go"] != key || f.owners["b.go"] != key {
		t.Fatal("the new scope is not owned")
	}
	if _, owned := f.owners["spare.go"]; owned {
		t.Fatal("an unused path stayed owned after leaving the scope")
	}
	if got := f.staged[key]; string(got.files["a.go"]) != "partial" || got.revision != staged.Revision || got.verdict != nil {
		t.Fatalf("delta = %+v; want the staged content and revision kept, no verdict", got)
	}
}

func TestRefusedReassignScopeLeavesNoDelta(t *testing.T) {
	f, _, _ := durable(t)
	key := bind(t, f, "author", "u", "dev", "a.go")
	bind(t, f, "peer", "other", "dev", "p.go")
	staged := apply(t, f, applyCase{key, "create", 0, OpCreate, "a.go", "done"})
	if err := f.ConsolidateCheckpoint(key, "cp", staged.Revision); err != nil {
		t.Fatal(err)
	}
	if _, kept := f.staged[key]; kept {
		t.Fatal("consolidation kept the delta")
	}
	retry := Identity{SessionID: "s", WorkUnitID: "u", AgentID: "author", Specialist: "dev"}
	if err := f.ReassignScope(retry, key, []string{"a.go", "p.go"}); !errors.Is(err, ErrCollision) {
		t.Fatalf("ReassignScope() = %v; want ErrCollision", err)
	}
	if _, kept := f.staged[key]; kept {
		t.Fatal("a refused reassignment left an empty delta behind")
	}
}

func TestConsolidateCheckpointDefaultsItsLabelAndRefusesNothing(t *testing.T) {
	r := newVerdictRig(t)
	if err := r.f.ConsolidateCheckpoint(r.author, "  ", r.staged.Revision); err != nil {
		t.Fatalf("blank checkpoint label = %v; want the default label to be used", err)
	}
	if err := r.f.ConsolidateCheckpoint(r.author, "again", r.staged.Revision); !errors.Is(err, ErrNothingStaged) {
		t.Fatalf("consolidating again = %v; want ErrNothingStaged", err)
	}
}

func TestRevokeOwnershipRefusesUnknownKey(t *testing.T) {
	f, _, _ := durable(t)
	if err := f.RevokeOwnership("ghost"); !errors.Is(err, ErrIdentity) {
		t.Fatalf("RevokeOwnership(ghost) = %v; want ErrIdentity", err)
	}
}

func TestCatalogLookups(t *testing.T) {
	if role, err := SpecialistRole("dev"); err != nil || role != model.RoleExecution {
		t.Fatalf("SpecialistRole(dev) = %q, %v", role, err)
	}
	// The coordinator is not a dispatchable specialist, so it has no role here.
	if _, err := SpecialistRole(OrchestratorInstance); !errors.Is(err, ErrIdentity) {
		t.Fatalf("SpecialistRole(%s) = %v; want ErrIdentity", OrchestratorInstance, err)
	}
	if _, err := SpecialistRole(unknownInstance); !errors.Is(err, ErrIdentity) {
		t.Fatalf("SpecialistRole(unknown) = %v; want ErrIdentity", err)
	}
	if err := RequireVFSCapability("dev", model.VFSCapabilityRead); err != nil {
		t.Fatalf("dev read grant: %v", err)
	}
	if err := RequireVFSCapability(noGrantInstance, model.VFSCapabilityRead); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("explicitly empty grant list = %v; want ErrScopeDenied", err)
	}
	if err := RequireVFSCapability(unknownInstance, model.VFSCapabilityRead); !errors.Is(err, ErrIdentity) {
		t.Fatalf("unknown instance = %v; want ErrIdentity", err)
	}
}

func TestBindableIdentityRefusals(t *testing.T) {
	f, _, _ := durable(t)
	if _, err := f.bindableIdentity(id("a", "u", unknownInstance), nil); !errors.Is(err, ErrIdentity) {
		t.Fatalf("unknown specialist = %v; want ErrIdentity", err)
	}
	// verify may bind but holds no write grant, so it can never take a scope.
	if _, err := f.bindableIdentity(id("a", "u", "verify"), []string{"a.go"}); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("scope for an instance without write = %v; want ErrScopeDenied", err)
	}
}

func TestCollisionErrorNamesOwnerWithoutBinding(t *testing.T) {
	f, _, _ := durable(t)
	f.owners["x.go"] = "orphaned-key"
	_, err := f.Bind(id("late", "u", "dev"), []string{"x.go"})
	var collision *CollisionError
	if !errors.As(err, &collision) || collision.OwnerAgent != "orphaned-key" || collision.RequestedPath != "x.go" {
		t.Fatalf("Bind() = %v; want a CollisionError naming the orphaned owner key", err)
	}
}

func TestUnknownKeyHasNoDelta(t *testing.T) {
	f, _, _ := durable(t)
	if got := f.revisionOf("ghost"); got != 0 {
		t.Fatalf("revisionOf(ghost) = %d; want 0", got)
	}
	if got, want := f.deltaHashLocked("ghost"), hashOf([]byte("{}"), true); got != want {
		t.Fatalf("deltaHashLocked(ghost) = %q; want the empty-delta hash %q", got, want)
	}
}

func TestApplyRefusals(t *testing.T) {
	f, _, _ := durable(t)
	key := bind(t, f, "author", "u", "dev", "a.go")
	created := apply(t, f, applyCase{key, "create", 0, OpCreate, "a.go", "v1"})
	forged := AgentID("no-grants")
	f.bindings[forged] = Identity{SessionID: "s", WorkUnitID: "forged", AttemptID: "1", AgentID: "forged", Specialist: noGrantInstance}
	f.owners["forged.go"] = forged

	for name, tc := range map[string]struct {
		op   Operation
		want error
	}{
		"unknown key":          {Operation{Key: "ghost", CallID: "c1", Action: OpRead, Path: "a.go"}, ErrIdentity},
		"empty call id":        {Operation{Key: key, Action: OpRead, Path: "a.go", ExpectedRevision: created.Revision}, ErrIdentity},
		"stale revision":       {Operation{Key: key, CallID: "c2", Action: OpRead, Path: "a.go", ExpectedRevision: created.Revision + 1}, ErrStaleRevision},
		"replayed call":        {Operation{Key: key, CallID: "create", Action: OpRead, Path: "a.go", ExpectedRevision: created.Revision}, ErrDuplicateCall},
		"read without grant":   {Operation{Key: forged, CallID: "c3", Action: OpRead, Path: "forged.go"}, ErrScopeDenied},
		"write without grant":  {Operation{Key: forged, CallID: "c4", Action: OpCreate, Path: "forged.go"}, ErrScopeDenied},
		"delete without grant": {Operation{Key: forged, CallID: "c5", Action: OpDelete, Path: "forged.go"}, ErrScopeDenied},
		"path outside scope":   {Operation{Key: key, CallID: "c6", Action: OpCreate, Path: "elsewhere.go", ExpectedRevision: created.Revision}, ErrScopeDenied},
		"path escapes":         {Operation{Key: key, CallID: "c7", Action: OpCreate, Path: escapingPath, ExpectedRevision: created.Revision}, ErrInvalidPath},
	} {
		if _, err := f.Apply(tc.op); !errors.Is(err, tc.want) {
			t.Errorf("%s: Apply() = %v; want %v", name, err, tc.want)
		}
	}
	if _, err := f.Apply(Operation{Key: key, CallID: "c8", Action: unsupportedAction, ExpectedRevision: created.Revision}); err == nil ||
		!strings.Contains(err.Error(), "unsupported operation") {
		t.Errorf("unsupported action = %v", err)
	}
	denied := 0
	for _, e := range f.JournalPage("", "", -1, MaxJournalPageSize) {
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
	f, _, _ := durable(t)
	author := bind(t, f, "author", "u", "dev", "a.go")
	created := apply(t, f, applyCase{author, "create", 0, OpCreate, "a.go", "staged"})
	verifier := bind(t, f, "judge", "u", "verify")
	stranger := bind(t, f, "stranger", "u2", "dev", "b.go")
	read := func(key AgentID, call string) Operation {
		return Operation{Key: key, CallID: call, ExpectedRevision: created.Revision, Action: OpRead, Path: "a.go"}
	}
	if _, err := f.ReadAs(Operation{Key: verifier, CallID: "w", Action: OpCreate, Path: "a.go"}, author); !errors.Is(err, ErrIdentity) {
		t.Errorf("ReadAs with a write action = %v; want ErrIdentity", err)
	}
	if _, err := f.ReadAs(read(verifier, "ghost-view"), "ghost"); !errors.Is(err, ErrIdentity) {
		t.Errorf("ReadAs of an unbound view = %v; want ErrIdentity", err)
	}
	if _, err := f.ReadAs(read(stranger, "no-verify"), author); !errors.Is(err, ErrScopeDenied) {
		t.Errorf("ReadAs by an instance without verify = %v; want ErrScopeDenied", err)
	}
	if _, err := f.ReadAs(read(verifier, "unlinked"), stranger); !errors.Is(err, ErrIdentity) {
		t.Errorf("ReadAs of an author the gate is not linked to = %v; want ErrIdentity", err)
	}
	got, err := f.ReadAs(read(verifier, "linked"), author)
	if err != nil || string(got.Content) != "staged" {
		t.Errorf("ReadAs of the linked author = %q, %v; want the staged content", got.Content, err)
	}
}

func TestDeletionIsStagedAndReadsAsAbsent(t *testing.T) {
	f, root, _ := durable(t)
	if err := os.WriteFile(filepath.Join(root, "d.txt"), []byte("on disk"), worldReadableFileMode); err != nil {
		t.Fatal(err)
	}
	key := bind(t, f, "author", "u", "dev", "d.txt")
	read := apply(t, f, applyCase{key, "read", 0, OpRead, "d.txt", ""})
	if string(read.Content) != "on disk" {
		t.Fatalf("read = %q; want the base content", read.Content)
	}
	deleted := apply(t, f, applyCase{key, "delete", read.Revision, OpDelete, "d.txt", ""})
	if deleted.Revision != read.Revision+1 {
		t.Fatalf("delete revision = %d; want %d", deleted.Revision, read.Revision+1)
	}
	if _, err := f.Apply(Operation{Key: key, CallID: "again", ExpectedRevision: deleted.Revision, Action: OpRead, Path: "d.txt"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read of a staged deletion = %v; want not-exist", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "d.txt")); err != nil || string(data) != "on disk" {
		t.Fatalf("staging a deletion touched the disk: %q, %v", data, err)
	}
	var outcomes []string
	for _, e := range f.JournalPage("", "", -1, MaxJournalPageSize) {
		outcomes = append(outcomes, string(e.Operation)+":"+e.Outcome)
	}
	if !strings.Contains(strings.Join(outcomes, ","), "read:not_found") {
		t.Fatalf("journal = %v; want the not_found read recorded", outcomes)
	}
}

func TestForeignPathRefusesOnlyThatOperation(t *testing.T) {
	f, _, _ := durable(t)
	var seen []CollisionEvent
	f.OnCollision(func(e CollisionEvent) { seen = append(seen, e) })
	bind(t, f, "owner", "u1", "dev", "a.go")
	intruder := bind(t, f, "intruder", "u2", "dev", "b.go")

	_, err := f.Apply(Operation{Key: intruder, CallID: "peek", Action: OpRead, Path: "a.go"})
	if !errors.Is(err, ErrCollision) || !strings.Contains(err.Error(), `"a.go"`) {
		t.Fatalf("foreign read = %v; want ErrCollision naming a.go", err)
	}
	if _, err := f.Apply(Operation{Key: intruder, CallID: "grab", Action: OpCreate, Path: "a.go", Content: []byte("x")}); !errors.Is(err, ErrCollision) {
		t.Fatalf("foreign write = %v; want ErrCollision", err)
	}
	if len(seen) != 2 || seen[0].Path != "a.go" || seen[0].AttemptingAgent != "intruder" || seen[0].OwningAgent != "owner" {
		t.Fatalf("collision handler saw %+v", seen)
	}
	if len(f.collisions) != 0 || f.notifiedCollisions != 0 {
		t.Fatalf("refused operations retained collision notifications: %+v, %d", f.collisions, f.notifiedCollisions)
	}
	// A refused operation leaves nothing behind: the agent's own work proceeds.
	staged := apply(t, f, applyCase{intruder, "own", 0, OpCreate, "b.go", "ok"})
	gate(t, f, intruder, staged)
	if err := f.ConsolidateCheckpoint(intruder, "own", staged.Revision); err != nil {
		t.Fatalf("ConsolidateCheckpoint() after a refused foreign access = %v", err)
	}
}

func TestRejectedBindNeverBlocksConsolidation(t *testing.T) {
	f, _, _ := durable(t)
	bind(t, f, "owner", "u1", "dev", "a.go")
	peer := bind(t, f, "peer", "u2", "dev", "c.go")
	staged := apply(t, f, applyCase{peer, "stage", 0, OpCreate, "c.go", "peer work"})
	gate(t, f, peer, staged)

	if _, err := f.Bind(id("loser", "u2", "dev"), []string{"a.go"}); !errors.Is(err, ErrCollision) {
		t.Fatalf("Bind() = %v; want ErrCollision", err)
	}
	if len(f.collisions) != 0 || f.notifiedCollisions != 0 {
		t.Fatalf("rejected bind retained collision notifications: %+v, %d", f.collisions, f.notifiedCollisions)
	}
	if err := f.ConsolidateCheckpoint(peer, "released", staged.Revision); err != nil {
		t.Fatalf("ConsolidateCheckpoint() after a rejected bind = %v", err)
	}
}

// verdictRig is an author with one staged file and a linked verifier gate.
type verdictRig struct {
	f        *FS
	author   AgentID
	verifier AgentID
	staged   OperationResult
}

func newVerdictRig(t *testing.T) verdictRig {
	t.Helper()
	f, _, _ := durable(t)
	author := bind(t, f, "author", "u", "dev", "a.go")
	staged := apply(t, f, applyCase{author, "create", 0, OpCreate, "a.go", "v1"})
	return verdictRig{f, author, bind(t, f, "judge", "u", "verify"), staged}
}

func TestVerifyRefusals(t *testing.T) {
	r := newVerdictRig(t)
	rev, hash := r.staged.Revision, r.staged.DeltaHash
	for name, tc := range map[string]struct {
		call func() error
		want error
	}{
		"empty call id":           {func() error { return r.f.Verify(r.verifier, r.author, "", rev, hash, true, "") }, ErrIdentity},
		"unknown verifier":        {func() error { return r.f.Verify("ghost", r.author, "v1", rev, hash, true, "") }, ErrIdentity},
		"unknown author":          {func() error { return r.f.Verify(r.verifier, "ghost", "v2", rev, hash, true, "") }, ErrIdentity},
		"caller lacks verify":     {func() error { return r.f.Verify(r.author, r.author, "v3", rev, hash, true, "") }, ErrScopeDenied},
		"stale revision":          {func() error { return r.f.Verify(r.verifier, r.author, "v4", rev+1, hash, true, "") }, ErrInvalidVerdict},
		"wrong delta hash":        {func() error { return r.f.Verify(r.verifier, r.author, "v5", rev, "deadbeef", true, "") }, ErrInvalidVerdict},
		"failure without finding": {func() error { return r.f.Verify(r.verifier, r.author, "v6", rev, hash, false, "  ") }, ErrInvalidVerdict},
	} {
		if err := tc.call(); !errors.Is(err, tc.want) {
			t.Errorf("%s: Verify() = %v; want %v", name, err, tc.want)
		}
	}
	if hasVerdict(r.f, r.author) {
		t.Fatal("a refused verdict was attached to the delta")
	}
}

func TestVerifyRefusesSelfVerification(t *testing.T) {
	r := newVerdictRig(t)
	self := AgentID("self-gate")
	author := r.f.bindings[r.author]
	r.f.bindings[self] = Identity{SessionID: author.SessionID, WorkUnitID: "u", AttemptID: "1", AgentID: author.AgentID,
		Specialist: "verify", GateAuthorKey: r.author, InvariantsHash: author.InvariantsHash}
	if err := r.f.Verify(self, r.author, "self", r.staged.Revision, r.staged.DeltaHash, true, ""); !errors.Is(err, ErrSelfVerification) {
		t.Fatalf("Verify() = %v; want ErrSelfVerification", err)
	}
}

func TestVerifyFailsWhenInvariantDocumentsAreUnreadable(t *testing.T) {
	r := newVerdictRig(t)
	author := r.f.bindings[r.author]
	author.Invariants = InvariantSet{escapingPath}
	r.f.bindings[r.author] = author
	if err := r.f.Verify(r.verifier, r.author, "inv", r.staged.Revision, r.staged.DeltaHash, true, ""); !errors.Is(err, ErrIdentity) {
		t.Fatalf("Verify() = %v; want ErrIdentity", err)
	}
	if hasVerdict(r.f, r.author) {
		t.Fatal("a verdict was attached although its invariants could not be pinned")
	}
}

func TestFailingVerdictBlocksConsolidationWithItsFinding(t *testing.T) {
	r := newVerdictRig(t)
	const finding = "missing edge case"
	if err := r.f.Verify(r.verifier, r.author, "reject", r.staged.Revision, r.staged.DeltaHash, false, finding); err != nil {
		t.Fatal(err)
	}
	err := r.f.ConsolidateCheckpoint(r.author, "cp", r.staged.Revision)
	if !errors.Is(err, ErrVerificationRequired) || !strings.Contains(err.Error(), finding) {
		t.Fatalf("ConsolidateCheckpoint() = %v; want ErrVerificationRequired carrying %q", err, finding)
	}
	for _, e := range r.f.JournalPage("", "", -1, MaxJournalPageSize) {
		if e.Operation == "verdict" && e.Outcome == "rejected" && strings.Contains(e.Path+e.CallID, finding) {
			t.Fatal("the finding leaked into the content-free journal")
		}
	}
}

func TestConsolidateCheckpointRefusals(t *testing.T) {
	r := newVerdictRig(t)
	rev := r.staged.Revision
	for name, tc := range map[string]struct {
		key        AgentID
		checkpoint string
		revision   uint64
		want       error
	}{
		"unknown key":           {"ghost", "cp", rev, ErrIdentity},
		"stale revision":        {r.author, "cp", rev + 1, ErrStaleRevision},
		"binding without delta": {r.verifier, "cp", 0, ErrNothingStaged},
	} {
		if err := r.f.ConsolidateCheckpoint(tc.key, tc.checkpoint, tc.revision); !errors.Is(err, tc.want) {
			t.Errorf("%s: ConsolidateCheckpoint() = %v; want %v", name, err, tc.want)
		}
	}
	if err := r.f.Verify(r.verifier, r.author, "pass", rev, r.staged.DeltaHash, true, ""); err != nil {
		t.Fatal(err)
	}
	r.f.staged[r.author].verdict.Invariants = InvariantSet{escapingPath}
	if err := r.f.ConsolidateCheckpoint(r.author, "cp", rev); !errors.Is(err, ErrIdentity) {
		t.Errorf("unreadable verdict invariants: ConsolidateCheckpoint() = %v; want ErrIdentity", err)
	}
}

func TestConsolidateWithoutVerifierPreservesOwnershipAndBaseChecks(t *testing.T) {
	r := newVerdictRig(t)
	if err := r.f.ConsolidateCheckpoint(r.author, "approved", r.staged.Revision+1); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale revision = %v", err)
	}
	if err := r.f.ConsolidateCheckpoint(r.author, "approved", r.staged.Revision); err != nil {
		t.Fatalf("authorized consolidation without gate = %v", err)
	}
	claims := r.f.OwnershipClaims("s")
	for _, claim := range claims {
		if claim.Key == r.author && len(claim.Scope) != 0 {
			t.Fatal("consolidation retained the author's ownership")
		}
	}
}
