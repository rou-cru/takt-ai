package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	protocol "github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// The package under test is a command, so these tests are black-box in the only
// way a main package allows: they compile it and drive `takt-ai dispatch` (and
// `gc coordinate` where a scenario needs it) as a process, judging exit status,
// the JSON answer and the execution history left in the state directory. The
// history is read with its exported package once the process has exited.

// rootSession is the root session every scenario runs under unless it needs two.
const rootSession = "root"

// causeIncomplete is the cause recorded for a recovery declaration that leaves
// a required part out.
const causeIncomplete = "declaration/recovery-incomplete"

// Modes of the memory session index a scenario seeds under its own HOME.
const (
	memoryDirMode  os.FileMode = 0o700
	memoryFileMode os.FileMode = 0o600
)

// engramReadyTimeout bounds the wait for a real `engram serve` to listen.
const engramReadyTimeout = 15 * time.Second

// request is the JSON envelope `dispatch` reads; only the fields these
// scenarios send.
type request struct {
	Action     string           `json:"action"`
	Event      string           `json:"event,omitempty"`
	ActivityID string           `json:"activity_id,omitempty"`
	NodeKind   history.NodeKind `json:"node_kind,omitempty"`
	Dispatch   string           `json:"dispatch,omitempty"`
	Session    string           `json:"session"`
	Agent      string           `json:"agent,omitempty"`
	Child      string           `json:"child,omitempty"`
	Outcome    string           `json:"outcome,omitempty"`
	Evidence   string           `json:"evidence,omitempty"`
	Pass       bool             `json:"pass,omitempty"`
	Objective  string           `json:"objective,omitempty"`
	Result     string           `json:"result,omitempty"`
	Point      string           `json:"point,omitempty"`
	Scope      []string         `json:"scope,omitempty"`
	Actions    int              `json:"actions,omitempty"`
	Attempts   int              `json:"attempts,omitempty"`
	Bound      string           `json:"bound,omitempty"`
	Allowance  int              `json:"allowance,omitempty"`
	Version    string           `json:"version,omitempty"`
	Plan       []gc.PlanUnit    `json:"plan,omitempty"`
	Artifact   string           `json:"artifact,omitempty"`
	ResultIDs  []int64          `json:"result_ids,omitempty"`
	Origin     string           `json:"origin,omitempty"`
}

// call is an ordinary-specialist request of the root session about one unit.
func call(action, event string) request {
	return request{Action: action, Event: event, Session: rootSession, Agent: "dev"}
}

// retry is a new delegation of a unit, which a distinct dispatch identity tells
// apart from a repeated host call.
func retry(unit string) request {
	r := call("admit", unit)
	r.Dispatch = "retry"
	return r
}

// tick is the accounting pass every request begins with, asked for on its own.
func tick() request { return request{Action: "tick", Session: rootSession} }

// declaration is a complete recovery declaration: a binary expected result,
// both budgets, the objective, a recoverable point and the explicit scope.
func declaration(unit, objective string, scope []string, actions, attempts int) request {
	return request{
		Action: "recovery", Event: unit, Session: rootSession, Objective: objective,
		Result: "the failing gate passes", Point: "journal/0", Scope: scope,
		Actions: actions, Attempts: attempts,
	}
}

// commit is a plan commitment covering the named units.
func commit(version string, units ...string) request {
	plan := make([]gc.PlanUnit, len(units))
	for i, unit := range units {
		plan[i] = gc.PlanUnit{Unit: unit, Contract: "deliver " + unit}
	}
	return request{Action: "commit", Session: rootSession, Version: version, Plan: plan}
}

// result is what one invocation of the binary produced.
type result struct {
	stdout, stderr string
	code           int
}

// decode reads the JSON answer into into.
func (r result) decode(t *testing.T, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.stdout), into); err != nil {
		t.Fatalf("answer %q is not the expected JSON: %v", r.stdout, err)
	}
}

// Wire names of the unit states a lifecycle answer reports.
const (
	wireInFlight = "in_flight"
	wireSettled  = "settled"
)

// transition is the part of a lifecycle answer that reports the slots held and
// the states a unit moved between.
type transition struct {
	Units    int    `json:"units"`
	InFlight int    `json:"in_flight"`
	From     string `json:"from_state"`
	To       string `json:"to_state"`
}

// session is one isolated installation: its own HOME, workspace and private
// state directory, driven through the compiled binary.
type session struct {
	t                 *testing.T
	bin               string
	root, state, home string
	env               []string
}

// commandPackage is the import path of the command under test.
const commandPackage = "github.com/rou-cru/takt-ai/takt/cli"

// buildTaktAI compiles the command in the package directory into a directory
// that disappears with t. Under `go test -cover` the binary is instrumented the
// same way, and the coverage directory go test exports reaches it through the
// session environment, so the statements it runs still count for this package.
func buildTaktAI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "takt-ai")
	args := []string{"build", "-buildvcs=false", "-o", bin}
	if mode := testing.CoverMode(); mode != "" {
		args = append(args, "-cover", "-covermode="+mode, "-coverpkg="+commandPackage)
	}
	if out, err := exec.Command("go", append(args, ".")...).CombinedOutput(); err != nil {
		t.Fatalf("build takt-ai: %v\n%s", err, out)
	}
	return bin
}

func newSession(t *testing.T, bin string) *session {
	t.Helper()
	return &session{t: t, bin: bin, root: t.TempDir(), state: filepath.Join(t.TempDir(), "state"), home: t.TempDir()}
}

// run executes the binary with only the environment the session grants it.
func (s *session) run(args ...string) result {
	s.t.Helper()
	cmd := exec.Command(s.bin, args...)
	cmd.Env = append([]string{"HOME=" + s.home, "TMPDIR=" + s.home, "PATH=" + os.Getenv("PATH")}, s.env...)
	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+dir)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		s.t.Fatalf("run takt-ai %v: %v", args, err)
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: cmd.ProcessState.ExitCode()}
}

func (s *session) invoke(command, subcommand string, r request) result {
	s.t.Helper()
	body, err := json.Marshal(r)
	if err != nil {
		s.t.Fatalf("marshal request: %v", err)
	}
	args := []string{command}
	if subcommand != "" {
		args = append(args, subcommand)
	}
	return s.run(append(args, "--workspace", s.root, "--state", s.state, "--request", string(body))...)
}

func (s *session) dispatch(r request) result { s.t.Helper(); return s.invoke("dispatch", "", r) }

// must requires the request to be accepted.
func (s *session) must(r request) result {
	s.t.Helper()
	res := s.dispatch(r)
	if res.code != 0 {
		s.t.Fatalf("%s %q was refused (exit %d): %s", r.Action, r.Event, res.code, res.stderr)
	}
	return res
}

// refused requires a non-zero exit and an empty answer.
func (s *session) refused(r request) result {
	s.t.Helper()
	res := s.dispatch(r)
	if res.code == 0 {
		s.t.Fatalf("%s %q was accepted: %s", r.Action, r.Event, res.stdout)
	}
	if res.stdout != "" {
		s.t.Fatalf("%s %q was refused yet answered: %s", r.Action, r.Event, res.stdout)
	}
	return res
}

// rejected requires a refusal that left the history untouched.
func (s *session) rejected(r request) result {
	s.t.Helper()
	before := len(s.entries())
	res := s.refused(r)
	if after := len(s.entries()); after != before {
		s.t.Fatalf("%s %q was refused but recorded %d entries", r.Action, r.Event, after-before)
	}
	return res
}

// deniedAs requires a refusal recorded as the harness's denial of r.Event for
// cause, with no work unit changing state.
func (s *session) deniedAs(r request, cause string) result {
	s.t.Helper()
	before := s.entries()
	res := s.refused(r)
	after := s.entries()
	var denials []history.Entry
	for _, e := range after[len(before):] {
		if e.Kind == history.KindDenied {
			denials = append(denials, e)
		}
	}
	if len(denials) != 1 || denials[0].Author != history.AuthorHarness || denials[0].Cause != cause || denials[0].WorkUnitID != r.Event {
		s.t.Fatalf("%s %q: recorded denials %+v, want one harness denial caused by %s", r.Action, r.Event, denials, cause)
	}
	if !reflect.DeepEqual(history.Project(before).Units, history.Project(after).Units) {
		s.t.Fatalf("%s %q was denied yet changed work state", r.Action, r.Event)
	}
	return res
}

// entries reads the recorded history once no process holds it.
func (s *session) entries() []history.Entry {
	s.t.Helper()
	h, err := history.Open(s.state)
	if err != nil {
		s.t.Fatalf("open history: %v", err)
	}
	defer func() {
		if err := h.Close(); err != nil {
			s.t.Fatalf("close history: %v", err)
		}
	}()
	return h.Entries()
}

func (s *session) projection() history.Projection { s.t.Helper(); return history.Project(s.entries()) }

func (s *session) unit(id string) history.Unit { s.t.Helper(); return s.projection().Units[id] }

func (s *session) recovery(objective string) history.Recovery {
	s.t.Helper()
	return s.projection().Budgets(rootSession).Recoveries[objective]
}

// count counts recorded entries of one kind.
func (s *session) count(kind history.Kind) int {
	s.t.Helper()
	n := 0
	for _, e := range s.entries() {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func (s *session) coordinator() *gc.Coordinator {
	s.t.Helper()
	c, err := gc.LoadCoordinator(s.state)
	if err != nil {
		s.t.Fatalf("load coordinator: %v", err)
	}
	return c
}

// stage records n tool executions for unit through the real VFS, the bus the
// plugin's tick reports. It returns the binding whose staged delta they build;
// a later call with that key continues the same attempt.
func (s *session) stage(key vfs.AgentID, unit, label string, n int) vfs.AgentID {
	s.t.Helper()
	fs, err := vfs.Open(s.root, s.state)
	if err != nil {
		s.t.Fatalf("open vfs: %v", err)
	}
	defer func() {
		if err := fs.Close(); err != nil {
			s.t.Fatalf("close vfs: %v", err)
		}
	}()
	if key == "" {
		if key, err = fs.Bind(vfs.Identity{SessionID: rootSession, WorkUnitID: unit, AgentID: "dev", Specialist: "dev"}, []string{unit + ".txt"}); err != nil {
			s.t.Fatalf("bind %s: %v", unit, err)
		}
	}
	for i := range n {
		op := vfs.Operation{
			Key: key, CallID: fmt.Sprintf("%s-%s-%d", unit, label, i), Action: vfs.OpCreate,
			Path: unit + ".txt", Content: []byte(fmt.Sprintf("%s %d\n", label, i)),
			ExpectedRevision: fs.InspectDelta(key).Revision,
		}
		if _, err := fs.Apply(op); err != nil {
			s.t.Fatalf("action %d of %s: %v", i, unit, err)
		}
	}
	return key
}

// staged counts the files a binding still holds staged.
func (s *session) staged(key vfs.AgentID) int {
	s.t.Helper()
	fs, err := vfs.Open(s.root, s.state)
	if err != nil {
		s.t.Fatalf("open vfs: %v", err)
	}
	n := len(fs.InspectDelta(key).Files)
	if err := fs.Close(); err != nil {
		s.t.Fatalf("close vfs: %v", err)
	}
	return n
}

// seedMemory writes the memory session index `takt-ai memory record` keeps
// under HOME: which author recorded which entry in the session.
func (s *session) seedMemory(id string, authors map[int64]string) {
	s.t.Helper()
	type entry struct {
		ID     int64  `json:"id"`
		Author string `json:"author"`
	}
	index := struct {
		Session string  `json:"session"`
		Entries []entry `json:"entries"`
	}{Session: id}
	for entryID, author := range authors {
		index.Entries = append(index.Entries, entry{ID: entryID, Author: author})
	}
	dir := filepath.Join(s.home, ".takt-ai", "memory", "sessions")
	if err := os.MkdirAll(dir, memoryDirMode); err != nil {
		s.t.Fatal(err)
	}
	data, err := json.Marshal(index)
	if err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, memoryFileMode); err != nil {
		s.t.Fatal(err)
	}
}

// policy is the admission policy the binary embeds.
func policy(t *testing.T) protocol.AdmissionPolicy {
	t.Helper()
	p, err := protocol.LoadAdmissionPolicy()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestDispatchCommand compiles the binary once and runs every scenario against
// it, each in its own installation.
func TestDispatchCommand(t *testing.T) {
	bin := buildTaktAI(t)
	for _, tc := range []struct {
		name string
		run  func(*testing.T, string)
	}{
		{"direct activities stay outside work units", directActivities},
		{"maintenance-cycle actions are left to gc coordinate", maintenanceActionsRefused},
		{"admission is recorded once and replays its disposition", admissionReplay},
		{"the ceiling denies admission until a termination releases a slot", admissionCeiling},
		{"an exception on the ceiling raises it by its allowance", ceilingException},
		{"admission recovers from an orphaned barrier hold", orphanedBarrierHold},
		{"launches collapse and an uncertain one is reconciled before repeating", launchAndReconciliation},
		{"cancellation keeps its slot while the controls stay available", cancellationRetainsCapacity},
		{"delegation without plan coverage is bounded", unplannedBound},
		{"contests need a recorded failure and are bounded", contestAllowance},
		{"consecutive failed recoveries close autonomous recovery", recoveryFailureStreak},
		{"an incomplete recovery declaration is recorded as such", incompleteDeclaration},
		{"exhausting the action budget forces backtracking and restoration reverts the scope", actionBudgetBacktracks},
		{"an exception on the spent bound reopens an abandoned recovery", exceptionReopensRecovery},
		{"the attempt budget bars admission and forces backtracking", attemptBudget},
		{"lost consumption blocks budgeted admission until reconciled", consumptionUnestablished},
		{"a restart or redeclaration grants no fresh allowance", consumptionSurvivesRestart},
		{"a handoff needs the results the holder recorded", handoffNeedsRecordedResults},
		{"the user ends a holder's turn", userAbortsSwitch},
		{"an optional filesystem copy never gates a nonstandard handoff", handoffOptionalCopy},
		{"validate_results against a real engram", validateResultsAgainstRealEngram},
		{"a delegation ends a maintenance cycle only once it is admitted", admissionYieldsCycle},
	} {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, bin) })
	}
}

func directActivities(t *testing.T, bin string) {
	s := newSession(t, bin)
	const id = "direct-activity-4a91"
	activity := request{Action: "activity_start", Session: rootSession, ActivityID: id, NodeKind: history.NodeKindOrchestrator}
	s.must(activity)
	if p := s.projection(); len(p.Units) != 0 || len(p.Activities) != 1 || p.Activities[id].State != history.StateInFlight {
		t.Fatalf("direct activity was not projected separately: %+v", p)
	}
	activity.Action, activity.Outcome = "activity_finish", string(history.OutcomeCompleted)
	s.must(activity)
	if a := s.projection().Activities[id]; a.State != history.StateSettled || a.Outcome != history.OutcomeCompleted {
		t.Fatalf("activity not settled: %+v", a)
	}
	for _, entry := range s.entries() {
		if entry.ActivityID == id && (entry.WorkUnitID != "" || entry.AttemptID != "") {
			t.Fatalf("activity used a work identity: %+v", entry)
		}
	}
}

func maintenanceActionsRefused(t *testing.T, bin string) {
	s := newSession(t, bin)
	s.rejected(request{Action: "baseline", Session: rootSession})
}

func admissionReplay(t *testing.T, bin string) {
	s := newSession(t, bin)
	admit := call("admit", "dispatch-1")
	var got transition
	s.must(admit).decode(t, &got)
	if got.Units != 1 || got.InFlight != 1 || got.From != "" || got.To != wireInFlight {
		t.Fatalf("first admission reported %+v", got)
	}
	s.must(admit).decode(t, &got)
	if got.Units != 1 || got.InFlight != 1 || got.To != wireInFlight {
		t.Fatalf("replayed admission reported %+v", got)
	}
	if n := s.count(history.KindAdmitted); n != 1 {
		t.Fatalf("a replayed admission was recorded %d times", n)
	}
	if u := s.unit("dispatch-1"); u.State != history.StateInFlight || u.Flight != history.FlightPendingLaunch || u.Launched {
		t.Fatalf("reservation reported as execution: %+v", u)
	}
	ordinary := call("admit", "cleanup-ordinary")
	ordinary.Agent = "simplify"
	s.must(ordinary)
	s.must(call("finish", "cleanup-ordinary"))
	s.must(call("finish", "dispatch-1")).decode(t, &got)
	if got.InFlight != 0 || got.From != wireInFlight || got.To != wireSettled {
		t.Fatalf("termination reported %+v", got)
	}
	if u := s.unit("dispatch-1"); u.State != history.StateSettled || u.Outcome != history.OutcomeCompleted {
		t.Fatalf("termination not settled: %+v", u)
	}
	s.must(call("finish", "dispatch-1")).decode(t, &got)
	if got.Units != 2 || s.count(history.KindTerminated) != 2 {
		t.Fatalf("a replayed termination counted again: %+v, %d terminations", got, s.count(history.KindTerminated))
	}
}

func admissionCeiling(t *testing.T, bin string) {
	s := newSession(t, bin)
	p := policy(t)
	for i := 1; i < p.Concurrency.Specialists; i++ {
		s.must(call("admit", fmt.Sprintf("dispatch-%d", i)))
	}
	// Two requests compete for the last slot: one reserves it, the other is denied.
	s.must(call("admit", "contender-a"))
	s.deniedAs(call("admit", "contender-b"), history.BoundConcurrency)
	before := s.projection()
	if before.InFlight() != p.Concurrency.Specialists {
		t.Fatalf("in flight %d; want %d", before.InFlight(), p.Concurrency.Specialists)
	}
	if _, ok := before.Units["contender-b"]; ok {
		t.Fatal("denied request created a work unit")
	}
	last := s.entries()[len(s.entries())-1]
	if last.PolicyRef != protocol.AdmissionPolicyRef {
		t.Fatalf("denial not attributed to the admission policy: %+v", last)
	}
	// Effective termination releases the slot. The four admissions also reached
	// the unplanned bound, so the denied work enters once a commitment covers
	// it: a plan enables the work it covers (PR-HAR-19, PR-DAG-MUT-1).
	s.must(call("finish", "contender-a"))
	s.deniedAs(call("admit", "contender-b"), history.BoundUnplanned)
	s.must(commit("v1", "contender-b"))
	s.must(call("admit", "contender-b"))
	if u := s.unit("contender-b"); u.Contract == "" || u.State != history.StateInFlight {
		t.Fatalf("commitment coverage lost: %+v", u)
	}
	// The commitment did not erase the accumulated unplanned count.
	if n := s.projection().Budgets(rootSession).Unplanned; n != p.Budgets.UnplannedUnits {
		t.Fatalf("unplanned count %d; want %d", n, p.Budgets.UnplannedUnits)
	}
}

func ceilingException(t *testing.T, bin string) {
	s := newSession(t, bin)
	p := policy(t)
	// Every unit is covered by the plan, so only the ceiling can bar them.
	units := make([]string, p.Concurrency.Specialists+2)
	for i := range units {
		units[i] = fmt.Sprintf("covered-%d", i)
	}
	s.must(commit("v1", units...))
	for _, unit := range units[:p.Concurrency.Specialists] {
		s.must(call("admit", unit))
	}
	s.deniedAs(call("admit", units[p.Concurrency.Specialists]), history.BoundConcurrency)
	s.must(request{Action: "exception", Session: rootSession, Bound: history.BoundConcurrency, Allowance: 1})
	s.must(call("admit", units[p.Concurrency.Specialists]))
	s.deniedAs(call("admit", units[p.Concurrency.Specialists+1]), history.BoundConcurrency)
	if n := s.projection().InFlight(); n != p.Concurrency.Specialists+1 {
		t.Fatalf("in flight %d; want the ceiling raised by exactly the allowance", n)
	}
}

// orphanedBarrierHold reproduces a real deadlock: a session leaves the
// workspace mid-resolution (an in-flight unit never finished, or a VFS delta
// staged and never verified/consolidated, which is what an interrupted session
// leaves behind with no liveness signal anywhere in the harness), then a second
// session insists on delegating new work the way an orchestrator does.
// Admission must recover within MaxDeferrals+1 attempts, never hang forever.
func orphanedBarrierHold(t *testing.T, bin string) {
	p, err := gc.LoadTriggerPolicy()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name           string
		orphanInFlight bool
		orphanDelta    bool
	}{
		{"orphaned in-flight unit", true, false},
		{"orphaned pending delta", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSession(t, bin)
			if tc.orphanInFlight {
				orphan := call("admit", "orphan-1")
				orphan.Session = "session-1"
				s.must(orphan)
				// admit/finish are history-only: on their own they leave
				// Mutations==0, and Decide forces Skip regardless of
				// UserRequested when Mutations==0 (ReasonNoDelta). A resolved
				// ordinary mutation, unrelated to the orphan, is needed so the
				// barrier is actually exercised for ActiveUnits alone.
				fs, err := vfs.Open(s.root, s.state)
				if err != nil {
					t.Fatal(err)
				}
				id := vfs.Identity{SessionID: "session-1", WorkUnitID: "keepalive", AgentID: "dev", Specialist: "dev"}
				author, err := fs.Bind(id, []string{"keepalive.txt"})
				if err != nil {
					t.Fatal(err)
				}
				applied, err := fs.Apply(vfs.Operation{Key: author, CallID: "write", Action: vfs.OpCreate, Path: "keepalive.txt", Content: []byte("ok\n")})
				if err != nil {
					t.Fatal(err)
				}
				id.AgentID, id.Specialist = "verify", "verify"
				verifier, err := fs.AssignVerifier(id, author)
				if err != nil {
					t.Fatal(err)
				}
				if err := fs.Verify(verifier, author, "verify", applied.Revision, applied.DeltaHash, true, ""); err != nil {
					t.Fatal(err)
				}
				if err := fs.ConsolidateCheckpoint(author, "keepalive", applied.Revision); err != nil {
					t.Fatal(err)
				}
				if err := fs.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if tc.orphanDelta {
				fs, err := vfs.Open(s.root, s.state)
				if err != nil {
					t.Fatal(err)
				}
				id := vfs.Identity{SessionID: "session-1", WorkUnitID: "orphan-delta", AgentID: "dev", Specialist: "dev"}
				author, err := fs.Bind(id, []string{"a.go"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = fs.Apply(vfs.Operation{Key: author, CallID: "write", Action: vfs.OpCreate, Path: "a.go", Content: []byte("package main\n")}); err != nil {
					t.Fatal(err)
				}
				if err := fs.Close(); err != nil {
					t.Fatal(err)
				}
			}
			// Makes a cycle due right away, without waiting out the cadence budget.
			if res := s.invoke("gc", "coordinate", request{Action: "request", Session: "session-1"}); res.code != 0 {
				t.Fatalf("request a cycle: %s", res.stderr)
			}
			// Session 2, the orchestrator, repeatedly tries to delegate brand-new
			// work: the "blocked from the first delegation attempt" of the incident.
			next := call("admit", "new-unit")
			next.Session = "session-2"
			var last result
			for range p.Cadence.MaxDeferrals + 1 {
				if last = s.dispatch(next); last.code == 0 {
					break
				}
			}
			if last.code != 0 {
				t.Fatalf("admission still blocked after %d attempts: %s", p.Cadence.MaxDeferrals+1, last.stderr)
			}
			if u := s.unit("new-unit"); u.State != history.StateInFlight {
				t.Fatalf("admission did not actually proceed: %+v", u)
			}
		})
	}
}

func launchAndReconciliation(t *testing.T, bin string) {
	s := newSession(t, bin)
	reconcile := func(event string, running bool) request {
		r := call("reconcile", event)
		r.Pass = running
		return r
	}
	s.must(call("admit", "one"))
	// The same launch delivered twice yields one execution, not two.
	s.must(call("launch", "one"))
	s.must(call("launch", "one"))
	if n := s.count(history.KindLaunched); n != 1 {
		t.Fatalf("launches recorded: %d", n)
	}
	// Communication fails after a possible launch: reconcile before any repeat,
	// and keep the reservation while the outcome is unknown.
	s.must(call("admit", "two"))
	s.must(call("uncertain", "two"))
	s.rejected(call("launch", "two"))
	if u := s.unit("two"); u.Flight != history.FlightUncertain || u.Launched {
		t.Fatalf("an uncertain launch was repeated: %+v", u)
	}
	if n := s.projection().InFlight(); n != 2 {
		t.Fatalf("uncertainty released capacity: in flight %d", n)
	}
	// Reconciliation that finds execution running keeps the slot and the unit.
	s.must(reconcile("two", true))
	if u := s.unit("two"); !u.Launched || u.State != history.StateInFlight {
		t.Fatalf("reconciled launch: %+v", u)
	}
	// Reconciliation that establishes non-start is the effective termination.
	s.must(call("admit", "three"))
	s.must(call("uncertain", "three"))
	s.must(reconcile("three", false))
	if u := s.unit("three"); u.State != history.StateSettled || u.Outcome != history.OutcomeInterrupted || u.Launched {
		t.Fatalf("non-start: %+v", u)
	}
}

func cancellationRetainsCapacity(t *testing.T, bin string) {
	s := newSession(t, bin)
	p := policy(t)
	for i := range p.Concurrency.Specialists {
		s.must(call("admit", fmt.Sprintf("unit-%d", i)))
	}
	s.must(call("cancel", "unit-0"))
	s.must(call("suspend", "unit-1"))
	before := s.projection()
	if before.InFlight() != p.Concurrency.Specialists {
		t.Fatalf("cancellation or suspension released capacity: in flight %d", before.InFlight())
	}
	if u := before.Units["unit-0"]; u.Flight != history.FlightCancelling {
		t.Fatalf("cancellation pending lost: %+v", u)
	}
	// Termination and resolution controls stay available with the ceiling full
	// and admit no further execution specialist.
	s.deniedAs(call("admit", "another"), history.BoundConcurrency)
	exception := call("exception", "unit-0")
	exception.Bound, exception.Allowance = history.BoundUnplanned, 1
	s.must(exception)
	s.must(call("finish", "unit-0"))
	after := s.projection()
	if u := after.Units["unit-0"]; u.State != history.StateSettled || u.Outcome != history.OutcomeInterrupted {
		t.Fatalf("cancelled unit settled as: %+v", u)
	}
	if after.InFlight() != p.Concurrency.Specialists-1 {
		t.Fatalf("effective termination did not release: in flight %d", after.InFlight())
	}
}

func unplannedBound(t *testing.T, bin string) {
	s := newSession(t, bin)
	p := policy(t)
	// Each unit is retired before the next so the ceiling never masks the bound.
	for i := range p.Budgets.UnplannedUnits {
		unit := fmt.Sprintf("loose-%d", i)
		s.must(call("admit", unit))
		// A unit cancelled and never launched still counts against the bound.
		if i == 0 {
			s.must(call("cancel", unit))
		}
		s.must(call("finish", unit))
		// A retry of the same unit adds no unit.
		s.must(retry(unit))
		s.must(call("finish", unit))
	}
	if n := s.projection().Budgets(rootSession).Unplanned; n != p.Budgets.UnplannedUnits {
		t.Fatalf("unplanned count %d; want %d", n, p.Budgets.UnplannedUnits)
	}
	s.deniedAs(call("admit", "loose-5"), history.BoundUnplanned)
	// Stopping delegation and sending an escalation are recorded distinctly and
	// reopen nothing.
	for _, action := range []string{"stop", "escalate"} {
		s.must(call(action, "loose-5"))
		s.deniedAs(call("admit", "loose-5"), history.BoundUnplanned)
	}
	if budgets := s.projection().Budgets(rootSession); budgets.Stopped != 1 || budgets.Escalated != 1 {
		t.Fatalf("stop and escalation not distinguishable: %+v", budgets)
	}
	// A scoped exception with a finite allowance enables exactly that much.
	exception := call("exception", "loose-5")
	exception.Bound, exception.Allowance = history.BoundUnplanned, 1
	s.must(exception)
	s.must(call("admit", "loose-5"))
	s.deniedAs(call("admit", "loose-6"), history.BoundUnplanned)
	// Consumption survives the restart every invocation already is.
	if n := s.projection().Budgets(rootSession).Unplanned; n != p.Budgets.UnplannedUnits+1 {
		t.Fatalf("consumption reset: %d", n)
	}
}

func contestAllowance(t *testing.T, bin string) {
	s := newSession(t, bin)
	p := policy(t)
	// Contested units end without a result: delegated, cancelled, then finished
	// as interrupted. All are planned, so the unplanned bound never masks the
	// contest allowance.
	failures := make([]string, 0, p.Budgets.Contests+2)
	for i := range p.Budgets.Contests {
		failures = append(failures, fmt.Sprintf("failure-%d", i))
	}
	failures = append(failures, "failure-late", "failure-later")
	s.must(commit("v1", failures...))
	for _, unit := range failures {
		s.must(call("admit", unit))
		s.must(call("cancel", unit))
		s.must(call("finish", unit))
	}
	// Work that completed, or never ran, has no failure to contest: the request
	// is refused and recorded, and it spends none of the allowance.
	s.must(call("admit", "delivered"))
	s.must(call("finish", "delivered"))
	s.deniedAs(call("contest", "delivered"), protocol.CauseNoFailure)
	s.deniedAs(call("contest", "never-ran"), protocol.CauseNoFailure)
	if n := len(s.projection().Budgets(rootSession).Contests); n != 0 {
		t.Fatalf("refused contests consumed %d of the allowance", n)
	}
	for _, unit := range failures[:p.Budgets.Contests] {
		s.must(call("contest", unit))
	}
	// A transport duplicate returns the recorded disposition and consumes nothing.
	s.must(call("contest", "failure-0"))
	if n := s.count(history.KindContested); n != p.Budgets.Contests {
		t.Fatalf("contests recorded: %d; want %d", n, p.Budgets.Contests)
	}
	s.deniedAs(call("contest", "failure-late"), history.BoundContests)
	if n := len(s.projection().Budgets(rootSession).Contests); n != p.Budgets.Contests {
		t.Fatalf("denied contest consumed: %d", n)
	}
	exception := call("exception", "failure-late")
	exception.Bound, exception.Allowance = history.BoundContests, 1
	s.must(exception)
	s.must(call("contest", "failure-late"))
	s.deniedAs(call("contest", "failure-later"), history.BoundContests)
}

func recoveryFailureStreak(t *testing.T, bin string) {
	s := newSession(t, bin)
	p := policy(t)
	declare := func(unit, objective string) request { return declaration(unit, objective, []string{unit}, 4, 1) }
	closeRecovery := func(unit, objective string, demonstrated bool) {
		t.Helper()
		s.must(request{Action: "recovered", Event: unit, Session: rootSession, Objective: objective, Pass: demonstrated, Evidence: "gate evidence"})
	}
	for i := range p.Budgets.RecoveryFailures {
		unit := fmt.Sprintf("attempt-%d", i)
		s.must(declare(unit, "goal"))
		closeRecovery(unit, "goal", false)
	}
	// Renaming the work does not reset the streak; the objective identity carries it.
	s.deniedAs(declare("renamed-work", "goal"), history.BoundRecovery)
	// Unrelated progress does not break it, and another objective is unaffected.
	s.must(call("admit", "elsewhere"))
	s.must(call("finish", "elsewhere"))
	s.deniedAs(declare("after-progress", "goal"), history.BoundRecovery)
	s.must(declare("other-attempt", "other-goal"))
	// An enabling decision names the objective it applies to, or it is not recorded.
	exception := request{Action: "exception", Session: rootSession, Bound: history.BoundRecovery, Allowance: 1}
	s.rejected(exception)
	// Scoped to the objective, it reopens exactly one recovery.
	exception.Objective = "goal"
	s.must(exception)
	s.must(declare("attempt-3", "goal"))
	// Only a demonstrated recovery breaks the streak.
	closeRecovery("attempt-3", "goal", true)
	if r := s.recovery("goal"); r.Failures != 0 || r.Open {
		t.Fatalf("demonstrated recovery did not break the streak: %+v", r)
	}
	s.must(declare("attempt-4", "goal"))
}

func incompleteDeclaration(t *testing.T, bin string) {
	s := newSession(t, bin)
	partial := declaration("unit", "goal", []string{"unit"}, 0, 2)
	partial.Point = ""
	s.deniedAs(partial, causeIncomplete)
	if r := s.recovery("goal"); r.Open || r.Actions != 0 {
		t.Fatalf("incomplete declaration opened bounded recovery: %+v", r)
	}
	// The record shows exactly what was declared and what was absent.
	recorded := 0
	for _, e := range s.entries() {
		if e.Kind != history.KindDenied {
			continue
		}
		recorded++
		if e.Objective != "goal" || e.Result == "" || e.Attempts != 2 || e.Point != "" || e.Actions != 0 {
			t.Fatalf("declaration not recorded as it arrived: %+v", e)
		}
	}
	if recorded != 1 {
		t.Fatalf("incomplete declaration recorded %d times", recorded)
	}
	// A declaration beyond the admissible ceiling is denied rather than trimmed.
	s.deniedAs(declaration("unit", "goal", []string{"unit"}, policy(t).Recovery.MaxActions+1, 1), history.BoundRecoveryActions)
	s.must(declaration("unit", "goal", []string{"unit"}, 4, 1))
	if r := s.recovery("goal"); !r.Open || r.Actions != 4 || r.Attempts != 1 || len(r.Scope) != 1 || r.Result == "" || r.Point == "" {
		t.Fatalf("complete declaration not recorded: %+v", r)
	}
}

// actionBudgetBacktracks covers PR-HAR-6 and PR-OBS-PRG-2: every recorded
// action in the scope consumes allowance whichever attempt performs it, and
// exhaustion forces backtracking through the tick the plugin already sends.
// Abandonment, termination and restoration stay separate facts, and restoring
// reverts the staged work of every unit in the scope, nothing else.
func actionBudgetBacktracks(t *testing.T, bin string) {
	s := newSession(t, bin)
	const budget = 3
	s.must(declaration("declaring-unit", "goal", []string{"scope-unit", "next-attempt"}, budget, 2))
	s.must(call("admit", "scope-unit"))
	// The admission declares its scope membership rather than being inferred.
	if entries := s.entries(); entries[len(entries)-1].Kind != history.KindAdmitted || entries[len(entries)-1].Objective != "goal" {
		t.Fatalf("attempt did not identify its scope: %+v", entries[len(entries)-1])
	}
	// Work outside the scope consumes nothing, whatever its timing.
	unrelated := s.stage("", "elsewhere", "noise", 2)
	s.must(tick())
	if r := s.recovery("goal"); r.Used != 0 || !r.Open {
		t.Fatalf("unrelated work consumed allowance: %+v", r)
	}
	// A first attempt spends part of the allowance.
	first := s.stage("", "scope-unit", "first", budget-1)
	s.must(tick())
	if r := s.recovery("goal"); r.Used != budget-1 || !r.Open {
		t.Fatalf("consumption after the first attempt: %+v", r)
	}
	// A second attempt of the same scope, under a binding of its own, keeps
	// spending the same allowance.
	s.must(call("admit", "next-attempt"))
	second := s.stage("", "next-attempt", "second", 1)
	s.must(tick())
	if r := s.recovery("goal"); r.Used != budget || r.Open || !r.Backtracked || r.Restored {
		t.Fatalf("exhausted action budget did not force backtracking: %+v", r)
	}
	// Restoration waits until every affected execution can no longer act.
	restore := request{Action: "restore", Session: rootSession, Objective: "goal"}
	s.rejected(restore)
	s.must(call("finish", "scope-unit"))
	s.rejected(restore)
	s.must(call("finish", "next-attempt"))
	for _, unit := range []string{"scope-unit", "next-attempt"} {
		if u := s.unit(unit); u.Outcome != history.OutcomeBacktracked {
			t.Fatalf("terminated scope work %s not backtracked: %+v", unit, u)
		}
	}
	if s.staged(first) == 0 || s.staged(second) == 0 {
		t.Fatal("abandonment alone reverted staged work")
	}
	// No further attempt enters the abandoned scope, and the refusal names the
	// two calls the agent goes on with.
	refusal := s.deniedAs(retry("scope-unit"), history.BoundRecoveryAttempts)
	for _, next := range []string{"dispatch_exception", "dispatch_restore"} {
		if !strings.Contains(refusal.stderr, next) {
			t.Fatalf("the refusal of an exhausted recovery does not name %s: %q", next, refusal.stderr)
		}
	}
	// A backtracked attempt is a terminal failure, so it can be contested.
	s.must(call("contest", "scope-unit"))
	if n := s.count(history.KindContested); n != 1 {
		t.Fatalf("contests recorded: %d", n)
	}
	// Restoring takes the objective alone and reverts the staged work of the
	// whole scope, never unrelated progress.
	s.must(restore)
	if r := s.recovery("goal"); !r.Restored {
		t.Fatalf("restoration not confirmed: %+v", r)
	}
	if s.staged(first) != 0 || s.staged(second) != 0 {
		t.Fatal("scope deltas survived restoration")
	}
	if s.staged(unrelated) == 0 {
		t.Fatal("restoration reverted work outside the scope")
	}
	// Abandonment, termination and restoration are ordered facts, and the
	// restoration came after both executions returned.
	var order []history.Kind
	for _, entry := range s.entries() {
		switch entry.Kind {
		case history.KindRecoveryClosed, history.KindTerminated, history.KindRestored:
			order = append(order, entry.Kind)
		}
	}
	want := []history.Kind{history.KindRecoveryClosed, history.KindTerminated, history.KindTerminated, history.KindRestored}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("abandonment, termination and restoration are not ordered facts: %v", order)
	}
	// A restoration is not repeatable, and missing evidence was not success.
	s.rejected(restore)
	for _, entry := range s.entries() {
		if entry.Kind == history.KindRecoveryClosed && (entry.Outcome != history.OutcomeBacktracked || entry.Evidence != "") {
			t.Fatalf("forced closure claimed a result: %+v", entry)
		}
	}
	// The scope no longer governs its units once restored.
	s.must(retry("scope-unit"))
	if entries := s.entries(); entries[len(entries)-1].Objective != "" {
		t.Fatalf("a restored scope still claims its units: %+v", entries[len(entries)-1])
	}
}

func exceptionReopensRecovery(t *testing.T, bin string) {
	s := newSession(t, bin)
	const budget, allowance = 2, 2
	s.must(declaration("declaring", "goal", []string{"scope-unit"}, budget, 2))
	s.must(call("admit", "scope-unit"))
	key := s.stage("", "scope-unit", "first", budget)
	s.must(tick())
	s.must(call("finish", "scope-unit"))
	if r := s.recovery("goal"); r.Open || !r.Backtracked || r.Restored {
		t.Fatalf("exhausted action budget did not abandon the recovery: %+v", r)
	}
	// Only the exception on the bound that was spent lifts the abandonment, and
	// it names the recovery it applies to.
	spentOther := request{Action: "exception", Session: rootSession, Bound: history.BoundRecoveryAttempts, Objective: "goal", Allowance: 1}
	s.rejected(spentOther)
	s.rejected(request{Action: "exception", Session: rootSession, Bound: history.BoundRecoveryActions, Allowance: allowance})
	s.must(request{Action: "exception", Session: rootSession, Bound: history.BoundRecoveryActions, Objective: "goal", Allowance: allowance})
	// The scope reopens with the allowance beside what it already consumed, and
	// its staged work was never touched.
	if r := s.recovery("goal"); !r.Open || r.Backtracked || r.Used != budget {
		t.Fatalf("the exception did not reopen the recovery beside its consumption: %+v", r)
	}
	if s.staged(key) == 0 {
		t.Fatal("reopening reverted staged work")
	}
	s.must(retry("scope-unit"))
	for i := range allowance {
		if r := s.recovery("goal"); !r.Open {
			t.Fatalf("the recovery closed with %d of the new allowance spent: %+v", i, r)
		}
		s.stage(key, "scope-unit", fmt.Sprintf("more-%d", i), 1)
		s.must(tick())
	}
	if r := s.recovery("goal"); r.Open || !r.Backtracked || r.Used != budget+allowance {
		t.Fatalf("the allowance did not exhaust at the recorded total: %+v", r)
	}
}

func attemptBudget(t *testing.T, bin string) {
	s := newSession(t, bin)
	s.must(declaration("declaring", "goal", []string{"last-attempt", "never"}, 40, 1))
	s.must(call("admit", "last-attempt"))
	s.deniedAs(call("admit", "never"), history.BoundRecoveryAttempts)
	// The admitted attempt finishes and presents evidence while allowance remains.
	s.must(tick())
	if r := s.recovery("goal"); !r.Open || r.Backtracked {
		t.Fatalf("last attempt cut short: %+v", r)
	}
	closing := request{Action: "recovered", Event: "declaring", Session: rootSession, Objective: "goal", Pass: true}
	s.rejected(closing)
	closing.Evidence = "journal/7"
	s.must(closing)
	if r := s.recovery("goal"); r.Open || r.Backtracked || r.Failures != 0 {
		t.Fatalf("demonstrated recovery: %+v", r)
	}
	// A second recovery whose only attempt finishes without the declared result is
	// backtracked by the harness, with no renewed approval.
	s.must(declaration("declaring", "other", []string{"only-attempt"}, 40, 1))
	s.must(call("admit", "only-attempt"))
	s.must(call("finish", "only-attempt"))
	s.must(tick())
	if r := s.recovery("other"); r.Open || !r.Backtracked || r.Failures != 1 {
		t.Fatalf("exhausted attempts did not force backtracking: %+v", r)
	}
	// The user's exception on the attempt bound reopens it for one more attempt.
	s.deniedAs(retry("only-attempt"), history.BoundRecoveryAttempts)
	s.must(request{Action: "exception", Session: rootSession, Bound: history.BoundRecoveryAttempts, Objective: "other", Allowance: 1})
	if r := s.recovery("other"); !r.Open || r.Backtracked {
		t.Fatalf("the exception did not reopen the recovery: %+v", r)
	}
	s.must(retry("only-attempt"))
}

func consumptionUnestablished(t *testing.T, bin string) {
	s := newSession(t, bin)
	s.must(declaration("declaring", "goal", []string{"scope-unit", "later"}, 8, 3))
	s.stage("", "scope-unit", "before", 2)
	s.must(tick())
	// The bus store is lost while the recorded history stands.
	if err := os.Remove(filepath.Join(s.state, "vfs.sqlite")); err != nil {
		t.Fatal(err)
	}
	s.must(tick())
	if r := s.recovery("goal"); !r.Unreconciled || r.Used != 2 {
		t.Fatalf("lost consumption not visible: %+v", r)
	}
	s.deniedAs(call("admit", "later"), history.BoundRecoveryActions)
	// Reconciliation is a recorded decision, and it keeps what was consumed.
	s.must(request{Action: "exception", Session: rootSession, Bound: history.BoundRecoveryActions, Objective: "goal", Allowance: 2})
	s.must(call("admit", "later"))
	if r := s.recovery("goal"); r.Unreconciled || r.Used != 2 {
		t.Fatalf("reconciliation erased consumption: %+v", r)
	}
}

// consumptionSurvivesRestart is the anti-fraud property of PR-OBS-PRG-2 and
// PR-HAR-22: a restart mid-recovery grants no fresh allowance, and redeclaring
// the same objective does not reset what was consumed.
func consumptionSurvivesRestart(t *testing.T, bin string) {
	s := newSession(t, bin)
	const budget = 4
	declare := declaration("declaring", "goal", []string{"scope-unit"}, budget, 2)
	s.must(declare)
	key := s.stage("", "scope-unit", "before", budget/2)
	s.must(tick())
	if r := s.recovery("goal"); r.Used != budget/2 {
		t.Fatalf("consumption before the restart: %+v", r)
	}
	// The runtime restarts: transient coordinator state is gone, the recorded
	// history is not.
	if err := os.Remove(filepath.Join(s.state, "gc-coordinator.json")); err != nil {
		t.Fatal(err)
	}
	s.must(tick())
	// Redeclaring the same objective mid-recovery yields the recorded
	// disposition; it is not a fresh budget.
	s.must(declare)
	if r := s.recovery("goal"); r.Used != budget/2 || !r.Open || r.Actions != budget {
		t.Fatalf("restart or redeclaration granted fresh allowance: %+v", r)
	}
	// The remaining allowance is what was left, so exhaustion arrives at the same
	// recorded total.
	s.stage(key, "scope-unit", "after", budget/2)
	s.must(tick())
	if r := s.recovery("goal"); r.Used != budget || !r.Backtracked {
		t.Fatalf("budget did not exhaust at the recorded total: %+v", r)
	}
}

func handoffNeedsRecordedResults(t *testing.T, bin string) {
	// A stand-in Engram that has recorded observation 123 and nothing else.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health", "/observations/123":
			_, _ = w.Write([]byte(`{"id":123}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	s := newSession(t, bin)
	s.env = append(s.env, "ENGRAM_BASE_URL="+server.URL)
	s.seedMemory(rootSession, map[int64]string{123: "pm", 124: "dev", 999: "pm"})
	validate := func(session string, ids ...int64) request {
		return request{Action: "validate_results", Session: session, Agent: "pm", ResultIDs: ids}
	}
	s.rejected(validate(rootSession, 999)) // indexed for pm, but Engram does not have it
	s.rejected(validate(rootSession, 124)) // recorded by another author
	s.rejected(validate("other", 123))     // recorded in another session
	s.must(validate(rootSession, 123))
	const artifact = "PRD.md"
	s.must(request{Action: "switch", Session: rootSession, Child: "child-1", Agent: "pm", Artifact: artifact})
	if holder := s.projection().Budgets(rootSession).InterlocutorHolder; holder != "child-1" {
		t.Fatalf("the switch did not hand the interface to the holder: %q", holder)
	}
	if err := os.WriteFile(filepath.Join(s.root, artifact), []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The optional copy exists, yet without a result the holder recorded the
	// handoff is refused and the holder keeps the interface.
	handoff := request{Action: "handoff", Session: rootSession, Child: "child-1", Agent: "pm", Result: "Standard"}
	s.rejected(handoff)
	handoff.ResultIDs = []int64{999}
	s.rejected(handoff)
	handoff.ResultIDs = []int64{124}
	s.rejected(handoff)
	if holder := s.projection().Budgets(rootSession).InterlocutorHolder; holder != "child-1" {
		t.Fatalf("a refused handoff released the interface: %q", holder)
	}
	// A valid ID suffices with no filesystem copy; the missing copy is reported.
	if err := os.Remove(filepath.Join(s.root, artifact)); err != nil {
		t.Fatal(err)
	}
	handoff.ResultIDs = []int64{123}
	var resp map[string]any
	s.must(handoff).decode(t, &resp)
	if resp["artifact_verified"] != false {
		t.Fatalf("missing optional copy was not reported: %+v", resp)
	}
	if resp["result"] != "Standard" {
		t.Fatalf("handoff result missing: %+v", resp)
	}
	if ids, ok := resp["memory"].([]any); !ok || len(ids) != 1 || ids[0] != float64(123) {
		t.Fatalf("handoff response did not carry the specific result ID: %+v", resp)
	}
	if holder := s.projection().Budgets(rootSession).InterlocutorHolder; holder != "" || s.count(history.KindInterlocutorHandoff) != 1 {
		t.Fatalf("the handoff did not return the interface: holder %q", holder)
	}
}

// userAbortsSwitch covers IR-24: the user may end a temporary holder's turn
// without negotiation, after a prior switch.
func userAbortsSwitch(t *testing.T, bin string) {
	s := newSession(t, bin)
	s.seedMemory(rootSession, map[int64]string{41: "pm", 42: "takt"})
	s.must(request{Action: "switch", Session: rootSession, Child: "child-2", Agent: "pm", Artifact: "PRD.md"})
	var resp map[string]any
	s.must(request{Action: "abort_switch", Session: rootSession, Child: "child-2", Evidence: "user cancelled the switch", Origin: "user"}).decode(t, &resp)
	if resp["result"] != "Aborted" {
		t.Fatalf("abort_switch result: %+v", resp)
	}
	// The envelope carries what the holder recorded, not the whole session's memory.
	if ids, ok := resp["memory"].([]any); !ok || len(ids) != 1 || ids[0] != float64(41) {
		t.Fatalf("abort_switch memory is not the holder's entries: %+v", resp)
	}
	// PR-HAR-24: the artifact is never required to abort, but its absence is
	// still verified and reported.
	if resp["artifact_verified"] != false {
		t.Fatalf("abort with no artifact on disk must report artifact_verified = false: %+v", resp)
	}
	if holder := s.projection().Budgets(rootSession).InterlocutorHolder; holder != "" {
		t.Fatalf("the abort left the holder in place: %q", holder)
	}
}

func handoffOptionalCopy(t *testing.T, bin string) {
	for _, result := range []string{"EarlyHandoff", "TechFault", "Outraged"} {
		for _, present := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/present=%t", result, present), func(t *testing.T) {
				s := newSession(t, bin)
				if present {
					if err := os.WriteFile(filepath.Join(s.root, "copy.md"), []byte("draft"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				s.must(request{Action: "switch", Session: rootSession, Child: "child", Agent: "pm", Artifact: "copy.md"})
				handoff := request{Action: "handoff", Session: rootSession, Child: "child", Agent: "pm", Result: result, ResultIDs: []int64{123}}
				s.rejected(handoff) // an unrecorded result ID is refused whatever the result
				handoff.ResultIDs = nil
				var resp map[string]any
				s.must(handoff).decode(t, &resp)
				if resp["artifact_verified"] != present || resp["result"] != result {
					t.Fatalf("unexpected handoff metadata: %+v", resp)
				}
			})
		}
	}
}

// scrubFakeEngramPath drops any PATH entry from the fake engram stub this
// package's TestMain installs (os.MkdirTemp("", "takt-tui-engram-")), so the
// real-engram scenario can force a genuine download instead of finding it.
func scrubFakeEngramPath(path string) string {
	entries := strings.Split(path, string(os.PathListSeparator))
	kept := entries[:0]
	for _, entry := range entries {
		if !strings.Contains(entry, "takt-tui-engram-") {
			kept = append(kept, entry)
		}
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// validateResultsAgainstRealEngram is E2E-D: every other coverage of
// validate_results fakes Engram with an httptest.Server. This one downloads the
// pinned release, runs a real `engram serve`, saves a real observation through
// its real HTTP API, and drives the real dispatch action against it, the one
// link the delivery rewrite depends on that no test exercised without a mock. It
// runs only inside the disposable test container
// (development/testing/test-containerized.sh): it never touches a host data
// directory.
func validateResultsAgainstRealEngram(t *testing.T, bin string) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Skip("E2E-D runs only inside the test container")
	}
	t.Setenv("HOME", t.TempDir())
	// The package's TestMain puts a fake `engram` stub first on PATH for every
	// other test's sake; strip it so Acquire's PATH lookup can't shadow the real
	// pinned release this scenario needs.
	t.Setenv("PATH", scrubFakeEngramPath(os.Getenv("PATH")))
	binary, err := engram.Acquire(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("acquire real engram: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	server := exec.Command(binary, "serve", strconv.Itoa(port))
	server.Env = append(os.Environ(), "ENGRAM_DATA_DIR="+t.TempDir())
	var serverOut bytes.Buffer
	server.Stdout = &serverOut
	server.Stderr = &serverOut
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- server.Wait() }()
	t.Cleanup(func() { _ = server.Process.Kill() })
	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.After(engramReadyTimeout)
waitLoop:
	for {
		select {
		case waitErr := <-exited:
			t.Fatalf("engram serve exited early (%v): %s", waitErr, serverOut.String())
		case <-deadline:
			t.Fatalf("engram serve never listened: %s", serverOut.String())
		case <-time.After(100 * time.Millisecond):
			if conn, dialErr := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); dialErr == nil {
				_ = conn.Close()
				break waitLoop
			}
		}
	}

	sessionBody, err := json.Marshal(map[string]any{
		"id": "e2e-d-session", "project": "fixture", "directory": "/tmp/e2e-d",
	})
	if err != nil {
		t.Fatal(err)
	}
	// An observation belongs to a session; the real server rejects a post
	// under a session id it hasn't seen yet.
	sessionResp, err := http.Post(baseURL+"/sessions", "application/json", bytes.NewReader(sessionBody))
	if err != nil {
		t.Fatalf("post real session: %v", err)
	}
	_ = sessionResp.Body.Close()

	body, err := json.Marshal(map[string]any{
		"session_id": "e2e-d-session",
		"type":       "decision",
		"title":      "real engram fixture",
		"content":    "written by the real-engram dispatch scenario",
		"tool_name":  "test",
		"project":    "fixture",
		"scope":      "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(baseURL+"/observations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post real observation: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var created struct {
		ID int64 `json:"id"`
	}
	if e := json.NewDecoder(resp.Body).Decode(&created); e != nil || created.ID == 0 {
		t.Fatalf("real engram did not return an id: %v", e)
	}

	missing := created.ID + 1_000_000
	s := newSession(t, bin)
	s.env = append(s.env, "ENGRAM_BASE_URL="+baseURL)
	s.seedMemory(rootSession, map[int64]string{created.ID: "pm", missing: "pm"})
	validate := func(id int64) request {
		return request{Action: "validate_results", Session: rootSession, Agent: "pm", ResultIDs: []int64{id}}
	}
	s.rejected(validate(missing)) // a nonexistent ID is refused by the real engram
	s.must(validate(created.ID))  // its own freshly-saved observation is accepted
}

func admissionYieldsCycle(t *testing.T, bin string) {
	s := newSession(t, bin)
	p := policy(t)
	for i := range p.Concurrency.Specialists {
		s.must(call("admit", fmt.Sprintf("busy-%d", i)))
	}
	c := s.coordinator()
	c.Cycle = &gc.Cycle{Plan: gc.Plan{Request: gc.Request{CycleID: "cycle-1", SessionID: rootSession, Mandate: gc.MandateDeadCode}}, Phase: "collect"}
	if err := gc.SaveCoordinator(s.state, c); err != nil {
		t.Fatal(err)
	}
	// A denied delegation leaves the cycle running.
	s.deniedAs(call("admit", "late"), history.BoundConcurrency)
	if c := s.coordinator(); c.Cycle == nil || c.Cycle.Phase != "collect" {
		t.Fatalf("a denied delegation ended the cycle: %+v", c.Cycle)
	}
	if _, ended := s.projection().Activities["cycle-1"]; ended {
		t.Fatal("a denied delegation settled the cycle's activity")
	}
	// An admitted one ends it, discarding the cycle and settling its activity as
	// interrupted (PR-MNT-3).
	s.must(call("finish", "busy-0"))
	s.must(commit("v1", "late"))
	s.must(call("admit", "late"))
	if c := s.coordinator(); c.Cycle != nil || len(c.History) != 1 || c.History[0].Plan.CycleID != "cycle-1" {
		t.Fatalf("admitted delegation did not end the cycle: %+v", c)
	}
	if a := s.projection().Activities["cycle-1"]; a.State != history.StateSettled || a.Outcome != history.OutcomeInterrupted {
		t.Fatalf("the cycle's activity was not settled as interrupted: %+v", a)
	}
}
