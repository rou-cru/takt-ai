package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// TestGCRejectsBadInvocations verifies verb, flag, and store errors surface before any work happens.
func TestGCRejectsBadInvocations(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "typo")
	cases := map[string]struct {
		args []string
		want string
	}{
		"no verb":         {nil, "usage: takt-ai gc"},
		"unknown verb":    {[]string{"bogus"}, "usage: takt-ai gc"},
		"missing state":   {[]string{"plan", "--workspace", root}, "--workspace and --state are required"},
		"positional":      {[]string{"plan", "--workspace", root, "--state", state, "x"}, "no positional arguments"},
		"unknown flag":    {[]string{"plan", "--nope"}, "flag provided but not defined"},
		"missing store":   {[]string{"plan", "--workspace", root, "--state", state}, "existing store required"},
		"findings no dir": {[]string{"findings", "--workspace", root, "--state", state}, "existing store required"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := runGC(tc.args, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runGC(%v) error = %v, want %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestGCAcceptanceValidatesBeforeClosing verifies acceptance needs a cycle and a known result.
func TestGCAcceptanceValidatesBeforeClosing(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	base := []string{"acceptance", "--workspace", root, "--state", state}
	err = runGC(base, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--cycle is required") {
		t.Fatalf("acceptance without cycle error = %v", err)
	}
	err = runGC(append(append([]string{}, base...), "--cycle", "c1", "--result", "maybe"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("acceptance with unknown result error = nil")
	}
}

// TestCycleFactsReadsFirstDeclaredValues verifies mandate and session come from the cycle's own entries only.
func TestCycleFactsReadsFirstDeclaredValues(t *testing.T) {
	entries := []vfs.JournalEntry{
		{CycleID: "other", MandateClass: "duplication", SessionID: "s-other"},
		{CycleID: "c1"},
		{CycleID: "c1", MandateClass: string(gc.MandateDeadCode), SessionID: "s1"},
		{CycleID: "c1", MandateClass: "complexity", SessionID: "s2"},
	}
	mandate, session := cycleFacts(entries, "c1")
	if mandate != gc.MandateDeadCode || session != "s1" {
		t.Fatalf("cycleFacts() = %q, %q; want dead-code, s1", mandate, session)
	}
	if mandate, session = cycleFacts(entries, "absent"); mandate != "" || session != "" {
		t.Fatalf("cycleFacts(absent) = %q, %q; want empty", mandate, session)
	}
}

// TestGCCoordinateRejectsBadInvocations verifies flag, JSON, and action errors precede any state change.
func TestGCCoordinateRejectsBadInvocations(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "state")
	cases := map[string]struct {
		args []string
		want string
	}{
		"missing request flags": {[]string{"coordinate", "--workspace", root}, "--workspace --state --request required"},
		"unknown flag":          {[]string{"coordinate", "--nope"}, "flag provided but not defined"},
		"malformed json":        {[]string{"coordinate", "--workspace", root, "--state", state, "--request", "{"}, "unexpected end of JSON"},
		"ordinary action":       {[]string{"coordinate", "--workspace", root, "--state", state, "--request", `{"action":"admit"}`}, "not a maintenance-cycle action"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := runGC(tc.args, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runGC(%v) error = %v, want %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestGCCoordinateWithoutCycle verifies every phase action refuses a caller when no cycle is active.
func TestGCCoordinateWithoutCycle(t *testing.T) {
	_, _, call := gcHarness(t)
	const notParticipant = "not the persisted cycle participant"
	for _, action := range []string{"baseline", "findings", "investigate", "authorize", "collected", "delta", "verdict", "acceptance", actionNoChange} {
		t.Run(action, func(t *testing.T) {
			_, err := call(coordinationRequest{Action: action, Session: "child"})
			if err == nil || !strings.Contains(err.Error(), notParticipant) {
				t.Fatalf("%s error = %v, want %q", action, err, notParticipant)
			}
		})
	}
	t.Run("status and abort are no-ops", func(t *testing.T) {
		for _, action := range []string{"status", "abort", "recover"} {
			c, err := call(coordinationRequest{Action: action, Session: "root"})
			if err != nil || c.Cycle != nil {
				t.Fatalf("%s = %+v, %v; want idle coordinator", action, c, err)
			}
		}
	})
	t.Run("attach needs a cycle", func(t *testing.T) {
		if _, err := call(coordinationRequest{Action: "attach", Session: "root", Role: "collector", Child: "c1"}); err == nil || !strings.Contains(err.Error(), "no cycle") {
			t.Fatalf("attach error = %v", err)
		}
	})
	t.Run("tick needs a root session", func(t *testing.T) {
		if _, err := call(coordinationRequest{Action: "tick"}); err == nil || !strings.Contains(err.Error(), "root session required") {
			t.Fatalf("tick error = %v", err)
		}
	})
	t.Run("prepare without project config fails", func(t *testing.T) {
		if _, err := call(coordinationRequest{Action: "prepare", Session: "root"}); err == nil {
			t.Fatal("prepare without .takt/gc.json succeeded")
		}
	})
}

// TestDispatchRejectsBadInvocations verifies flag, JSON, and action errors on the ordinary dispatch surface.
func TestDispatchRejectsBadInvocations(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "state")
	cases := map[string]struct {
		args []string
		want string
	}{
		"missing flags":  {[]string{"--workspace", root}, "--workspace --state --request required"},
		"unknown flag":   {[]string{"--nope"}, "flag provided but not defined"},
		"malformed json": {[]string{"--workspace", root, "--state", state, "--request", "{"}, "unexpected end of JSON"},
		"unknown action": {[]string{"--workspace", root, "--state", state, "--request", `{"action":"bogus"}`}, "maintenance-cycle action"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := runDispatch(tc.args, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runDispatch(%v) error = %v, want %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestDispatchActivityActionsOnlyAcceptOrchestratorNodes verifies the harness-owned node kind cannot be claimed by a caller.
func TestDispatchActivityActionsOnlyAcceptOrchestratorNodes(t *testing.T) {
	_, _, call := dispatchHarness(t)
	for _, action := range []string{"activity_start", "activity_finish"} {
		_, err := call(coordinationRequest{Action: action, Session: "root", ActivityID: "a1", NodeKind: history.NodeKindMaintenance})
		if err == nil || !strings.Contains(err.Error(), "only accepts node_kind orchestrator") {
			t.Errorf("%s error = %v", action, err)
		}
	}
}

// TestJournalRefAndArtifactCheckWhenNothingDeclared verifies empty inputs yield an empty reference and a verified artifact.
func TestJournalRefAndArtifactCheckWhenNothingDeclared(t *testing.T) {
	if got := journalRef(nil); got != "" {
		t.Fatalf("journalRef(nil) = %q, want empty", got)
	}
	state := filepath.Join(t.TempDir(), "state")
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	if !checkArtifact(h, t.TempDir(), "root") {
		t.Fatal("checkArtifact() = false with no declared artifact")
	}
}
