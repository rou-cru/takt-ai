package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	protocol "github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestOpenCodeContracts(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "takt", "agents", "opencode", "testdata", "contracts.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	check := func(section string, value any) {
		t.Helper()
		got, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(fixture[section]) {
			t.Errorf("%s drift\n got: %s\nwant: %s", section, got, fixture[section])
		}
	}
	type memoryEnvelope struct {
		OK     bool   `json:"ok"`
		Result any    `json:"result,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	check("inspect_page_limit", vfs.MaxJournalPageSize)
	check("vfs_bind", response{OK: true, Key: "bind-opaque", AttemptID: "1", InvariantsVersion: "inv1:abc"})
	check("memory_record", memoryEnvelope{OK: true, Result: memory.RecordResult{ID: 42, Deduplicated: false}})
	check("memory_close", memoryEnvelope{OK: true, Result: memory.CloseResult{EndAnchorID: 43, Entries: 2}})
	check("memory_search", memoryEnvelope{OK: true, Result: memory.SearchResult{Results: []memory.SearchHit{{ID: 42, Type: "decision", Title: "[dev] Cache for ten minutes", Preview: "Cache policy.", Disputed: true, Exception: true}}, OmittedSuperseded: 1}})
	check("memory_error", memoryEnvelope{OK: false, Error: "session is closed"})
	check("coordinator", gc.Coordinator{Version: 1, Units: 3, Mutations: 1, Cursor: 2, NextMandate: 1, Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root-session", CycleID: "cycle-1", Mandate: gc.MandateComplexity}, Delta: []gc.Change{{Path: "src/a.go", Introduced: true}}, Closure: []string{"src/a.go", "src/b.go"}, Reachability: gc.ReachCodegraph}, Phase: "collect", Scope: []string{"src/a.go"}, Sessions: map[string]string{"collector": "session-collector", "verifier": "session-verifier"}}})
	handoff, err := interlocutorReturn{Envelope: protocol.Envelope{Result: "Standard", AdditionalContext: "Implemented the requested change.", ExtraArtifacts: []string{"result.md"}, Memory: protocol.MemoryIndex{Entries: []int64{42, 43}}}, Artifact: &artifactReport{Path: "PRD.md", Present: true}}.withNotice("pm")
	if err != nil {
		t.Fatal(err)
	}
	check("handoff", handoff)
	abort, err := interlocutorReturn{Envelope: protocol.Envelope{Result: "Aborted", AdditionalContext: "User cancelled the task.", ExtraArtifacts: []string{}, Memory: protocol.MemoryIndex{Entries: []int64{42}, NextAfter: 42}}, Child: "child-session", Root: "root-session"}.withNotice("pm, whose lent session was aborted")
	if err != nil {
		t.Fatal(err)
	}
	check("abort", abort)
	check("lent_entries", protocol.MemoryIndex{Entries: []int64{43}})
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
	// Keep concrete response/request DTO coverage: their exact optional-field JSON is contractual.
	check("dispatch_request", coordinationRequest{Action: "handoff", Event: "unit-1", Session: "root-session", Agent: "builder", Role: "interlocutor", Child: "child-session", Outcome: "Standard", Scope: []string{"src/a.go"}})
	check("dispatch_switch_request", coordinationRequest{Action: "switch", Session: "root-session", Agent: "builder", Child: "child-session", Artifact: "result.md"})
	check("dispatch_validate_request", coordinationRequest{Action: "validate_results", Session: "root-session", Agent: "builder", ResultIDs: []int64{42}})
	check("dispatch_inputs_request", coordinationRequest{Action: "inputs", Event: "unit-1", Session: "root-session", ResultIDs: []int64{42}})
}
