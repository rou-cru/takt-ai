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

package main_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// shellReply is the answer of shell-prepare: the sandbox plan the core resolved.
type shellReply struct {
	OK    bool           `json:"ok"`
	Shell *vfs.ShellPlan `json:"shell"`
}

// shellPrepare runs shell-prepare with req; a refusal is the command's exit status and stderr.
func shellPrepare(fx *vfsFixture, req map[string]any) (shellReply, error) {
	fx.t.Helper()
	var resp shellReply
	err := fx.call("shell-prepare", req, &resp)
	return resp, err
}

// mustShellPrepare is shellPrepare for a request that has to be accepted.
func mustShellPrepare(fx *vfsFixture, req map[string]any) shellReply {
	fx.t.Helper()
	resp, err := shellPrepare(fx, req)
	if err != nil || !resp.OK {
		fx.t.Fatalf("shell-prepare = %+v, %v", resp, err)
	}
	return resp
}

func TestVFSShellUnboundPlanProtectsWorkspace(t *testing.T) {
	fx := newVFSFixture(t, buildTaktAI(t))
	canonicalRoot, err := filepath.EvalSymlinks(fx.root)
	if err != nil {
		t.Fatal(err)
	}
	state := fx.state
	for _, command := range []string{"cat data.txt", "git diff", "ls"} {
		t.Run(command, func(t *testing.T) {
			prepared, err := shellPrepare(fx, vfsReq("u1", "analyst", map[string]any{
				"call_id": "inspect-" + command, "command": command,
			}))
			if err != nil {
				t.Fatal(err)
			}
			plan := prepared.Shell
			if !prepared.OK || plan == nil || plan.Decision != vfs.ShellAllow {
				t.Fatalf("inspection plan = %+v", prepared)
			}
			if plan.Capture || plan.Cwd != canonicalRoot {
				t.Errorf("unbound inspection must read the workspace without capturing: %+v", plan)
			}
			if plan.Scratch == "" || !slices.Equal(plan.Writable, []string{plan.Scratch}) {
				t.Errorf("only scratch may be writable: %+v", plan)
			}
			if !slices.Contains(plan.Protected, canonicalRoot) || !slices.Contains(plan.Private, state) {
				t.Errorf("workspace/state protection missing: %+v", plan)
			}
		})
	}
}

// TestVFSShellPlansOnlyForTheHeldBinding: shell-prepare captures into the
// author binding the calling host session holds; any other caller gets an
// unbound plan, whatever key it names.
func TestVFSShellPlansOnlyForTheHeldBinding(t *testing.T) {
	fx := newVFSFixture(t, buildTaktAI(t))
	bound := fx.delegate(rootA, "u1", "dev", "data.txt")
	for _, tc := range []struct {
		name string
		req  map[string]any
	}{
		{"other root", vfsReqIn(rootB, "u1", "dev", nil)},
		{"other unit", vfsReq("u2", "dev", nil)},
		{"other agent", vfsReq("u1", "analyst", nil)},
		{"other host session", vfsReq("u1", "dev", map[string]any{"caller_session": "elsewhere"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.req["author_key"], tc.req["call_id"], tc.req["command"] = bound, "inspect-"+tc.name, "cat data.txt"
			prepared, err := shellPrepare(fx, tc.req)
			if err != nil {
				return
			} // malformed caller identities are refused, never captured
			if prepared.Shell == nil || prepared.Shell.Capture {
				t.Fatalf("plan for a caller not holding the key = %+v; want an unbound plan", prepared.Shell)
			}
		})
	}
	prepared := mustShellPrepare(fx, vfsReq("u1", "dev", map[string]any{"call_id": "inspect-owner", "command": "cat data.txt"}))
	if prepared.Shell == nil || prepared.Shell.Decision != vfs.ShellAllow || !prepared.Shell.Capture {
		t.Fatalf("holder's inspection = %+v", prepared.Shell)
	}
}

func TestVFSShellUnboundStillValidatesRequest(t *testing.T) {
	bin := buildTaktAI(t)
	for _, tc := range []struct{ name, callID, command string }{
		{"missing call ID", "", "cat data.txt"},
		{"empty command", "inspect", ""},
		{"blank command", "inspect", " \t\n "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := shellPrepare(newVFSFixture(t, bin), vfsReq("u1", "analyst", map[string]any{
				"call_id": tc.callID, "command": tc.command,
			}))
			refusedWith(t, "an invalid unbound request", err, vfs.ErrIdentity.Error())
		})
	}
}

// TestVFSShellIPC drives the shell capture path the plugin uses: the core
// resolves the sandbox plan from the held binding's scope, the command's
// projection comes back as a staged delta, and the workspace stays untouched
// until the ordinary verdict and consolidation.
func TestVFSShellIPC(t *testing.T) {
	fx := newVFSFixture(t, buildTaktAI(t))
	if err := os.WriteFile(filepath.Join(fx.root, "data.txt"), []byte("line1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bound := fx.delegate(rootA, "u1", "dev", "data.txt")
	plan := prepareShell(t, fx, "shell-1", "sed 's/line1/changed/' data.txt > out && cat out > data.txt")
	// The model never supplies these: the core resolved every path from the
	// binding's own scope and from its private state directory.
	if len(plan.Writable) != 1 || plan.Writable[0] != filepath.Join(plan.Cwd, "data.txt") {
		t.Fatalf("writable = %v; want only the projected scope path", plan.Writable)
	}
	if !strings.HasPrefix(plan.Scratch, filepath.Dir(fx.state)) || plan.Cwd == fx.root {
		t.Fatalf("plan runs in the workspace itself: %+v", plan)
	}
	runSandboxed(t, plan, "changed\n")
	imported := fx.must("shell-complete", vfsReq("u1", "dev", map[string]any{"call_id": "shell-1"}))
	if imported.Revision == nil || *imported.Revision == 0 || imported.DeltaHash == "" {
		t.Fatalf("import = %+v; want a staged revision and delta hash", imported)
	}
	if got := fx.disk("data.txt"); got != "line1\n" {
		t.Fatalf("workspace = %q; want it untouched before consolidation", got)
	}
	// The staged shell delta consolidates through the ordinary gate, unchanged.
	fx.judge(rootA, "review", "verify", bound)
	fx.read(rootA, "review", "verify", "r1", "data.txt", "")
	if err := fx.verify(rootA, "review", "verify", "gate", bound, true); err != nil {
		t.Fatal(err)
	}
	fx.must("consolidate", orchReq(rootA, map[string]any{"author_key": bound, "checkpoint": "shell"}))
	if got := fx.disk("data.txt"); got != "changed\n" {
		t.Fatalf("workspace = %q after consolidation; want the shell delta materialized", got)
	}
}

// TestVFSShellImportRefusesAConcurrentWrite: a write staged while the command
// ran is never overwritten by its import.
func TestVFSShellImportRefusesAConcurrentWrite(t *testing.T) {
	fx := newVFSFixture(t, buildTaktAI(t))
	fx.delegate(rootA, "u1", "dev", "data.txt")
	plan := prepareShell(t, fx, "shell-race", "printf shell > data.txt")
	fx.write(rootA, "u1", "dev", "w1", "data.txt", "concurrent")
	runSandboxed(t, plan, "shell")
	_, err := fx.mutate("shell-complete", vfsReq("u1", "dev", map[string]any{"call_id": "shell-race"}))
	refusedWith(t, "an import over staged work that moved", err, "run the command again")
	if read := fx.read(rootA, "u1", "dev", "r1", "data.txt", ""); read.Content != "concurrent" {
		t.Fatalf("staged data.txt = %q; want the concurrent write kept", read.Content)
	}
}

// prepareShell prepares command for dev's delegation on u1 and requires a
// captured plan.
func prepareShell(t *testing.T, fx *vfsFixture, call, command string) *vfs.ShellPlan {
	t.Helper()
	plan := mustShellPrepare(fx, vfsReq("u1", "dev", map[string]any{"call_id": call, "command": command})).Shell
	if plan == nil || plan.Decision != vfs.ShellAllow || !plan.Capture || plan.Cwd == "" {
		t.Fatalf("shell plan = %+v; want an allowed captured mutation with a projection", plan)
	}
	return plan
}

// runSandboxed stands in for the sandboxed command and its supervisor: it
// leaves content in the projected data.txt and confirms a clean exit.
func runSandboxed(t *testing.T, plan *vfs.ShellPlan, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(plan.Cwd, "data.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.Confirm, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A command whose mutations cannot be captured is denied, and the denial carries
// no sandbox layout to run with.
func TestVFSShellUncapturableDenied(t *testing.T) {
	fx := newVFSFixture(t, buildTaktAI(t))
	author := fx.delegate(rootA, "u1", "dev", "data.txt")
	fx.judge(rootA, "review", "verify", author)
	prepared := mustShellPrepare(fx, vfsReq("review", "verify", map[string]any{"call_id": "shell-deny", "command": "printf x > data.txt"}))
	if prepared.Shell.Decision != vfs.ShellDeny || prepared.Shell.Cwd != "" || prepared.Shell.Writable != nil {
		t.Fatalf("shell plan = %+v; want a denial with no sandbox layout", prepared.Shell)
	}
	if !strings.Contains(prepared.Shell.Reason, "printf x > data.txt") {
		t.Fatalf("reason %q does not identify the exact command", prepared.Shell.Reason)
	}
}

// An unbound caller attempting a mutation is still denied, but with the
// uncapturable-mutation reason, not a spurious identity mismatch.
func TestVFSShellUnboundCallerMutationDenied(t *testing.T) {
	prepared, err := shellPrepare(newVFSFixture(t, buildTaktAI(t)), vfsReq("u1", "analyst", map[string]any{
		"call_id": "shell-unbound-mutate", "command": "printf x > data.txt",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Shell.Decision != vfs.ShellDeny {
		t.Fatalf("shell plan = %+v; want the mutation denied", prepared.Shell)
	}
	if !strings.Contains(prepared.Shell.Reason, "does not modify the workspace through the shell") {
		t.Fatalf("reason %q; want the uncapturable-mutation reason, not an identity error", prepared.Shell.Reason)
	}
}
