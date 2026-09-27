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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestVFSOfflineJournalCommand(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	key, err := fs.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: "author", Specialist: "dev", InvariantsHash: "h"}, []string{"file"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fs.Apply(vfs.Operation{Key: key, CallID: "call", Action: vfs.OpCreate, Path: "file", Content: []byte("private content")}); err != nil {
		t.Fatal(err)
	}
	if err = fs.Close(); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err = run([]string{"vfs", "journal", "--workspace", root, "--state", state, "--session", "s", "--unit", "u", "--limit", "1"}, strings.NewReader(""), &out, &stderr); err != nil {
		t.Fatal(err)
	}
	var entries []vfs.JournalEntry
	if err = json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].CallID != "call" || entries[0].Agent != "author" {
		t.Fatalf("%s", out.String())
	}
	if strings.Contains(out.String(), "private content") || strings.Contains(out.String(), string(key)) {
		t.Fatal("journal export leaked content or binding")
	}
	if err = runVFS([]string{"recover", "--workspace", root, "--state", state}, strings.NewReader(""), &out, &stderr); err == nil {
		t.Fatal("recovery lacked explicit intent")
	}
}

func TestVFSClaimControlCommands(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)

	assigned, err := mutate("assign", vfsReq("u1", "dev", map[string]any{"scope": []string{"owned.go"}}))
	assertVFSAssigned(t, assigned, err)
	claims, err := mutate("claims", map[string]any{"ipc_version": IPCVersion, "session_id": "s1"})
	assertVFSPendingClaim(t, claims, err, assigned.Key)

	refused, err := mutate("assign", vfsReq("u2", "fix", map[string]any{"scope": []string{"owned.go"}}))
	assertVFSCollisionRefusal(t, refused, err, "dev", "s1", "u1")

	bound, err := mutate("bind", vfsReq("u1", "dev", nil))
	if err != nil || !bound.OK || bound.Key != assigned.Key {
		t.Fatalf("target adopted assigned claim = %+v, %v", bound, err)
	}
	claims, err = mutate("claims", map[string]any{"ipc_version": IPCVersion, "session_id": "s1"})
	assertVFSActiveClaim(t, claims, err)
	released, err := mutate("release", map[string]any{"ipc_version": IPCVersion, "session_id": "s1", "key": assigned.Key})
	if err != nil || !released.OK {
		t.Fatalf("release = %+v, %v", released, err)
	}
	claims, err = mutate("claims", map[string]any{"ipc_version": IPCVersion, "session_id": "s1"})
	if err != nil || len(claims.Claims) != 0 {
		t.Fatalf("claims after release = %+v, %v", claims, err)
	}
}

func assertVFSAssigned(t *testing.T, assigned response, err error) {
	t.Helper()
	if err != nil || !assigned.OK || assigned.Key == "" {
		t.Fatalf("assign = %+v, %v", assigned, err)
	}
}

func assertVFSPendingClaim(t *testing.T, claims response, err error, wantKey string) {
	t.Helper()
	if err != nil || len(claims.Claims) != 1 || !claims.Claims[0].Pending || claims.Claims[0].Active || string(claims.Claims[0].Key) != wantKey {
		t.Fatalf("pending claim inspection = %+v, %v", claims, err)
	}
}

func assertVFSActiveClaim(t *testing.T, claims response, err error) {
	t.Helper()
	if err != nil || len(claims.Claims) != 1 || claims.Claims[0].Pending || !claims.Claims[0].Active {
		t.Fatalf("active claim inspection = %+v, %v", claims, err)
	}
}

func assertVFSCollisionRefusal(t *testing.T, refused response, err error, ownerAgent vfs.AgentID, ownerSession, ownerUnit string) {
	t.Helper()
	if err != nil || refused.OK || refused.Collision == nil {
		t.Fatalf("collision refusal = %+v, %v", refused, err)
	}
	if refused.Collision.RequestedPath != "owned.go" || refused.Collision.TargetAgent != "fix" ||
		refused.Collision.OwnerAgent != ownerAgent || refused.Collision.OwnerSession != ownerSession || refused.Collision.OwnerUnit != ownerUnit {
		t.Fatalf("collision evidence = %+v", refused.Collision)
	}
}

func TestVFSAssignVerifierCommand(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)

	author, err := mutate("assign", vfsReq("u1", "dev", map[string]any{"scope": []string{"owned.go"}}))
	if err != nil || !author.OK || author.Key == "" {
		t.Fatalf("author assignment = %+v, %v", author, err)
	}
	request := vfsReq("u1", "verify", map[string]any{"author_key": author.Key})
	assigned, err := mutate("assign-verifier", request)
	if err != nil || !assigned.OK || assigned.Key == "" {
		t.Fatalf("assign verifier = %+v, %v", assigned, err)
	}

	for _, tc := range []struct {
		name  string
		extra map[string]any
	}{
		{name: "missing author key"},
		{name: "invalid author key", extra: map[string]any{"author_key": "not-a-binding-key"}},
		{name: "non-empty scope", extra: map[string]any{"author_key": author.Key, "scope": []string{"other.go"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := vfsReq("u1", "verify", map[string]any{})
			for key, value := range tc.extra {
				req[key] = value
			}
			if _, err := mutate("assign-verifier", req); err == nil {
				t.Fatal("invalid verifier assignment was accepted")
			}
		})
	}
}

// newVFSMutator returns a caller of the mutation IPC against one workspace and
// state directory, one process-equivalent invocation per call.
func newVFSMutator(t *testing.T, root, state string) func(command string, req map[string]any) (response, error) {
	t.Helper()
	return func(command string, req map[string]any) (response, error) {
		payload, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		err = runVFS([]string{command, "--workspace", root, "--state", state}, bytes.NewReader(payload), &out, &stderr)
		if err != nil {
			return response{}, fmt.Errorf("%s: %w (%s)", command, err, stderr.String())
		}
		var resp response
		if err = json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp, nil
	}
}

// vfsReq builds a request as the plugin does: identity of one dispatch plus
// operation fields. The agent is also its catalog instance, as in the plugin.
func vfsReq(unit, agent string, extra map[string]any) map[string]any {
	req := map[string]any{
		"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": unit,
		"agent_id": agent, "specialist": agent,
	}
	for k, v := range extra {
		req[k] = v
	}
	return req
}

// TestVFSMutationIPC drives the full governed write path the OpenCode plugin
// uses: bind, staged create, out-of-scope denial, verifier inspect, verdict,
// checkpoint consolidation, and physical flush.
func TestVFSMutationIPC(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)

	bind := map[string]any{
		"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": "u1",
		"agent_id": "author", "specialist": "dev",
		"scope": []string{"src/app.go"}, "invariants": []string{"AGENTS.md"},
	}
	bound, err := mutate("bind", bind)
	if err != nil {
		t.Fatal(err)
	}
	// The attempt and the invariant set's version are issued here, not declared.
	if !bound.OK || bound.Key == "" || bound.AttemptID != "1" || !strings.HasPrefix(bound.InvariantsVersion, "inv1:") {
		t.Fatalf("bind: %+v", bound)
	}

	created, err := mutate("op", map[string]any{
		"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": "u1",
		"agent_id": "author", "specialist": "dev",
		"author_key": bound.Key, "call_id": "c1", "expected_revision": 0,
		"action": "create", "path": "src/app.go", "content": "package main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.OK || created.Revision != 1 || created.DeltaHash == "" {
		t.Fatalf("create: %+v", created)
	}

	// Out-of-scope mutation must be denied and never reach disk.
	if _, err = mutate("op", map[string]any{
		"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": "u1",
		"agent_id": "author", "specialist": "dev",
		"author_key": bound.Key, "call_id": "c2", "expected_revision": 1,
		"action": "create", "path": "escape.go", "content": "x",
	}); err == nil {
		t.Fatal("out-of-scope create was allowed")
	}
	if _, err = os.Stat(filepath.Join(root, "escape.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("denied mutation reached disk")
	}

	vbound, err := mutate("assign-verifier", map[string]any{
		"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": "u1",
		"agent_id": "verif", "specialist": "verify", "author_key": bound.Key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mutate("bind", map[string]any{
		"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": "u1",
		"agent_id": "verif", "specialist": "verify", "author_key": bound.Key,
	}); err != nil {
		t.Fatal(err)
	}
	// Knowing the author's author_key is not owning it: a different specialist
	// naming it must be rejected, not silently operate under the author's
	// identity.
	if _, err = mutate("op", map[string]any{
		"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": "u1",
		"agent_id": "verif", "specialist": "verify",
		"author_key": bound.Key, "call_id": "v0", "expected_revision": 1,
		"action": "read", "path": "src/app.go",
	}); err == nil || !errors.Is(err, vfs.ErrIdentity) {
		t.Fatalf("op with another identity's author_key: %v", err)
	}

	if _, err = mutate("verify", map[string]any{
		"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": "u1",
		"agent_id": "verif", "specialist": "verify",
		"verifier_key": vbound.Key, "author_key": bound.Key, "call_id": "v1",
		"expected_revision": 1, "delta_hash": created.DeltaHash, "pass": true,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err = mutate("consolidate", map[string]any{
		"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": "u1",
		"agent_id": "author", "specialist": "dev",
		"author_key": bound.Key, "checkpoint": "cp1", "expected_revision": 1,
	}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(root, "src", "app.go"))
	if err != nil || string(content) != "package main" {
		t.Fatalf("consolidated file missing or wrong: %q, %v", content, err)
	}
}

// TestVFSIPCVersionMismatch rejects requests whose wire version differs so an
// old plugin cannot drive a new binary with guessed semantics.
func TestVFSIPCVersionMismatch(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	var out, stderr bytes.Buffer
	payload := `{"ipc_version":99,"session_id":"s1","work_unit_id":"u1","agent_id":"author","specialist":"dev"}`
	if err := runVFS([]string{"bind", "--workspace", root, "--state", state}, strings.NewReader(payload), &out, &stderr); err == nil {
		t.Fatal("version mismatch was accepted")
	} else if !strings.Contains(err.Error(), "ipc_version 99") {
		t.Fatalf("unhelpful version error: %v", err)
	}
}

// TestVFSBindRejectsUncatalogedIdentity refuses a bind for an identity that is
// not a catalog instance, and does so before any store is created.
func TestVFSBindRejectsUncatalogedIdentity(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	// "takt" is the orchestrator: registered in opencode.json but not bindable.
	for _, agent := range []string{"made-up-specialist", "takt", "pm"} {
		_, err := mutate("bind", vfsReq("u1", agent, map[string]any{"scope": []string{"a.go"}}))
		if err == nil {
			t.Fatalf("bind as %q was accepted", agent)
		}
		if !strings.Contains(err.Error(), agent) {
			t.Fatalf("error does not name the rejected identity %q: %v", agent, err)
		}
	}
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected bind opened the store: %v", err)
	}
}

// TestVFSVerifyCallerMustOwnVerifierKey lets a verdict through only from the
// identity its verifier_key was issued to.
func TestVFSVerifyCallerMustOwnVerifierKey(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	author, err := mutate("bind", vfsReq("u1", "dev", map[string]any{"scope": []string{"a.go"}}))
	if err != nil {
		t.Fatal(err)
	}
	created, err := mutate("op", vfsReq("u1", "dev", map[string]any{
		"author_key": author.Key, "call_id": "c1", "expected_revision": 0, "action": "create", "path": "a.go", "content": "x",
	}))
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
	verify := func(caller, call string) error {
		_, err := mutate("verify", vfsReq("u1", caller, map[string]any{
			"verifier_key": verifier.Key, "author_key": author.Key, "call_id": call,
			"expected_revision": 1, "delta_hash": created.DeltaHash, "pass": true,
		}))
		return err
	}
	// Another instance, and the author itself, present the verifier's key.
	for i, caller := range []string{"fix", "dev"} {
		if err = verify(caller, fmt.Sprintf("bad%d", i)); err == nil || !strings.Contains(err.Error(), "verifier_key") {
			t.Fatalf("verify as %q: %v", caller, err)
		}
	}
	// The right identity in another unit is not the binding's identity either.
	if _, err = mutate("verify", vfsReq("u2", "verify", map[string]any{
		"verifier_key": verifier.Key, "author_key": author.Key, "call_id": "bad-unit",
		"expected_revision": 1, "delta_hash": created.DeltaHash, "pass": true,
	})); err == nil {
		t.Fatal("verify from another unit was accepted")
	}
	if err = verify("verify", "ok"); err != nil {
		t.Fatalf("verify by the bound caller: %v", err)
	}
}

// TestVFSUnitsOfOneSessionAreSeparate keeps two units of one session apart in
// journal and ownership, and lets one unit's collision block only that unit.
func TestVFSUnitsOfOneSessionAreSeparate(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	units := map[string]*separateSessionUnit{"u1": {path: "a.go"}, "u2": {path: "b.go"}}
	for id, u := range units {
		bindUnitAuthorAndVerifier(t, mutate, id, u)
	}
	// u2 reaches for a path u1 owns: a collision inside u2 only.
	if _, err := mutate("op", vfsReq("u2", "dev", map[string]any{
		"author_key": units["u2"].author.Key, "call_id": "trespass", "expected_revision": 1, "action": "create", "path": "a.go", "content": "x",
	})); err == nil {
		t.Fatal("u2 wrote a path owned by u1")
	}
	for id, u := range units {
		if _, err := mutate("verify", vfsReq(id, "verify", map[string]any{
			"verifier_key": u.verifier.Key, "author_key": u.author.Key, "call_id": "v-" + id,
			"expected_revision": 1, "delta_hash": u.delta, "pass": true,
		})); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mutate("consolidate", vfsReq("u1", "dev", map[string]any{
		"author_key": units["u1"].author.Key, "checkpoint": "cp", "expected_revision": 1,
	})); err != nil {
		t.Fatalf("u1 blocked by u2's collision: %v", err)
	}
	if _, err := mutate("consolidate", vfsReq("u2", "dev", map[string]any{
		"author_key": units["u2"].author.Key, "checkpoint": "cp", "expected_revision": 1,
	})); err == nil || !strings.Contains(err.Error(), "unresolved collision") {
		t.Fatalf("u2 consolidation over its own unresolved collision: %v", err)
	}
	for id, u := range units {
		assertUnitJournalIsolated(t, root, state, id, u)
	}
	if content, err := os.ReadFile(filepath.Join(root, "a.go")); err != nil || string(content) != "u1" {
		t.Fatalf("u1 not consolidated: %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(root, "b.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("u2 reached disk despite its collision")
	}
}

// separateSessionUnit is one unit's fixture in
// TestVFSUnitsOfOneSessionAreSeparate: its author and verifier bindings, the
// delta hash it staged, and the path it owns.
type separateSessionUnit struct {
	author, verifier response
	delta            string
	path             string
}

// bindUnitAuthorAndVerifier stages id's create and binds both its author and
// verifier claims, recording the delta hash TestVFSUnitsOfOneSessionAreSeparate
// verifies against.
func bindUnitAuthorAndVerifier(t *testing.T, mutate func(string, map[string]any) (response, error), id string, u *separateSessionUnit) {
	t.Helper()
	var err error
	if u.author, err = mutate("bind", vfsReq(id, "dev", map[string]any{"scope": []string{u.path}})); err != nil {
		t.Fatal(err)
	}
	created, err := mutate("op", vfsReq(id, "dev", map[string]any{
		"author_key": u.author.Key, "call_id": "w-" + id, "expected_revision": 0, "action": "create", "path": u.path, "content": id,
	}))
	if err != nil {
		t.Fatal(err)
	}
	u.delta = created.DeltaHash
	if u.verifier, err = mutate("assign-verifier", vfsReq(id, "verify", map[string]any{"author_key": u.author.Key})); err != nil {
		t.Fatal(err)
	}
	if _, err = mutate("bind", vfsReq(id, "verify", map[string]any{"author_key": u.author.Key})); err != nil {
		t.Fatal(err)
	}
}

// assertUnitJournalIsolated checks that id's journal holds only its own
// entries (no foreign unit or path leaks in) and contains its create.
func assertUnitJournalIsolated(t *testing.T, root, state, id string, u *separateSessionUnit) {
	t.Helper()
	var out, stderr bytes.Buffer
	if err := run([]string{"vfs", "journal", "--workspace", root, "--state", state, "--session", "s1", "--unit", id}, strings.NewReader(""), &out, &stderr); err != nil {
		t.Fatal(err)
	}
	var entries []vfs.JournalEntry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	created := false
	for _, e := range entries {
		created = created || (e.Path == u.path && e.Operation == vfs.OpCreate)
		if e.WorkUnitID != id || (e.Path != "" && e.Path != u.path) {
			t.Fatalf("unit %s journal holds a foreign entry: %+v", id, e)
		}
	}
	if !created {
		t.Fatalf("unit %s journal lacks its create: %s", id, out.String())
	}
}

// TestVFSGateChainAsThePluginDrivesIt runs the exact sequence the OpenCode
// plugin sends, with the delegations' admissions in between: the orchestrator
// assigns the author's scope, the author binds and stages, the verifier runs as
// its own unit linked only by the author key, and the orchestrator consolidates.
// Every request carries the invariants the plugin declares.
func TestVFSGateChainAsThePluginDrivesIt(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	invariants := []string{"AGENTS.md"}
	delegate := func(action, unit, agent string) {
		t.Helper()
		b, err := json.Marshal(coordinationRequest{Action: action, Event: unit, Session: "s1", Agent: agent, Dispatch: "call-" + unit})
		if err != nil {
			t.Fatal(err)
		}
		var out, errout bytes.Buffer
		if err = runDispatch([]string{"--workspace", root, "--state", state, "--request", string(b)}, &out, &errout); err != nil {
			t.Fatalf("%s %s: %v (%s)", action, unit, err, errout.String())
		}
	}

	claim, err := mutate("assign", vfsReq("impl", "dev", map[string]any{"scope": []string{"app.go"}, "invariants": invariants}))
	if err != nil {
		t.Fatal(err)
	}
	delegate("admit", "impl", "dev")
	author, err := mutate("bind", vfsReq("impl", "dev", map[string]any{"scope": []string{"app.go"}, "invariants": invariants}))
	if err != nil || author.Key != claim.Key {
		t.Fatalf("author adopted its claim = %+v, %v", author, err)
	}
	staged, err := mutate("op", vfsReq("impl", "dev", map[string]any{
		"author_key": author.Key, "call_id": "w1", "expected_revision": 0,
		"action": "create", "path": "app.go", "content": "package app",
	}))
	if err != nil {
		t.Fatal(err)
	}
	delegate("finish", "impl", "dev")

	gate, err := mutate("assign-verifier", vfsReq("gate", "verify", map[string]any{"author_key": author.Key, "invariants": invariants}))
	if err != nil {
		t.Fatal(err)
	}
	delegate("admit", "gate", "verify")
	verifier, err := mutate("bind", vfsReq("gate", "verify", map[string]any{"author_key": author.Key, "invariants": invariants}))
	if err != nil || verifier.Key != gate.Key {
		t.Fatalf("verifier adopted its gate = %+v, %v", verifier, err)
	}
	read, err := mutate("op", vfsReq("gate", "verify", map[string]any{
		"author_key": verifier.Key, "view_key": author.Key, "call_id": "r1",
		"expected_revision": staged.Revision, "action": "read", "path": "app.go",
	}))
	if err != nil || read.Content != "package app" {
		t.Fatalf("verifier read = %+v, %v", read, err)
	}
	if _, err = mutate("verify", vfsReq("gate", "verify", map[string]any{
		"verifier_key": verifier.Key, "author_key": author.Key, "call_id": "v1",
		"expected_revision": staged.Revision, "delta_hash": staged.DeltaHash, "pass": true, "finding": "ok",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err = mutate("consolidate", vfsReq("impl", "dev", map[string]any{
		"author_key": author.Key, "checkpoint": "gate", "expected_revision": staged.Revision,
	})); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "app.go")); err != nil || string(got) != "package app" {
		t.Fatalf("consolidated app.go = %q, %v", got, err)
	}
}

// TestVFSDiscardReleasesRetainedWork discards a failed author's staged work,
// the orchestrator's decision, so its paths can be assigned again.
func TestVFSDiscardReleasesRetainedWork(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	author, err := mutate("bind", vfsReq("u1", "dev", map[string]any{"scope": []string{"a.go"}}))
	if err != nil {
		t.Fatal(err)
	}
	if refused, err := mutate("assign", vfsReq("u2", "fix", map[string]any{"scope": []string{"a.go"}})); err != nil || refused.OK {
		t.Fatalf("retained work's path was assigned again: %+v, %v", refused, err)
	}
	if _, err = mutate("op", vfsReq("u1", "dev", map[string]any{
		"author_key": author.Key, "call_id": "d1", "expected_revision": 0, "action": "rollback",
	})); err != nil {
		t.Fatal(err)
	}
	if assigned, err := mutate("assign", vfsReq("u2", "fix", map[string]any{"scope": []string{"a.go"}})); err != nil || !assigned.OK {
		t.Fatalf("discarded work still holds its path: %+v, %v", assigned, err)
	}
}

// Maintenance attribution is harness authority, not an ordinary bind argument.
func TestVFSIPCRejectsForgedCycleAttribution(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	req := map[string]any{"ipc_version": IPCVersion, "session_id": "s1", "work_unit_id": "u1", "agent_id": "maint", "specialist": "dev", "scope": []string{"a.go"}, "cycle_id": "forged", "mandate_class": "dead-code"}
	if _, err := mutate("bind", req); err == nil {
		t.Fatal("ordinary bind forged maintenance attribution")
	}
	delete(req, "cycle_id")
	delete(req, "mandate_class")
	if _, err := mutate("bind", req); err != nil {
		t.Fatal(err)
	}
}

// TestVFSVerifyIgnoresCycleAttributionButNotIdentity: a verifier bound with
// cycle attribution may verify without repeating it, while a caller whose real
// identity differs is still refused even when its cycle fields match.
func TestVFSVerifyIgnoresCycleAttributionButNotIdentity(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	mutate := newVFSMutator(t, root, state)
	cycle := map[string]any{"cycle_id": "cycle-7", "mandate_class": "dead-code"}
	author, err := mutate("bind", vfsReq("u1", "dev", map[string]any{"scope": []string{"a.go"}}))
	if err != nil {
		t.Fatal(err)
	}
	created, err := mutate("op", vfsReq("u1", "dev", map[string]any{
		"author_key": author.Key, "call_id": "c1", "expected_revision": 0, "action": "create", "path": "a.go", "content": "x",
	}))
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
	verify := func(caller, call string, extra map[string]any) error {
		req := map[string]any{
			"verifier_key": verifier.Key, "author_key": author.Key, "call_id": call,
			"expected_revision": 1, "delta_hash": created.DeltaHash, "pass": true,
		}
		for k, v := range extra {
			req[k] = v
		}
		_, err := mutate("verify", vfsReq("u1", caller, req))
		return err
	}
	if err = verify("verify", "bad", cycle); err == nil || !strings.Contains(err.Error(), "maintenance cycle") {
		t.Fatalf("verify with forged cycle fields accepted: %v", err)
	}
	if err = verify("verify", "ok", nil); err != nil {
		t.Fatalf("verify without repeating cycle fields: %v", err)
	}
}
