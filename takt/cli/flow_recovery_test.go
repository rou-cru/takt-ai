package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
)

func TestExecutionQueriesNeverPersistOrRebindCoordination(t *testing.T) {
	root, state, _ := dispatchHarness(t)
	original := &gc.Coordinator{Version: 1, Session: "bound-root", Cursor: 91, Units: 7, Mutations: 9, Since: 23}
	saveCoordinator(t, root, state, original)
	before, err := loadCoordinator(t, root, state)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"resolve", "execution", "executions"} {
		raw, _ := json.Marshal(coordinationRequest{Action: action, Session: "unrelated", Child: "unrelated", Dispatch: "missing"})
		var out, stderr bytes.Buffer
		if err := runDispatch([]string{"--workspace", root, "--state", state, "--request", string(raw)}, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		after, err := loadCoordinator(t, root, state)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("query %s mutated coordinator: %+v -> %+v %v", action, before, after, err)
		}
	}
}

func TestAdmissionWithoutExecutionIsReconciledByExactAttempt(t *testing.T) {
	_, state, call := dispatchHarness(t)
	if _, err := call(coordinationRequest{Action: "admit", Session: "root", Event: "unit", Agent: "dev", Dispatch: "root:lost"}); err != nil {
		t.Fatal(err)
	}
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	result := listExecutions(h, time.Now())
	d, ok := result["executions"].(map[string]history.Execution)["root:lost"]
	if !ok || d.Ready || d.Attempt != "1" || d.Caller != "root" {
		t.Fatalf("missing admission diagnostic: %+v", result)
	}
	if len(h.Project().Executions) != 0 {
		t.Fatal("query manufactured durable execution authority")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := call(coordinationRequest{Action: "finish", Session: "root", Event: "unit", Dispatch: "root:lost", Attempt: "1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(coordinationRequest{Action: "admit", Session: "root", Event: "unit", Agent: "dev", Dispatch: "root:retry"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(coordinationRequest{Action: "finish", Session: "root", Event: "unit", Dispatch: "root:lost", Attempt: "1"}); err != nil {
		t.Fatal(err)
	}
	u, _ := gcProjection(t, state).Unit("root", "unit")
	if u.State != history.StateInFlight || u.Dispatch != "root:retry" || u.AttemptID != "2" {
		t.Fatalf("late callback affected retry: %+v", u)
	}
}

func TestGCAdmissionWaitsForExactLaneTermination(t *testing.T) {
	root, state, call := gcHarness(t)
	saveCoordinator(t, root, state, &gc.Coordinator{Version: 1, Session: "root", Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root", CycleID: "cycle"}}, Phase: "baseline"}})
	if _, err := call(coordinationRequest{Action: "attach", Owner: testOwner, Event: "cycle", Role: "verifier", Child: "lane"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(coordinationRequest{Action: "abort", Event: "cycle"}); err == nil {
		t.Fatal("abort released a running lane")
	}
	if d := gcProjection(t, state).Executions["cycle/verifier"]; d.Settled || d.Terminated {
		t.Fatal("host failure was treated as termination")
	}
	if _, err := call(coordinationRequest{Action: "abort", Event: "cycle", Stopped: []laneStop{{Dispatch: "cycle/verifier", Attempt: "old", Child: "lane"}}}); err == nil {
		t.Fatal("stale proof accepted")
	}
	c, err := call(coordinationRequest{Action: "abort", Event: "cycle", Stopped: []laneStop{{Dispatch: "cycle/verifier", Attempt: "1", Child: "lane"}}})
	if err != nil || c.Cycle != nil {
		t.Fatalf("normal abort failed after terminal observation: %+v %v", c, err)
	}
	if d := gcProjection(t, state).Executions["cycle/verifier"]; !d.Settled || !d.Terminated {
		t.Fatal("termination not persisted")
	}
	if _, err := call(coordinationRequest{Action: "abort", Event: "cycle"}); err != nil {
		t.Fatal("repeat closure not idempotent", err)
	}
	// Reopen with the last close update missing but interruption already durable.
	saveCoordinator(t, root, state, &gc.Coordinator{Version: 1, Session: "root", Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root", CycleID: "cycle"}}, Phase: "aborting"}})
	if c, err := call(coordinationRequest{Action: "abort", Event: "cycle"}); err != nil || c.Cycle != nil {
		t.Fatalf("restart could not complete recorded interruption: %+v %v", c, err)
	}
	if activity := gcProjection(t, state).Activities["cycle"]; activity.Outcome != history.OutcomeInterrupted {
		t.Fatal("resumed closure changed the recorded outcome")
	}
}
