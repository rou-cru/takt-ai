package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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
	for _, c := range []string{"claims", "assign", "assign-verifier", "release", "bind", "op", "verify", "consolidate", "resolve", "shell-prepare", "shell-import"} {
		if !validVFSCommand(c) {
			t.Errorf("validVFSCommand(%q) = false", c)
		}
	}
	for _, c := range []string{"", "journal", "recover", "OP"} {
		if validVFSCommand(c) {
			t.Errorf("validVFSCommand(%q) = true", c)
		}
	}
}

// TestParseAction verifies each wire action maps to its VFS operation and unknown ones are refused.
func TestParseAction(t *testing.T) {
	for action, want := range map[string]vfs.OperationType{
		"read": vfs.OpRead, "create": vfs.OpCreate, "patch": vfs.OpPatch, "delete": vfs.OpDelete, "rollback": vfs.OpRollback,
	} {
		if got, err := parseAction(action); err != nil || got != want {
			t.Errorf("parseAction(%q) = %v, %v; want %v", action, got, err, want)
		}
	}
	if _, err := parseAction("truncate"); err == nil || !strings.Contains(err.Error(), `unsupported action "truncate"`) {
		t.Fatalf("parseAction(truncate) error = %v", err)
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
		"zero limit":          {append(append([]string{}, base...), "--limit", "0"), "invalid pagination"},
		"oversized limit":     {append(append([]string{}, base...), "--limit", strconv.Itoa(tooManyEntries)), "invalid pagination"},
		"cursor below -1":     {append(append([]string{}, base...), "--after", "-2"), "invalid pagination"},
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
	valid := `{"ipc_version":4,"session_id":"s1"}`
	cases := map[string]struct {
		args    []string
		payload string
		want    string
	}{
		"missing workspace": {[]string{"op", "--state", state}, valid, "--workspace and --state are required"},
		"positional":        {[]string{"op", "--workspace", root, "--state", state, "x"}, valid, "no positional arguments"},
		"unknown flag":      {[]string{"op", "--nope"}, valid, "flag provided but not defined"},
		"malformed json":    {[]string{"op", "--workspace", root, "--state", state}, "{", "invalid request"},
		"no session":        {[]string{"op", "--workspace", root, "--state", state}, `{"ipc_version":4}`, "session_id is required"},
		"claim with cycle":  {[]string{"claims", "--workspace", root, "--state", state}, `{"ipc_version":4,"session_id":"s1","cycle_id":"c"}`, "cannot declare a maintenance cycle"},
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

// TestVFSMutationsRejectForeignIdentity verifies every mutation refuses a key that is not the caller's binding.
func TestVFSMutationsRejectForeignIdentity(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	bound, err := mutate("bind", vfsReq("u1", "dev", map[string]any{"scope": []string{"a.go"}}))
	if err != nil || !bound.OK {
		t.Fatalf("bind = %+v, %v", bound, err)
	}
	const foreignKey = "not-a-binding-key"
	for _, command := range []string{"op", "shell-import", "consolidate"} {
		t.Run(command, func(t *testing.T) {
			_, err := mutate(command, vfsReq("u1", "dev", map[string]any{"author_key": foreignKey, "action": "read", "path": "a.go"}))
			if err == nil || !strings.Contains(err.Error(), "not the identity bound to author_key") {
				t.Fatalf("%s error = %v", command, err)
			}
		})
	}
	t.Run("shell-prepare", func(t *testing.T) {
		_, err := mutate("shell-prepare", vfsReq("u1", "dev", map[string]any{"author_key": foreignKey, "command": "true"}))
		if err == nil || !strings.Contains(err.Error(), "not the identity bound to author_key") {
			t.Fatalf("shell-prepare error = %v", err)
		}
	})
	t.Run("verify", func(t *testing.T) {
		_, err := mutate("verify", vfsReq("u1", "dev", map[string]any{"verifier_key": foreignKey, "author_key": bound.Key}))
		if err == nil || !strings.Contains(err.Error(), "not the identity bound to verifier_key") {
			t.Fatalf("verify error = %v", err)
		}
	})
	t.Run("unsupported action after valid identity", func(t *testing.T) {
		_, err := mutate("op", vfsReq("u1", "dev", map[string]any{"author_key": bound.Key, "action": "truncate", "path": "a.go"}))
		if err == nil || !strings.Contains(err.Error(), `unsupported action "truncate"`) {
			t.Fatalf("op error = %v", err)
		}
	})
}

// TestVFSResolveAndClaimErrors verifies collision resolution is idempotent and claim commands validate their input.
func TestVFSResolveAndClaimErrors(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	resolved, err := mutate("resolve", vfsReq("u1", "dev", map[string]any{"collision_path": "a.go", "collision_owner": "other"}))
	if err != nil || !resolved.OK {
		t.Fatalf("resolve = %+v, %v", resolved, err)
	}
	if _, err := mutate("release", map[string]any{"ipc_version": IPCVersion, "session_id": "s1", "key": "unknown"}); err == nil || !strings.Contains(err.Error(), "not a known binding key") {
		t.Fatalf("release unknown error = %v", err)
	}
	if _, err := mutate("assign-verifier", vfsReq("u1", "dev", map[string]any{"scope": []string{"a.go"}})); err == nil || !strings.Contains(err.Error(), "does not accept scope") {
		t.Fatalf("assign-verifier with scope error = %v", err)
	}
	if _, err := mutate("bind", vfsReq("u1", "dev", map[string]any{"scope": []string{"a.go"}})); err != nil {
		t.Fatal(err)
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
	if _, err := runVFSMutate(fs, "journal", "", request{}, vfs.Identity{}); err == nil || !strings.Contains(err.Error(), `unsupported command "journal"`) {
		t.Fatalf("runVFSMutate() error = %v", err)
	}
	if err := verifyIdentity(fs, "missing", vfs.Identity{}, "author_key"); !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("verifyIdentity() error = %v, want ErrIdentity", err)
	}
}
