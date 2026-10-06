// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// shellRun stands in for the sandboxed command: it writes into the projection
// the coordinator prepared and records the status the supervisor confirms.
func shellRun(t *testing.T, plan ShellPlan, status string, mutate func(projection string)) {
	t.Helper()
	if mutate != nil {
		mutate(plan.Cwd)
	}
	if err := os.WriteFile(plan.Confirm, []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestShellMutationIsStagedAndDiskUntouched(t *testing.T) {
	f, root, state := durable(t)
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("line1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := bind(t, f, "author", "unit", "dev", "data.txt")

	plan, err := f.PrepareShell(key, "shell-1", "sed 's/line1/changed/' data.txt > out && cat out > data.txt", state, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != ShellAllow || !plan.Capture {
		t.Fatalf("plan = %+v; want an allowed, captured mutation", plan)
	}
	projected := filepath.Join(plan.Cwd, "data.txt")
	if plan.Writable[0] != projected {
		t.Fatalf("writable = %v; want the projection copy %q", plan.Writable, projected)
	}
	// The real workspace is invisible to the command; only the projection is.
	if len(plan.Private) != 2 || plan.Private[1] != f.rootDir {
		t.Fatalf("private = %v; want the state directory and the workspace", plan.Private)
	}
	shellRun(t, plan, "0", func(projection string) {
		if err := os.WriteFile(filepath.Join(projection, "data.txt"), []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	result, err := f.ImportShell(key, "shell-1", state, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stagedCount(f, key) != 1 {
		t.Fatalf("staged %d paths; want the shell mutation staged", stagedCount(f, key))
	}
	if result.Revision == 0 || result.DeltaHash == "" {
		t.Fatalf("result = %+v; want a staged revision and delta hash", result)
	}
	content, err := os.ReadFile(filepath.Join(root, "data.txt"))
	if err != nil || string(content) != "line1\n" {
		t.Fatalf("workspace = %q (%v); want the untouched base before consolidation", content, err)
	}
	if got, _, _ := f.readMergedLocked(key, "data.txt"); string(got) != "changed\n" {
		t.Fatalf("merged view = %q; want the staged shell delta", got)
	}
	// One transaction per command: identity, input revision, both delta hashes
	// and the command's own status.
	entry := f.journal[len(f.journal)-1]
	if entry.Operation != OpShell || entry.CallID != "shell-1" || entry.AfterHash != result.DeltaHash ||
		entry.BeforeHash == entry.AfterHash || entry.Agent != "author" || !strings.Contains(entry.Outcome, "status 0 from revision 0") {
		t.Fatalf("journal entry = %+v; want one shell transaction with its hashes and outcome", entry)
	}
}

func TestShellFailedCommandKeepsUnverifiedDelta(t *testing.T) {
	f, _, state := durable(t)
	key := bind(t, f, "author", "unit", "dev", "notes.txt")
	plan, err := f.PrepareShell(key, "shell-fail", "printf half > notes.txt && exit 1", state, 0)
	if err != nil {
		t.Fatal(err)
	}
	shellRun(t, plan, "1", func(projection string) {
		if err := os.WriteFile(filepath.Join(projection, "notes.txt"), []byte("half"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.ImportShell(key, "shell-fail", state, 0); err != nil {
		t.Fatal(err)
	}
	if stagedCount(f, key) != 1 || hasVerdict(f, key) {
		t.Fatalf("staged=%d verdict=%v; want the failed command's delta retained and unverified", stagedCount(f, key), hasVerdict(f, key))
	}
	if entry := f.journal[len(f.journal)-1]; !strings.Contains(entry.Outcome, "status 1") {
		t.Fatalf("journal outcome = %q; want the failing status recorded", entry.Outcome)
	}
}

func TestShellImportRefusesUnconfirmedDescendants(t *testing.T) {
	f, _, state := durable(t)
	key := bind(t, f, "author", "unit", "dev", "data.txt")
	plan, err := f.PrepareShell(key, "shell-escape", "printf x > data.txt", state, 0)
	if err != nil {
		t.Fatal(err)
	}
	// No confirmation record: a descendant may still be writing.
	if err := os.WriteFile(filepath.Join(plan.Cwd, "data.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ImportShell(key, "shell-escape", state, 0); !errors.Is(err, ErrShellDescendants) {
		t.Fatalf("ImportShell = %v; want ErrShellDescendants", err)
	}
	if stagedCount(f, key) != 0 {
		t.Fatalf("staged %d paths; want nothing imported while descendants are unconfirmed", stagedCount(f, key))
	}
}

func TestShellImportIsSingleUse(t *testing.T) {
	f, _, state := durable(t)
	key := bind(t, f, "author", "unit", "dev", "data.txt")
	plan, err := f.PrepareShell(key, "shell-replay", "printf x > data.txt", state, 0)
	if err != nil {
		t.Fatal(err)
	}
	shellRun(t, plan, "0", func(projection string) {
		if err := os.WriteFile(filepath.Join(projection, "data.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	result, err := f.ImportShell(key, "shell-replay", state, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ImportShell(key, "shell-replay", state, result.Revision); err == nil {
		t.Fatal("ImportShell replay = nil; want the consumed call rejected")
	}
}

// TestShellDecisions checks the allow, deny and approval-gated decision for shell
// commands under each binding kind: scoped author, verifier, and no binding.
func TestShellDecisions(t *testing.T) {
	f, _, state := durable(t)
	scoped := bind(t, f, "author", "unit", "dev", "data.txt")
	verifier := bind(t, f, "verifier", "unit", "verify")
	cases := []struct {
		name     string
		key      AgentID
		command  string
		decision string
	}{
		{"read-only inspection needs no approval", scoped, "grep -rn todo . 2>/dev/null", ShellAllow},
		{"inspection without a binding", "", "ls -la", ShellAllow},
		{"captured workspace mutation", scoped, "printf x > data.txt", ShellAllow},
		{"verifier reads project context", verifier, "cat AGENTS.md && grep -rn invariants src", ShellAllow},
		{"verifier runs validation", verifier, "go test ./...", ShellAllow},
		{"verifier uses network for validation", verifier, "curl -I https://example.com", ShellAllow},
		{"mutation without a declared scope", verifier, "printf x > data.txt", ShellDeny},
		{"mutation with no binding at all", "", "printf x > data.txt", ShellDeny},
		{"quoted arrow is not a redirect", verifier, `node -e "const f = x => x"`, ShellAllow},
		{"quoted mutating word is a pattern", verifier, `grep -n "install" README.md`, ShellAllow},
		{"unquoted redirect after a quoted span", verifier, `echo "a" > data.txt`, ShellDeny},
		{"mutating git denied for execution", scoped, "git commit -m wip", ShellDeny},
		{"read-only git allowed", scoped, "git log --oneline", ShellAllow},
		{"privilege escalation is approval-gated", scoped, "sudo tee /etc/hosts", ShellAsk},
		{"host-wide install is approval-gated", scoped, "brew install jq", ShellAsk},
		{"global toolchain operation is approval-gated", scoped, "npm install -g takt", ShellAsk},
		{"mutation outside the workspace is approval-gated", scoped, "cp data.txt /etc/takt.conf", ShellAsk},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, err := f.PrepareShell(c.key, "call-"+c.name, c.command, state, 0)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Decision != c.decision {
				t.Fatalf("decision = %q (%s); want %q", plan.Decision, plan.Reason, c.decision)
			}
			if plan.Decision == ShellDeny {
				if plan.Cwd != "" || plan.Writable != nil {
					t.Fatalf("denied plan carries a sandbox layout: %+v", plan)
				}
				return
			}
			if c.key == verifier && plan.Decision == ShellAllow {
				if plan.Cwd != f.rootDir || len(plan.Writable) != 1 || plan.Writable[0] != plan.Scratch ||
					len(plan.Protected) == 0 || plan.Protected[0] != f.rootDir {
					t.Fatalf("verifier validation must read project context and write only scratch: %+v", plan)
				}
			}
			if !strings.Contains(plan.Reason, c.command) {
				t.Fatalf("reason %q does not identify the exact command", plan.Reason)
			}
			// Only an approved command may write outside the workspace, and never
			// the workspace itself.
			if plan.Decision == ShellAsk && plan.Writable[0] != everythingWritable {
				t.Fatalf("approved plan writable = %v; want the approval's grant", plan.Writable)
			}
			if plan.Decision == ShellAsk && plan.Protected[0] != f.rootDir {
				t.Fatalf("approved plan protected = %v; want the workspace still denied", plan.Protected)
			}
		})
	}
}

func TestShellPrepareRejectsStaleRevisionAndUnknownBinding(t *testing.T) {
	f, _, state := durable(t)
	key := bind(t, f, "author", "unit", "dev", "data.txt")
	if _, err := f.PrepareShell(key, "call", "ls", state, 7); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("PrepareShell(stale) = %v; want ErrStaleRevision", err)
	}
	if _, err := f.PrepareShell("nobody", "call", "ls", state, 0); !errors.Is(err, ErrIdentity) {
		t.Fatalf("PrepareShell(unknown key) = %v; want ErrIdentity", err)
	}
	if _, err := f.PrepareShell(key, "", "ls", state, 0); !errors.Is(err, ErrIdentity) {
		t.Fatalf("PrepareShell(no call id) = %v; want ErrIdentity", err)
	}
}

func TestShellInspectionDeniesReadingSecrets(t *testing.T) {
	f, root, state := durable(t)
	store := filepath.Join(obs.StateDirName, obs.StoreFileName)
	for _, rel := range []string{".env", "cmd/app/.env.local", "deploy/tls.key", "secrets/token", "src/main.go", "node_modules/pkg/.env", store} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := f.PrepareShell("", "inspect", "cat .env", state, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != ShellAllow {
		t.Fatalf("inspection decision = %s (%s)", plan.Decision, plan.Reason)
	}
	for _, rel := range []string{".env", "cmd/app/.env.local", "deploy/tls.key", "secrets", store} {
		want := filepath.Join(root, filepath.FromSlash(rel))
		if !containsCanonicalPath(t, plan.Private, want) {
			t.Errorf("inspection may read %s; private = %v", rel, plan.Private)
		}
	}
	for _, rel := range []string{"src/main.go", "secrets/token"} {
		if containsCanonicalPath(t, plan.Private, filepath.Join(root, filepath.FromSlash(rel))) {
			t.Errorf("%s listed individually; private = %v", rel, plan.Private)
		}
	}
}

// containsCanonicalPath compares filesystem identities, not macOS's /var and
// /private/var spellings of the same temporary directory.
func containsCanonicalPath(t *testing.T, paths []string, want string) bool {
	t.Helper()
	want, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve expected path: %v", err)
	}
	for _, path := range paths {
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatalf("resolve private path %q: %v", path, err)
		}
		if canonical == want {
			return true
		}
	}
	return false
}
