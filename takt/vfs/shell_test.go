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

package vfs_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// everythingWritable is the write grant of an approved out-of-workspace command.
const everythingWritable = "/"

// shellRun stands in for the sandboxed command: it writes into the projection
// the coordinator prepared and records the status the supervisor confirms.
func shellRun(t *testing.T, plan vfs.ShellPlan, status string, mutate func(projection string)) {
	t.Helper()
	if mutate != nil {
		mutate(plan.Cwd)
	}
	if err := os.WriteFile(plan.Confirm, []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeProjected writes content at rel in the projection.
func writeProjected(t *testing.T, rel, content string) func(string) {
	return func(projection string) {
		if err := os.WriteFile(filepath.Join(projection, rel), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// canonical resolves path's symlinks, so macOS's /var and /private/var
// spellings of one temporary directory compare equal.
func canonical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %q: %v", path, err)
	}
	return resolved
}

// lastEntry is the latest journal entry.
func lastEntry(t *testing.T, f *vfs.FS) vfs.JournalEntry {
	t.Helper()
	var last vfs.JournalEntry
	for after := -1; ; {
		page := f.JournalPage("", "", after, vfs.MaxJournalPageSize)
		if len(page) == 0 {
			return last
		}
		last = page[len(page)-1]
		after = last.Seq
	}
}

func TestShellMutationIsStagedAndDiskUntouched(t *testing.T) {
	f, root, state := newStore(t)
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("line1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, key := delegate(t, f, state, testSession, "dev", "unit", "data.txt")

	plan, err := f.PrepareShell(c, "shell-1", "sed 's/line1/changed/' data.txt > out && cat out > data.txt", state)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != vfs.ShellAllow || !plan.Capture {
		t.Fatalf("plan = %+v; want an allowed, captured mutation", plan)
	}
	if plan.Writable[0] != filepath.Join(plan.Cwd, "data.txt") {
		t.Fatalf("writable = %v; want the projection copy", plan.Writable)
	}
	// The real workspace is invisible to the command; only the projection is.
	if len(plan.Private) != 2 || plan.Private[1] != canonical(t, root) {
		t.Fatalf("private = %v; want the state directory and the workspace", plan.Private)
	}
	shellRun(t, plan, "0", writeProjected(t, "data.txt", "changed\n"))

	result, err := f.CompleteShell(c, "shell-1", state)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.InspectDelta(key).Files) != 1 || result.Revision == 0 || result.DeltaHash == "" {
		t.Fatalf("result = %+v; want the shell mutation staged", result)
	}
	content, err := os.ReadFile(filepath.Join(root, "data.txt"))
	if err != nil || string(content) != "line1\n" {
		t.Fatalf("workspace = %q (%v); want the untouched base before consolidation", content, err)
	}
	if got := readAs(t, f, c, "read", "data.txt", ""); string(got.Content) != "changed\n" {
		t.Fatalf("merged view = %q; want the staged shell delta", got.Content)
	}
	// One transaction per command: identity, input revision, both delta hashes
	// and the command's own status.
	var shell vfs.JournalEntry
	for _, e := range f.JournalPage("", "", -1, vfs.MaxJournalPageSize) {
		if e.Operation == vfs.OpShell {
			shell = e
		}
	}
	if shell.CallID != "shell-1" || shell.AfterHash != result.DeltaHash || shell.BeforeHash == shell.AfterHash ||
		shell.Agent != "dev" || !strings.Contains(shell.Outcome, "status 0 from revision 0") {
		t.Fatalf("journal entry = %+v; want one shell transaction with its hashes and outcome", shell)
	}
}

func TestShellFailedCommandKeepsUnverifiedDelta(t *testing.T) {
	f, _, state := newStore(t)
	c, key := delegate(t, f, state, testSession, "dev", "unit", "notes.txt")
	plan, err := f.PrepareShell(c, "shell-fail", "printf half > notes.txt && exit 1", state)
	if err != nil {
		t.Fatal(err)
	}
	shellRun(t, plan, "1", writeProjected(t, "notes.txt", "half"))
	if _, err := f.CompleteShell(c, "shell-fail", state); err != nil {
		t.Fatal(err)
	}
	claim, _ := claimOf(f, key)
	if len(f.InspectDelta(key).Files) != 1 || len(claim.Verdicts) != 0 {
		t.Fatalf("delta = %+v, claim = %+v; want the failed command's delta retained and unverified", f.InspectDelta(key), claim)
	}
	if entry := lastEntry(t, f); !strings.Contains(entry.Outcome, "status 1") {
		t.Fatalf("journal outcome = %q; want the failing status recorded", entry.Outcome)
	}
}

func TestShellImportRefusesUnconfirmedDescendants(t *testing.T) {
	f, _, state := newStore(t)
	c, key := delegate(t, f, state, testSession, "dev", "unit", "data.txt")
	plan, err := f.PrepareShell(c, "shell-escape", "printf x > data.txt", state)
	if err != nil {
		t.Fatal(err)
	}
	// No confirmation record: a descendant may still be writing.
	writeProjected(t, "data.txt", "x")(plan.Cwd)
	if _, err := f.CompleteShell(c, "shell-escape", state); !errors.Is(err, vfs.ErrShellDescendants) {
		t.Fatalf("CompleteShell = %v; want ErrShellDescendants", err)
	}
	if len(f.InspectDelta(key).Files) != 0 {
		t.Fatal("imported while descendants were unconfirmed")
	}
}

func TestShellImportIsSingleUse(t *testing.T) {
	f, _, state := newStore(t)
	c, _ := delegate(t, f, state, testSession, "dev", "unit", "data.txt")
	plan, err := f.PrepareShell(c, "shell-replay", "printf x > data.txt", state)
	if err != nil {
		t.Fatal(err)
	}
	shellRun(t, plan, "0", writeProjected(t, "data.txt", "x"))
	if _, err := f.CompleteShell(c, "shell-replay", state); err != nil {
		t.Fatal(err)
	}
	if _, err := f.CompleteShell(c, "shell-replay", state); err == nil {
		t.Fatal("CompleteShell replay = nil; want the consumed call rejected")
	}
}

// A write staged while the command ran is never overwritten by its import.
func TestShellImportRefusesStagedWorkThatMoved(t *testing.T) {
	f, _, state := newStore(t)
	c, key := delegate(t, f, state, testSession, "dev", "unit", "data.txt", "other.txt")
	plan, err := f.PrepareShell(c, "shell-race", "printf x > data.txt", state)
	if err != nil {
		t.Fatal(err)
	}
	write(t, f, c, "concurrent", "other.txt", "kept")
	shellRun(t, plan, "0", writeProjected(t, "data.txt", "x"))
	if _, err := f.CompleteShell(c, "shell-race", state); !errors.Is(err, vfs.ErrStaleRevision) {
		t.Fatalf("CompleteShell after a concurrent write = %v; want ErrStaleRevision", err)
	}
	if got, ok := stagedContent(f, key, "other.txt"); !ok || got != "kept" {
		t.Fatalf("concurrent write = %q, %v; want it kept", got, ok)
	}
	if _, ok := stagedContent(f, key, "data.txt"); ok {
		t.Fatal("the refused import staged its projection")
	}
}

func TestShellImportBelongsToThePreparingDelegation(t *testing.T) {
	f, _, state := newStore(t)
	c, _ := delegate(t, f, state, testSession, "dev", "unit", "data.txt")
	if _, err := f.CompleteShell(c, "never-prepared", state); !errors.Is(err, vfs.ErrIdentity) && !errors.Is(err, vfs.ErrNoAdmission) {
		t.Fatalf("CompleteShell without preparation = %v; want ErrIdentity", err)
	}
	plan, err := f.PrepareShell(c, "shell-own", "printf x > data.txt", state)
	if err != nil {
		t.Fatal(err)
	}
	shellRun(t, plan, "0", writeProjected(t, "data.txt", "x"))
	other := c
	other.HostSession = "another-host"
	if _, err := f.CompleteShell(other, "shell-own", state); !errors.Is(err, vfs.ErrIdentity) && !errors.Is(err, vfs.ErrNoAdmission) {
		t.Fatalf("CompleteShell from another host session = %v; want ErrIdentity", err)
	}
	if _, err := f.PrepareShell(c, "", "ls", state); !errors.Is(err, vfs.ErrIdentity) && !errors.Is(err, vfs.ErrNoAdmission) {
		t.Fatalf("PrepareShell(no call id) = %v; want ErrIdentity", err)
	}
}

// TestShellDecisions checks the allow, deny and approval-gated decision for shell
// commands of a scoped author, a verifier, and a caller holding no binding.
func TestShellDecisions(t *testing.T) {
	f, root, state := newStore(t)
	scoped, author := delegate(t, f, state, testSession, "dev", "unit", "data.txt")
	verifier, _ := judge(t, f, state, testSession, "verify", "unit/verify", author)
	unbound := callerOf(testSession, "fix", "elsewhere")
	workspace := canonical(t, root)
	cases := []struct {
		name     string
		caller   vfs.Caller
		command  string
		decision string
	}{
		{"read-only inspection needs no approval", scoped, "grep -rn todo . 2>/dev/null", vfs.ShellAllow},
		{"inspection without a binding", unbound, "ls -la", vfs.ShellAllow},
		{"captured workspace mutation", scoped, "printf x > data.txt", vfs.ShellAllow},
		{"verifier reads project context", verifier, "cat AGENTS.md && grep -rn invariants src", vfs.ShellAllow},
		{"verifier runs validation", verifier, "go test ./...", vfs.ShellAllow},
		{"verifier uses network for validation", verifier, "curl -I https://example.com", vfs.ShellAllow},
		{"mutation without a declared scope", verifier, "printf x > data.txt", vfs.ShellDeny},
		{"mutation with no binding at all", unbound, "printf x > data.txt", vfs.ShellDeny},
		{"quoted arrow is not a redirect", verifier, `node -e "const f = x => x"`, vfs.ShellAllow},
		{"quoted mutating word is a pattern", verifier, `grep -n "install" README.md`, vfs.ShellAllow},
		{"unquoted redirect after a quoted span", verifier, `echo "a" > data.txt`, vfs.ShellDeny},
		{"mutating git denied for execution", scoped, "git commit -m wip", vfs.ShellDeny},
		{"read-only git allowed", scoped, "git log --oneline", vfs.ShellAllow},
		{"privilege escalation is approval-gated", scoped, "sudo tee /etc/hosts", vfs.ShellAsk},
		{"host-wide install is approval-gated", scoped, "brew install jq", vfs.ShellAsk},
		{"global toolchain operation is approval-gated", scoped, "npm install -g takt", vfs.ShellAsk},
		{"mutation outside the workspace is approval-gated", scoped, "cp data.txt /etc/takt.conf", vfs.ShellAsk},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, err := f.PrepareShell(c.caller, "call-"+c.name, c.command, state)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Decision != c.decision {
				t.Fatalf("decision = %q (%s); want %q", plan.Decision, plan.Reason, c.decision)
			}
			if plan.Decision == vfs.ShellDeny {
				if plan.Cwd != "" || plan.Writable != nil {
					t.Fatalf("denied plan carries a sandbox layout: %+v", plan)
				}
				return
			}
			if c.caller == verifier && plan.Decision == vfs.ShellAllow {
				if plan.Cwd != workspace || len(plan.Writable) != 1 || plan.Writable[0] != plan.Scratch ||
					len(plan.Protected) == 0 || plan.Protected[0] != workspace {
					t.Fatalf("verifier validation must read project context and write only scratch: %+v", plan)
				}
			}
			if !strings.Contains(plan.Reason, c.command) {
				t.Fatalf("reason %q does not identify the exact command", plan.Reason)
			}
			// Only an approved command may write outside the workspace, and never
			// the workspace itself.
			if plan.Decision == vfs.ShellAsk && (plan.Writable[0] != everythingWritable || plan.Protected[0] != workspace) {
				t.Fatalf("approved plan = %+v; want the approval's grant with the workspace still denied", plan)
			}
		})
	}
}

func TestShellInspectionDeniesReadingSecrets(t *testing.T) {
	f, root, state := newStore(t)
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
	plan, err := f.PrepareShell(callerOf(testSession, "fix", "u"), "inspect", "cat .env", state)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Decision != vfs.ShellAllow {
		t.Fatalf("inspection decision = %s (%s)", plan.Decision, plan.Reason)
	}
	for _, rel := range []string{".env", "cmd/app/.env.local", "deploy/tls.key", "secrets", store} {
		if !containsCanonicalPath(t, plan.Private, filepath.Join(root, filepath.FromSlash(rel))) {
			t.Errorf("inspection may read %s; private = %v", rel, plan.Private)
		}
	}
	for _, rel := range []string{"src/main.go", "secrets/token"} {
		if containsCanonicalPath(t, plan.Private, filepath.Join(root, filepath.FromSlash(rel))) {
			t.Errorf("%s listed individually; private = %v", rel, plan.Private)
		}
	}
}

// containsCanonicalPath compares filesystem identities.
func containsCanonicalPath(t *testing.T, paths []string, want string) bool {
	t.Helper()
	want = canonical(t, want)
	for _, path := range paths {
		if canonical(t, path) == want {
			return true
		}
	}
	return false
}
