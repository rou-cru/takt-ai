package gc

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

const (
	// dueMutations alone makes a cycle due under the shipped cadence policy.
	dueMutations = 40
	// advanceSession is the session whose delta the cycle plan declares.
	advanceSession = "s-advance"
)

// advanceEnv opens the history and virtual filesystem Advance reads.
func advanceEnv(t *testing.T) (*history.History, *vfs.FS) {
	t.Helper()
	h, err := history.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	fs, err := vfs.Open(t.TempDir(), filepath.Join(t.TempDir(), "vfs-state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	return h, fs
}

func sessionDelta() []vfs.JournalEntry {
	var entries []vfs.JournalEntry
	for seq := range dueMutations {
		entries = append(entries, vfs.JournalEntry{Seq: seq, SessionID: advanceSession, Path: "a.go", Operation: vfs.OpCreate, BeforeHash: "before", AfterHash: "after"})
	}
	return entries
}

func TestAdvanceIdleLeavesCountersAlone(t *testing.T) {
	h, fs := advanceEnv(t)
	c := &Coordinator{Version: 1, Cursor: -1, Since: -1, Session: advanceSession}
	if err := c.Admit(h, "", "unit", advanceSession, "dev", "dispatch"); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Finish(h, "", "unit", advanceSession); err != nil {
		t.Fatal(err)
	}
	d, b, err := c.Advance(context.Background(), fs, h, nil, advanceSession, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeIdle || b.Held() || b.Proceed || c.Cycle != nil || c.Units != 1 {
		t.Errorf("Advance() = %+v %+v cycle=%v units=%d; want an untouched idle coordinator", d, b, c.Cycle, c.Units)
	}
}

func TestAdvanceSkipAndAbortResetPace(t *testing.T) {
	h, fs := advanceEnv(t)
	p, err := LoadTriggerPolicy()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		coordinator *Coordinator
		want        Outcome
	}{
		"user request with no delta is skipped": {&Coordinator{Version: 1, Cursor: -1, Since: -1, Session: advanceSession, Requested: true, Units: 5}, OutcomeSkip},
		"starved cycle is aborted":              {&Coordinator{Version: 1, Cursor: -1, Since: -1, Session: advanceSession, Mutations: dueMutations, Deferrals: p.Cadence.MaxDeferrals}, OutcomeAbort},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d, _, err := tc.coordinator.Advance(context.Background(), fs, h, func() []vfs.JournalEntry {
				if tc.coordinator.Mutations > 0 {
					return sessionDelta()
				}
				return nil
			}(), advanceSession, nil)
			if err != nil {
				t.Fatal(err)
			}
			c := tc.coordinator
			if d.Outcome != tc.want {
				t.Fatalf("Outcome = %v, want %v", d.Outcome, tc.want)
			}
			if c.Units != 0 || c.Mutations != 0 || c.Deferrals != 0 || c.Requested || c.Cycle != nil {
				t.Errorf("coordinator after %v = %+v, want the pace counters reset", tc.want, c)
			}
		})
	}
}

func TestAdvanceLeavesARunningCycleAlone(t *testing.T) {
	h, fs := advanceEnv(t)
	running := &Cycle{Phase: "collect"}
	c := &Coordinator{Version: 1, Cursor: -1, Since: -1, Session: advanceSession, Mutations: dueMutations, Cycle: running}
	d, b, err := c.Advance(context.Background(), fs, h, sessionDelta(), advanceSession, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeSkip || d.Reason != ReasonInFlight || b.Held() {
		t.Errorf("Advance() = %+v %+v, want a skip because a cycle is in flight", d, b)
	}
	if c.Cycle != running || c.Mutations != dueMutations {
		t.Errorf("coordinator = %+v, want the running cycle and counters untouched", c)
	}
}

func TestAdvanceDefersWhileUnitsAreInFlight(t *testing.T) {
	h, fs := advanceEnv(t)
	c := &Coordinator{Version: 1, Cursor: -1, Since: -1, Session: advanceSession}
	if err := c.Admit(h, "", "unit-1", advanceSession, "dev", "d1"); err != nil {
		t.Fatal(err)
	}
	c.Mutations = dueMutations

	d, b, err := c.Advance(context.Background(), fs, h, sessionDelta(), advanceSession, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeRun || b.Proceed || b.Reason != ReasonActiveUnits {
		t.Fatalf("Advance() = %+v %+v, want a due cycle held by the active unit", d, b)
	}
	if !slices.Equal(b.ActiveUnitIDs, []string{"unit-1"}) {
		t.Errorf("ActiveUnitIDs = %v, want the blocking unit named", b.ActiveUnitIDs)
	}
	if c.Cycle != nil || c.Deferrals != 1 || c.Held() {
		t.Errorf("coordinator = %+v, want one deferral and no cycle", c)
	}
}

func TestAdvanceStartsCycleWithDeclaredPlan(t *testing.T) {
	h, fs := advanceEnv(t)
	c := &Coordinator{Version: 1, Cursor: -1, Since: -1, Session: advanceSession, Mutations: dueMutations, Deferrals: 2, NextMandate: 2}
	reach := func(_ context.Context, path string) ([]string, error) { return []string{"dep-of-" + path}, nil }

	d, b, err := c.Advance(context.Background(), fs, h, sessionDelta(), advanceSession, reach)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeRun || !b.Proceed {
		t.Fatalf("Advance() = %+v %+v, want the cycle to proceed", d, b)
	}
	if c.Cycle == nil || c.Cycle.Phase != "baseline" || c.Deferrals != 0 {
		t.Fatalf("coordinator = %+v, want a baseline cycle", c)
	}
	plan := c.Cycle.Plan
	if plan.Mandate != MandateDuplication || plan.SessionID != advanceSession || len(plan.CycleID) != 2*cycleIDBytes {
		t.Errorf("plan = %+v, want the rotation's third mandate and a random hex cycle id", plan)
	}
	if plan.Reachability != ReachCodegraph || !slices.Equal(plan.Closure, []string{"a.go", "dep-of-a.go"}) {
		t.Errorf("plan closure = %v (%s), want the delta plus its dependents", plan.Closure, plan.Reachability)
	}
	if c.Cursor != -1 {
		t.Errorf("Cursor = %d, want the cadence watermark unchanged until closure", c.Cursor)
	}
	if !c.Held() {
		t.Error("Held() = false while a cycle is in flight")
	}
}

func TestAdvanceFallsBackToJournalOnlyWhenGraphFails(t *testing.T) {
	h, fs := advanceEnv(t)
	c := &Coordinator{Version: 1, Cursor: -1, Since: -1, Session: advanceSession, Mutations: dueMutations}
	broken := func(context.Context, string) ([]string, error) { return nil, errors.New("index stale") }
	if _, _, err := c.Advance(context.Background(), fs, h, sessionDelta(), advanceSession, broken); err != nil {
		t.Fatal(err)
	}
	if c.Cycle == nil || c.Cycle.Plan.Reachability != ReachJournalOnly || !strings.Contains(c.Cycle.Plan.Gap, "index stale") {
		t.Fatalf("plan = %+v, want the recorded journal-only gap", c.Cycle)
	}
}

func TestAdvanceCountsDeclarationFailureTowardAbort(t *testing.T) {
	h, fs := advanceEnv(t)
	c := &Coordinator{Version: 1, Cursor: -1, Since: -1, Session: advanceSession, Mutations: dueMutations, Deferrals: 1}
	// An empty session id makes the declaration invalid.
	_, _, err := c.Advance(context.Background(), fs, h, sessionDelta(), "", nil)
	if err == nil {
		t.Fatal("Advance() error = nil, want the declaration failure")
	}
	if c.Cycle != nil || c.Deferrals < 1 {
		t.Errorf("coordinator = %+v, want no cycle and the failure counted as a deferral", c)
	}
}

func TestUnfinishedUnitIDsAreSorted(t *testing.T) {
	proj := history.Projection{Units: map[string]history.Unit{
		history.UnitKey("s", "b"):     {WorkUnitID: "b", State: history.StateInFlight},
		history.UnitKey("s", "a"):     {WorkUnitID: "a", State: history.StatePlanned},
		history.UnitKey("other", "a"): {WorkUnitID: "a", State: history.StateInFlight},
		history.UnitKey("s", "c"):     {WorkUnitID: "c", State: history.State("done")},
	}}
	if got := unfinishedUnitIDs(proj); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("unfinishedUnitIDs() = %v, want [a b]", got)
	}
	if got := unfinishedUnitIDs(history.Projection{}); len(got) != 0 {
		t.Errorf("unfinishedUnitIDs(empty) = %v", got)
	}
}

func TestLoadCoordinatorRejectsBadCoreState(t *testing.T) {
	for name, content := range map[string]string{"not json": "{ nope", "future version": `{"version":2}`} {
		t.Run(name, func(t *testing.T) {
			_, fs := advanceEnv(t)
			if err := fs.SetMaintenance([]byte(content)); err != nil {
				if name == "not json" {
					return
				}
				t.Fatal(err)
			}
			if _, err := LoadCoordinator(fs); err == nil {
				t.Fatal("corrupt core state accepted")
			}
		})
	}
}

func TestAuthorizedScopeCapsFindingsAndFiles(t *testing.T) {
	p, err := LoadTriggerPolicy()
	if err != nil {
		t.Fatal(err)
	}
	var closure []string
	var findings []Finding
	var investigations []Investigation
	// One finding per file, more files than the policy authorizes.
	for i := range p.Limits.Files + 2 {
		path := string(rune('a'+i)) + ".go"
		id := "f" + path
		closure = append(closure, path)
		findings = append(findings, Finding{ID: id, Path: path, Tool: "deadcode", ToolVersion: "1", Rule: "r", Evidence: "e"})
		investigations = append(investigations, Investigation{FindingID: id, Outcome: "confirmed", Evidence: "traced"})
	}
	scope, err := AuthorizedScope(Plan{Closure: closure}, findings, investigations)
	if err != nil {
		t.Fatal(err)
	}
	wantLen := min(p.Limits.Files, p.Limits.Findings)
	if len(scope) != wantLen || !slices.IsSorted(scope) {
		t.Errorf("scope = %v, want %d sorted paths", scope, wantLen)
	}

	// Several findings in one file authorize the file once.
	same := []Finding{
		{ID: "x1", Path: "a.go", Tool: "deadcode", ToolVersion: "1", Rule: "r", Evidence: "e"},
		{ID: "x2", Path: "a.go", Tool: "deadcode", ToolVersion: "1", Rule: "r", Evidence: "e"},
	}
	proof := []Investigation{{FindingID: "x1", Outcome: "confirmed", Evidence: "t"}, {FindingID: "x2", Outcome: "confirmed", Evidence: "t"}}
	scope, err = AuthorizedScope(Plan{Closure: []string{"a.go"}}, same, proof)
	if err != nil || !slices.Equal(scope, []string{"a.go"}) {
		t.Errorf("AuthorizedScope(same file) = %v, %v; want [a.go] once", scope, err)
	}
	if scope, err := AuthorizedScope(Plan{}, nil, nil); err != nil || scope == nil || len(scope) != 0 {
		t.Errorf("AuthorizedScope(nothing) = %v, %v; want an empty non-nil scope", scope, err)
	}
}
