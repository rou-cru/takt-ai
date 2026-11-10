package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// contractFixture is the wire-contract fixture the TypeScript plugin tests read.
func contractFixture(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "agents", "opencode", "testdata", "contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// sameJSON fails unless got and want decode to the same value.
func sameJSON(t *testing.T, section string, got []byte, want json.RawMessage) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: the binary answered %q: %v", section, got, err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s drift\n got: %s\nwant: %s", section, got, want)
	}
}

func jsonKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("%q: %v", raw, err)
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestOpenCodeContracts pins the wire fixtures the plugin tests read: those a Go type defines are
// compared with it, and those the binary answers or accepts are produced by driving the binary.
func TestOpenCodeContracts(t *testing.T) {
	fixture := contractFixture(t)
	check := func(section string, value any) {
		t.Helper()
		got, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		sameJSON(t, section, got, fixture[section])
	}
	check("inspect_page_limit", vfs.MaxJournalPageSize)
	check("coordinator", gc.Coordinator{Version: 1, Units: 3, Mutations: 1, Cursor: 2, NextMandate: 1, Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root-session", CycleID: "cycle-1", Mandate: gc.MandateComplexity}, Delta: []gc.Change{{Path: "src/a.go", Introduced: true}}, Closure: []string{"src/a.go", "src/b.go"}, Reachability: gc.ReachCodegraph}, Phase: "collect", Scope: []string{"src/a.go"}, Sessions: map[string]string{"collector": "session-collector", "verifier": "session-verifier"}}})
	check("dispatch_null", any(nil))
	check("dag_normal", history.BuildSnapshot([]history.Entry{
		{Seq: 0, Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "root-session", WorkUnitID: "unit-1", AttemptID: "1", NodeKind: history.NodeKindDelegated, Cause: history.CauseNone, PlanVersion: "plan-1", Contract: "Implement feature"},
		{Seq: 1, Author: history.AuthorHarness, Kind: history.KindAdmitted, SessionID: "root-session", WorkUnitID: "unit-1", AttemptID: "1", NodeKind: history.NodeKindDelegated, Cause: history.CauseNone},
		{Seq: 2, Author: history.AuthorOrchestrator, Kind: history.KindActivityStarted, SessionID: "root-session", ActivityID: "activity-1", NodeKind: history.NodeKindOrchestrator, Cause: history.CauseNone},
	}))
	// An unmonitored execution and a settled unit whose delivery has a result: the
	// flight and assurance names the TS decoder reads.
	revision := uint64(2)
	check("dag_assurance", history.BuildSnapshot([]history.Entry{
		{Seq: 0, Author: history.AuthorHarness, Kind: history.KindAdmitted, SessionID: "root-session", WorkUnitID: "unit-1", AttemptID: "1", NodeKind: history.NodeKindDelegated, Cause: history.CauseNone},
		{Seq: 1, Author: history.AuthorHarness, Kind: history.KindLaunched, SessionID: "root-session", WorkUnitID: "unit-1", AttemptID: "1", Cause: history.CauseNone},
		{Seq: 2, Author: history.AuthorHarness, Kind: history.KindUnmonitored, SessionID: "root-session", WorkUnitID: "unit-1", AttemptID: "1", Cause: history.CauseNone},
		{Seq: 3, Author: history.AuthorHarness, Kind: history.KindAdmitted, SessionID: "root-session", WorkUnitID: "unit-2", AttemptID: "1", NodeKind: history.NodeKindDelegated, Cause: history.CauseNone},
		{Seq: 4, Author: history.AuthorHarness, Kind: history.KindTerminated, SessionID: "root-session", WorkUnitID: "unit-2", AttemptID: "1", Cause: history.CauseNone, Outcome: history.OutcomeCompleted},
		{Seq: 5, Author: history.AuthorVerification, Kind: history.KindResult, SessionID: "root-session", WorkUnitID: "unit-2", AttemptID: "1", Cause: history.CauseNone, Status: history.ResultFailed, Revision: &revision, Evidence: "journal/7"},
	}))
	// An orchestrator activity whose capture was lost: still in flight, never
	// settled, with the unmonitored flight the TS decoder reads.
	check("dag_activity_unmonitored", history.BuildSnapshot([]history.Entry{
		{Seq: 0, Author: history.AuthorOrchestrator, Kind: history.KindActivityStarted, SessionID: "root-session", ActivityID: "activity-1", NodeKind: history.NodeKindOrchestrator, Cause: history.CauseNone},
		{Seq: 1, Author: history.AuthorHarness, Kind: history.KindUnmonitored, SessionID: "root-session", ActivityID: "activity-1", NodeKind: history.NodeKindOrchestrator, Cause: history.CauseNone},
	}))
	check("dag_unavailable", struct {
		Capture string `json:"capture"`
	}{history.CaptureUnavailable})
}

// TestOpenCodeDispatchContracts drives the binary through the exchanges the plugin makes: the
// interlocutor returns it answers are compared with the fixture, and the requests the fixture holds are
// each accepted as the action they name.
func TestOpenCodeDispatchContracts(t *testing.T) {
	bin := buildTaktAI(t)
	fixture := contractFixture(t)
	s := newSession(t, bin)
	const (
		root  = "root-session"
		child = "child-session"
	)
	// A handoff returns the interface with the entries the lent session delivered.
	s.record("child-session", "recorded before the switch", 0)
	s.must(request{Action: "switch", Session: root, Child: "child-session", Agent: "pm", Requirements: []int64{}})
	delivered := []int64{s.record("child-session", "delivered one", 0), s.record("child-session", "delivered two", 0)}
	handoff := s.must(request{Action: "handoff", Session: root, Child: "child-session", Agent: "pm", Result: "Standard", AdditionalContext: "Implemented the requested change.", ExtraArtifacts: []string{"result.md"}, ResultIDs: delivered})
	sameJSON(t, "handoff", []byte(handoff.stdout), fixture["handoff"])

	s.must(request{Action: "switch", Session: root, Child: child, Agent: "pm", Requirements: []int64{}})
	abort := s.must(request{Action: "abort_switch", Session: root, Child: child, Evidence: "User cancelled the task.", Origin: "user"})
	sameJSON(t, "abort", []byte(abort.stdout), fixture["abort"])

	// Each fixture request reaches the action it names and is refused for what its content states.
	raw := func(section string) result {
		return s.run("dispatch", "--workspace", s.root, "--state", s.state, "--request", string(fixture[section]))
	}
	for section, want := range map[string]string{
		"dispatch_request":          "only the temporary interlocutor holder may hand off",
		"dispatch_switch_request":   "name the Takt memory entry IDs",
		"dispatch_validate_request": "#42 does not exist",
		"dispatch_inputs_request":   "#42 does not exist",
	} {
		res := raw(section)
		if res.code == 0 {
			t.Errorf("%s was accepted: %s", section, res.stdout)
		}
		requireNamed(t, res, want)
	}
}

// TestOpenCodeVFSBindContract pins the shape of a bind answer: the fixture holds exactly the keys the
// binary answers with.
func TestOpenCodeVFSBindContract(t *testing.T) {
	bin := buildTaktAI(t)
	fixture := contractFixture(t)
	s := newSession(t, bin)
	const (
		root = "root-session"
		unit = "unit-1"
		host = "host/dev/unit-1"
	)
	req := func(extra map[string]any) string {
		body := map[string]any{"ipc_version": vfsIPCVersion, "session_id": root, "caller_session": root, "caller_agent": vfs.OrchestratorInstance, "work_unit_id": root}
		for k, v := range extra {
			body[k] = v
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	s.must(request{Action: "inputs", Session: root, Event: unit})
	if res := s.runIn(req(map[string]any{"work_unit_id": unit, "agent_id": "dev", "specialist": "dev", "scope": []string{"a.txt"}}), "vfs", "assign", "--workspace", s.root, "--state", s.state); res.code != 0 {
		t.Fatalf("assign: %s", res.stderr)
	}
	s.must(request{Action: "prepare", Owner: testOwnerInstance, Session: root, Event: unit, Dispatch: "call", Caller: root, Agent: "dev", Child: host})
	bind := s.runIn(req(map[string]any{"caller_session": host, "caller_agent": "dev", "work_unit_id": unit, "scope": []string{"a.txt"}}), "vfs", "bind", "--workspace", s.root, "--state", s.state)
	if bind.code != 0 {
		t.Fatalf("bind: %s", bind.stderr)
	}
	if got, want := jsonKeys(t, []byte(bind.stdout)), jsonKeys(t, fixture["vfs_bind"]); !reflect.DeepEqual(got, want) {
		t.Errorf("vfs_bind keys = %v, want the fixture's %v", got, want)
	}
}
