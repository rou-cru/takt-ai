package main

import (
	"sync"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
)

func TestGCLateTransportCannotActOnNewCycle(t *testing.T) {
	root, state, call := gcHarness(t)
	saveCoordinator(t, root, state, &gc.Coordinator{Version: 1, Session: "root", Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root", CycleID: "current"}}, Phase: "baseline"}})
	if _, err := call(coordinationRequest{Action: "attach", Owner: testOwner, Event: "previous", Role: "verifier", Child: "late-child"}); err == nil {
		t.Fatal("late child attached to new cycle")
	}
	if _, err := call(coordinationRequest{Action: "prompt", Event: "previous", Phase: "baseline", Child: "late-child"}); err != nil {
		t.Fatal(err)
	}
	c, err := call(coordinationRequest{Action: "abort", Event: "previous", Evidence: "late transport failure"})
	if err != nil || c.Cycle == nil || c.Cycle.Plan.CycleID != "current" {
		t.Fatalf("late abort: %+v %v", c, err)
	}
	if len(gcProjection(t, state).Executions) != 0 {
		t.Fatal("late attach recorded a lane")
	}
}

func TestConcurrentGCAttachHasOneHistoryOwner(t *testing.T) {
	root, state, call := gcHarness(t)
	saveCoordinator(t, root, state, &gc.Coordinator{Version: 1, Session: "root", Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root", CycleID: "current"}}, Phase: "baseline"}})
	var wg sync.WaitGroup
	result := make(chan error, 2)
	for _, child := range []string{"one", "two"} {
		wg.Go(func() {
			_, err := call(coordinationRequest{Action: "attach", Owner: testOwner, Event: "current", Role: "verifier", Child: child})
			result <- err
		})
	}
	wg.Wait()
	close(result)
	success := 0
	for err := range result {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful attachments=%d", success)
	}
	p := gcProjection(t, state)
	if len(p.Executions) != 1 {
		t.Fatalf("lane facts=%+v", p.Executions)
	}
	for _, d := range p.Executions {
		if d.Attempt != "1" || d.Role != "verifier" {
			t.Fatalf("lane not issued by history: %+v", d)
		}
	}
}

func TestGCLaneCallKeepsCycleRootPace(t *testing.T) {
	root, state, call := gcHarness(t)
	saveCoordinator(t, root, state, &gc.Coordinator{Version: 1, Session: "root", Cursor: 100, Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root", CycleID: "current"}}, Phase: "baseline"}})
	c, err := call(coordinationRequest{Action: "status", Session: "lane-child"})
	if err != nil || c.Session != "root" || c.Cursor != 100 {
		t.Fatalf("lane call rebound pace: session %q cursor %d err %v", c.Session, c.Cursor, err)
	}
}
