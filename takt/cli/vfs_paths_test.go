package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// newStore creates the empty VFS store of a session's workspace.
func newStore(t *testing.T, s *session) {
	t.Helper()
	fs, err := vfs.Open(s.root, s.state)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestRunVFSRoutesAndRejectsUsage verifies empty and unknown commands fail with the usage line.
func TestRunVFSRoutesAndRejectsUsage(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	requireRefusal(t, s.run("vfs"), "usage: takt-ai vfs")
	requireRefusal(t, s.run("vfs", "bogus"), `unknown vfs command "bogus"`)
}

// TestVFSOfflineValidation verifies offline flags are validated before any store is touched.
func TestVFSOfflineValidation(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	base := []string{"vfs", "journal", "--workspace", "w", "--state", "s"}
	with := func(extra ...string) []string { return append(append([]string{}, base...), extra...) }
	cases := map[string]struct {
		args []string
		want string
	}{
		"missing state":       {[]string{"vfs", "journal", "--workspace", "w"}, "--workspace and --state are required"},
		"positional argument": {with("extra"), "no positional arguments"},
		"zero limit":          {with("--limit", "0"), "--limit between 1 and"},
		"oversized limit":     {with("--limit", strconv.Itoa(vfs.MaxJournalPageSize+1)), "--limit between 1 and"},
		"cursor below -1":     {with("--after", "-2"), "--limit between 1 and"},
		"recover no restore":  {[]string{"vfs", "recover", "--workspace", "w", "--state", "s"}, "explicit --restore"},
		"journal restore":     {with("--restore"), "--restore applies only to recover"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { requireRefusal(t, s.run(tc.args...), tc.want) })
	}
}

// TestVFSOfflineRequiresExistingStore verifies a typo in --state never creates an empty store.
func TestVFSOfflineRequiresExistingStore(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	requireRefusal(t, s.run("vfs", "journal", "--workspace", s.root, "--state", s.state), "existing store required")
	if _, err := os.Stat(s.state); err == nil {
		t.Fatal("a refused offline command created the state directory")
	}
}

// TestVFSRecoverRestoreCompletes verifies explicit recovery on a clean store reports completion.
func TestVFSRecoverRestoreCompletes(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	newStore(t, s)
	res := s.run("vfs", "recover", "--workspace", s.root, "--state", s.state, "--restore")
	if res.code != 0 || strings.TrimSpace(res.stdout) != `{"recovery":"complete"}` {
		t.Fatalf("recover = %+v", res)
	}
}

// TestVFSOperationRequestValidation verifies flag and request errors are reported before any store is opened.
func TestVFSOperationRequestValidation(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	caller := `"ipc_version":` + strconv.Itoa(vfsIPCVersion) + `,"session_id":"s1","caller_session":"s1","caller_agent":"takt"`
	valid := "{" + caller + "}"
	cases := map[string]struct {
		args    []string
		payload string
		want    string
	}{
		"missing workspace": {[]string{"op", "--state", s.state}, valid, "--workspace and --state are required"},
		"positional":        {[]string{"op", "--workspace", s.root, "--state", s.state, "x"}, valid, "no positional arguments"},
		"unknown flag":      {[]string{"op", "--nope"}, valid, "flag provided but not defined"},
		"malformed json":    {[]string{"op", "--workspace", s.root, "--state", s.state}, "{", "invalid request"},
		"no caller block":   {[]string{"op", "--workspace", s.root, "--state", s.state}, `{"ipc_version":` + strconv.Itoa(vfsIPCVersion) + `,"session_id":"s1"}`, "caller_session and caller_agent are required"},
		"claim with cycle":  {[]string{"claims", "--workspace", s.root, "--state", s.state}, "{" + caller + `,"cycle_id":"c"}`, "cannot declare a maintenance cycle"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, s.runIn(tc.payload, append([]string{"vfs"}, tc.args...)...), tc.want)
		})
	}
}

// TestVFSMutationsRejectKeysTheCallerDoesNotHold verifies every command acts
// only through a binding the calling host session holds.
func TestVFSMutationsRejectKeysTheCallerDoesNotHold(t *testing.T) {
	fx := newVFSFixture(t, buildTaktAI(t))
	bound := fx.delegate(rootA, "u1", "dev", "a.go")
	const foreignKey = "not-a-binding-key"
	_, err := fx.mutate("op", vfsReq("u1", "dev", map[string]any{"call_id": "c1", "author_key": foreignKey, "action": "read", "path": "a.go"}))
	refusedWith(t, "op naming a foreign key", err, "is not held by this delegation")
	_, err = fx.mutate("op", vfsReq("u2", "dev", map[string]any{"call_id": "c2", "author_key": bound, "action": "create", "path": "a.go", "content": "x"}))
	refusedWith(t, "op from another unit naming the key", err, "is not held by this delegation")
	_, err = fx.mutate("shell-complete", vfsReq("u1", "dev", map[string]any{"call_id": "never-prepared"}))
	refusedWith(t, "shell-complete of an unprepared call", err, "run the command again")
	_, err = fx.mutate("consolidate", orchReq(rootA, map[string]any{"author_key": foreignKey}))
	refusedWith(t, "consolidate of a foreign key", err, "claim_list")
	_, err = fx.mutate("verify", vfsReq("u1", "dev", map[string]any{"call_id": "v1", "author_key": bound, "pass": true}))
	refusedWith(t, "verify without a gate", err, "is not one of your gates")
	// Discarding staged work is the orchestrator's discard command, never an op.
	for _, action := range []string{"truncate", "rollback"} {
		_, err = fx.mutate("op", vfsReq("u1", "dev", map[string]any{"call_id": "c3-" + action, "action": action, "path": "a.go"}))
		refusedWith(t, "an unsupported action", err, `unsupported action "`+action+`"`)
	}
}

// TestVFSClaimErrors verifies claim commands validate their input.
func TestVFSClaimErrors(t *testing.T) {
	fx := newVFSFixture(t, buildTaktAI(t))
	_, err := fx.mutate("release", orchReq(rootA, map[string]any{"key": "unknown"}))
	refusedWith(t, "release of an unknown key", err, "claim_list")
}

// TestVFSOfflineServesTheExecutionHistory verifies the history source pages the recorded order of an existing store.
func TestVFSOfflineServesTheExecutionHistory(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	newStore(t, s)
	h, err := history.Open(s.state)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"root-a", "root-b"} {
		if err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched, SessionID: session, WorkUnitID: "child", AttemptID: history.FirstAttempt, Cause: history.CauseNone, Agent: "architect"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	res := s.run("vfs", "history", "--workspace", s.root, "--state", s.state, "--session", "root-b")
	var page []history.Entry
	if err := json.Unmarshal([]byte(res.stdout), &page); err != nil || res.code != 0 || len(page) != 1 || page[0].SessionID != "root-b" {
		t.Fatalf("history page = %+v, %+v, %v; want the one entry of root-b", res, page, err)
	}
}

// TestVFSOfflineRefusesAnUnreadableRecord verifies a malformed record is an error, not an empty page.
func TestVFSOfflineRefusesAnUnreadableRecord(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	newStore(t, s)
	foreign := []byte(strings.Repeat("not a sqlite database, only text; ", 100))
	for source, file := range map[string]string{"journal": "vfs.sqlite", "history": "history.sqlite"} {
		if err := os.WriteFile(filepath.Join(s.state, file), foreign, 0o600); err != nil {
			t.Fatal(err)
		}
		if res := s.run("vfs", source, "--workspace", s.root, "--state", s.state); res.code == 0 {
			t.Fatalf("%s: succeeded on a malformed record", source)
		}
	}
}
