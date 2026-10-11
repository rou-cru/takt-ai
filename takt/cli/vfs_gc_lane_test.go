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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// Sessions of the maintenance lane fixture.
const (
	gcLaneRoot      = "root"
	gcCollectorLane = "collect-child"
	gcVerifierLane  = "verify-child"
)

// gcLaneReq is a request from a lane session as the plugin builds it: the
// cycle's root, the lane as host session and unit, and no cycle attribution.
func gcLaneReq(lane string, extra map[string]any) map[string]any {
	req := map[string]any{"ipc_version": IPCVersion, "session_id": gcLaneRoot, "caller_session": lane,
		"caller_agent": gc.CollectorSpecialistID}
	for k, v := range extra {
		req[k] = v
	}
	return req
}

// TestVFSCollectorLaneResolvesItsCycleBinding: the coordinator's
// authorization stamps the collector lane as the binding's holder, so the
// lane's ordinary vfs commands resolve the cycle binding with no key or cycle
// named, and no other session holds it.
func TestVFSCollectorLaneResolvesItsCycleBinding(t *testing.T) {
	root, state, call := gcHarness(t)
	for path, content := range map[string]string{
		"a.go":   "package main\nfunc dead() {}\nfunc main() {}\n",
		"go.mod": "module fixture\n\ngo 1.25.0\n",
		"analyzer": `#!/bin/sh
if [ "$1" = version ]; then echo fixture-1; exit; fi
printf '[{"Funcs":[{"Name":"fixture.dead","Position":{"File":"a.go","Line":2}}]}]'
`,
		"check": "#!/bin/sh\ntrue\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := json.Marshal(gc.ProjectConfig{Version: 1, Locks: []string{"go.mod"}, Checks: [][]string{{"./check"}},
		Analyzers: []gc.Analyzer{{Language: "go", Mandate: gc.MandateDeadCode, Tool: "deadcode", Version: "fixture-1",
			Command: []string{"./analyzer", "-json", "./..."}, VersionCommand: []string{"./analyzer", "version"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(root, ".takt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, ".takt", "gc.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	invoke := func(r coordinationRequest) gc.Coordinator {
		t.Helper()
		c, err := call(r)
		if err != nil {
			t.Fatalf("%s: %v", r.Action, err)
		}
		return c
	}
	invoke(coordinationRequest{Action: "prepare", Owner: testOwner, Session: gcLaneRoot})
	consolidateOrdinaryEdit(t, root, state)
	c := invoke(coordinationRequest{Action: "request", Session: gcLaneRoot})
	if c.Cycle == nil {
		t.Fatalf("request opened no cycle: %+v", c)
	}
	invoke(coordinationRequest{Action: "attach", Owner: testOwner, Role: "verifier", Child: gcVerifierLane})
	invoke(coordinationRequest{Action: "attach", Owner: testOwner, Role: "collector", Child: gcCollectorLane})
	c = invoke(coordinationRequest{Action: "baseline", Session: gcVerifierLane})
	invoke(coordinationRequest{Action: "investigate", Session: gcCollectorLane, Finding: c.Cycle.Report.Findings[0].ID, Outcome: "confirmed",
		Evidence: "No callbacks, reflection, config entry, framework entry or public contract; production consumers absent."})
	invoke(coordinationRequest{Action: "authorize", Session: gcCollectorLane})

	mutate := newVFSMutator(t, root, state)
	staged, err := mutate("op", gcLaneReq(gcCollectorLane, map[string]any{"call_id": "cleanup", "action": "patch", "path": "a.go", "content": "package main\nfunc main() {}\n"}))
	if err != nil || staged.Revision == nil || *staged.Revision != 1 {
		t.Fatalf("collector lane write = %+v, %v", staged, err)
	}
	_, err = mutate("op", gcLaneReq(gcVerifierLane, map[string]any{"call_id": "intrude", "action": "patch", "path": "a.go", "content": "x"}))
	refusedWith(t, "a write from another lane", err, "vfs_bind")
}

// consolidateOrdinaryEdit lands one verified ordinary edit, the mutation a
// requested cycle follows.
func consolidateOrdinaryEdit(t *testing.T, root, state string) {
	t.Helper()
	fs, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fs.Close(); err != nil {
			t.Error(err)
		}
	}()
	id := vfs.Identity{SessionID: gcLaneRoot, WorkUnitID: "ordinary", AttemptID: "1", AgentID: "dev", Specialist: "dev"}
	author, err := fs.Bind(id, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fs.Apply(vfs.Operation{Key: author, CallID: "write", Action: vfs.OpPatch, Path: "a.go",
		Content: []byte("package main\nfunc dead() {}\nfunc main() {}\n// ordinary edit\n")})
	if err != nil {
		t.Fatal(err)
	}
	id.AgentID, id.Specialist = "verify", "verify"
	verifier, err := fs.AssignVerifier(id, author)
	if err != nil {
		t.Fatal(err)
	}
	if err = fs.Verify(verifier, author, "verify", result.Revision, result.DeltaHash, true, ""); err != nil {
		t.Fatal(err)
	}
	if err = fs.ConsolidateCheckpoint(author, "ordinary", result.Revision, false); err != nil {
		t.Fatal(err)
	}
}
