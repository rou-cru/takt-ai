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
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/vfs"
	// modernc.org/sqlite registers the "sqlite" driver the legacy-store test edits through.
	_ "modernc.org/sqlite"
)

// otherSession is a second root session.
const otherSession = "root-b"

// shortLockWait bounds the test's wait for a lock it holds itself.
const shortLockWait = 50 * time.Millisecond

// parallelCommands is how many concurrent openers queue on one workspace.
const parallelCommands = 8

func TestDelegatedBindRequiresAnAdmission(t *testing.T) {
	f, _, state := newStore(t)
	if _, err := f.AssignScope(ident("dev", "u", "dev"), []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	c := callerOf(testSession, "dev", "u")
	if _, err := f.BindAs(c, nil, nil); !errors.Is(err, vfs.ErrNoAdmission) {
		t.Fatalf("BindAs() without admission = %v; want ErrNoAdmission", err)
	}
	// The admission is read under the lock when the bind decides, never earlier.
	admit(t, state, testSession, "u", "1")
	bound, err := f.BindAs(c, nil, nil)
	if err != nil || bound.AttemptID != "1" {
		t.Fatalf("BindAs() after admission = %+v, %v; want attempt 1", bound, err)
	}
}

func TestRebindingReturnsTheHeldKey(t *testing.T) {
	f, _, state := newStore(t)
	c, key := delegate(t, f, state, testSession, "dev", "u", "a.go")
	for range 2 {
		again, err := f.BindAs(c, []string{"a.go"}, nil)
		if err != nil || again.Key != key {
			t.Fatalf("rebind = %+v, %v; want %s", again, err, key)
		}
	}
	if _, err := f.BindAs(c, []string{"b.go"}, nil); !errors.Is(err, vfs.ErrScopeDenied) {
		t.Fatalf("rebind with another scope = %v; want ErrScopeDenied", err)
	}
	// Another host session of the unit does not hold the key.
	other := c
	other.HostSession = "elsewhere"
	if _, err := f.Operate(other, vfs.CallerOperation{CallID: "x", Action: vfs.OpCreate, Path: "a.go", Content: []byte("x")}); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("op from another host session = %v; want ErrIdentity", err)
	}
}

func TestAuthorOperationsChainRevisions(t *testing.T) {
	f, _, state := newStore(t)
	c, key := delegate(t, f, state, testSession, "dev", "u", "a.go", "b.go")
	first := write(t, f, c, "w1", "a.go", "one")
	second := write(t, f, c, "w2", "b.go", "two")
	if second.Revision != first.Revision+1 || second.DeltaHash == first.DeltaHash {
		t.Fatalf("revisions %d then %d; want consecutive", first.Revision, second.Revision)
	}
	named, err := f.Operate(c, vfs.CallerOperation{CallID: "w3", Action: vfs.OpDelete, Path: "b.go", AuthorKey: key})
	if err != nil || named.Revision != second.Revision+1 {
		t.Fatalf("delete naming its own key = %+v, %v", named, err)
	}
	if _, err := f.Operate(c, vfs.CallerOperation{CallID: "w1", Action: vfs.OpCreate, Path: "a.go", Content: []byte("x")}); !errors.Is(err, vfs.ErrDuplicateCall) {
		t.Fatalf("replayed call = %v; want ErrDuplicateCall", err)
	}
}

// TestAuthorRefusalsNameTheContinuation checks that each refusal of an author operation names what
// the author does next.
func TestAuthorRefusalsNameTheContinuation(t *testing.T) {
	f, _, state := newStore(t)
	c, _ := delegate(t, f, state, testSession, "dev", "u", "a.go")
	_, foreign := delegate(t, f, state, testSession, "fix", "v", "b.go")
	_, err := f.Operate(c, vfs.CallerOperation{CallID: "x", Action: vfs.OpCreate, Path: "b.go", AuthorKey: foreign})
	refusedNaming(t, "op naming another unit's key", err, vfs.ErrIdentity, "not an input", "orchestrator")
	_, err = f.Operate(c, vfs.CallerOperation{CallID: "z", Action: vfs.OpRead, Path: "a.go", AuthorKey: "unknown"})
	refusedNaming(t, "op naming a key no binding holds", err, vfs.ErrIdentity, "vfs_bind")
	_, err = f.Operate(callerOf(testSession, "dev", "unbound"), vfs.CallerOperation{CallID: "y", Action: vfs.OpRead, Path: "a.go"})
	refusedNaming(t, "op without a binding", err, vfs.ErrIdentity, "vfs_bind")
}

// An author reading another unit's staged work is pointed at its own inputs,
// by ID, and at the blocker that replaces guessing.
func TestForeignAuthorReadNamesTheUnitsInputs(t *testing.T) {
	f, _, state := newStore(t)
	_, implementer := delegate(t, f, state, testSession, "dev", "impl", "a.go")
	if _, err := f.AssignScope(vfs.Identity{SessionID: testSession, WorkUnitID: "tests", AgentID: "test", Specialist: "test", Invariants: vfs.InvariantSet{vfs.EngramInvariant(7), vfs.EngramInvariant(9)}}, []string{"b.go"}); err != nil {
		t.Fatal(err)
	}
	ensureAdmitted(t, f, state, testSession, "tests", "test")
	c := callerOf(testSession, "test", "tests")
	if _, err := f.BindAs(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	_, err := f.Operate(c, vfs.CallerOperation{CallID: "r", Action: vfs.OpRead, Path: "a.go", AuthorKey: implementer})
	refusedNaming(t, "test author reading the implementer's staged work", err, vfs.ErrIdentity, "#7, #9", "mem_get_observation", "blocker")
}

func TestAReleasedKeyNoLongerWorks(t *testing.T) {
	f, _, state := newStore(t)
	c, key := delegate(t, f, state, testSession, "dev", "u", "a.go")
	err := f.ReleaseClaim(testSession, key, false)
	refusedNaming(t, "unconfirmed release of an active claim", err, vfs.ErrScopeDenied, "claim_release", "confirmed")
	write(t, f, c, "w", "a.go", "staged")
	if err := f.ReleaseClaim(testSession, key, true); !errors.Is(err, vfs.ErrStagedWork) {
		t.Fatalf("release of staged work = %v; want ErrStagedWork", err)
	}
	if err := f.ReleaseClaim(otherSession, key, false); !errors.Is(err, vfs.ErrStagedWork) {
		t.Fatalf("cross-root release of staged work = %v; want ErrStagedWork", err)
	}
	if err := f.DiscardAs(orchestrator(testSession), key); err != nil {
		t.Fatal(err)
	}
	if err := f.ReleaseClaim(otherSession, key, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Operate(c, vfs.CallerOperation{CallID: "late", Action: vfs.OpCreate, Path: "a.go", Content: []byte("x")}); err == nil {
		t.Fatal("a released key still staged work")
	}
	// A pending claim needs no confirmation.
	pending, err := f.AssignScope(ident("dev", "w", "dev"), []string{"c.go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ReleaseClaim(testSession, pending, false); err != nil {
		t.Fatalf("release of a pending claim = %v", err)
	}
}

// gateFixture is one author per path, judged by one verifier unit.
type gateFixture struct {
	f              *vfs.FS
	root, state    string
	devA, devB     vfs.Caller
	authorA, authB vfs.AgentID
	verifier       vfs.Caller
	gates          []vfs.AdoptedGate
}

func newGateFixture(t *testing.T) gateFixture {
	t.Helper()
	f, root, state := newStore(t)
	g := gateFixture{f: f, root: root, state: state}
	g.devA, g.authorA = delegate(t, f, state, testSession, "dev", "a", "a.go")
	g.devB, g.authB = delegate(t, f, state, testSession, "fix", "b", "b.go")
	write(t, f, g.devA, "a1", "a.go", "A1")
	write(t, f, g.devB, "b1", "b.go", "B1")
	g.verifier, g.gates = judge(t, f, state, testSession, "verify", "round", g.authorA, g.authB)
	return g
}

func TestGateReadChoosesThePathOwnerOrTheFirstGate(t *testing.T) {
	g := newGateFixture(t)
	if got := readAs(t, g.f, g.verifier, "r1", "b.go", ""); string(got.Content) != "B1" {
		t.Fatalf("read of b.go = %q; want author b's staged content", got.Content)
	}
	if got := readAs(t, g.f, g.verifier, "r2", "a.go", g.authorA); string(got.Content) != "A1" {
		t.Fatalf("read naming author a = %q", got.Content)
	}
	// A path no author owns reads through the first gate.
	if err := os.WriteFile(filepath.Join(g.root, "README.md"), []byte("context"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readAs(t, g.f, g.verifier, "r3", "README.md", ""); string(got.Content) != "context" {
		t.Fatalf("read of an unowned path = %q; want it through the first gate", got.Content)
	}
	_, err := g.f.Operate(g.verifier, vfs.CallerOperation{CallID: "r4", Action: vfs.OpRead, Path: "a.go", AuthorKey: "stranger"})
	refused(t, "read naming an author the judge has no gate over", err, vfs.ErrIdentity)
}

func TestGateReadReportsOutdatedPaths(t *testing.T) {
	f, _, state := newStore(t)
	dev, author := delegate(t, f, state, testSession, "dev", "u", "a.go", "b.go")
	write(t, f, dev, "w1", "a.go", "A1")
	write(t, f, dev, "w2", "b.go", "B1")
	gate, _ := judge(t, f, state, testSession, "verify", "u/verify", author)
	readAs(t, f, gate, "r1", "a.go", "")
	if r := readAs(t, f, gate, "r2", "b.go", ""); len(r.OutdatedPaths) != 0 {
		t.Fatalf("outdated = %v; want none while the author's state holds", r.OutdatedPaths)
	}
	write(t, f, dev, "w3", "b.go", "B2")
	if r := readAs(t, f, gate, "r3", "b.go", ""); !slices.Equal(r.OutdatedPaths, []string{"a.go"}) {
		t.Fatalf("outdated = %v; want a.go, read before the author changed", r.OutdatedPaths)
	}
}

func TestVerifyUsesTheJudgedState(t *testing.T) {
	f, _, state := newStore(t)
	dev, author := delegate(t, f, state, testSession, "dev", "u", "a.go")
	write(t, f, dev, "w1", "a.go", "A1")
	gate, _ := judge(t, f, state, testSession, "verify", "u/verify", author)
	err := f.VerifyAs(gate, "v0", author, true, "")
	refusedNaming(t, "verdict before any read", err, vfs.ErrInvalidVerdict, "vfs_read")
	readAs(t, f, gate, "r1", "a.go", "")
	write(t, f, dev, "w2", "a.go", "A2")
	err = f.VerifyAs(gate, "v1", author, true, "")
	refusedNaming(t, "verdict after the author changed", err, vfs.ErrInvalidVerdict, "vfs_read")
	readAs(t, f, gate, "r2", "a.go", "")
	if err := f.VerifyAs(gate, "v2", author, true, ""); err != nil {
		t.Fatalf("verdict over the read state = %v", err)
	}
	if err := f.VerifyAs(gate, "v3", "stranger", true, ""); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("verdict over an author the judge has no gate over = %v; want ErrIdentity", err)
	}
}

func TestANewGateSupersedesTheEarlierOne(t *testing.T) {
	f, _, state := newStore(t)
	dev, author := delegate(t, f, state, testSession, "dev", "u", "a.go")
	write(t, f, dev, "w1", "a.go", "A1")
	early, _ := judge(t, f, state, testSession, "verify", "review", author)
	readAs(t, f, early, "r1", "a.go", "")
	settle(t, state, testSession, "review", "1")
	admit(t, state, testSession, "review", "2", "verify")
	late := callerOf(testSession, "verify", "review")
	late.HostSession = "retried-host"
	replaceChild(t, state, testSession, "review", "2", "verify", late.HostSession)
	if _, err := f.AssignVerifier(ident("verify", "review", "verify"), author); err != nil {
		t.Fatal(err)
	}
	gates, err := f.AdoptGates(late)
	if err != nil || len(gates) != 1 || gates[0].Error != "" {
		t.Fatalf("adopt = %+v, %v", gates, err)
	}
	if err := f.VerifyAs(early, "v-early", author, true, ""); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("verdict through the superseded gate = %v; want ErrIdentity", err)
	}
	readAs(t, f, late, "r2", "a.go", "")
	if err := f.VerifyAs(late, "v-late", author, true, ""); err != nil {
		t.Fatalf("verdict through the current gate = %v", err)
	}
}

func TestAnotherRootsStagedWorkClosesOnceAssignedToThisRoot(t *testing.T) {
	f, _, state := newStore(t)
	dev, author := delegate(t, f, state, testSession, "dev", "u", "a.go")
	write(t, f, dev, "w1", "a.go", "A1")
	if err := f.DiscardAs(orchestrator(otherSession), author); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("discard from another root = %v; want ErrIdentity", err)
	}
	if err := f.ReassignScope(vfs.Identity{SessionID: otherSession, WorkUnitID: "u", AgentID: "dev", Specialist: "dev"}, author, []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	if err := f.DiscardAs(orchestrator(otherSession), author); err != nil {
		t.Fatalf("discard after claim_assign into this root = %v", err)
	}
}

func TestTwoConcurrentJudgesKeepTheirOwnVerdicts(t *testing.T) {
	f, _, state := newStore(t)
	dev, author := delegate(t, f, state, testSession, "dev", "u", "a.go")
	write(t, f, dev, "w1", "a.go", "A1")
	first, _ := judge(t, f, state, testSession, "verify", "review-1", author)
	second, _ := judge(t, f, state, testSession, "judge-a", "review-2", author)
	readAs(t, f, first, "r1", "a.go", "")
	readAs(t, f, second, "r2", "a.go", "")
	if err := f.VerifyAs(first, "v1", author, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.VerifyAs(second, "v2", author, false, "misses a case"); err != nil {
		t.Fatal(err)
	}
	if claim, _ := claimOf(f, author); len(claim.Verdicts) != 2 {
		t.Fatalf("verdicts = %+v; want both judges'", claim.Verdicts)
	}
}

func TestConsolidateAndDiscardBelongToTheOrchestratorOfTheRoot(t *testing.T) {
	f, root, state := newStore(t)
	dev, author := delegate(t, f, state, testSession, "dev", "u", "a.go")
	write(t, f, dev, "w1", "a.go", "A1")
	if _, err := f.ConsolidateAs(dev, author, "", nil); !errors.Is(err, vfs.ErrScopeDenied) {
		t.Fatalf("consolidation by the author = %v; want ErrScopeDenied", err)
	}
	if _, err := f.ConsolidateAs(orchestrator(otherSession), author, "", nil); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("consolidation from another root = %v; want ErrIdentity", err)
	}
	if err := f.DiscardAs(orchestrator(otherSession), author); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("discard from another root = %v; want ErrIdentity", err)
	}
	gate, _ := judge(t, f, state, testSession, "verify", "u/verify", author)
	readAs(t, f, gate, "r1", "a.go", "")
	if err := f.VerifyAs(gate, "v1", author, false, "wrong"); err != nil {
		t.Fatal(err)
	}
	_, err := f.ConsolidateAs(orchestrator(testSession), author, "", nil)
	var failing *vfs.FailingVerdictError
	if !errors.As(err, &failing) || !errors.Is(err, vfs.ErrFailingVerdict) {
		t.Fatalf("consolidation over a failing verdict = %v; want FailingVerdictError", err)
	}
	stale := failing.StagedState
	stale.Revision++
	if _, err := f.ConsolidateAs(orchestrator(testSession), author, "", &stale); !errors.Is(err, vfs.ErrStaleRevision) {
		t.Fatalf("acceptance of another state = %v; want ErrStaleRevision", err)
	}
	seq, err := f.ConsolidateAs(orchestrator(testSession), author, "", &failing.StagedState)
	if err != nil || seq <= 0 {
		t.Fatalf("accepted consolidation = %d, %v", seq, err)
	}
	if got, ok := diskContent(t, root, "a.go"); !ok || got != "A1" {
		t.Fatalf("workspace a.go = %q, %v; want the consolidated content", got, ok)
	}
}

func TestReassignedKeyIsTakenOverByAnotherSpecialistAtTheAdmittedAttempt(t *testing.T) {
	f, _, state := newStore(t)
	dev, key := delegate(t, f, state, testSession, "dev", "u", "a.go")
	write(t, f, dev, "w1", "a.go", "partial")
	// Another root carries the work on: the key is re-homed, repeatedly.
	next := vfs.Identity{SessionID: otherSession, WorkUnitID: "continue", AgentID: "fix", Specialist: "fix"}
	reassign(t, f, next, key, "a.go")
	admit(t, state, otherSession, "continue", "1", "fix")
	reassign(t, f, next, key, "a.go", "b.go")
	if _, err := f.Operate(dev, vfs.CallerOperation{CallID: "late", Action: vfs.OpCreate, Path: "a.go", Content: []byte("x")}); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("op by the earlier holder = %v; want ErrIdentity", err)
	}
	fix := callerOf(otherSession, "fix", "continue")
	bound, err := f.BindAs(fix, nil, nil)
	if err != nil || bound.Key != key || bound.AttemptID != "1" || !bound.Staged {
		t.Fatalf("takeover = %+v, %v; want key %s at the admitted attempt 1 with its staged work", bound, err, key)
	}
	if r := write(t, f, fix, "w2", "b.go", "more"); r.Revision != bound.Revision+1 {
		t.Fatalf("revision = %d; want it to continue from %d", r.Revision, bound.Revision)
	}
}

func TestALegacyStoreLoadsAndAdoptsTheAdmittedAttempt(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	f, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := f.AssignScope(ident("dev", "u", "dev"), []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	// An adopted binding written before holders were recorded.
	adopted, err := f.Bind(vfs.Identity{SessionID: testSession, WorkUnitID: "v", AttemptID: "1", AgentID: "fix", Specialist: "fix"}, []string{"b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Apply(vfs.Operation{Key: adopted, CallID: "w", Action: vfs.OpCreate, Path: "b.go", Content: []byte("kept")}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// The earlier store stamped its own attempt on a prelaunch claim.
	stampLegacyAttempt(t, state, pending, "7")

	f, err = vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	admit(t, state, testSession, "u", "1")
	bound, err := f.BindAs(callerOf(testSession, "dev", "u"), nil, nil)
	if err != nil || bound.Key != pending || bound.AttemptID != "1" {
		t.Fatalf("adoption over a legacy claim = %+v, %v; want %s at attempt 1", bound, err, pending)
	}
	admit(t, state, testSession, "v", "1", "fix")
	fix := callerOf(testSession, "fix", "v")
	reassign(t, f, ident("fix", "v", "fix"), adopted, "b.go")
	taken, err := f.BindAs(fix, nil, nil)
	if err != nil || taken.Key != adopted || !taken.Staged {
		t.Fatalf("takeover of an unheld binding = %+v, %v; want %s with its staged work", taken, err, adopted)
	}
	if got := readAs(t, f, fix, "r", "b.go", ""); string(got.Content) != "kept" {
		t.Fatalf("staged work = %q; want it kept", got.Content)
	}
}

// diskContent is the workspace content at rel, false when absent.
func diskContent(t *testing.T, root, rel string) (string, bool) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return "", false
	}
	return string(content), true
}

// stampLegacyAttempt rewrites the stored binding key with an attempt, the way
// stores written before the history owned delegated attempts recorded it.
func stampLegacyAttempt(t *testing.T, state string, key vfs.AgentID, attempt string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(state, "vfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var raw []byte
	if err := db.QueryRow("SELECT data FROM state WHERE id=1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	var bindings map[string]map[string]any
	if err := json.Unmarshal(stored["Bindings"], &bindings); err != nil {
		t.Fatal(err)
	}
	bindings[string(key)]["AttemptID"] = attempt
	if stored["Bindings"], err = json.Marshal(bindings); err != nil {
		t.Fatal(err)
	}
	if raw, err = json.Marshal(stored); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE state SET data=? WHERE id=1", raw); err != nil {
		t.Fatal(err)
	}
}

func TestAHeldWorkspaceLockTimesOutWithTheRetryRefusal(t *testing.T) {
	_, root, state := newStore(t)
	t.Cleanup(vfs.SetWorkspaceLockWait(shortLockWait))
	_, err := vfs.Open(root, state)
	refusedNaming(t, "Open() while another handle holds the lock", err, vfs.ErrWorkspaceBusy, "retry the same call")
}

func TestOpenersQueueOnTheWorkspaceLock(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	var wg sync.WaitGroup
	errs := make(chan error, parallelCommands)
	for i := range parallelCommands {
		wg.Go(func() {
			f, err := vfs.Open(root, state)
			if err == nil {
				_, err = f.AssignScope(ident("dev", "u"+string(rune('a'+i)), "dev"), []string{string(rune('a'+i)) + ".go"})
				err = errors.Join(err, f.Close())
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	f, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if claims := f.OwnershipClaims(testSession); len(claims) != parallelCommands {
		t.Fatalf("claims = %d; want every queued opener's", len(claims))
	}
}

func TestANativeEditOnAClaimedPathProceedsOnceTheClaimIsClosed(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(map[bool]string{false: "released", true: "discarded"}[staged], func(t *testing.T) {
			f, _, state := newStore(t)
			dev, author := delegate(t, f, state, testSession, "dev", "u", "a.go")
			if staged {
				write(t, f, dev, "w1", "a.go", "A1")
			}
			if err := f.GuardNativeEdit(testSession, []string{"a.go"}); err == nil {
				t.Fatal("native edit allowed on a claimed path")
			}
			var err error
			if staged {
				err = f.DiscardAs(orchestrator(testSession), author)
			} else {
				err = f.ReleaseClaim(testSession, author, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.GuardNativeEdit(testSession, []string{"a.go"}); err != nil {
				t.Fatalf("native edit after the claim closed = %v", err)
			}
		})
	}
}
