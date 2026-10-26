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
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
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

// Root sessions of the fixtures: the one that stages work and the one that
// carries it on.
const (
	rootA = "s1"
	rootB = "s2"
)

// parallelInvocations is how many CLI invocations race on one workspace: one
// per delegation the harness admits without a committed plan.
const parallelInvocations = 4

// vfsHost is the host session a delegation of agent on unit runs in.
func vfsHost(agent, unit string) string { return "host/" + agent + "/" + unit }

// vfsReqIn builds a request as the plugin does for agent's delegation on unit
// of root: the harness-delivered caller block plus the command's fields.
func vfsReqIn(root, unit, agent string, extra map[string]any) map[string]any {
	req := map[string]any{"ipc_version": IPCVersion, "session_id": root, "caller_session": vfsHost(agent, unit),
		"caller_agent": agent, "work_unit_id": unit}
	maps.Copy(req, extra)
	return req
}

// vfsReq is vfsReqIn for root rootA.
func vfsReq(unit, agent string, extra map[string]any) map[string]any {
	return vfsReqIn(rootA, unit, agent, extra)
}

// orchReq is a request from root's orchestrator: its host session is the root
// itself, which the plugin also derives as its unit.
func orchReq(root string, extra map[string]any) map[string]any {
	req := map[string]any{"ipc_version": IPCVersion, "session_id": root, "caller_session": root,
		"caller_agent": vfs.OrchestratorInstance, "work_unit_id": root}
	maps.Copy(req, extra)
	return req
}

// claimReq is root's orchestrator naming agent, its own specialist, on unit as
// the target of a claim command.
func claimReq(root, unit, agent string, extra map[string]any) map[string]any {
	req := orchReq(root, map[string]any{"work_unit_id": unit, "agent_id": agent, "specialist": agent})
	maps.Copy(req, extra)
	return req
}

// invokeVFS runs one vfs command against a workspace, one process-equivalent
// invocation, decoding its answer into out.
func invokeVFS(root, state, command string, req map[string]any, out any) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	if err = runVFS([]string{command, "--workspace", root, "--state", state}, bytes.NewReader(payload), &stdout, &stderr); err != nil {
		return fmt.Errorf("%s: %w (%s)", command, err, stderr.String())
	}
	return json.Unmarshal(stdout.Bytes(), out)
}

// newVFSMutator returns a caller of the mutation IPC against one workspace and
// state directory.
func newVFSMutator(t *testing.T, root, state string) func(command string, req map[string]any) (response, error) {
	t.Helper()
	return func(command string, req map[string]any) (response, error) {
		if command == "assign" {
			p, e := history.ReadProjection(state)
			if e != nil {
				return response{}, e
			}
			root, _ := req["session_id"].(string)
			unit, _ := req["work_unit_id"].(string)
			if _, declared := p.Inputs[history.UnitKey(root, unit)]; !declared {
				h, e := history.Open(state)
				if e != nil {
					return response{}, e
				}
				ids := []int64{}
				if refs, ok := req["invariants"].([]string); ok {
					for _, ref := range refs {
						if strings.HasPrefix(ref, "engram:") {
							var id int64
							if _, err := fmt.Sscanf(ref, "engram:%d", &id); err != nil {
								t.Errorf("invariant reference %q: %v", ref, err)
							}
							ids = append(ids, id)
						}
					}
				}
				e = h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInputs, SessionID: root, WorkUnitID: unit, ResultIDs: ids, None: len(ids) == 0, Cause: history.CauseNone})
				_ = h.Close()
				if e != nil {
					return response{}, e
				}
			}
		}
		var resp response
		err := invokeVFS(root, state, command, req, &resp)
		return resp, err
	}
}

// vfsFixture drives one workspace as the plugin and the dispatch hook do.
type vfsFixture struct {
	t           *testing.T
	root, state string
	mutate      func(string, map[string]any) (response, error)
	dispatches  map[string]int
}

func newVFSFixture(t *testing.T) *vfsFixture {
	t.Helper()
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "private")
	return &vfsFixture{t: t, root: root, state: state, mutate: newVFSMutator(t, root, state), dispatches: map[string]int{}}
}

// must runs a command that has to succeed.
func (fx *vfsFixture) must(command string, req map[string]any) response {
	fx.t.Helper()
	resp, err := fx.mutate(command, req)
	if err != nil || !resp.OK {
		fx.t.Fatalf("%s = %+v, %v", command, resp, err)
	}
	return resp
}

// dispatch records a delegation's admission or finish in the execution
// history through the dispatch command.
func (fx *vfsFixture) dispatch(action, root, unit, agent string) {
	fx.t.Helper()
	if action == "admit" {
		fx.dispatches[root+"/"+unit]++
	}
	b, err := json.Marshal(coordinationRequest{Action: action, Event: unit, Session: root, Agent: agent,
		Dispatch: fmt.Sprintf("call-%s-%s-%d", root, unit, fx.dispatches[root+"/"+unit])})
	if err != nil {
		fx.t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if err = runDispatch([]string{"--workspace", fx.root, "--state", fx.state, "--request", string(b)}, &out, &errout); err != nil {
		fx.t.Fatalf("%s %s: %v (%s)", action, unit, err, errout.String())
	}
	if action == "admit" {
		h, e := history.Open(fx.state)
		if e != nil {
			fx.t.Fatal(e)
		}
		defer func() {
			if err := h.Close(); err != nil {
				fx.t.Error(err)
			}
		}()
		u, _ := h.Project().Unit(root, unit)
		if e = h.Append(history.Entry{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: root, WorkUnitID: unit, AttemptID: u.AttemptID, Dispatch: u.Dispatch, Agent: agent, Caller: root, Cause: history.CauseNone}); e != nil {
			fx.t.Fatal(e)
		}
		if e = h.ExecutionFact(h.Project().Executions[u.Dispatch], history.KindPrepared, "", "", nil); e != nil {
			fx.t.Fatal(e)
		}
		if _, e = h.AttachChild(u.Dispatch, vfsHost(agent, unit), agent); e != nil {
			fx.t.Fatal(e)
		}
	}

}

// delegate assigns scope to agent on root's unit, admits the unit and binds it
// from its delegation, returning the key.
func (fx *vfsFixture) delegate(root, unit, agent string, scope ...string) string {
	fx.t.Helper()
	claim := fx.must("assign", claimReq(root, unit, agent, map[string]any{"scope": scope}))
	fx.dispatch("admit", root, unit, agent)
	bound := fx.must("bind", vfsReqIn(root, unit, agent, map[string]any{"scope": scope}))
	if bound.Key != claim.Key {
		fx.t.Fatalf("bind = %s; want the assigned %s", bound.Key, claim.Key)
	}
	return bound.Key
}

// adoptGates adopts the gates of agent's delegation on root's unit.
func (fx *vfsFixture) adoptGates(root, unit, agent string) ([]vfs.AdoptedGate, error) {
	var resp gatesResponse
	err := invokeVFS(fx.root, fx.state, "adopt-gates", vfsReqIn(root, unit, agent, nil), &resp)
	return resp.Gates, err
}

// judge prepares the native delegation and adopts its core-issued gates.
func (fx *vfsFixture) judge(root, unit, agent string, authors ...string) []vfs.AdoptedGate {
	fx.t.Helper()
	if _, err := executionRequest(fx.t, fx, coordinationRequest{Action: "inputs", Session: root, Event: unit}); err != nil {
		fx.t.Fatal(err)
	}
	fx.dispatches[root+"/"+unit]++
	r := coordinationRequest{Action: "prepare", Owner: testOwner, Session: root, Event: unit, Agent: agent, Caller: root,
		Dispatch: fmt.Sprintf("call-%s-%s-%d", root, unit, fx.dispatches[root+"/"+unit]), Child: vfsHost(agent, unit)}
	for _, author := range authors {
		r.AuthorKeys = append(r.AuthorKeys, vfs.AgentID(author))
	}
	if _, err := executionRequest(fx.t, fx, r); err != nil {
		fx.t.Fatal(err)
	}
	gates, err := fx.adoptGates(root, unit, agent)
	if err != nil || len(gates) != len(authors) {
		fx.t.Fatalf("adopt-gates = %+v, %v; want %d gates", gates, err, len(authors))
	}
	for _, gate := range gates {
		if gate.Error != "" {
			fx.t.Fatalf("gate over %s: %s", gate.AuthorKey, gate.Error)
		}
	}
	return gates
}

// write stages content at path through the binding of agent's delegation.
func (fx *vfsFixture) write(root, unit, agent, call, path, content string) response {
	fx.t.Helper()
	return fx.must("op", vfsReqIn(root, unit, agent, map[string]any{"call_id": call, "action": "create", "path": path, "content": content}))
}

// read reads path through agent's delegation, naming author when given.
func (fx *vfsFixture) read(root, unit, agent, call, path, author string) response {
	fx.t.Helper()
	fields := map[string]any{"call_id": call, "action": "read", "path": path}
	if author != "" {
		fields["author_key"] = author
	}
	return fx.must("op", vfsReqIn(root, unit, agent, fields))
}

// verify attaches agent's verdict over author.
func (fx *vfsFixture) verify(root, unit, agent, call, author string, pass bool) error {
	_, err := fx.mutate("verify", vfsReqIn(root, unit, agent, map[string]any{"call_id": call, "author_key": author, "pass": pass, "finding": "checked"}))
	return err
}

// disk is the workspace content at rel.
func (fx *vfsFixture) disk(rel string) string {
	content, err := os.ReadFile(filepath.Join(fx.root, rel))
	if err != nil {
		return ""
	}
	return string(content)
}

func refusedWith(t *testing.T, what string, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s was accepted", what)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s = %v; want it to name %q", what, err, want)
		}
	}
}

func TestVFSClaimControlCommands(t *testing.T) {
	fx := newVFSFixture(t)
	assigned, err := fx.mutate("assign", claimReq(rootA, "u1", "dev", map[string]any{"scope": []string{"owned.go"}}))
	assertVFSAssigned(t, assigned, err)
	claims, err := fx.mutate("claims", orchReq(rootA, nil))
	assertVFSPendingClaim(t, claims, err, assigned.Key)

	refused, err := fx.mutate("assign", claimReq(rootA, "u2", "fix", map[string]any{"scope": []string{"owned.go"}}))
	assertVFSCollisionRefusal(t, refused, err, "dev", rootA, "u1")

	fx.dispatch("admit", rootA, "u1", "dev")
	if bound := fx.must("bind", vfsReq("u1", "dev", nil)); bound.Key != assigned.Key {
		t.Fatalf("target adopted %s; want the assigned %s", bound.Key, assigned.Key)
	}
	claims, err = fx.mutate("claims", orchReq(rootA, nil))
	assertVFSActiveClaim(t, claims, err)
	_, err = fx.mutate("release", orchReq(rootA, map[string]any{"key": assigned.Key}))
	refusedWith(t, "release of a claim whose unit is running", err, "dispatch_abandon dispatch ", "claim_release")
	fx.dispatch("abandon", rootA, "u1", "dev")
	fx.must("release", orchReq(rootB, map[string]any{"key": assigned.Key}))
	if claims = fx.must("claims", orchReq(rootA, nil)); len(claims.Claims) != 0 {
		t.Fatalf("claims after release = %+v", claims.Claims)
	}
	_, err = fx.mutate("op", vfsReq("u1", "dev", map[string]any{"call_id": "late", "action": "create", "path": "owned.go", "content": "x"}))
	refusedWith(t, "a write through a released key", err)
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

func TestVFSAssignWithAuthorKeyAndPartialScopeReturnsTheNewKey(t *testing.T) {
	fx := newVFSFixture(t)
	key := fx.delegate(rootA, "u1", "dev", "a.go", "b.go")
	fx.write(rootA, "u1", "dev", "c1", "a.go", "A")
	fx.write(rootA, "u1", "dev", "c2", "b.go", "B")
	handed := fx.must("assign", claimReq(rootA, "u1", "fix", map[string]any{"scope": []string{"a.go"}, "author_key": key}))
	if handed.Key == "" || handed.Key == key {
		t.Fatalf("hand-over key = %q; want a new key distinct from %q", handed.Key, key)
	}
	byKey := map[string][]string{}
	for _, claim := range fx.must("claims", orchReq(rootA, nil)).Claims {
		byKey[string(claim.Key)] = claim.Scope
	}
	if !slices.Equal(byKey[handed.Key], []string{"a.go"}) || !slices.Equal(byKey[key], []string{"b.go"}) {
		t.Fatalf("claims = %+v; want a.go under the new key and b.go left with %s", byKey, key)
	}
}

func TestVFSAssignWithAuthorKeyKeepsStagedWork(t *testing.T) {
	fx := newVFSFixture(t)
	key := fx.delegate(rootA, "u1", "dev", "a.go")
	fx.write(rootA, "u1", "dev", "c1", "a.go", "partial")
	_, err := fx.mutate("release", orchReq(rootA, map[string]any{"key": key}))
	refusedWith(t, "release of staged work", err, vfs.ErrStagedWork.Error())
	if retry := fx.must("assign", claimReq(rootA, "u1", "dev", map[string]any{"scope": []string{"a.go", "b.go"}, "author_key": key})); retry.Key != key {
		t.Fatalf("reassign = %s; want the same author key %s", retry.Key, key)
	}
	claims := fx.must("claims", orchReq(rootA, nil))
	if len(claims.Claims) != 1 || !claims.Claims[0].Staged || !claims.Claims[0].Pending ||
		!slices.Equal(claims.Claims[0].Scope, []string{"a.go", "b.go"}) {
		t.Fatalf("claims after reassign = %+v", claims.Claims)
	}
}

func TestVerifierPreparationAssignsOnlyValidatedGates(t *testing.T) {
	fx := newVFSFixture(t)
	author := fx.delegate(rootA, "u1", "dev", "owned.go")
	_, err := fx.mutate("assign-verifier", claimReq(rootA, "review", "verify", map[string]any{"author_key": author}))
	refusedWith(t, "the retired standalone gate writer", err, "unknown vfs command")
	if _, err = executionRequest(t, fx, coordinationRequest{Action: "inputs", Session: rootA, Event: "review"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		authors []vfs.AgentID
	}{
		{name: "empty author keys", authors: []vfs.AgentID{}},
		{name: "invalid author key", authors: []vfs.AgentID{"not-a-binding-key"}},
		{name: "duplicate author key", authors: []vfs.AgentID{vfs.AgentID(author), vfs.AgentID(author)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The plugin sends author_keys as given, an empty list included.
			data, err := json.Marshal(map[string]any{"action": "prepare", "session": rootA, "event": "review", "agent": "verify", "caller": rootA, "owner": testOwner, "dispatch": "review-" + tc.name, "author_keys": tc.authors})
			if err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if err := runDispatch([]string{"--workspace", fx.root, "--state", fx.state, "--request", string(data)}, &out, &stderr); err == nil {
				t.Fatal("invalid verifier preparation was accepted")
			}
		})
	}
	fx.judge(rootA, "review", "verify", author)
}

// TestVFSMutationIPC drives the full governed write path the OpenCode plugin
// uses: bind, staged create, out-of-scope denial, gate read, verdict,
// orchestrator consolidation, and physical flush.
func TestVFSMutationIPC(t *testing.T) {
	fx := newVFSFixture(t)
	fx.dispatch("admit", rootA, "u1", "dev")
	bound := fx.must("bind", vfsReq("u1", "dev", map[string]any{"scope": []string{"src/app.go"}, "invariants": []string{"AGENTS.md"}}))
	// The attempt and the invariant set's version are issued here, not declared.
	if bound.Key == "" || bound.AttemptID != "1" || bound.InvariantsVersion == "" {
		t.Fatalf("bind: %+v", bound)
	}
	created := fx.write(rootA, "u1", "dev", "c1", "src/app.go", "package main")
	if *created.Revision != 1 || created.DeltaHash == "" {
		t.Fatalf("create: %+v", created)
	}
	// Out-of-scope mutation is denied and never reaches disk.
	_, err := fx.mutate("op", vfsReq("u1", "dev", map[string]any{"call_id": "c2", "action": "create", "path": "escape.go", "content": "x"}))
	refusedWith(t, "out-of-scope create", err)
	if _, err = os.Stat(filepath.Join(fx.root, "escape.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("denied mutation reached disk")
	}
	fx.judge(rootA, "review", "verify", bound.Key)
	// Knowing the author's key is not holding it: the judge reads through its
	// gate, and the author's own key names no binding the judge holds.
	if read := fx.read(rootA, "review", "verify", "r1", "src/app.go", bound.Key); read.Content != "package main" {
		t.Fatalf("gate read = %+v", read)
	}
	_, err = fx.mutate("op", vfsReq("review", "verify", map[string]any{"call_id": "w-judge", "action": "create", "path": "src/app.go", "content": "x"}))
	refusedWith(t, "a judge writing", err, "vfs_bind")
	if err = fx.verify(rootA, "review", "verify", "v1", bound.Key, true); err != nil {
		t.Fatal(err)
	}
	_, err = fx.mutate("consolidate", vfsReq("u1", "dev", map[string]any{"author_key": bound.Key}))
	refusedWith(t, "consolidation by the author", err, "only the orchestrator")
	fx.must("consolidate", orchReq(rootA, map[string]any{"author_key": bound.Key, "checkpoint": "cp1"}))
	if got := fx.disk("src/app.go"); got != "package main" {
		t.Fatalf("consolidated file = %q", got)
	}
}

// TestVFSIPCVersionMismatch rejects requests whose wire version differs so an
// old plugin cannot drive a new binary with guessed semantics.
func TestVFSIPCVersionMismatch(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	var out, stderr bytes.Buffer
	payload := `{"ipc_version":4,"session_id":"s1","caller_session":"s1","caller_agent":"dev","work_unit_id":"u1"}`
	err := runVFS([]string{"bind", "--workspace", root, "--state", state}, strings.NewReader(payload), &out, &stderr)
	refusedWith(t, "a v4 request", err, "ipc_version 4", "setup sync")
}

// TestVFSBindRejectsUncatalogedIdentity refuses a bind for an identity that is
// not a catalog instance, and does so before any store is created.
func TestVFSBindRejectsUncatalogedIdentity(t *testing.T) {
	fx := newVFSFixture(t)
	// "takt" is the orchestrator: registered in opencode.json but not bindable.
	for _, agent := range []string{"made-up-specialist", "takt", "pm"} {
		_, err := fx.mutate("bind", vfsReq("u1", agent, map[string]any{"scope": []string{"a.go"}}))
		refusedWith(t, "bind as "+agent, err, agent)
	}
	if _, err := os.Stat(fx.state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected bind opened the store: %v", err)
	}
}

// TestVFSVerifyOnlyThroughAHeldGate lets a verdict through only from the host
// session holding the gate over the author.
func TestVFSVerifyOnlyThroughAHeldGate(t *testing.T) {
	fx := newVFSFixture(t)
	author := fx.delegate(rootA, "u1", "dev", "a.go")
	fx.write(rootA, "u1", "dev", "c1", "a.go", "x")
	fx.judge(rootA, "review", "verify", author)
	fx.read(rootA, "review", "verify", "r1", "a.go", "")
	for _, who := range []struct{ unit, agent string }{{"u1", "dev"}, {"review", "fix"}, {"elsewhere", "verify"}} {
		refusedWith(t, "verify by "+who.agent+" in "+who.unit, fx.verify(rootA, who.unit, who.agent, "bad-"+who.unit+who.agent, author, true), "is not one of your gates")
	}
	if err := fx.verify(rootA, "review", "verify", "ok", author, true); err != nil {
		t.Fatalf("verify by the gate's holder: %v", err)
	}
}

// TestVFSUnitsOfOneSessionAreSeparate keeps two units of one session apart in
// journal and ownership; a refused operation blocks nothing afterwards.
func TestVFSUnitsOfOneSessionAreSeparate(t *testing.T) {
	fx := newVFSFixture(t)
	units := map[string]*separateSessionUnit{"u1": {path: "a.go"}, "u2": {path: "b.go"}}
	for id, u := range units {
		u.author = fx.delegate(rootA, id, "dev", u.path)
		fx.write(rootA, id, "dev", "w-"+id, u.path, id)
	}
	// u2 reaches for a path u1 owns: only that operation is refused.
	_, err := fx.mutate("op", vfsReq("u2", "dev", map[string]any{"call_id": "trespass", "action": "create", "path": "a.go", "content": "x"}))
	refusedWith(t, "u2 writing a path owned by u1", err)
	fx.judge(rootA, "review", "verify", units["u1"].author, units["u2"].author)
	for id, u := range units {
		fx.read(rootA, "review", "verify", "r-"+id, u.path, "")
		if err := fx.verify(rootA, "review", "verify", "v-"+id, u.author, true); err != nil {
			t.Fatal(err)
		}
	}
	for id, u := range units {
		if _, err := fx.mutate("consolidate", orchReq(rootA, map[string]any{"author_key": u.author, "checkpoint": "cp"})); err != nil {
			t.Fatalf("%s blocked: %v", id, err)
		}
	}
	for id, u := range units {
		assertUnitJournalIsolated(t, fx.root, fx.state, id, u)
		if got := fx.disk(u.path); got != id {
			t.Fatalf("%s not consolidated: %q", id, got)
		}
	}
}

// separateSessionUnit is one unit's fixture in
// TestVFSUnitsOfOneSessionAreSeparate: its author key and the path it owns.
type separateSessionUnit struct {
	author string
	path   string
}

// assertUnitJournalIsolated checks that id's journal holds only its own
// entries (no foreign unit or path leaks in) and contains its create.
func assertUnitJournalIsolated(t *testing.T, root, state, id string, u *separateSessionUnit) {
	t.Helper()
	var out, stderr bytes.Buffer
	if err := run([]string{"vfs", "journal", "--workspace", root, "--state", state, "--session", rootA, "--unit", id}, strings.NewReader(""), &out, &stderr); err != nil {
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

// TestVFSCrossSessionContinuation carries work staged under root A into root
// B through the command layer: B's orchestrator reassigns the author key, the
// history admits B's unit, B's dev adopts the key at that attempt and keeps
// staging, B's judge adopts a gate over every author, reads, and verifies, and
// B's orchestrator consolidates or discards.
func TestVFSCrossSessionContinuation(t *testing.T) {
	for _, ending := range []string{"consolidate", "discard"} {
		t.Run(ending, func(t *testing.T) {
			fx := newVFSFixture(t)
			key := fx.delegate(rootA, "impl", "dev", "a.go")
			fx.write(rootA, "impl", "dev", "a1", "a.go", "A1")
			fx.dispatch("finish", rootA, "impl", "dev")

			scope := []string{"a.go", "b.go"}
			for range 2 {
				if again := fx.must("assign", claimReq(rootB, "continue", "dev", map[string]any{"scope": scope, "author_key": key})); again.Key != key {
					t.Fatalf("reassign = %s; want %s", again.Key, key)
				}
			}
			fx.dispatch("admit", rootB, "continue", "dev")
			bound := fx.must("bind", vfsReqIn(rootB, "continue", "dev", map[string]any{"scope": scope}))
			if bound.Key != key || bound.AttemptID != "1" || bound.Revision == nil || *bound.Revision != 1 {
				t.Fatalf("continuation bind = %+v; want %s at the admitted attempt 1, revision 1", bound, key)
			}
			_, err := fx.mutate("op", vfsReqIn(rootA, "impl", "dev", map[string]any{"call_id": "stale", "action": "create", "path": "a.go", "content": "x"}))
			refusedWith(t, "a write from root A's earlier holder", err, "vfs_bind")
			fx.write(rootB, "continue", "dev", "b1", "b.go", "B1")
			docs := fx.delegate(rootB, "docs", "fix", "c.md")
			fx.write(rootB, "docs", "fix", "c1", "c.md", "C1")

			gates := fx.judge(rootB, "review", "verify", key, docs)
			if gates[0].Scope == nil || gates[1].Scope == nil {
				t.Fatalf("gates = %+v; want each author's scope", gates)
			}
			if again, err := fx.adoptGates(rootB, "review", "verify"); err != nil || !reflect.DeepEqual(again, gates) {
				t.Fatalf("adopt-gates again = %+v, %v; want the same gates listed", again, err)
			}
			if read := fx.read(rootB, "review", "verify", "r1", "a.go", ""); read.Content != "A1" {
				t.Fatalf("gate read of a.go = %+v", read)
			}
			fx.read(rootB, "review", "verify", "r2", "c.md", "")
			fx.write(rootB, "continue", "dev", "b2", "b.go", "B2")
			if read := fx.read(rootB, "review", "verify", "r3", "b.go", ""); read.Content != "B2" || !slices.Equal(read.OutdatedPaths, []string{"a.go"}) {
				t.Fatalf("gate read of b.go = %+v; want a.go outdated", read)
			}
			if ending == "discard" {
				_, err = fx.mutate("discard", orchReq(rootA, map[string]any{"author_key": key}))
				refusedWith(t, "discard from root A", err, "claim_assign")
				fx.must("discard", orchReq(rootB, map[string]any{"author_key": key}))
				if got := fx.disk("a.go") + fx.disk("b.go"); got != "" {
					t.Fatalf("discarded work reached the workspace: %q", got)
				}
				fx.must("assign", claimReq(rootB, "redo", "dev", map[string]any{"scope": scope}))
				return
			}
			if err = fx.verify(rootB, "review", "verify", "v1", key, true); err != nil {
				t.Fatal(err)
			}
			fx.read(rootB, "review", "verify", "r4", "c.md", "")
			if err = fx.verify(rootB, "review", "verify", "v2", docs, false); err != nil {
				t.Fatal(err)
			}
			_, err = fx.mutate("consolidate", orchReq(rootA, map[string]any{"author_key": key}))
			refusedWith(t, "consolidation from root A", err, "claim_assign")
			fx.must("consolidate", orchReq(rootB, map[string]any{"author_key": key, "checkpoint": "continued"}))
			if got := fx.disk("a.go") + fx.disk("b.go"); got != "A1B2" {
				t.Fatalf("consolidated workspace = %q", got)
			}
			// A failing verdict is refused with the state it refused; the user's
			// acceptance has to name exactly that state.
			failing, err := fx.mutate("consolidate", orchReq(rootB, map[string]any{"author_key": docs}))
			if err != nil || failing.OK || failing.Revision == nil || failing.DeltaHash == "" || !strings.Contains(failing.Error, vfs.ErrFailingVerdict.Error()) {
				t.Fatalf("consolidation over a failing verdict = %+v, %v", failing, err)
			}
			accept := func(revision uint64, hash string) (response, error) {
				return fx.mutate("consolidate", orchReq(rootB, map[string]any{"author_key": docs, "accept_failing": true,
					"accepted_revision": revision, "accepted_hash": hash}))
			}
			_, err = accept(*failing.Revision, "stale")
			refusedWith(t, "acceptance of another staged state", err, "vfs_consolidate")
			if accepted, err := accept(*failing.Revision, failing.DeltaHash); err != nil || !accepted.OK {
				t.Fatalf("accepted consolidation = %+v, %v", accepted, err)
			}
			if got := fx.disk("c.md"); got != "C1" {
				t.Fatalf("accepted c.md = %q", got)
			}
		})
	}
}

// TestVFSRepeatedReassignKeepsTheAdmittedAttempt: however often a key is
// reassigned, its next bind takes the attempt the history admitted.
func TestVFSRepeatedReassignKeepsTheAdmittedAttempt(t *testing.T) {
	fx := newVFSFixture(t)
	key := fx.delegate(rootA, "impl", "dev", "a.go")
	fx.write(rootA, "impl", "dev", "a1", "a.go", "A1")
	fx.dispatch("finish", rootA, "impl", "dev")
	for want := range []string{"1", "2"} {
		for range 3 {
			fx.must("assign", claimReq(rootB, "continue", "dev", map[string]any{"scope": []string{"a.go"}, "author_key": key}))
		}
		fx.dispatch("admit", rootB, "continue", "dev")
		bound := fx.must("bind", vfsReqIn(rootB, "continue", "dev", nil))
		if bound.Key != key || bound.AttemptID != fmt.Sprint(want+1) {
			t.Fatalf("bind = %+v; want %s at attempt %d", bound, key, want+1)
		}
		fx.dispatch("finish", rootB, "continue", "dev")
	}
}

// TestVFSDelegatedBindWithoutAdmissionIsRefused: a unit binds only while the
// history holds its delegation in flight.
func TestVFSDelegatedBindWithoutAdmissionIsRefused(t *testing.T) {
	fx := newVFSFixture(t)
	claim := fx.must("assign", claimReq(rootA, "u1", "dev", map[string]any{"scope": []string{"a.go"}}))
	_, err := fx.mutate("bind", vfsReq("u1", "dev", nil))
	refusedWith(t, "bind before admission", err, "work unit u1 has no admitted delegation", "the orchestrator delegates it")
	fx.dispatch("admit", rootA, "u1", "dev")
	fx.dispatch("finish", rootA, "u1", "dev")
	_, err = fx.mutate("bind", vfsReq("u1", "dev", nil))
	refusedWith(t, "bind after the delegation returned", err, "has no admitted delegation")
	claims := fx.must("claims", orchReq(rootA, nil))
	assertVFSPendingClaim(t, claims, nil, claim.Key)
}

// TestVFSParallelInvocationsQueue runs concurrent invocations against one
// workspace: each waits for the workspace lock and every one succeeds.
func TestVFSParallelInvocationsQueue(t *testing.T) {
	fx := newVFSFixture(t)
	units := make([]string, parallelInvocations)
	for i := range units {
		units[i] = fmt.Sprintf("u%d", i)
		fx.delegate(rootA, units[i], "dev", units[i]+".go")
	}
	errs := make(chan error, len(units))
	var wg sync.WaitGroup
	for _, unit := range units {
		wg.Go(func() {
			var resp response
			errs <- invokeVFS(fx.root, fx.state, "op", vfsReq(unit, "dev", map[string]any{"call_id": "w-" + unit, "action": "create", "path": unit + ".go", "content": unit}), &resp)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, claim := range fx.must("claims", orchReq(rootA, nil)).Claims {
		if !claim.Staged {
			t.Fatalf("claim %+v lost its queued write", claim)
		}
	}
}

// TestVFSDiscardReleasesRetainedWork discards a failed author's staged work,
// the orchestrator's decision, so its paths can be assigned again.
func TestVFSDiscardReleasesRetainedWork(t *testing.T) {
	fx := newVFSFixture(t)
	author := fx.delegate(rootA, "u1", "dev", "a.go")
	fx.write(rootA, "u1", "dev", "w", "a.go", "x")
	if refused, err := fx.mutate("assign", claimReq(rootA, "u2", "fix", map[string]any{"scope": []string{"a.go"}})); err != nil || refused.OK {
		t.Fatalf("retained work's path was assigned again: %+v, %v", refused, err)
	}
	_, err := fx.mutate("discard", vfsReq("u1", "dev", map[string]any{"author_key": author}))
	refusedWith(t, "discard by the author", err, "only the orchestrator")
	fx.must("discard", orchReq(rootA, map[string]any{"author_key": author}))
	fx.must("assign", claimReq(rootA, "u2", "fix", map[string]any{"scope": []string{"a.go"}}))
}

// Maintenance attribution is harness authority, not an ordinary bind argument.
func TestVFSIPCRejectsForgedCycleAttribution(t *testing.T) {
	fx := newVFSFixture(t)
	fx.dispatch("admit", rootA, "u1", "dev")
	req := vfsReq("u1", "dev", map[string]any{"scope": []string{"a.go"}, "cycle_id": "forged", "mandate_class": "dead-code"})
	_, err := fx.mutate("bind", req)
	refusedWith(t, "ordinary bind with maintenance attribution", err, "gc:")
	delete(req, "cycle_id")
	delete(req, "mandate_class")
	fx.must("bind", req)
}

// TestVFSVerifyRefusesForgedCycleAttribution: a verdict over ordinary work
// that declares a maintenance cycle is refused; the same verdict without it
// is attached.
func TestVFSVerifyRefusesForgedCycleAttribution(t *testing.T) {
	fx := newVFSFixture(t)
	author := fx.delegate(rootA, "u1", "dev", "a.go")
	fx.write(rootA, "u1", "dev", "c1", "a.go", "x")
	fx.judge(rootA, "review", "verify", author)
	fx.read(rootA, "review", "verify", "r1", "a.go", "")
	_, err := fx.mutate("verify", vfsReq("review", "verify", map[string]any{"call_id": "bad", "author_key": author, "pass": true,
		"cycle_id": "cycle-7", "mandate_class": "dead-code"}))
	refusedWith(t, "verify with forged cycle fields", err, "maintenance cycle")
	if err = fx.verify(rootA, "review", "verify", "ok", author, true); err != nil {
		t.Fatalf("verify without cycle fields: %v", err)
	}
}

// TestVFSReadBeforeAnyWriteReportsRevisionZero covers a fix that reads the
// workspace right after binding: the response must still state its revision.
func TestVFSReadBeforeAnyWriteReportsRevisionZero(t *testing.T) {
	fx := newVFSFixture(t)
	if err := os.WriteFile(filepath.Join(fx.root, "app.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	fx.delegate(rootA, "u1", "dev", "app.go")
	var raw map[string]any
	if err := invokeVFS(fx.root, fx.state, "op", vfsReq("u1", "dev", map[string]any{"call_id": "r1", "action": "read", "path": "app.go"}), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["revision"] != float64(0) || raw["delta_hash"] == "" || raw["content"] != "package main" {
		t.Fatalf("read response = %v", raw)
	}
}
