package main_test

import (
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

// gcCall sends a maintenance-cycle request and returns the coordinator it leaves behind.
func gcCall(s *session, fields map[string]any) (gc.Coordinator, error) {
	s.t.Helper()
	_, err := coordinate(s, fields)
	return *s.coordinator(), err
}

func TestGCCoordinatorFullAcceptanceAndRecovery(t *testing.T) {
	bin := buildTaktAI(t)
	for _, outcome := range []string{"pass", "regress", "reject", "restart", "diverge", "no-change"} {
		t.Run(outcome, func(t *testing.T) {
			s := newSession(t, bin)
			root, state := s.root, s.state
			call := func(fields map[string]any) (gc.Coordinator, error) { return gcCall(s, fields) }
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
			if _, e := call(map[string]any{"action": "prepare", "owner": testOwnerInstance, "session": "root"}); e != nil {
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
			if e = fs.ConsolidateCheckpoint(author, "ordinary", result.Revision, false); e != nil {
				t.Fatal(e)
			}
			if e := fs.Close(); e != nil {
				t.Fatal(e)
			}
			c, e := call(map[string]any{"action": "request", "session": "root"})
			if e != nil || c.Cycle == nil {
				t.Fatalf("request: %+v %v", c, e)
			}
			if c.Cycle.Phase != "baseline" {
				t.Fatal(c.Cycle.Phase)
			}
			cycleID := c.Cycle.Plan.CycleID
			started := s.projection()
			if _, recorded := started.Activities[cycleID]; recorded {
				t.Fatalf("GC cycle %q was recorded as an activity of the session", cycleID)
			}
			if _, unit := started.Units[history.UnitKey("root", cycleID)]; unit {
				t.Fatalf("GC CycleID %q was fabricated as a work unit", cycleID)
			}
			invoke := func(r map[string]any) gc.Coordinator {
				t.Helper()
				c, e := call(r)
				if e != nil {
					t.Fatalf("%v: %v", r["action"], e)
				}
				return c
			}
			invoke(map[string]any{"action": "attach", "owner": testOwnerInstance, "role": "verifier", "child": "verify-child"})
			if _, e = call(map[string]any{"action": "attach", "owner": testOwnerInstance, "role": "collector", "child": "verify-child"}); e == nil {
				t.Fatal("same child allowed to author and verify")
			}
			invoke(map[string]any{"action": "attach", "owner": testOwnerInstance, "role": "collector", "child": "collect-child"})
			c = invoke(map[string]any{"action": "baseline", "session": "verify-child"})
			if !gc.ChecksPass(c.Cycle.Baseline) || len(c.Cycle.Report.Findings) != 1 {
				t.Fatalf("baseline %+v", c.Cycle)
			}
			if fc := invoke(map[string]any{"action": "findings", "session": "collect-child"}); fc.Cycle == nil || len(fc.Cycle.Report.Findings) != 1 {
				t.Fatalf("findings: %+v", fc.Cycle)
			}
			if outcome == "no-change" {
				c = invoke(map[string]any{"action": "no-change", "session": "collect-child"})
				if c.Cycle != nil || len(c.History) != 1 {
					t.Fatalf("no-change did not close cycle: %+v", c)
				}
				return
			}
			if _, e = call(map[string]any{"action": "authorize", "session": "collect-child"}); e == nil {
				t.Fatal("absence of refutation authorized mutation")
			}
			invoke(map[string]any{"action": "investigate", "session": "collect-child", "finding": c.Cycle.Report.Findings[0].ID, "outcome": "confirmed", "evidence": "No callbacks, reflection, config entry, framework entry or public contract; production consumers absent."})
			// An authorization whose coordinator save was lost is retried: the
			// collector's binding and the verifier's gate it reserved are found
			// again rather than refused as duplicates.
			saved, e := loadCoordinator(t, root, state)
			if e != nil {
				t.Fatal(e)
			}
			first := invoke(map[string]any{"action": "authorize", "session": "collect-child"})
			if e = saveCoordinator(t, root, state, saved); e != nil {
				t.Fatal(e)
			}
			c = invoke(map[string]any{"action": "authorize", "session": "collect-child"})
			if c.Cycle.AuthorKey != first.Cycle.AuthorKey || c.Cycle.VerifierKey != first.Cycle.VerifierKey {
				t.Fatalf("retried authorization = %s/%s; want the reserved %s/%s", c.Cycle.AuthorKey, c.Cycle.VerifierKey, first.Cycle.AuthorKey, first.Cycle.VerifierKey)
			}
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
			invoke(map[string]any{"action": "collected", "session": "collect-child"})
			if _, e = call(map[string]any{"action": "delta", "session": "collect-child"}); e == nil {
				t.Fatal("collector allowed to inspect delta through the verifier-only action")
			}
			invoke(map[string]any{"action": "delta", "session": "verify-child"})
			if _, e = call(map[string]any{"action": "verdict", "session": "collect-child", "pass": true, "evidence": "self"}); e == nil {
				t.Fatal("self verification passed")
			}
			stops := []map[string]string{{"dispatch": cycleID + "/collector", "attempt": "1", "child": "collect-child"}, {"dispatch": cycleID + "/verifier", "attempt": "1", "child": "verify-child"}}
			verdict := map[string]any{"action": "verdict", "session": "verify-child", "pass": outcome != "reject", "evidence": "Inspected exact candidate; behavior checked independently."}
			if outcome == "reject" {
				c, e = call(verdict)
				if e == nil || c.Cycle == nil || c.Cycle.Phase != "aborting" {
					t.Fatalf("rejection released live lanes: %+v %v", c, e)
				}
				c = invoke(map[string]any{"action": "abort", "event": cycleID, "stopped": stops, "evidence": "independent rejection"})
			} else {
				c = invoke(verdict)
			}

			switch outcome {
			case "pass":
				c = invoke(map[string]any{"action": "acceptance", "session": "verify-child"})
			case "regress":
				c, e = call(map[string]any{"action": "acceptance", "session": "verify-child"})
				if e == nil || c.Cycle == nil {
					t.Fatal("acceptance regression released running lanes")
				}
				c = invoke(map[string]any{"action": "abort", "event": cycleID, "stopped": stops, "evidence": "acceptance regression"})
			case "restart":
				c = invoke(map[string]any{"action": "recover", "event": cycleID, "stopped": stops, "evidence": "process restarted"})
			case "diverge":
				write("a.go", "external user content\n")
				out, e := coordinate(s, map[string]any{"action": "recover", "event": cycleID, "stopped": stops, "evidence": "restart"})
				var refusal struct {
					OK     bool        `json:"ok"`
					Repair *vfs.Repair `json:"repair"`
				}
				if err := json.Unmarshal([]byte(out), &refusal); e == nil || err != nil || refusal.OK || refusal.Repair == nil || refusal.Repair.ID != cycleID {
					t.Fatalf("divergence failed open: %s %v", out, e)
				}
				if held, e := loadCoordinator(t, root, state); e != nil || held.Cycle == nil || held.Cycle.Phase != "blocked" {
					t.Fatalf("blocked cycle lost: %+v %v", held, e)
				}
				approval := &vfs.RepairApproval{Kind: "cycle", ID: refusal.Repair.ID, State: refusal.Repair.State, Preserve: true}
				stale := *approval
				stale.State = "stale"
				if _, err := call(map[string]any{"action": "abort", "event": cycleID, "repair": &stale}); err == nil {
					t.Fatal("stale approval accepted")
				}
				c = invoke(map[string]any{"action": "abort", "event": cycleID, "repair": approval})
				if c.Cycle != nil || !strings.Contains(c.History[0].Reason, "interrupted") {
					t.Fatal("divergent cycle not closed as interrupted")
				}
				fs, err := vfs.Open(root, state)
				if err != nil {
					t.Fatal(err)
				}
				if !fs.CycleInterrupted(cycleID) {
					t.Fatal("cycle evidence lost")
				}
				for _, claim := range fs.OwnershipClaims("root") {
					if binding, ok := fs.BindingIdentity(vfs.AgentID(claim.Key)); ok && binding.CycleID == cycleID {
						t.Fatal("cycle ownership remains active")
					}
				}
				if err := fs.Close(); err != nil {
					t.Fatal(err)
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
			for _, entry := range s.entries() {
				if entry.ActivityID == cycleID {
					t.Fatalf("GC cycle was recorded as an activity of the session: %+v", entry)
				}
			}
		})
	}
}
