package main_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
)

// finishAttempt finishes the delegation of unit under dispatch, naming the
// attempt the call settles.
func finishAttempt(s *session, unit, dispatch, attempt string) {
	s.t.Helper()
	body, err := json.Marshal(map[string]any{"action": "finish", "session": rootSession, "event": unit, "dispatch": dispatch, "attempt": attempt})
	if err != nil {
		s.t.Fatal(err)
	}
	if res := s.run("dispatch", "--workspace", s.root, "--state", s.state, "--request", string(body)); res.code != 0 {
		s.t.Fatalf("finish %s/%s attempt %s refused (exit %d): %s", unit, dispatch, attempt, res.code, res.stderr)
	}
}

func TestExecutionQueriesNeverPersistOrRebindCoordination(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	if err := saveCoordinator(t, s.root, s.state, &gc.Coordinator{Version: 1, Session: "bound-root", Cursor: 91, Units: 7, Mutations: 9, Since: 23}); err != nil {
		t.Fatal(err)
	}
	before := s.coordinator()
	for _, action := range []string{"resolve", "execution", "executions"} {
		s.must(request{Action: action, Session: "unrelated", Child: "unrelated", Dispatch: "missing"})
		if after := s.coordinator(); !reflect.DeepEqual(before, after) {
			t.Fatalf("query %s mutated coordinator: %+v -> %+v", action, before, after)
		}
	}
}

func TestAdmissionWithoutExecutionIsReconciledByExactAttempt(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	lost := call("admit", "unit")
	lost.Dispatch = "root:lost"
	s.must(lost)
	var listed struct {
		Executions map[string]history.Execution `json:"executions"`
	}
	s.must(request{Action: "executions", Session: rootSession}).decode(t, &listed)
	d, ok := listed.Executions["root:lost"]
	if !ok || d.Ready || d.Attempt != "1" || d.Caller != "root" {
		t.Fatalf("missing admission diagnostic: %+v", listed)
	}
	if len(s.projection().Executions) != 0 {
		t.Fatal("query manufactured durable execution authority")
	}
	finishAttempt(s, "unit", "root:lost", "1")
	s.must(retry("unit"))
	finishAttempt(s, "unit", "root:lost", "1")
	u := s.unit("unit")
	if u.State != history.StateInFlight || u.Dispatch != "retry" || u.AttemptID != "2" {
		t.Fatalf("late callback affected retry: %+v", u)
	}
}

func TestGCAdmissionWaitsForExactLaneTermination(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	cycle := func(phase string) gc.Coordinator {
		return gc.Coordinator{Version: 1, Session: "root", Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root", CycleID: "cycle"}}, Phase: phase}}
	}
	stopped := func(attempt string) []map[string]string {
		return []map[string]string{{"dispatch": "cycle/verifier", "attempt": attempt, "child": "lane"}}
	}
	openCycle(t, s, cycle("baseline"))
	if _, err := gcCall(s, map[string]any{"action": "attach", "owner": testOwnerInstance, "event": "cycle", "role": "verifier", "child": "lane"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gcCall(s, map[string]any{"action": "abort", "event": "cycle"}); err == nil {
		t.Fatal("abort released a running lane")
	}
	if d := s.projection().Executions["cycle/verifier"]; d.Settled || d.Terminated {
		t.Fatal("host failure was treated as termination")
	}
	if _, err := gcCall(s, map[string]any{"action": "abort", "event": "cycle", "stopped": stopped("old")}); err == nil {
		t.Fatal("stale proof accepted")
	}
	c, err := gcCall(s, map[string]any{"action": "abort", "event": "cycle", "stopped": stopped("1")})
	if err != nil || c.Cycle != nil {
		t.Fatalf("normal abort failed after terminal observation: %+v %v", c, err)
	}
	if d := s.projection().Executions["cycle/verifier"]; !d.Settled || !d.Terminated {
		t.Fatal("termination not persisted")
	}
	if _, err := gcCall(s, map[string]any{"action": "abort", "event": "cycle"}); err != nil {
		t.Fatal("repeat closure not idempotent", err)
	}
	// Reopen with the last close update missing but interruption already durable.
	openCycle(t, s, cycle("aborting"))
	if c, err := gcCall(s, map[string]any{"action": "abort", "event": "cycle"}); err != nil || c.Cycle != nil {
		t.Fatalf("restart could not complete recorded interruption: %+v %v", c, err)
	}
	if activity := s.projection().Activities["cycle"]; activity.Outcome != history.OutcomeInterrupted {
		t.Fatal("resumed closure changed the recorded outcome")
	}
}
