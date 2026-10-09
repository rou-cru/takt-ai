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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestGCPlanDeclaresDeltaFromJournal(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	key, err := fs.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: "author", Specialist: "dev", InvariantsHash: "h"}, []string{"new.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fs.Apply(vfs.Operation{Key: key, CallID: "c", Action: vfs.OpCreate, Path: "new.go", Content: []byte("package x")}); err != nil {
		t.Fatal(err)
	}
	if err = fs.Close(); err != nil {
		t.Fatal(err)
	}
	args := []string{"gc", "plan", "--workspace", root, "--state", state, "--session", "s", "--cycle", "c1", "--mandate", "dead-code"}
	var out, stderr bytes.Buffer
	if err = run(args, strings.NewReader(""), &out, &stderr); err != nil {
		t.Fatal(err)
	}
	var plan gc.Plan
	if err = json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	// The closure holds the delta whether or not a code graph is installed and indexed here.
	if len(plan.Delta) != 1 || plan.Delta[0] != (gc.Change{Path: "new.go", Introduced: true}) || !slices.Contains(plan.Closure, "new.go") || plan.Mandate != gc.MandateDeadCode {
		t.Fatalf("plan = %s", out.String())
	}
	bad := append(slices.Clone(args[:len(args)-1]), "dead-code,complexity")
	if err = run(bad, strings.NewReader(""), &out, &stderr); err == nil {
		t.Fatal("two mandate classes accepted")
	}
}

// TestGCPlanFindsCodegraphUnderInstallRoot pins that codegraph is looked up where
// setup installs it (the install root), not inside the governed workspace.
func TestGCPlanFindsCodegraphUnderInstallRoot(t *testing.T) {
	workspace := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(workspace, state)
	if err != nil {
		t.Fatal(err)
	}
	key, err := fs.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: "author", Specialist: "dev", InvariantsHash: "h"}, []string{"new.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fs.Apply(vfs.Operation{Key: key, CallID: "c", Action: vfs.OpCreate, Path: "new.go", Content: []byte("package x")}); err != nil {
		t.Fatal(err)
	}
	if err = fs.Close(); err != nil {
		t.Fatal(err)
	}
	// Keep any codegraph on the developer's PATH or home out of the result.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	installRoot := t.TempDir()
	bin := codegraph.ManagedBinaryPath(installRoot)
	if err = os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(bin, []byte("#!/bin/sh\necho "+codegraph.CodegraphVersion+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := func(extra ...string) gc.Plan {
		t.Helper()
		args := append([]string{"gc", "plan", "--workspace", workspace, "--state", state, "--session", "s", "--cycle", "c1", "--mandate", "dead-code"}, extra...)
		var out, stderr bytes.Buffer
		if err := run(args, strings.NewReader(""), &out, &stderr); err != nil {
			t.Fatal(err)
		}
		var p gc.Plan
		if err := json.Unmarshal(out.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if got := plan("--root", installRoot); got.Reachability != gc.ReachCodegraph {
		t.Errorf("with --root: reachability = %q (gap %q), want %q", got.Reachability, got.Gap, gc.ReachCodegraph)
	}
	if got := plan(); got.Reachability != gc.ReachJournalOnly {
		t.Errorf("without an install: reachability = %q, want %q", got.Reachability, gc.ReachJournalOnly)
	}
}

// fakeCodegraphScript answers only what a dead-code cycle asks: new.go (created by
// the session, so pending) reaches old.go, whose Dead function has no callers.
const fakeCodegraphScript = `#!/bin/sh
case "$1" in
--version) echo ` + codegraph.CodegraphVersion + ` ;;
status) echo "Index Statistics" ;;
files) echo '[{"path":"new.go","language":"go"},{"path":"old.go","language":"go"}]' ;;
node)
  if [ "$3" = "new.go" ]; then echo '- ` + "`Fresh`" + ` (function) () — :1'; else echo '- ` + "`Dead`" + ` (function) () — :1'; fi ;;
query)
  case "$5" in
  *Fresh*) echo '[{"node":{"qualifiedName":"Fresh","kind":"function","filePath":"new.go","startLine":1}}]' ;;
  *) echo '[{"node":{"qualifiedName":"Dead","kind":"function","filePath":"old.go","startLine":1}}]' ;;
  esac ;;
impact) echo '{"affected":[{"filePath":"old.go"}]}' ;;
callers) echo '{"callers":[]}' ;;
esac
`

func TestGCRefuteRecordsAndAttachesToFindings(t *testing.T) {
	workspace := t.TempDir()
	state := filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(workspace, state)
	if err != nil {
		t.Fatal(err)
	}
	prepareGCFindingsFixture(t, workspace, state)
	key, err := fs.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: "author", Specialist: "dev", InvariantsHash: "h"}, []string{"new.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fs.Apply(vfs.Operation{Key: key, CallID: "c", Action: vfs.OpCreate, Path: "new.go", Content: []byte("package x")}); err != nil {
		t.Fatal(err)
	}
	if err = fs.Close(); err != nil {
		t.Fatal(err)
	}
	// Findings consumes flushed source, not fake CodeGraph symbol listings.
	if err = os.WriteFile(filepath.Join(workspace, "new.go"), []byte("package x\nfunc Fresh() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	installRoot := t.TempDir()
	bin := codegraph.ManagedBinaryPath(installRoot)
	if err = os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(bin, []byte(fakeCodegraphScript), 0o755); err != nil {
		t.Fatal(err)
	}
	gcRun := func(verb string, extra ...string) (struct {
		Report      gc.Report       `json:"report"`
		Refutations []gc.Refutation `json:"refutations"`
		Actionable  []gc.Finding    `json:"actionable"`
	}, error) {
		args := append([]string{"gc", verb, "--workspace", workspace, "--state", state, "--session", "s", "--cycle", "c1", "--mandate", "dead-code", "--root", installRoot}, extra...)
		var out, stderr bytes.Buffer
		var view struct {
			Report      gc.Report       `json:"report"`
			Refutations []gc.Refutation `json:"refutations"`
			Actionable  []gc.Finding    `json:"actionable"`
		}
		if err := run(args, strings.NewReader(""), &out, &stderr); err != nil {
			return view, err
		}
		return view, json.Unmarshal(out.Bytes(), &view)
	}
	view, err := gcRun("findings")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Actionable) != 1 || view.Actionable[0].ID != "dead-code:old.go#Dead" || len(view.Refutations) != 0 {
		t.Fatalf("before refuting: %+v", view)
	}
	refute := []string{"--finding", "dead-code:old.go#Dead", "--class", "value-reference", "--evidence", "main.go:9 registers Dead as a handler", "--instance", "simplifier-1"}
	view, err = gcRun("refute", refute...)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Actionable) != 0 || len(view.Refutations) != 1 || view.Refutations[0].At.IsZero() || view.Refutations[0].CycleID != "c1" || len(view.Report.Findings) != 2 {
		t.Fatalf("after refuting: %+v", view)
	}
	// Idempotent, and visible to a later read of the same cycle.
	if view, err = gcRun("refute", refute...); err != nil || len(view.Refutations) != 1 {
		t.Fatalf("second refute: %+v, %v", view, err)
	}
	if view, err = gcRun("findings"); err != nil || len(view.Refutations) != 1 || len(view.Actionable) != 0 {
		t.Fatalf("findings after refute: %+v, %v", view, err)
	}
	// Private state only: the record is a file beside the store, never in the journal.
	if _, err = os.Stat(filepath.Join(state, "gc-refutations.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = gcRun("refute", "--finding", "dead-code:old.go#Typo", "--class", "reflection", "--evidence", "x", "--instance", "i"); err == nil {
		t.Fatal("unknown finding accepted")
	}
	if _, err = gcRun("refute", "--finding", "dead-code:new.go#Fresh", "--class", "reflection", "--instance", "i"); err == nil {
		t.Fatal("refutation without evidence accepted")
	}
}

func prepareGCFindingsFixture(t *testing.T, workspace, state string) {
	t.Helper()
	files := map[string]string{
		"old.go": "package x\nfunc Dead() {}\n",
		"go.mod": "module fixture\n\ngo 1.25\n",
		"analyzer": `#!/bin/sh
if [ "$1" = version ]; then echo fixture-v1; exit 0; fi
echo '[{"Funcs":[{"Name":"Dead","Position":{"File":"old.go","Line":2}},{"Name":"Fresh","Position":{"File":"new.go","Line":2}}]}]'
`,
		".takt/gc.json": `{"version":1,"locks":["go.mod"],"checks":[["/bin/sh","-c","exit 0"]],"analyzers":[{"language":"go","mandate":"dead-code","tool":"deadcode","version":"fixture-v1","command":["./analyzer","-json","./..."],"version_command":["./analyzer","version"]}]}`,
	}
	for name, content := range files {
		path := filepath.Join(workspace, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gc.Prepare(context.Background(), workspace, state, "s"); err != nil {
		t.Fatal(err)
	}
}

// gcCycleWorkspace stages a maintenance cycle that rewrites keep.go and creates
// gone.go, consolidates it, and returns the workspace and state directories.
func gcCycleWorkspace(t *testing.T) (string, string) {
	t.Helper()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "keep.go"), []byte("package x // original"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(workspace, state)
	if err != nil {
		t.Fatal(err)
	}
	id := vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: "collector", Specialist: "simplify", InvariantsHash: "inv", CycleID: "c1", MandateClass: string(gc.MandateDeadCode)}
	key, err := fs.Bind(id, []string{"keep.go", "gone.go"})
	if err != nil {
		t.Fatal(err)
	}
	var r vfs.OperationResult
	for i, op := range []vfs.Operation{
		{Action: vfs.OpPatch, Path: "keep.go", Content: []byte("package x // collected")},
		{Action: vfs.OpCreate, Path: "gone.go", Content: []byte("package x")},
	} {
		op.Key, op.CallID, op.ExpectedRevision = key, fmt.Sprintf("op-%d", i), r.Revision
		if r, err = fs.Apply(op); err != nil {
			t.Fatal(err)
		}
	}
	verifierID := vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: "judge", Specialist: "verify", InvariantsHash: "inv"}
	if _, err = fs.AssignVerifier(verifierID, key); err != nil {
		t.Fatal(err)
	}
	verifierID.GateAuthorKey = key
	verifier, err := fs.Bind(verifierID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = fs.Verify(verifier, key, "v", r.Revision, r.DeltaHash, true, ""); err != nil {
		t.Fatal(err)
	}
	if err = fs.ConsolidateCheckpoint(key, "cp", r.Revision, false); err != nil {
		t.Fatal(err)
	}
	if err = fs.Close(); err != nil {
		t.Fatal(err)
	}
	return workspace, state
}

func TestGCAcceptanceRegressionDiscardsTheCycle(t *testing.T) {
	workspace, state := gcCycleWorkspace(t)
	var out, stderr bytes.Buffer
	args := []string{"gc", "acceptance", "--workspace", workspace, "--state", state, "--cycle", "c1", "--result", "regress"}
	if err := run(args, strings.NewReader(""), &out, &stderr); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Closure gc.Closure `json:"closure"`
		Record  struct {
			ActionClass string `json:"action_class"`
		} `json:"record"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Closure.Restored, []string{"gone.go", "keep.go"}) {
		t.Errorf("restored = %v", got.Closure.Restored)
	}
	if got.Closure.Mandate != gc.MandateDeadCode || got.Record.ActionClass != "ESCALATE" {
		t.Errorf("closure = %+v, record class = %q", got.Closure, got.Record.ActionClass)
	}
	kept, err := os.ReadFile(filepath.Join(workspace, "keep.go"))
	if err != nil || string(kept) != "package x // original" {
		t.Errorf("keep.go = %q, %v; want the pre-cycle content", kept, err)
	}
	if _, err = os.Stat(filepath.Join(workspace, "gone.go")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("gone.go still there: %v", err)
	}
	// PR-MNT-5: the cycle's outcome is on the control bus, not only on stdout.
	assertCycleRecorded(t, workspace, "ESCALATE", "discarded")
}

// assertCycleRecorded fails unless exactly one control action for the cycle
// landed in the workspace bus with the expected class and wording.
func assertCycleRecorded(t *testing.T, workspace, class, outcome string) {
	t.Helper()
	actions := queryStoredActions(t, workspace, "s")
	if len(actions) != 1 {
		t.Fatalf("control actions = %d, want exactly the cycle's own", len(actions))
	}
	r := actions[0].Record
	if string(r.ActionClass) != class || r.PolicyRef != gc.AcceptancePolicyRef || !strings.Contains(r.TriggeringCondition, outcome) {
		t.Errorf("recorded action = %+v, want %s naming %q", r, class, outcome)
	}
}

func TestGCAcceptancePassKeepsTheCycleAndReleasesIt(t *testing.T) {
	workspace, state := gcCycleWorkspace(t)
	args := []string{"gc", "acceptance", "--workspace", workspace, "--state", state, "--cycle", "c1", "--result", "pass"}
	var out, stderr bytes.Buffer
	if err := run(args, strings.NewReader(""), &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "OBSERVE") {
		t.Errorf("record = %s, want OBSERVE", out.String())
	}
	kept, err := os.ReadFile(filepath.Join(workspace, "keep.go"))
	if err != nil || string(kept) != "package x // collected" {
		t.Errorf("keep.go = %q, %v; a passing cycle stands", kept, err)
	}
	assertCycleRecorded(t, workspace, "OBSERVE", "completed")
	// Released: the cycle can no longer be discarded.
	out.Reset()
	regress := []string{"gc", "acceptance", "--workspace", workspace, "--state", state, "--cycle", "c1", "--result", "regress"}
	if err = run(regress, strings.NewReader(""), &out, &stderr); !errors.Is(err, vfs.ErrUnknownCycle) {
		t.Errorf("discard after completion = %v, want ErrUnknownCycle", err)
	}
	// A bad result value is refused before anything happens.
	bad := []string{"gc", "acceptance", "--workspace", workspace, "--state", state, "--cycle", "c1", "--result", "ok"}
	if err = run(bad, strings.NewReader(""), &out, &stderr); err == nil {
		t.Error("unknown acceptance result accepted")
	}
}
