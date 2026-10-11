package gc_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestLoadCoordinatorMissingFileIsFresh(t *testing.T) {
	c, err := gc.LoadCoordinator(coordinatorFS(t))
	if err != nil {
		t.Fatalf("LoadCoordinator() error = %v", err)
	}
	if c.Version != 1 || c.Cursor != -1 {
		t.Errorf("LoadCoordinator() on a fresh state dir = %+v, want Version=1, Cursor=-1", c)
	}
	if c.Held() {
		t.Error("Held() on a fresh coordinator = true, want false")
	}
}

func TestSaveLoadCoordinatorRoundTrip(t *testing.T) {
	state := coordinatorFS(t)
	c, err := gc.LoadCoordinator(state)
	if err != nil {
		t.Fatalf("LoadCoordinator() error = %v", err)
	}
	c.Units = 3
	c.Mutations = 5
	c.Requested = true
	if err := gc.SaveCoordinator(state, c); err != nil {
		t.Fatalf("SaveCoordinator() error = %v", err)
	}

	reloaded, err := gc.LoadCoordinator(state)
	if err != nil {
		t.Fatalf("LoadCoordinator() reload error = %v", err)
	}
	if reloaded.Units != 0 || reloaded.Mutations != 0 || !reloaded.Requested {
		t.Errorf("reloaded coordinator = %+v, want derived counters absent from persisted authority and Requested=true", reloaded)
	}
}

func TestLoadCoordinatorRejectsCorruptState(t *testing.T) {
	state := coordinatorFS(t)
	c := &gc.Coordinator{Version: 2, Cursor: -1}
	if err := gc.SaveCoordinator(state, c); err != nil {
		t.Fatalf("SaveCoordinator() error = %v", err)
	}
	if _, err := gc.LoadCoordinator(state); err == nil {
		t.Fatal("LoadCoordinator() with an out-of-range NextMandate error = nil, want an error")
	}
}

func TestCoordinatorObserveCountsEffectiveMutations(t *testing.T) {
	c := &gc.Coordinator{Version: 1, Cursor: -1}
	entries := []vfs.JournalEntry{
		{Seq: 0, Operation: vfs.OpCreate, BeforeHash: "", AfterHash: "a"},
		{Seq: 1, Operation: vfs.OpPatch, BeforeHash: "a", AfterHash: "a"}, // no-op: before == after
		{Seq: 2, Operation: vfs.OpPatch, BeforeHash: "a", AfterHash: "b"},
		{Seq: 3, Operation: vfs.OpPatch, BeforeHash: "b", AfterHash: "c", CycleID: "maintenance-cycle"}, // GC's own cycle work is excluded
	}
	c.Observe(entries)
	if c.Cursor != -1 {
		t.Errorf("Observe() Cursor = %d, want -1 (cadence reset watermark, not an observation cursor)", c.Cursor)
	}
	if c.Mutations != 2 {
		t.Errorf("Observe() Mutations = %d, want 2 (create+patch effective changes, no-op and cycle-owned excluded)", c.Mutations)
	}

	// A second Observe with the same or older entries must not double-count.
	c.Observe(entries)
	if c.Mutations != 2 {
		t.Errorf("Observe() replayed the same entries and counted Mutations = %d, want still 2", c.Mutations)
	}
}

func TestCoordinatorObserveNilIsNoop(t *testing.T) {
	c := &gc.Coordinator{Version: 1, Cursor: -1}
	c.Observe(nil)
	if c.Cursor != -1 || c.Mutations != 0 {
		t.Errorf("Observe(nil) = Cursor:%d Mutations:%d, want unchanged", c.Cursor, c.Mutations)
	}
}

func TestCoordinatorCloseArchivesCycleAndRotatesMandate(t *testing.T) {
	c := &gc.Coordinator{Version: 1, Cursor: -1, Cycle: &gc.Cycle{Reason: ""}}
	c.Close("done")
	if c.Cycle != nil {
		t.Error("Close() left Cycle non-nil, want it cleared")
	}
	if len(c.History) != 1 || c.History[0].Reason != "done" {
		t.Errorf("Close() History = %+v, want one entry with Reason=done", c.History)
	}
	if c.NextMandate != 1 {
		t.Errorf("Close() NextMandate = %d, want 1 after rotating from 0", c.NextMandate)
	}
}

func TestCoordinatorCloseWithoutCycleIsNoop(t *testing.T) {
	c := &gc.Coordinator{Version: 1, Cursor: -1}
	c.Close("done")
	if len(c.History) != 0 {
		t.Errorf("Close() without a cycle History = %+v, want none archived", c.History)
	}
}

func TestCoordinatorAttachAndRequire(t *testing.T) {
	c := &gc.Coordinator{Version: 1, Cursor: -1, Cycle: &gc.Cycle{Sessions: map[string]string{}}}
	h, err := history.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	}()
	attach := func(role, child string) error {
		err := h.AttachMaintenance("root", "cycle", role, child, "")
		c.Cycle.Sessions = map[string]string{}
		c.Cycle.Plan.SessionID = "root"
		c.Cycle.Plan.CycleID = "cycle"
		c.ObserveExecutions(h)
		return err
	}

	if err := attach("collector", "session-a"); err != nil {
		t.Fatalf("Attach(collector) error = %v", err)
	}
	if err := c.Require("session-a", "collector"); err != nil {
		t.Errorf("Require(session-a, collector) error = %v, want nil", err)
	}
	if err := c.Require("session-b", "collector"); err == nil {
		t.Error("Require(session-b, collector) error = nil, want an error for a different session")
	}

	if err := attach("invalid-role", "session-c"); err == nil {
		t.Error("Attach(invalid-role) error = nil, want an error")
	}
	if err := attach("verifier", "session-a"); err == nil {
		t.Error("Attach(verifier, session-a) error = nil, want an error: session-a is already the collector")
	}
	if err := attach("collector", "session-x"); err == nil {
		t.Error("Attach(collector, session-x) error = nil, want an error: collector is already attached to session-a")
	}
}

func TestCoordinatorAttachRequiresActiveCycle(t *testing.T) {
	h, err := history.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := h.AttachMaintenance("root", "", "collector", "session-a", ""); err == nil {
		t.Error("Attach() with no active cycle error = nil, want an error")
	}
}

func TestCoordinatorAdmit(t *testing.T) {
	h, err := history.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("history.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Fatalf("h.Close() error = %v", err)
		}
	})

	c := &gc.Coordinator{Version: 1, Cursor: -1}
	if err := c.Admit(h, "", "unit-1", "session-1", "agent", "d1"); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if c.Units != 1 {
		t.Errorf("Admit() Units = %d, want 1", c.Units)
	}

	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindAdmitted {
		t.Errorf("Admit() last entry = %+v, want KindAdmitted", last)
	}
}

// A cycle in flight never refuses an admission: the dispatching caller ends the
// cycle, the coordinator does not hold the orchestrator.
func TestCoordinatorAdmitIgnoresCycleInFlight(t *testing.T) {
	h, err := history.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("history.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Fatalf("h.Close() error = %v", err)
		}
	})

	c := &gc.Coordinator{Version: 1, Cursor: -1, Cycle: &gc.Cycle{}}
	if err := c.Admit(h, "", "unit-1", "session-1", "some-other-agent", "d1"); err != nil {
		t.Fatalf("Admit() while a cycle is in flight error = %v, want none", err)
	}
	if c.Units != 1 {
		t.Errorf("Admit() Units = %d, want 1", c.Units)
	}
}

func TestCoordinatorBindStartsPaceOverForAnotherSession(t *testing.T) {
	c := &gc.Coordinator{Version: 1, Cursor: 100, Units: 4, Mutations: 18, Session: "old"}
	c.Bind("")
	if c.Units != 4 || c.Mutations != 18 || c.Session != "old" {
		t.Fatalf("Bind(\"\") = %+v, want no change", c)
	}
	c.Bind("old")
	if c.Units != 4 || c.Mutations != 18 {
		t.Fatalf("Bind(same session) = %+v, want counters kept", c)
	}
	c.Bind("new")
	if c.Units != 0 || c.Mutations != 0 || c.Session != "new" || c.Cursor != -1 {
		t.Fatalf("Bind(new session) = %+v, want counters and cursor reset", c)
	}
}

func TestAuthorizedScope(t *testing.T) {
	plan := gc.Plan{Closure: []string{"a.go", "b.go"}}
	findings := []gc.Finding{
		{ID: "f1", Path: "a.go", Tool: "deadcode", ToolVersion: "1.0", Rule: "unreachable", Evidence: "e"},
		{ID: "f2", Path: "b.go", Tool: "deadcode", ToolVersion: "1.0", Rule: "unreachable", Evidence: "e"},
	}
	investigations := []gc.Investigation{
		{FindingID: "f1", Outcome: "confirmed", Evidence: "traced"},
	}

	scope, err := gc.AuthorizedScope(plan, findings, investigations)
	if err != nil {
		t.Fatalf("AuthorizedScope() error = %v", err)
	}
	if len(scope) != 1 || scope[0] != "a.go" {
		t.Errorf("AuthorizedScope() = %v, want [a.go] (only f1 is authorized)", scope)
	}
}

func TestAuthorizedScopeRejectsFindingOutsideClosure(t *testing.T) {
	plan := gc.Plan{Closure: []string{"a.go"}}
	findings := []gc.Finding{
		{ID: "f1", Path: "outside.go", Tool: "deadcode", ToolVersion: "1.0", Rule: "unreachable", Evidence: "e"},
	}
	investigations := []gc.Investigation{{FindingID: "f1", Outcome: "confirmed", Evidence: "traced"}}

	if _, err := gc.AuthorizedScope(plan, findings, investigations); err == nil {
		t.Fatal("AuthorizedScope() with an authorized finding outside the closure error = nil, want an error")
	}
}

func TestCoordinatorObserveCountsOnlyTheBoundSession(t *testing.T) {
	c := &gc.Coordinator{Version: 1, Cursor: -1, Session: "root"}
	c.Observe([]vfs.JournalEntry{
		{Seq: 0, SessionID: "old", Operation: vfs.OpCreate, AfterHash: "a"},
		{Seq: 1, SessionID: "root", Operation: vfs.OpCreate, AfterHash: "b"},
	})
	if c.Cursor != -1 || c.Mutations != 1 {
		t.Errorf("Observe() = Cursor:%d Mutations:%d, want 1 and only the bound session's mutation", c.Cursor, c.Mutations)
	}
}

func TestDeclareWithoutSessionDeltaSerializesAnEmptyArray(t *testing.T) {
	plan, err := gc.Declare(context.Background(), nil, gc.Request{SessionID: "s", CycleID: "c", Mandate: gc.MandateDeadCode}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"delta":[]`) {
		t.Errorf("plan = %s, want delta as an empty array", b)
	}
}

func coordinatorFS(t *testing.T) *vfs.FS {
	t.Helper()
	fs, err := vfs.Open(t.TempDir(), filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	return fs
}

// TestCoordinatorCycleKeysAndScopeAreNeverPersisted: a cycle's keys and scope
// are read back from the core bindings, so a value the core never granted
// cannot survive a save.
func TestCoordinatorCycleKeysAndScopeAreNeverPersisted(t *testing.T) {
	fs, err := vfs.Open(t.TempDir(), filepath.Join(t.TempDir(), "vfs-state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	c := &gc.Coordinator{Version: 1, Cursor: -1, Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{CycleID: "c1"}}, Phase: "investigate", Scope: []string{"never-granted.go"}, AuthorKey: "stale", VerifierKey: "stale"}}
	if err := gc.SaveCoordinator(fs, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := gc.LoadCoordinator(fs)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Cycle.AuthorKey != "" || loaded.Cycle.VerifierKey != "" || len(loaded.Cycle.Scope) != 0 || loaded.Cycle.Scope == nil {
		t.Fatalf("loaded cycle = %q/%q scope %v; want an empty scope and no keys the core did not grant", loaded.Cycle.AuthorKey, loaded.Cycle.VerifierKey, loaded.Cycle.Scope)
	}
}
