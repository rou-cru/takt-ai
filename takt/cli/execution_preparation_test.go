package main_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// TestExecutionPreparation runs the preparation scenarios against one build of the command, each in its own installation.
func TestExecutionPreparation(t *testing.T) {
	bin := buildTaktAI(t)
	for _, tc := range []struct {
		name string
		run  func(*testing.T, string)
	}{
		{"absent inputs spend no attempt", scenarioPrepareInputsAbsenceDoesNotSpendAttempt},
		{"an invalid second author leaves no gate behind", scenarioPrepareInvalidSecondAuthorDoesNotLeaveFirstGate},
		{"a readiness persistence failure is compensated", scenarioPrepareReadyPersistenceFailureCompensates},
		{"an interrupted preparation resumes its attempt and gates", scenarioInterruptedPreparationResumesItsExactAttemptAndGates},
		{"an interrupted preparation uses the current inputs and exact identity", scenarioInterruptedPreparationUsesCurrentInputsAndExactIdentity},
	} {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, bin) })
	}
}

func scenarioPrepareInputsAbsenceDoesNotSpendAttempt(t *testing.T, bin string) {
	fx := newVFSFixture(t, bin)
	r := request{Action: "prepare", Owner: testOwnerInstance, Session: rootA, Event: "brief", Dispatch: "call", Caller: rootA, Agent: "pm"}
	if _, err := fx.exec(r); err == nil || !strings.Contains(err.Error(), "dispatch_inputs") {
		t.Fatalf("missing inputs: %v", err)
	}
	p := fx.s.projection()
	if len(p.Executions) != 0 || p.InFlight() != 0 {
		t.Fatal("invalid input declaration admitted execution")
	}
	if _, err := fx.exec(request{Action: "inputs", Session: rootA, Event: "brief"}); err != nil {
		t.Fatal(err)
	}
	got, err := fx.exec(r)
	if err != nil || got["attempt"] != "1" || got["launchable"] != true {
		t.Fatalf("continuation: %v %v", got, err)
	}
	if inputs, ok := got["inputs"].([]any); !ok || len(inputs) != 0 {
		t.Fatalf("explicit none lost: %+v", got)
	}
}

func scenarioPrepareInvalidSecondAuthorDoesNotLeaveFirstGate(t *testing.T, bin string) {
	fx := newVFSFixture(t, bin)
	a := fx.delegate(rootA, "writer", "dev", "a.txt")
	fx.must("op", vfsReq("writer", "dev", map[string]any{"call_id": "write", "action": "create", "path": "a.txt", "content": "valid work"}))
	fx.dispatch("finish", rootA, "writer", "dev")
	_, err := fx.exec(request{Action: "inputs", Session: rootA, Event: "review"})
	if err != nil {
		t.Fatal(err)
	}
	r := request{Action: "prepare", Owner: testOwnerInstance, Session: rootA, Event: "review", Dispatch: "review-call", Caller: rootA, Agent: "verify", AuthorKeys: []vfs.AgentID{vfs.AgentID(a), "missing-author"}}
	if _, err = fx.exec(r); err == nil {
		t.Fatal("invalid second author admitted")
	}
	p := fx.s.projection()
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
	got, err := fx.exec(r)
	if err != nil || got["attempt"] != "1" {
		t.Fatalf("valid retry: %v %v", got, err)
	}
}

func scenarioPrepareReadyPersistenceFailureCompensates(t *testing.T, bin string) {
	fx := newVFSFixture(t, bin)
	a := fx.delegate(rootA, "writer", "dev", "a.txt")
	fx.must("op", vfsReq("writer", "dev", map[string]any{"call_id": "write", "action": "create", "path": "a.txt", "content": "valid work"}))
	fx.dispatch("finish", rootA, "writer", "dev")
	if _, err := fx.exec(request{Action: "inputs", Session: rootA, Event: "review"}); err != nil {
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
	r := request{Action: "prepare", Owner: testOwnerInstance, Session: rootA, Event: "review", Dispatch: "failed-call", Caller: rootA, Agent: "verify", AuthorKeys: []vfs.AgentID{vfs.AgentID(a)}}
	if _, err = fx.exec(r); err == nil || !strings.Contains(err.Error(), "new subagent call") {
		t.Fatalf("preparation failure continuation: %v", err)
	}
	p := fx.s.projection()
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
	got, err := fx.exec(r)
	if err != nil || got["attempt"] != "2" {
		t.Fatalf("orchestrator continuation: %v %v", got, err)
	}
	got, err = fx.exec(r)
	if err != nil || got["launchable"] != false {
		t.Fatalf("duplicate effect allowed: %v %v", got, err)
	}
	p = fx.s.projection()
	if p.InFlight() != 1 || p.Executions[r.Dispatch].Settled {
		t.Fatal("duplicate refusal cancelled existing admission")
	}
	// Failure and retry retain the author's staged content and ownership.
	fx.must("consolidate", orchReq(rootA, map[string]any{"author_key": a}))
	if p.Executions[r.Dispatch].Inputs == nil {
		t.Fatal("none declaration replaced with absence")
	}
}

func scenarioInterruptedPreparationResumesItsExactAttemptAndGates(t *testing.T, bin string) {
	fx := newVFSFixture(t, bin)
	a := fx.delegate(rootA, "writer", "dev", "a.txt")
	fx.dispatch("finish", rootA, "writer", "dev")
	b := fx.delegate(rootA, "writer-b", "dev", "b.txt")
	fx.dispatch("finish", rootA, "writer-b", "dev")
	r := request{Action: "inputs", Session: rootA, Event: "review"}
	if _, err := fx.exec(r); err != nil {
		t.Fatal(err)
	}
	r = request{Action: "admit", Session: rootA, Event: "review", Dispatch: "interrupted-call", Caller: rootA, Agent: "verify"}
	if _, err := fx.exec(r); err != nil {
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
	// The interrupted record names no model, as one from before models were
	// recorded; the retry names the model it runs and still resumes.
	r.Action, r.Owner, r.Model, r.Configured = "prepare", testOwnerInstance, "p/a", "p/a"
	r.AuthorKeys = []vfs.AgentID{vfs.AgentID(a)}
	got, err := fx.exec(r)
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

func scenarioInterruptedPreparationUsesCurrentInputsAndExactIdentity(t *testing.T, bin string) {
	fx := newVFSFixture(t, bin)
	r := request{Action: "admit", Session: rootA, Event: "brief", Dispatch: "interrupted-call", Caller: rootA, Agent: "pm"}
	if _, err := fx.exec(r); err != nil {
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
	r.Action, r.Owner = "prepare", testOwnerInstance
	wrong := r
	wrong.Agent = "analyst"
	if _, err = fx.exec(wrong); err == nil || !strings.Contains(err.Error(), "new subagent call") {
		t.Fatalf("identity mismatch: %v", err)
	}
	if fx.s.projection().Executions[r.Dispatch].Settled {
		t.Fatal("wrong caller cancelled preparation")
	}
	if _, err = fx.exec(r); err == nil || !strings.Contains(err.Error(), "inputs changed") || !strings.Contains(err.Error(), "new subagent call") {
		t.Fatalf("stale input snapshot: %v", err)
	}
	if !fx.s.projection().Executions[r.Dispatch].Settled {
		t.Fatal("stale preparation left admission stranded")
	}
	r.Dispatch = "next-call"
	got, err := fx.exec(r)
	if err != nil || got["attempt"] != "2" {
		t.Fatalf("current-input continuation: %v %v", got, err)
	}
}
