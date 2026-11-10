package main_test

import (
	"encoding/json"
	"errors"
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
			if activity, ok := started.Activities[cycleID]; !ok || activity.NodeKind != history.NodeKindMaintenance || activity.State != history.StateInFlight {
				t.Fatalf("GC cycle activity did not start with its real CycleID %q: %+v", cycleID, started.Activities)
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
				activity := s.projection().Activities[cycleID]
				if activity.State != history.StateSettled || activity.Outcome != history.OutcomeCompleted {
					t.Fatalf("no-change did not settle maintenance activity: %+v", activity)
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
				c, e = call(map[string]any{"action": "recover", "event": cycleID, "stopped": stops, "evidence": "restart"})
				if e == nil || c.Cycle == nil || c.Cycle.Phase != "blocked" {
					t.Fatalf("divergence failed open: %+v %v", c, e)
				}
				// A delegation never runs over a cycle whose delta could not be
				// discarded: the admission waits, and nothing is admitted.
				res := s.dispatch(request{Action: "admit", Event: "after-divergence", Session: "root", Agent: "dev"})
				if res.code != 0 {
					t.Fatalf("admission of a delegation was refused (exit %d): %s", res.code, res.stderr)
				}
				var refusal struct {
					OK     bool        `json:"ok"`
					Repair *vfs.Repair `json:"repair"`
				}
				if err := json.Unmarshal([]byte(res.stdout), &refusal); err != nil || refusal.OK || refusal.Repair == nil || refusal.Repair.ID != cycleID {
					t.Fatalf("missing cycle continuation: %s %v", res.stdout, err)
				}
				if _, admitted := s.projection().Units[history.UnitKey("root", "after-divergence")]; admitted {
					t.Fatal("delegation admitted while the cycle's delta is live")
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
			activity := s.projection().Activities[cycleID]
			if outcome == "diverge" {
				if activity.State != history.StateSettled || activity.Outcome != history.OutcomeInterrupted {
					t.Fatalf("preserved cycle was not interrupted: %+v", activity)
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
			for _, entry := range s.entries() {
				if entry.ActivityID == cycleID && (entry.WorkUnitID != "" || entry.AttemptID != "") {
					t.Fatalf("GC cycle ID leaked into work-unit identity: %+v", entry)
				}
			}
		})
	}
}

// closingNotice decodes the closing notice a cycle's final answer carries.
type closingNotice struct {
	Closing *struct {
		Cycle     string       `json:"cycle"`
		Outcome   string       `json:"outcome"`
		Unapplied []gc.Finding `json:"unapplied_findings"`
	} `json:"closing"`
}

// closeOpenCycle opens a cycle holding two findings, answers action from its collector, and returns
// what the closing answer reports.
func closeOpenCycle(t *testing.T, bin, action string) closingNotice {
	t.Helper()
	const collector = "collector-session"
	s := newSession(t, bin)
	c := s.coordinator()
	c.Cycle = &gc.Cycle{
		Plan:     gc.Plan{Request: gc.Request{CycleID: "cycle-9", SessionID: rootSession, Mandate: gc.MandateDeadCode}},
		Phase:    "investigate",
		Sessions: map[string]string{"collector": collector},
		Report:   gc.Report{Findings: []gc.Finding{{ID: "f1", Path: "a.go", Status: gc.StatusDead}, {ID: "f2", Path: "b.go", Status: gc.StatusPending}}},
	}
	if err := saveCoordinator(t, s.root, s.state, c); err != nil {
		t.Fatal(err)
	}
	// The collector lane's execution is what makes its session the cycle's participant.
	h, err := history.Open(s.state)
	if err != nil {
		t.Fatal(err)
	}
	lane := history.Entry{Author: history.AuthorHarness, Kind: history.KindLane, SessionID: rootSession, WorkUnitID: "cycle-9", AttemptID: "1", Dispatch: "lane-call", Agent: "simplify", Child: collector, CycleID: "cycle-9", Role: "collector", Cause: history.CauseNone}
	err = h.Append(lane)
	if action == "abort" {
		// An abort waits for its lanes' observed termination.
		err = errors.Join(err, h.ExecutionFact(h.Project().Executions["lane-call"], history.KindLaneStopped, "", "", nil))
	}
	if err := errors.Join(err, h.Close()); err != nil {
		t.Fatal(err)
	}
	res := s.invoke("gc", "coordinate", request{Action: action, Session: collector, Event: "cycle-9", Evidence: "user unwind"})
	if res.code != 0 {
		t.Fatalf("gc coordinate %s exited %d: %s", action, res.code, res.stderr)
	}
	var notice closingNotice
	if err := json.Unmarshal([]byte(res.stdout), &notice); err != nil {
		t.Fatalf("answer %q: %v", res.stdout, err)
	}
	return notice
}

// TestCycleCloseCarriesUnappliedFindings covers PR-MNT-5: the closing answer to the orchestrator
// reports the findings the cycle left unapplied, whether it closed without a change or was aborted.
func TestCycleCloseCarriesUnappliedFindings(t *testing.T) {
	bin := buildTaktAI(t)
	for _, tc := range []struct{ action, outcome string }{
		{"no-change", "proposals-or-refuted"},
		{"abort", "aborted: user unwind"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			notice := closeOpenCycle(t, bin, tc.action)
			if notice.Closing == nil || notice.Closing.Cycle != "cycle-9" || notice.Closing.Outcome != tc.outcome {
				t.Fatalf("closing notice = %+v, want cycle-9 closed as %q", notice.Closing, tc.outcome)
			}
			if got := notice.Closing.Unapplied; len(got) != 2 || got[0].ID != "f1" || got[1].ID != "f2" {
				t.Fatalf("unapplied findings = %+v, want f1 and f2", got)
			}
		})
	}
}
