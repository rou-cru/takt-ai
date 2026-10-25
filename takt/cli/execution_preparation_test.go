package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// testOwner names the harness instance the tests act as.
const testOwner = "test-instance"

func executionRequest(t *testing.T, fx *vfsFixture, r coordinationRequest) (map[string]any, error) {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	err = runDispatch([]string{"--workspace", fx.root, "--state", fx.state, "--request", string(data)}, &out, &stderr)
	var reply map[string]any
	if err == nil {
		if err = json.Unmarshal(out.Bytes(), &reply); err != nil {
			t.Fatalf("decode %s: %v", out.String(), err)
		}
	}
	return reply, err
}

func TestPrepareInputsAbsenceDoesNotSpendAttempt(t *testing.T) {
	fx := newVFSFixture(t)
	r := coordinationRequest{Action: "prepare", Owner: testOwner, Session: rootA, Event: "brief", Dispatch: "call", Caller: rootA, Agent: "pm"}
	if _, err := executionRequest(t, fx, r); err == nil || !strings.Contains(err.Error(), "dispatch_inputs") {
		t.Fatalf("missing inputs: %v", err)
	}
	p := gcProjection(t, fx.state)
	if len(p.Executions) != 0 || p.InFlight() != 0 {
		t.Fatal("invalid input declaration admitted execution")
	}
	if _, err := executionRequest(t, fx, coordinationRequest{Action: "inputs", Session: rootA, Event: "brief"}); err != nil {
		t.Fatal(err)
	}
	got, err := executionRequest(t, fx, r)
	if err != nil || got["attempt"] != "1" || got["launchable"] != true {
		t.Fatalf("continuation: %v %v", got, err)
	}
	if inputs, ok := got["inputs"].([]any); !ok || len(inputs) != 0 {
		t.Fatalf("explicit none lost: %+v", got)
	}
}

func TestPrepareInvalidSecondAuthorDoesNotLeaveFirstGate(t *testing.T) {
	fx := newVFSFixture(t)
	a := fx.delegate(rootA, "writer", "dev", "a.txt")
	fx.must("op", vfsReq("writer", "dev", map[string]any{"call_id": "write", "action": "create", "path": "a.txt", "content": "valid work"}))
	fx.dispatch("finish", rootA, "writer", "dev")
	_, err := executionRequest(t, fx, coordinationRequest{Action: "inputs", Session: rootA, Event: "review"})
	if err != nil {
		t.Fatal(err)
	}
	r := coordinationRequest{Action: "prepare", Owner: testOwner, Session: rootA, Event: "review", Dispatch: "review-call", Caller: rootA, Agent: "verify", AuthorKeys: []vfs.AgentID{vfs.AgentID(a), "missing-author"}}
	if _, err = executionRequest(t, fx, r); err == nil {
		t.Fatal("invalid second author admitted")
	}
	p := gcProjection(t, fx.state)
	if _, ok := p.Executions[r.Dispatch]; ok {
		t.Fatal("invalid authors spent attempt")
	}
	claims := fx.must("claims", orchReq(rootA, nil))
	for _, c := range claims.Claims {
		if c.AuthorKey != "" {
			t.Fatalf("orphan gate: %+v", c)
		}
	}
	r.AuthorKeys = []vfs.AgentID{vfs.AgentID(a)}
	got, err := executionRequest(t, fx, r)
	if err != nil || got["attempt"] != "1" {
		t.Fatalf("valid retry: %v %v", got, err)
	}
}

func TestPrepareReadyPersistenceFailureCompensates(t *testing.T) {
	fx := newVFSFixture(t)
	a := fx.delegate(rootA, "writer", "dev", "a.txt")
	fx.must("op", vfsReq("writer", "dev", map[string]any{"call_id": "write", "action": "create", "path": "a.txt", "content": "valid work"}))
	fx.dispatch("finish", rootA, "writer", "dev")
	if _, err := executionRequest(t, fx, coordinationRequest{Action: "inputs", Session: rootA, Event: "review"}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(fx.state, "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = db.Exec(`CREATE TRIGGER fail_ready BEFORE INSERT ON history WHEN json_extract(CAST(NEW.data AS TEXT),'$.kind')='execution-ready' BEGIN SELECT RAISE(ABORT,'injected readiness failure'); END;`); err != nil {
		t.Fatal(err)
	}
	r := coordinationRequest{Action: "prepare", Owner: testOwner, Session: rootA, Event: "review", Dispatch: "failed-call", Caller: rootA, Agent: "verify", AuthorKeys: []vfs.AgentID{vfs.AgentID(a)}}
	if _, err = executionRequest(t, fx, r); err == nil || !strings.Contains(err.Error(), "new subagent call") {
		t.Fatalf("preparation failure continuation: %v", err)
	}
	p := gcProjection(t, fx.state)
	if p.InFlight() != 0 || !p.Executions[r.Dispatch].Settled {
		t.Fatalf("attempt stranded: %+v", p.Executions)
	}
	claims := fx.must("claims", orchReq(rootA, nil))
	for _, c := range claims.Claims {
		if c.AuthorKey != "" {
			t.Fatalf("prepared gate left behind: %+v", c)
		}
	}
	if _, err = db.Exec(`DROP TRIGGER fail_ready`); err != nil {
		t.Fatal(err)
	}
	r.Dispatch = "next-call"
	got, err := executionRequest(t, fx, r)
	if err != nil || got["attempt"] != "2" {
		t.Fatalf("orchestrator continuation: %v %v", got, err)
	}
	got, err = executionRequest(t, fx, r)
	if err != nil || got["launchable"] != false {
		t.Fatalf("duplicate effect allowed: %v %v", got, err)
	}
	p = gcProjection(t, fx.state)
	if p.InFlight() != 1 || p.Executions[r.Dispatch].Settled {
		t.Fatal("duplicate refusal cancelled existing admission")
	}
	// Failure and retry retain the author's staged content and ownership.
	fx.must("consolidate", orchReq(rootA, map[string]any{"author_key": a}))
	if p.Executions[r.Dispatch].Inputs == nil {
		t.Fatal("none declaration replaced with absence")
	}
}

func TestInterruptedPreparationResumesItsExactAttemptAndGates(t *testing.T) {
	fx := newVFSFixture(t)
	a := fx.delegate(rootA, "writer", "dev", "a.txt")
	fx.dispatch("finish", rootA, "writer", "dev")
	b := fx.delegate(rootA, "writer-b", "dev", "b.txt")
	fx.dispatch("finish", rootA, "writer-b", "dev")
	r := coordinationRequest{Action: "inputs", Session: rootA, Event: "review"}
	if _, err := executionRequest(t, fx, r); err != nil {
		t.Fatal(err)
	}
	r = coordinationRequest{Action: "admit", Session: rootA, Event: "review", Dispatch: "interrupted-call", Caller: rootA, Agent: "verify"}
	if _, err := executionRequest(t, fx, r); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss after the admission and core gate transaction, before
	// execution-ready was appended. No plugin cache or host liveness is invented.
	h, err := history.Open(fx.state)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := h.Project().Unit(rootA, "review")
	err = h.Append(history.Entry{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: rootA, WorkUnitID: "review", AttemptID: u.AttemptID, Dispatch: r.Dispatch, Agent: "verify", Caller: rootA, Cause: history.CauseNone})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.AttachChild(r.Dispatch, "too-early", "verify"); err == nil {
		t.Fatal("incomplete preparation acquired native child")
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	fs, err := vfs.Open(fx.root, fx.state)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := fs.AssignVerifier(vfs.Identity{SessionID: rootA, WorkUnitID: "review", AttemptID: u.AttemptID, AgentID: "verify", Specialist: "verify"}, vfs.AgentID(a))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fs.AssignVerifier(vfs.Identity{SessionID: rootA, WorkUnitID: "review", AttemptID: u.AttemptID, AgentID: "verify", Specialist: "verify"}, vfs.AgentID(b)); err != nil {
		t.Fatal(err)
	}
	if err = fs.Close(); err != nil {
		t.Fatal(err)
	}
	r.Action, r.Owner = "prepare", testOwner
	r.AuthorKeys = []vfs.AgentID{vfs.AgentID(a)}
	got, err := executionRequest(t, fx, r)
	if err != nil || got["attempt"] != u.AttemptID || got["launchable"] != true {
		t.Fatalf("interrupted retry: %v %v", got, err)
	}
	claims := fx.must("claims", orchReq(rootA, nil))
	count := 0
	for _, c := range claims.Claims {
		if c.AuthorKey != "" {
			count++
			if c.Key != gate {
				t.Fatalf("retry replaced reserved gate %s with %s", gate, c.Key)
			}
		}
	}
	if count != 1 {
		t.Fatalf("retry gate count=%d", count)
	}
}

func TestInterruptedPreparationUsesCurrentInputsAndExactIdentity(t *testing.T) {
	fx := newVFSFixture(t)
	r := coordinationRequest{Action: "admit", Session: rootA, Event: "brief", Dispatch: "interrupted-call", Caller: rootA, Agent: "pm"}
	if _, err := executionRequest(t, fx, r); err != nil {
		t.Fatal(err)
	}
	h, err := history.Open(fx.state)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := h.Project().Unit(rootA, r.Event)
	// The first input was valid at preparation time. Its validation is not the
	// authority for a replay after the orchestrator declares a different set.
	for _, entry := range []history.Entry{
		{Author: history.AuthorOrchestrator, Kind: history.KindInputs, SessionID: rootA, WorkUnitID: r.Event, ResultIDs: []int64{7}, Cause: history.CauseNone},
		{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: rootA, WorkUnitID: r.Event, AttemptID: u.AttemptID, Dispatch: r.Dispatch, Caller: rootA, Agent: "pm", Cause: history.CauseNone},
		{Author: history.AuthorOrchestrator, Kind: history.KindInputs, SessionID: rootA, WorkUnitID: r.Event, None: true, Cause: history.CauseNone},
	} {
		if err = h.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	r.Action, r.Owner = "prepare", testOwner
	wrong := r
	wrong.Agent = "analyst"
	if _, err = executionRequest(t, fx, wrong); err == nil || !strings.Contains(err.Error(), "new subagent call") {
		t.Fatalf("identity mismatch: %v", err)
	}
	if gcProjection(t, fx.state).Executions[r.Dispatch].Settled {
		t.Fatal("wrong caller cancelled preparation")
	}
	if _, err = executionRequest(t, fx, r); err == nil || !strings.Contains(err.Error(), "inputs changed") || !strings.Contains(err.Error(), "new subagent call") {
		t.Fatalf("stale input snapshot: %v", err)
	}
	if !gcProjection(t, fx.state).Executions[r.Dispatch].Settled {
		t.Fatal("stale preparation left admission stranded")
	}
	r.Dispatch = "next-call"
	got, err := executionRequest(t, fx, r)
	if err != nil || got["attempt"] != "2" {
		t.Fatalf("current-input continuation: %v %v", got, err)
	}
}

// TestPrepareRefusesResumingPMOnItsOwnInvariant covers PR-CRW-20: the pm
// session that recorded a consumed invariant cannot be resumed to formalize
// it, while a fresh pm delegation consuming the same invariant is admitted.
func TestPrepareRefusesResumingPMOnItsOwnInvariant(t *testing.T) {
	fakeEngramServer(t)
	home, _ := os.UserHomeDir()
	index := map[string]any{"session": rootA, "entries": []map[string]any{{"id": 7, "author": "pm", "caller": "pm-stories"}}}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".takt-ai", "memory", "sessions")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, rootA+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	fx := newVFSFixture(t)
	if _, err = executionRequest(t, fx, coordinationRequest{Action: "inputs", Session: rootA, Event: "business-docs", ResultIDs: []int64{7}}); err != nil {
		t.Fatal(err)
	}
	r := coordinationRequest{Action: "prepare", Owner: testOwner, Session: rootA, Event: "business-docs", Dispatch: "resume-call", Caller: rootA, Agent: "pm", Child: "pm-stories"}
	if _, err = executionRequest(t, fx, r); err == nil || !strings.Contains(err.Error(), "fresh pm unit") {
		t.Fatalf("resumed authoring pm: %v", err)
	}
	if p := gcProjection(t, fx.state); len(p.Executions) != 0 || p.InFlight() != 0 {
		t.Fatal("refused resumption admitted execution")
	}
	r.Dispatch, r.Child = "fresh-call", ""
	got, err := executionRequest(t, fx, r)
	if err != nil || got["launchable"] != true {
		t.Fatalf("fresh pm: %v %v", got, err)
	}
}
