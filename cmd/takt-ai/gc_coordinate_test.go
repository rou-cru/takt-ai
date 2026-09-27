package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func gcHarness(t *testing.T) (string, string, func(coordinationRequest) (gc.Coordinator, error)) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	call := func(r coordinationRequest) (gc.Coordinator, error) {
		var out, errout bytes.Buffer
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		e := runGC([]string{"coordinate", "--workspace", root, "--state", state, "--request", string(b)}, &out, &errout)
		c, load := gc.LoadCoordinator(state)
		if load != nil {
			return gc.Coordinator{}, load
		}
		return *c, e
	}
	return root, state, call
}

func TestGCCoordinatorFullAcceptanceAndRecovery(t *testing.T) {
	for _, outcome := range []string{"pass", "regress", "reject", "restart", "diverge", "no-change"} {
		t.Run(outcome, func(t *testing.T) {
			root, state, call := gcHarness(t)
			write := func(p, s string) {
				t.Helper()
				if e := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(root, p), []byte(s), 0755); e != nil {
					t.Fatal(e)
				}
			}
			initial := "package main\nfunc dead() {}\nfunc main() {}\n"
			write("a.go", initial)
			write("go.mod", "module fixture\n\ngo 1.25.0\n")
			write("analyzer", `#!/bin/sh
if [ "$1" = version ]; then echo fixture-1; exit; fi
printf '[{"Funcs":[{"Name":"fixture.dead","Position":{"File":"a.go","Line":2}}]}]'
`)
			write("check", "#!/bin/sh\n! grep -q BROKEN a.go\n")
			cfg := gc.ProjectConfig{Version: 1, Locks: []string{"go.mod"}, Checks: [][]string{{"./check"}}, Analyzers: []gc.Analyzer{{Language: "go", Mandate: gc.MandateDeadCode, Tool: "deadcode", Version: "fixture-1", Command: []string{"./analyzer", "-json", "./..."}, VersionCommand: []string{"./analyzer", "version"}}}}
			b, e := json.Marshal(cfg)
			if e != nil {
				t.Fatal(e)
			}
			write(".takt/gc.json", string(b))
			if _, e := call(coordinationRequest{Action: "prepare", Session: "root"}); e != nil {
				t.Fatal(e)
			}
			// Ordinary mutation is staged and independently verified through the real VFS.
			fs, e := vfs.Open(root, state)
			if e != nil {
				t.Fatal(e)
			}
			id := vfs.Identity{SessionID: "root", WorkUnitID: "ordinary", AgentID: "dev", Specialist: "dev"}
			author, e := fs.Bind(id, []string{"a.go"})
			if e != nil {
				t.Fatal(e)
			}
			// The version an empty invariant set derives, which is what a cycle
			// dispatched without its declared invariant would be judged against.
			ordinary, _ := fs.BindingIdentity(author)
			changed := initial + "// ordinary edit\n"
			result, e := fs.Apply(vfs.Operation{Key: author, CallID: "write", Action: vfs.OpPatch, Path: "a.go", Content: []byte(changed)})
			if e != nil {
				t.Fatal(e)
			}
			id.AgentID = "verify"
			id.Specialist = "verify"
			verifier, e := fs.AssignVerifier(id, author)
			if e != nil {
				t.Fatal(e)
			}
			if e = fs.Verify(verifier, author, "verify", result.Revision, result.DeltaHash, true, ""); e != nil {
				t.Fatal(e)
			}
			if e = fs.ConsolidateCheckpoint(author, "ordinary", result.Revision); e != nil {
				t.Fatal(e)
			}
			if e := fs.Close(); e != nil {
				t.Fatal(e)
			}
			c, e := call(coordinationRequest{Action: "request", Session: "root"})
			if e != nil || c.Cycle == nil {
				t.Fatalf("request: %+v %v", c, e)
			}
			if c.Cycle.Phase != "baseline" {
				t.Fatal(c.Cycle.Phase)
			}
			cycleID := c.Cycle.Plan.CycleID
			started := gcProjection(t, state)
			if activity, ok := started.Activities[cycleID]; !ok || activity.NodeKind != history.NodeKindMaintenance || activity.State != history.StateInFlight {
				t.Fatalf("GC cycle activity did not start with its real CycleID %q: %+v", cycleID, started.Activities)
			}
			if _, unit := started.Units[cycleID]; unit {
				t.Fatalf("GC CycleID %q was fabricated as a work unit", cycleID)
			}
			invoke := func(r coordinationRequest) gc.Coordinator {
				t.Helper()
				c, e := call(r)
				if e != nil {
					t.Fatalf("%s: %v", r.Action, e)
				}
				return c
			}
			invoke(coordinationRequest{Action: "attach", Role: "verifier", Child: "verify-child"})
			if _, e = call(coordinationRequest{Action: "attach", Role: "collector", Child: "verify-child"}); e == nil {
				t.Fatal("same child allowed to author and verify")
			}
			invoke(coordinationRequest{Action: "attach", Role: "collector", Child: "collect-child"})
			c = invoke(coordinationRequest{Action: "baseline", Session: "verify-child"})
			if !gc.ChecksPass(c.Cycle.Baseline) || len(c.Cycle.Report.Findings) != 1 {
				t.Fatalf("baseline %+v", c.Cycle)
			}
			if outcome == "no-change" {
				c = invoke(coordinationRequest{Action: "no-change", Session: "collect-child"})
				if c.Cycle != nil || len(c.History) != 1 {
					t.Fatalf("no-change did not close cycle: %+v", c)
				}
				activity := gcProjection(t, state).Activities[cycleID]
				if activity.State != history.StateSettled || activity.Outcome != history.OutcomeCompleted {
					t.Fatalf("no-change did not settle maintenance activity: %+v", activity)
				}
				return
			}
			if _, e = call(coordinationRequest{Action: "authorize", Session: "collect-child"}); e == nil {
				t.Fatal("absence of refutation authorized mutation")
			}
			invoke(coordinationRequest{Action: "investigate", Session: "collect-child", Finding: c.Cycle.Report.Findings[0].ID, Outcome: "confirmed", Evidence: "No callbacks, reflection, config entry, framework entry or public contract; production consumers absent."})
			c = invoke(coordinationRequest{Action: "authorize", Session: "collect-child"})
			fs, e = vfs.Open(root, state)
			if e != nil {
				t.Fatal(e)
			}
			// The collector's delta is judged against the reviewed project
			// configuration the cycle froze, never an empty invariant set
			// (PR-MNT-15, PR-MNT-17, PR-VFS-CSL-3).
			collector, bound := fs.BindingIdentity(c.Cycle.AuthorKey)
			if !bound || !reflect.DeepEqual(collector.Invariants, vfs.InvariantSet{gc.ProjectConfigPath}) ||
				collector.InvariantsHash == ordinary.InvariantsHash {
				t.Fatalf("cycle invariant set %v version %q", collector.Invariants, collector.InvariantsHash)
			}
			delta := "package main\nfunc main() {}\n"
			if outcome == "regress" {
				delta += "// BROKEN\n"
			}
			if _, e = fs.Apply(vfs.Operation{Key: c.Cycle.AuthorKey, CallID: "cleanup", Action: vfs.OpPatch, Path: "a.go", Content: []byte(delta)}); e != nil {
				t.Fatal(e)
			}
			if e := fs.Close(); e != nil {
				t.Fatal(e)
			}
			invoke(coordinationRequest{Action: "collected", Session: "collect-child"})
			if _, e = call(coordinationRequest{Action: "verdict", Session: "collect-child", Pass: true, Evidence: "self"}); e == nil {
				t.Fatal("self verification passed")
			}
			c = invoke(coordinationRequest{Action: "verdict", Session: "verify-child", Pass: outcome != "reject", Evidence: "Inspected exact candidate; behavior checked independently."})
			switch outcome {
			case "pass", "regress":
				c = invoke(coordinationRequest{Action: "acceptance", Session: "verify-child"})
			case "restart":
				c = invoke(coordinationRequest{Action: "recover", Evidence: "process restarted"})
			case "diverge":
				write("a.go", "external user content\n")
				c, e = call(coordinationRequest{Action: "recover", Evidence: "restart"})
				if e == nil || c.Cycle == nil || c.Cycle.Phase != "blocked" {
					t.Fatalf("divergence failed open: %+v %v", c, e)
				}
			}
			got, e := os.ReadFile(filepath.Join(root, "a.go"))
			if e != nil {
				t.Fatal(e)
			}
			want := changed
			if outcome == "pass" {
				want = delta
			}
			if outcome == "diverge" {
				want = "external user content\n"
			}
			if string(got) != want {
				t.Fatalf("workspace: %q want %q", got, want)
			}
			if outcome != "diverge" && (c.Cycle != nil || len(c.History) != 1 || c.NextMandate != 1) {
				t.Fatalf("not closed: %+v", c)
			}
			if outcome == "pass" && !strings.Contains(c.History[0].Reason, "applied") {
				t.Fatal(c.History[0].Reason)
			}
			activity := gcProjection(t, state).Activities[cycleID]
			if outcome == "diverge" {
				if activity.State != history.StateInFlight {
					t.Fatalf("blocked cycle activity should remain active: %+v", activity)
				}
			} else {
				wantOutcome := history.OutcomeInterrupted
				if outcome == "pass" {
					wantOutcome = history.OutcomeCompleted
				}
				if activity.State != history.StateSettled || activity.Outcome != wantOutcome {
					t.Fatalf("GC activity not settled at cycle end: %+v; want %q", activity, wantOutcome)
				}
			}
			for _, entry := range gcEntries(t, state) {
				if entry.ActivityID == cycleID && (entry.WorkUnitID != "" || entry.AttemptID != "") {
					t.Fatalf("GC cycle ID leaked into work-unit identity: %+v", entry)
				}
			}
		})
	}
}
