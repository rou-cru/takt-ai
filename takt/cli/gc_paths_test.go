package main_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// requireRefusal requires a failed invocation whose stderr names want.
func requireRefusal(t *testing.T, res result, want string) {
	t.Helper()
	if res.code == 0 || !strings.Contains(res.stderr, want) {
		t.Fatalf("exit %d, stderr %q; want a refusal naming %q", res.code, res.stderr, want)
	}
}

// TestGCRejectsBadInvocations verifies verb, flag, and store errors surface before any work happens.
func TestGCRejectsBadInvocations(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	state := filepath.Join(t.TempDir(), "typo")
	cases := map[string]struct {
		args []string
		want string
	}{
		"no verb":         {nil, "usage: takt-ai gc"},
		"unknown verb":    {[]string{"bogus"}, "usage: takt-ai gc"},
		"missing state":   {[]string{"plan", "--workspace", s.root}, "--workspace and --state are required"},
		"positional":      {[]string{"plan", "--workspace", s.root, "--state", state, "x"}, "no positional arguments"},
		"unknown flag":    {[]string{"plan", "--nope"}, "flag provided but not defined"},
		"missing store":   {[]string{"plan", "--workspace", s.root, "--state", state}, "existing store required"},
		"findings no dir": {[]string{"findings", "--workspace", s.root, "--state", state}, "existing store required"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, s.run(append([]string{"gc"}, tc.args...)...), tc.want)
		})
	}
}

// TestGCAcceptanceValidatesBeforeClosing verifies acceptance needs a cycle and a known result.
func TestGCAcceptanceValidatesBeforeClosing(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	fs, err := vfs.Open(s.root, s.state)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	base := []string{"gc", "acceptance", "--workspace", s.root, "--state", s.state}
	requireRefusal(t, s.run(base...), "--cycle is required")
	if res := s.run(append(base, "--cycle", "c1", "--result", "maybe")...); res.code == 0 {
		t.Fatal("acceptance with unknown result succeeded")
	}
}

// TestGCCoordinateRejectsBadInvocations verifies flag, JSON, and action errors precede any state change.
func TestGCCoordinateRejectsBadInvocations(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	cases := map[string]struct {
		args []string
		want string
	}{
		"missing request flags": {[]string{"--workspace", s.root}, "--workspace --state --request required"},
		"unknown flag":          {[]string{"--nope"}, "flag provided but not defined"},
		"malformed json":        {[]string{"--workspace", s.root, "--state", s.state, "--request", "{"}, "unexpected end of JSON"},
		"ordinary action":       {[]string{"--workspace", s.root, "--state", s.state, "--request", `{"action":"admit"}`}, "not a maintenance-cycle action"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, s.run(append([]string{"gc", "coordinate"}, tc.args...)...), tc.want)
		})
	}
}

// coordinate sends a maintenance-cycle request given as raw fields; a refusal is the exit status or an ok:false answer.
func coordinate(s *session, fields map[string]any) (string, error) {
	s.t.Helper()
	body, err := json.Marshal(fields)
	if err != nil {
		s.t.Fatal(err)
	}
	res := s.run("gc", "coordinate", "--workspace", s.root, "--state", s.state, "--request", string(body))
	if res.code != 0 {
		return res.stdout, refusal(res.stderr)
	}
	var answer struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(res.stdout), &answer) == nil && answer.OK != nil && !*answer.OK {
		return res.stdout, refusal(answer.Error)
	}
	return res.stdout, nil
}

// refusal is a command's refusal text as an error.
type refusal string

func (r refusal) Error() string { return string(r) }

// TestGCCoordinateWithoutCycle verifies every phase action refuses a caller when no cycle is active.
func TestGCCoordinateWithoutCycle(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	const notParticipant = "not the persisted cycle participant"
	for _, action := range []string{"baseline", "findings", "investigate", "authorize", "collected", "delta", "verdict", "acceptance", "no-change"} {
		t.Run(action, func(t *testing.T) {
			_, err := coordinate(s, map[string]any{"action": action, "session": "child"})
			if err == nil || !strings.Contains(err.Error(), notParticipant) {
				t.Fatalf("%s error = %v, want %q", action, err, notParticipant)
			}
		})
	}
	t.Run("status and abort are no-ops", func(t *testing.T) {
		for _, action := range []string{"status", "abort", "recover"} {
			if _, err := coordinate(s, map[string]any{"action": action, "session": "root"}); err != nil || s.coordinator().Cycle != nil {
				t.Fatalf("%s = %v, cycle %+v; want idle coordinator", action, err, s.coordinator().Cycle)
			}
		}
	})
	t.Run("attach needs a cycle", func(t *testing.T) {
		_, err := coordinate(s, map[string]any{"action": "attach", "owner": testOwnerInstance, "session": "root", "role": "collector", "child": "c1"})
		if err == nil || !strings.Contains(err.Error(), "no active cycle") {
			t.Fatalf("attach error = %v", err)
		}
	})
	t.Run("tick needs a root session", func(t *testing.T) {
		if _, err := coordinate(s, map[string]any{"action": "tick"}); err == nil || !strings.Contains(err.Error(), "root session required") {
			t.Fatalf("tick error = %v", err)
		}
	})
	t.Run("prepare without project config fails", func(t *testing.T) {
		if _, err := coordinate(s, map[string]any{"action": "prepare", "owner": testOwnerInstance, "session": "root"}); err == nil {
			t.Fatal("prepare without .takt/gc.json succeeded")
		}
	})
}

// TestDispatchRejectsBadInvocations verifies flag, JSON, and action errors on the ordinary dispatch surface.
func TestDispatchRejectsBadInvocations(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	cases := map[string]struct {
		args []string
		want string
	}{
		"missing flags":  {[]string{"--workspace", s.root}, "--workspace --state --request required"},
		"unknown flag":   {[]string{"--nope"}, "flag provided but not defined"},
		"malformed json": {[]string{"--workspace", s.root, "--state", s.state, "--request", "{"}, "unexpected end of JSON"},
		"unknown action": {[]string{"--workspace", s.root, "--state", s.state, "--request", `{"action":"bogus"}`}, "maintenance-cycle action"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, s.run(append([]string{"dispatch"}, tc.args...)...), tc.want)
		})
	}
}

// TestDispatchActivityActionsOnlyAcceptOrchestratorNodes verifies the harness-owned node kind cannot be claimed by a caller.
func TestDispatchActivityActionsOnlyAcceptOrchestratorNodes(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	for _, action := range []string{"activity_start", "activity_finish"} {
		res := s.dispatch(request{Action: action, Session: rootSession, ActivityID: "a1", NodeKind: history.NodeKindMaintenance})
		requireRefusal(t, res, "only accepts node_kind orchestrator")
	}
}
