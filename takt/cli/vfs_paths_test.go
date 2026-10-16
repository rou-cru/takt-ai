package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// TestRunVFSRoutesAndRejectsUsage verifies empty and unknown commands fail with the usage line.
func TestRunVFSRoutesAndRejectsUsage(t *testing.T) {
	var out, stderr bytes.Buffer
	if err := runVFS(nil, strings.NewReader(""), &out, &stderr); err == nil || err.Error() != usageVFS {
		t.Fatalf("runVFS(nil) error = %v, want usage", err)
	}
	err := runVFS([]string{"bogus"}, strings.NewReader(""), &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), `unknown vfs command "bogus"`) {
		t.Fatalf("runVFS(bogus) error = %v", err)
	}
}

// TestValidVFSCommand verifies every wire command is accepted and anything else is not.
func TestValidVFSCommand(t *testing.T) {
	for _, c := range []string{"claims", "assign", "release", "bind", "adopt-gates", "op", "verify", "consolidate", "discard", "shell-prepare", "shell-complete", "shell-guard"} {
		if !validVFSCommand(c) {
			t.Errorf("validVFSCommand(%q) = false", c)
		}
	}
	for _, c := range []string{"", "journal", "recover", "resolve", "OP", "assign-verifier"} {
		if validVFSCommand(c) {
			t.Errorf("validVFSCommand(%q) = true", c)
		}
	}
}

// TestParseAction verifies each wire action maps to its VFS operation and unknown ones are refused.
func TestParseAction(t *testing.T) {
	for action, want := range map[string]vfs.OperationType{
		"read": vfs.OpRead, "create": vfs.OpCreate, "patch": vfs.OpPatch, "delete": vfs.OpDelete,
	} {
		if got, err := parseAction(action); err != nil || got != want {
			t.Errorf("parseAction(%q) = %v, %v; want %v", action, got, err, want)
		}
	}
	// Discarding staged work is the orchestrator's discard command, never an op.
	for _, action := range []string{"truncate", "rollback"} {
		if _, err := parseAction(action); err == nil || !strings.Contains(err.Error(), `unsupported action "`+action+`"`) {
			t.Fatalf("parseAction(%s) error = %v", action, err)
		}
	}
}

// TestParseVFSOfflineValidation verifies offline flags are validated before any store is touched.
func TestParseVFSOfflineValidation(t *testing.T) {
	const tooManyEntries = vfs.MaxJournalPageSize + 1
	base := []string{"journal", "--workspace", "w", "--state", "s"}
	cases := map[string]struct {
		args []string
		want string
	}{
		"missing state":       {[]string{"journal", "--workspace", "w"}, "--workspace and --state are required"},
		"positional argument": {append(append([]string{}, base...), "extra"), "no positional arguments"},
		"zero limit":          {append(append([]string{}, base...), "--limit", "0"), "--limit between 1 and"},
		"oversized limit":     {append(append([]string{}, base...), "--limit", strconv.Itoa(tooManyEntries)), "--limit between 1 and"},
		"cursor below -1":     {append(append([]string{}, base...), "--after", "-2"), "--limit between 1 and"},
		"recover no restore":  {[]string{"recover", "--workspace", "w", "--state", "s"}, "explicit --restore"},
		"journal restore":     {append(append([]string{}, base...), "--restore"), "--restore applies only to recover"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseVFSOffline(tc.args, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseVFSOffline(%v) error = %v, want %q", tc.args, err, tc.want)
			}
		})
	}
	got, err := parseVFSOffline([]string{"recover", "--workspace", "w", "--state", "s", "--restore"}, &bytes.Buffer{})
	if err != nil || !got.restore || got.limit != defaultVFSPageSize || got.after != -1 {
		t.Fatalf("parseVFSOffline(recover) = %+v, %v", got, err)
	}
}

// TestVFSOfflineRequiresExistingStore verifies a typo in --state never creates an empty store.
func TestVFSOfflineRequiresExistingStore(t *testing.T) {
	state := filepath.Join(t.TempDir(), "typo")
	err := runVFS([]string{"journal", "--workspace", t.TempDir(), "--state", state}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "existing store required") {
		t.Fatalf("journal error = %v", err)
	}
}

// TestVFSRecoverRestoreCompletes verifies explicit recovery on a clean store reports completion.
func TestVFSRecoverRestoreCompletes(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runVFS([]string{"recover", "--workspace", root, "--state", state, "--restore"}, strings.NewReader(""), &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != `{"recovery":"complete"}` {
		t.Fatalf("recover output = %q", out.String())
	}
}

// TestVFSOperationRequestValidation verifies flag and request errors are reported before any store is opened.
func TestVFSOperationRequestValidation(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	caller := `"ipc_version":` + strconv.Itoa(IPCVersion) + `,"session_id":"s1","caller_session":"s1","caller_agent":"takt"`
	valid := "{" + caller + "}"
	cases := map[string]struct {
		args    []string
		payload string
		want    string
	}{
		"missing workspace": {[]string{"op", "--state", state}, valid, "--workspace and --state are required"},
		"positional":        {[]string{"op", "--workspace", root, "--state", state, "x"}, valid, "no positional arguments"},
		"unknown flag":      {[]string{"op", "--nope"}, valid, "flag provided but not defined"},
		"malformed json":    {[]string{"op", "--workspace", root, "--state", state}, "{", "invalid request"},
		"no caller block":   {[]string{"op", "--workspace", root, "--state", state}, `{"ipc_version":` + strconv.Itoa(IPCVersion) + `,"session_id":"s1"}`, "caller_session and caller_agent are required"},
		"claim with cycle":  {[]string{"claims", "--workspace", root, "--state", state}, "{" + caller + `,"cycle_id":"c"}`, "cannot declare a maintenance cycle"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := runVFS(tc.args, strings.NewReader(tc.payload), &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runVFS(%v) error = %v, want %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestVFSMutationsRejectKeysTheCallerDoesNotHold verifies every command acts
// only through a binding the calling host session holds.
func TestVFSMutationsRejectKeysTheCallerDoesNotHold(t *testing.T) {
	fx := newVFSFixture(t)
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
	_, err = fx.mutate("op", vfsReq("u1", "dev", map[string]any{"call_id": "c3", "action": "truncate", "path": "a.go"}))
	refusedWith(t, "an unsupported action", err, `unsupported action "truncate"`)
}

// TestVFSClaimErrors verifies claim commands validate their input.
func TestVFSClaimErrors(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	if _, err := mutate("release", orchReq(rootA, map[string]any{"key": "unknown"})); err == nil || !strings.Contains(err.Error(), "claim_list") {
		t.Fatalf("release unknown error = %v", err)
	}
}

// TestRunVFSMutateRejectsUnknownCommand verifies the mutation switch refuses commands it does not serve.
func TestRunVFSMutateRejectsUnknownCommand(t *testing.T) {
	fs, err := vfs.Open(t.TempDir(), filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fs.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := runVFSMutate(fs, "journal", "", request{}, vfs.Caller{}); err == nil || !strings.Contains(err.Error(), `unsupported command "journal"`) {
		t.Fatalf("runVFSMutate() error = %v", err)
	}
}

// TestVFSOfflineServesTheExecutionHistory verifies the history source pages the recorded order of an existing store.
func TestVFSOfflineServesTheExecutionHistory(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"root-a", "root-b"} {
		if err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched, SessionID: session, WorkUnitID: "child", AttemptID: history.FirstAttempt, Cause: history.CauseNone, Agent: "architect", Artifact: "artifacts/report.md"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runVFS([]string{"history", "--workspace", root, "--state", state, "--session", "root-b"}, strings.NewReader(""), &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var page []history.Entry
	if err := json.Unmarshal(out.Bytes(), &page); err != nil || len(page) != 1 || page[0].SessionID != "root-b" {
		t.Fatalf("history page = %+v, %v; want the one entry of root-b", page, err)
	}
}

// TestVFSOfflineRefusesAnUnreadableRecord verifies a malformed record is an error, not an empty page.
func TestVFSOfflineRefusesAnUnreadableRecord(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	foreign := []byte(strings.Repeat("not a sqlite database, only text; ", 100))
	for source, file := range map[string]string{"journal": "vfs.sqlite", "history": "history.sqlite"} {
		if err := os.WriteFile(filepath.Join(state, file), foreign, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := runVFS([]string{source, "--workspace", root, "--state", state}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("%s: runVFS succeeded on a malformed record", source)
		}
	}
}
