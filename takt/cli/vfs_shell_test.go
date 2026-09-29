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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// TestVFSShellIPC drives the shell capture path the plugin uses: the coordinator
// resolves the sandbox plan from the binding's scope, the command's projection
// comes back as a staged delta, and the workspace stays untouched until the
// ordinary verdict and consolidation.
func TestVFSShellIPC(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("line1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	bound, err := mutate("bind", vfsReq("u1", "dev", map[string]any{"scope": []string{"data.txt"}}))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := mutate("shell-prepare", vfsReq("u1", "dev", map[string]any{
		"author_key": bound.Key, "call_id": "shell-1", "expected_revision": 0,
		"command": "sed 's/line1/changed/' data.txt > out && cat out > data.txt",
	}))
	if err != nil {
		t.Fatal(err)
	}
	plan := prepared.Shell
	if plan == nil || plan.Decision != vfs.ShellAllow || !plan.Capture || plan.Cwd == "" {
		t.Fatalf("shell plan = %+v; want an allowed captured mutation with a projection", plan)
	}
	// The model never supplies these: the coordinator resolved every path from
	// the binding's own scope and from its private state directory.
	if len(plan.Writable) != 1 || plan.Writable[0] != filepath.Join(plan.Cwd, "data.txt") {
		t.Fatalf("writable = %v; want only the projected scope path", plan.Writable)
	}
	if !strings.HasPrefix(plan.Scratch, filepath.Dir(state)) || plan.Cwd == root {
		t.Fatalf("plan runs in the workspace itself: %+v", plan)
	}

	// Stand in for the sandboxed command and its supervisor.
	if err := os.WriteFile(filepath.Join(plan.Cwd, "data.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.Confirm, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}

	imported, err := mutate("shell-import", vfsReq("u1", "dev", map[string]any{
		"author_key": bound.Key, "call_id": "shell-1", "expected_revision": 0,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if imported.Revision == 0 || imported.DeltaHash == "" {
		t.Fatalf("import = %+v; want a staged revision and delta hash", imported)
	}
	if content, _ := os.ReadFile(filepath.Join(root, "data.txt")); string(content) != "line1\n" {
		t.Fatalf("workspace = %q; want it untouched before consolidation", content)
	}

	// The staged shell delta consolidates through the ordinary gate, unchanged.
	verifier, err := mutate("assign-verifier", vfsReq("u1", "verify", map[string]any{"author_key": bound.Key}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mutate("bind", vfsReq("u1", "verify", map[string]any{"author_key": bound.Key})); err != nil {
		t.Fatal(err)
	}
	if _, err = mutate("verify", vfsReq("u1", "verify", map[string]any{
		"verifier_key": verifier.Key, "author_key": bound.Key, "call_id": "gate",
		"expected_revision": imported.Revision, "delta_hash": imported.DeltaHash, "pass": true,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err = mutate("consolidate", vfsReq("u1", "dev", map[string]any{
		"author_key": bound.Key, "checkpoint": "shell", "expected_revision": imported.Revision,
	})); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(filepath.Join(root, "data.txt")); string(content) != "changed\n" {
		t.Fatalf("workspace = %q after consolidation; want the shell delta materialized", content)
	}
}

// A command whose mutations cannot be captured is denied, and the denial carries
// no sandbox layout to run with.
func TestVFSShellUncapturableDenied(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	author, err := mutate("bind", vfsReq("u1", "dev", map[string]any{"scope": []string{"data.txt"}}))
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := mutate("assign-verifier", vfsReq("u1", "verify", map[string]any{"author_key": author.Key}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mutate("bind", vfsReq("u1", "verify", map[string]any{"author_key": author.Key})); err != nil {
		t.Fatal(err)
	}
	prepared, err := mutate("shell-prepare", vfsReq("u1", "verify", map[string]any{
		"author_key": verifier.Key, "call_id": "shell-deny", "expected_revision": 0,
		"command": "printf x > data.txt",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Shell.Decision != vfs.ShellDeny || prepared.Shell.Cwd != "" || prepared.Shell.Writable != nil {
		t.Fatalf("shell plan = %+v; want a denial with no sandbox layout", prepared.Shell)
	}
	if !strings.Contains(prepared.Shell.Reason, "printf x > data.txt") {
		t.Fatalf("reason %q does not identify the exact command", prepared.Shell.Reason)
	}
}

// An unbound caller carries no author_key, so it must not be refused as an
// identity mismatch: it may still inspect the workspace.
func TestVFSShellUnboundCallerInspectAllowed(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("line1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := mutate("shell-prepare", vfsReq("u1", "analyst", map[string]any{
		"call_id": "shell-unbound", "expected_revision": 0, "command": "cat data.txt",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Shell == nil || prepared.Shell.Decision == vfs.ShellDeny {
		t.Fatalf("shell plan = %+v; want an unbound caller's inspection admitted", prepared.Shell)
	}
}

// An unbound caller attempting a mutation is still denied, but with the
// uncapturable-mutation reason, not a spurious identity mismatch.
func TestVFSShellUnboundCallerMutationDenied(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	prepared, err := mutate("shell-prepare", vfsReq("u1", "analyst", map[string]any{
		"call_id": "shell-unbound-mutate", "expected_revision": 0, "command": "printf x > data.txt",
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
