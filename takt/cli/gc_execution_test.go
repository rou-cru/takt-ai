package main_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
)

// openCycle persists a coordinator holding one cycle at baseline, the state a
// late call from an earlier cycle meets.
func openCycle(t *testing.T, s *session, c gc.Coordinator) {
	t.Helper()
	if err := saveCoordinator(t, s.root, s.state, &c); err != nil {
		t.Fatal(err)
	}
}

// attachOutcome is how one concurrent attach process ended.
type attachOutcome struct {
	runErr  error
	code    int
	refused bool
}

// runAttach attaches child as a verifier of the "current" cycle. It never
// fails the test, so it can run on any goroutine.
func runAttach(s *session, child string) attachOutcome {
	body, err := json.Marshal(map[string]any{"action": "attach", "owner": testOwnerInstance, "event": "current", "role": "verifier", "child": child})
	if err != nil {
		return attachOutcome{runErr: err}
	}
	cmd := exec.Command(s.bin, "gc", "coordinate", "--workspace", s.root, "--state", s.state, "--request", string(body))
	cmd.Env = append([]string{"HOME=" + s.home, "TMPDIR=" + s.home, "PATH=" + os.Getenv("PATH")}, s.env...)
	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+dir)
	}
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return attachOutcome{runErr: err}
	}
	var answer struct {
		OK *bool `json:"ok"`
	}
	refused := json.Unmarshal(out, &answer) == nil && answer.OK != nil && !*answer.OK
	return attachOutcome{code: cmd.ProcessState.ExitCode(), refused: refused}
}

func TestGCLateTransportCannotActOnNewCycle(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	openCycle(t, s, gc.Coordinator{Version: 1, Session: "root", Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root", CycleID: "current"}}, Phase: "baseline"}})
	if _, err := gcCall(s, map[string]any{"action": "attach", "owner": testOwnerInstance, "event": "previous", "role": "verifier", "child": "late-child"}); err == nil {
		t.Fatal("late child attached to new cycle")
	}
	if _, err := gcCall(s, map[string]any{"action": "prompt", "event": "previous", "phase": "baseline", "child": "late-child"}); err != nil {
		t.Fatal(err)
	}
	c, err := gcCall(s, map[string]any{"action": "abort", "event": "previous", "evidence": "late transport failure"})
	if err != nil || c.Cycle == nil || c.Cycle.Plan.CycleID != "current" {
		t.Fatalf("late abort: %+v %v", c, err)
	}
	if len(s.projection().Executions) != 0 {
		t.Fatal("late attach recorded a lane")
	}
}

func TestConcurrentGCAttachHasOneHistoryOwner(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	openCycle(t, s, gc.Coordinator{Version: 1, Session: "root", Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root", CycleID: "current"}}, Phase: "baseline"}})
	// The goroutines only run the processes; every assertion stays on the test goroutine.
	children := []string{"one", "two"}
	outcomes := make([]attachOutcome, len(children))
	var wg sync.WaitGroup
	for i, child := range children {
		wg.Go(func() { outcomes[i] = runAttach(s, child) })
	}
	wg.Wait()
	success := 0
	for i, o := range outcomes {
		if o.runErr != nil {
			t.Fatalf("attach %s: %v", children[i], o.runErr)
		}
		if o.code == 0 && !o.refused {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful attachments=%d", success)
	}
	p := s.projection()
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
	s := newSession(t, buildTaktAI(t))
	openCycle(t, s, gc.Coordinator{Version: 1, Session: "root", Cursor: 100, Cycle: &gc.Cycle{Plan: gc.Plan{Request: gc.Request{SessionID: "root", CycleID: "current"}}, Phase: "baseline"}})
	c, err := gcCall(s, map[string]any{"action": "status", "session": "lane-child"})
	if err != nil || c.Session != "root" || c.Cursor != 100 {
		t.Fatalf("lane call rebound pace: session %q cursor %d err %v", c.Session, c.Cursor, err)
	}
}
