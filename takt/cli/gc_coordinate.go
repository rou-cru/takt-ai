package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	protocol "github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// actionNoChange names the coordination action/reason recorded when a cycle
// closes without proposing or refuting any findings.
const actionNoChange = "no-change"

var gcCoordinateActions = map[string]bool{
	"prepare": true, "status": true, "request": true, "tick": true,
	"attach": true, "prompt": true, "recover": true, "abort": true,
	"baseline": true, "findings": true, "investigate": true,
	"authorize": true, "collected": true, "delta": true,
	"verdict": true, "acceptance": true, actionNoChange: true,
}

func runGCCoordinate(args []string, stdout, stderr io.Writer) (err error) {
	flags := flag.NewFlagSet("gc coordinate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspace := flags.String("workspace", "", "workspace")
	state := flags.String("state", "", "private state")
	raw := flags.String("request", "", "harness JSON request")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if *workspace == "" || *state == "" || flags.NArg() != 0 {
		return errors.New("gc coordinate: --workspace --state --request required")
	}
	var req coordinationRequest
	if e := json.Unmarshal([]byte(*raw), &req); e != nil {
		return e
	}
	if !gcCoordinateActions[req.Action] {
		return fmt.Errorf("gc coordinate: %q is not a maintenance-cycle action", req.Action)
	}
	fs, e := vfs.Open(*workspace, *state)
	if e != nil {
		return e
	}
	defer func() { err = errors.Join(err, fs.Close()) }()
	h, e := history.Open(*state)
	if e != nil {
		return e
	}
	defer func() { err = errors.Join(err, h.Close()) }()
	if req.Action != "status" {
		_, laneCaller := h.Project().ExecutionForChild(req.Session, "")
		if handled, err := admitRepair(stdout, fs, &req.Repair, !laneCaller); handled {
			return err
		}
	}
	c, e := gc.LoadCoordinator(fs)
	if e != nil {
		return e
	}
	result, e := coordinateGC(context.Background(), fs, h, *workspace, *state, c, req)
	if e != nil {
		if handled, err := refuseForRecovery(stdout, fs); handled {
			return err
		}
	}
	// Persist abort/escalation state even if the effect failed. The barrier stays held.
	if save := saveGCCoordinator(fs, c, req.Action); save != nil {
		return errors.Join(e, save)
	}
	if e != nil {
		if out := repairRefusal(e); out != nil {
			return json.NewEncoder(stdout).Encode(out)
		}
		return e
	}
	return json.NewEncoder(stdout).Encode(result)
}

func coordinatePrepare(ctx context.Context, workspace, state string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if c.Held() {
		return nil, errors.New("gc: preparation is ordinary work, not permitted during a cycle")
	}
	return gc.Prepare(ctx, workspace, state, r.Session)
}

func coordinateGC(ctx context.Context, fs *vfs.FS, h *history.History, workspace, state string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if r.Action == "status" {
		c.ObserveExecutions(h)
		return c, nil
	}
	entries, err := observeCoordination(fs, h, c, r.Session)
	if err != nil {
		return nil, err
	}
	switch r.Action {
	case "prepare":
		return coordinatePrepare(ctx, workspace, state, c, r)
	case "status":
		return c, nil
	case "request", "tick":
		if r.Action == "request" {
			c.Requested = true
		}
		return coordinateAdvance(ctx, fs, h, workspace, c, entries, r)
	case "attach":
		return attachGCLane(h, c, r)
	case "prompt":
		return claimGCPrompt(h, c, r)
	case "recover", "abort":
		return coordinateAbort(h, fs, workspace, c, r)
	case "baseline":
		return coordinateBaseline(ctx, workspace, state, c, r)
	case "findings":
		return coordinateFindings(c, r)
	case "investigate":
		return coordinateInvestigation(c, r)
	case "authorize":
		return coordinateAuthorization(fs, h, c, r)
	case "collected":
		return coordinateCollected(fs, c, r)
	case "delta":
		return coordinateDelta(fs, c, r)
	case "verdict":
		return coordinateVerdict(h, fs, workspace, state, c, r)
	case "acceptance":
		return coordinateAcceptance(ctx, h, fs, workspace, state, c, r)
	case actionNoChange:
		return coordinateNoChange(h, fs, workspace, c, r)
	default:
		return nil, fmt.Errorf("gc: unknown coordination action %q", r.Action)
	}
}

func coordinateAbort(h *history.History, fs *vfs.FS, workspace string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if c.Cycle == nil || (r.Event != "" && r.Event != c.Cycle.Plan.CycleID) {
		return c, nil
	}
	if err := recordLaneStops(h, c, r.Stopped); err != nil {
		return c, err
	}
	return c, abortGCCycle(h, fs, workspace, c, "aborted: "+r.Evidence, r.Repair)
}

func coordinateBaseline(ctx context.Context, workspace, state string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "verifier"); e != nil {
		return nil, e
	}
	if c.Cycle.Phase != "baseline" {
		return nil, errors.New("gc: baseline phase required")
	}
	p, e := gc.LoadPreparation(workspace, state)
	if e != nil {
		return nil, e
	}
	c.Cycle.Baseline = gc.RunChecks(ctx, workspace, p)
	c.Cycle.Phase = "investigate"
	report, e := gc.AnalyzePrepared(ctx, workspace, c.Cycle.Plan, p)
	if e != nil {
		c.Cycle.Reason = e.Error()
		return c, e
	}
	c.Cycle.Report = report
	return c, nil
}

func coordinateFindings(c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "collector"); e != nil {
		return nil, e
	}
	return c.Cycle, nil
}

func coordinateInvestigation(c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "collector"); e != nil {
		return nil, e
	}
	if c.Cycle.Phase != "investigate" {
		return nil, errors.New("gc: investigation phase required")
	}
	rs, e := gc.Investigate(c.Cycle.Plan, c.Cycle.Report, c.Cycle.Investigations, gc.Investigation{FindingID: r.Finding, Outcome: r.Outcome, Evidence: r.Evidence, Instance: gc.CollectorSpecialistID, At: time.Now().UTC()})
	if e != nil {
		return nil, e
	}
	c.Cycle.Investigations = rs
	return c.Cycle, nil
}

// coordinateAuthorization turns a cycle's investigated findings into the
// collector's scoped binding and the verifier's gate over it; a retry reuses
// what an earlier try reserved.
func coordinateAuthorization(fs *vfs.FS, h *history.History, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "collector"); e != nil {
		return nil, e
	}
	if c.Cycle.Phase != "investigate" {
		return nil, errors.New("gc: investigation phase required")
	}
	if !gc.ChecksPass(c.Cycle.Baseline) {
		return c, errors.New("gc: failing or missing baseline permits proposals only")
	}
	scope, e := gc.AuthorizedScope(c.Cycle.Plan, c.Cycle.Report.Findings, c.Cycle.Investigations)
	if e != nil {
		return nil, e
	}
	if len(scope) == 0 {
		return nil, errors.New("gc: no explicitly investigated findings authorize mutation")
	}
	// The collector lane's session holds the binding, so its commands resolve it.
	collector := cycleIdentity(h, c.Cycle, gc.CollectorSpecialistID)
	collector.HostSession = r.Session
	verifier := cycleIdentity(h, c.Cycle, gc.VerifierSpecialistID)
	c.Cycle.AuthorKey, c.Cycle.VerifierKey, e = fs.PrepareMaintenance(collector, verifier, scope)
	if e != nil {
		return nil, fmt.Errorf("gc authorization preparation refused: %w; no partial lane reservation was retained; retry gc_authorize", e)
	}
	c.Cycle.Scope, c.Cycle.Phase = scope, "collect"
	return c.Cycle, nil
}

func coordinateCollected(fs *vfs.FS, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "collector"); e != nil {
		return nil, e
	}
	if c.Cycle.Phase != "collect" {
		return nil, errors.New("gc: collect phase required")
	}
	c.Cycle.Phase = "verify"
	return fs.InspectDelta(c.Cycle.AuthorKey), nil
}

func coordinateDelta(fs *vfs.FS, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "verifier"); e != nil {
		return nil, e
	}
	return fs.JudgeDelta(c.Cycle.VerifierKey, c.Cycle.AuthorKey)
}

func coordinateVerdict(h *history.History, fs *vfs.FS, workspace, state string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "verifier"); e != nil {
		return nil, e
	}
	if c.Cycle.Phase != "verify" {
		return nil, errors.New("gc: verify phase required")
	}
	if r.Evidence == "" {
		return nil, errors.New("gc: verdict evidence required")
	}
	delta := fs.InspectDelta(c.Cycle.AuthorKey)
	if e := fs.VerifyJudgedDelta(c.Cycle.VerifierKey, c.Cycle.AuthorKey, "gc-verdict/"+c.Cycle.Plan.CycleID, r.Pass, r.Evidence); e != nil {
		return nil, e
	}
	if !r.Pass {
		return c, abortGCCycle(h, fs, workspace, c, "independent verifier rejected delta", nil)
	}
	// Persist intent before the physical effect; restart always discards uncertain work.
	c.Cycle.Phase = "consolidating"
	if e := gc.SaveCoordinator(fs, c); e != nil {
		return nil, e
	}
	if e := fs.ConsolidateCheckpoint(c.Cycle.AuthorKey, c.Cycle.Plan.CycleID, delta.Revision, false); e != nil {
		return nil, e
	}
	c.Cycle.Phase = "acceptance"
	return c, nil
}

func coordinateAcceptance(ctx context.Context, h *history.History, fs *vfs.FS, workspace, state string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "verifier"); e != nil {
		return nil, e
	}
	if c.Cycle.Phase != "acceptance" {
		return nil, errors.New("gc: materialized acceptance phase required")
	}
	p, e := gc.LoadPreparation(workspace, state)
	if e != nil {
		return c, errors.Join(e, abortGCCycle(h, fs, workspace, c, "preparation changed", nil))
	}
	c.Cycle.Acceptance = gc.RunChecks(ctx, workspace, p)
	if !gc.ChecksPass(c.Cycle.Acceptance) {
		return c, abortGCCycle(h, fs, workspace, c, "acceptance regression or missing evidence", nil)
	}
	cycleRecord(workspace, c.Cycle, "applied")
	if e = settleGCCycleActivity(h, fs, c.Cycle, history.OutcomeCompleted); e != nil {
		return nil, e
	}
	// Closure is durably recorded before releasing retained rollback state.
	id := c.Cycle.Plan.CycleID
	closed := c.Cycle
	c.Close("applied")
	reportGCTelemetry(evaluateMandateReversionRate(workspace, closed))
	if e = gc.SaveCoordinator(fs, c); e != nil {
		return nil, e
	}
	return c, fs.CompleteCycle(id)
}

func coordinateNoChange(h *history.History, fs *vfs.FS, workspace string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "collector"); e != nil {
		return nil, e
	}
	if c.Cycle.Phase != "investigate" {
		return nil, errors.New("gc: no-change is only allowed before mutation")
	}
	reason := actionNoChange
	if len(c.Cycle.Report.Findings) > 0 {
		reason = "proposals-or-refuted"
	}
	cycleRecord(workspace, c.Cycle, reason)
	if e := settleGCCycleActivity(h, fs, c.Cycle, history.OutcomeCompleted); e != nil {
		return nil, e
	}
	closed := c.Cycle
	c.Close(reason)
	reportGCTelemetry(evaluateMandateReversionRate(workspace, closed))
	return c, nil
}

func coordinateAdvance(ctx context.Context, fs *vfs.FS, h *history.History, workspace string, c *gc.Coordinator, entries []vfs.JournalEntry, r coordinationRequest) (any, error) {
	if r.Session == "" {
		return nil, errors.New("gc: root session required")
	}
	d, b, e := c.Advance(ctx, fs, h, entries, r.Session, nil)
	if e != nil {
		return nil, e
	}
	if c.Cycle != nil {
		if e = startGCCycleActivity(h, c.Cycle); e != nil {
			return nil, e
		}
	}
	if rec, ok := d.ControlRecord(harnessAgent); ok {
		rec.Correlations = obs.CorrelationIDs{SessionID: r.Session}
		reportGCTelemetry(recordControlAction(workspace, rec))
	}
	if rec, ok := b.ControlRecord(harnessAgent); ok {
		rec.Correlations = obs.CorrelationIDs{SessionID: r.Session}
		reportGCTelemetry(recordControlAction(workspace, rec))
	}
	return c, nil
}
func cycleIdentity(h *history.History, c *gc.Cycle, agent string) vfs.Identity {
	// The invariant a maintenance delta is judged against is the reviewed project
	// configuration the cycle already froze: it declares the acceptance checks and
	// each analyzer with its tool and exact version, which is the declared norm a
	// tool can check conformance against (PR-MNT-15, PR-MNT-17, PR-VFS-CSL-3).
	// The attempt is the one history issued when this lane attached; a lane
	// that is not attached carries none, which the core refuses.
	role := "collector"
	if agent == gc.VerifierSpecialistID {
		role = "verifier"
	}
	unit, _ := history.LaneUnit(c.Plan.CycleID, role)
	var attempt string
	if d, ok := h.Project().ExecutionForChild(c.Sessions[role], agent); ok && d.Cycle == c.Plan.CycleID && d.Role == role {
		attempt = d.Attempt
	}
	return vfs.Identity{AttemptID: attempt, SessionID: c.Plan.SessionID, WorkUnitID: unit, AgentID: vfs.AgentID(agent), Specialist: agent, Invariants: vfs.InvariantSet{gc.ProjectConfigPath}, CycleID: c.Plan.CycleID, MandateClass: string(c.Plan.Mandate)}
}
func reportGCTelemetry(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "gc telemetry unavailable: %v; execution history and VFS remain authoritative\n", err)
	}
}

// cycleRecord reports the cycle's outcome as telemetry; closure never depends on it.
func cycleRecord(workspace string, c *gc.Cycle, outcome string) {
	reportGCTelemetry(recordControlAction(workspace, obs.ControlRecord{ActionClass: obs.ActionObserve, TriggeringCondition: fmt.Sprintf("gc cycle %s (cycle=%s mandate=%s)", outcome, c.Plan.CycleID, c.Plan.Mandate), PolicyRef: gc.AcceptancePolicyRef, ActingAgent: harnessAgent, Correlations: obs.CorrelationIDs{SessionID: c.Plan.SessionID}}))
}

// evaluateMandateReversionRate runs PR-MNT-32's demotion check for the
// mandate class the closing cycle declared, over that mandate's whole
// session history (PR-MNT-9/PR-MNT-11: registered work, never a wall-clock
// window) — every cycle close is a natural point to re-check whether that
// mandate class's consolidated work keeps coming back.
func evaluateMandateReversionRate(workspace string, c *gc.Cycle) error {
	store, err := obs.OpenStore(workspace)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	clock := obs.NewClock()
	bus := obs.NewBus(c.Plan.SessionID, clock)
	bus.AttachStore(store)
	_, err = gc.EvaluateMandateReversionRate(store, bus, clock, gc.MandateReversionRateInput{
		Agent: harnessAgent, SessionID: c.Plan.SessionID, Mandate: c.Plan.Mandate,
		AfterID: 0, BeforeID: 0, WorkUnitID: c.Plan.CycleID,
	})
	return err
}
func saveGCCoordinator(fs *vfs.FS, c *gc.Coordinator, action string) error {
	if action == "status" {
		return nil
	}
	return gc.SaveCoordinator(fs, c)
}

type laneStop struct {
	Dispatch string `json:"dispatch"`
	Attempt  string `json:"attempt"`
	Child    string `json:"child"`
}

// recordLaneStops records exact host termination without releasing lane resources.
// All supplied correlations are checked before the first fact is appended.
func recordLaneStops(h *history.History, c *gc.Coordinator, stops []laneStop) error {
	for _, stop := range stops {
		d, ok := h.Project().Executions[stop.Dispatch]
		if !ok || d.Cycle != c.Cycle.Plan.CycleID || d.Attempt != stop.Attempt || d.Child != stop.Child {
			return errors.New("gc: stale lane termination; stop and observe the current cycle's exact sessions through host controls")
		}
	}
	for _, stop := range stops {
		d := h.Project().Executions[stop.Dispatch]
		if d.Settled || d.Terminated {
			continue
		}
		if err := h.ExecutionFact(d, history.KindLaneStopped, "", "", nil); err != nil {
			return err
		}
	}
	return nil
}

type requiredLanes struct {
	Cycle string
	Lanes []laneStop
}

func (e *requiredLanes) Error() string {
	return fmt.Sprintf("gc: cycle %s still has active native lanes %v; stop these exact sessions through host controls and observe termination before abort or admission; resources remain held", e.Cycle, e.Lanes)
}

type requiredRepair struct {
	Repair *vfs.Repair
	err    error
}

func (e *requiredRepair) Error() string { return e.Repair.Reason + ": " + e.err.Error() }
func (e *requiredRepair) Unwrap() error { return e.err }
func repairRefusal(err error) *response {
	var r *requiredRepair
	if errors.As(err, &r) {
		return &response{OK: false, Error: r.Error(), Repair: r.Repair}
	}
	var lanes *requiredLanes
	if errors.As(err, &lanes) {
		return &response{OK: false, Error: lanes.Error(), StopCycle: &laneTermination{Cycle: lanes.Cycle, Lanes: lanes.Lanes}}
	}
	return nil
}

func abortGCCycle(h *history.History, fs *vfs.FS, workspace string, c *gc.Coordinator, reason string, approval *vfs.RepairApproval) error {
	if c.Cycle.Phase != "aborting" && c.Cycle.Phase != "blocked" {
		c.Cycle.Phase = "aborting"
		c.Cycle.Reason = reason
	}
	var active []laneStop
	for _, d := range h.Project().Executions {
		if d.Cycle == c.Cycle.Plan.CycleID && !d.Settled && !d.Terminated {
			active = append(active, laneStop{d.Dispatch, d.Attempt, d.Child})
		}
	}
	if len(active) > 0 {
		return &requiredLanes{c.Cycle.Plan.CycleID, active}
	}

	cycle := c.Cycle.Plan.CycleID
	if !fs.CycleInterrupted(cycle) {
		if approval != nil {
			if err := fs.PreserveInterruptedCycle(cycle, vfs.StateHash(c.Cycle), *approval); err != nil {
				return err
			}
		} else if _, e := fs.DiscardCycle(cycle); e != nil && !errors.Is(e, vfs.ErrUnknownCycle) {
			if !errors.Is(e, vfs.ErrCycleDiverged) {
				return e
			}
			c.Cycle.Phase = "blocked"
			repair, err := fs.CycleRepair(cycle, vfs.StateHash(c.Cycle))
			if err != nil {
				return errors.Join(e, err)
			}
			return &requiredRepair{repair, e}
		}
	}
	if e := fs.DropCycleStaging(cycle); e != nil {
		return e
	}
	if e := settleGCCycleActivity(h, fs, c.Cycle, history.OutcomeInterrupted); e != nil {
		return e
	}
	closed := c.Cycle
	reason = closed.Reason
	if fs.CycleInterrupted(cycle) {
		reason = "interrupted: current files preserved with explicit approval"
	}
	c.Close(reason)
	cycleRecord(workspace, closed, reason)
	reportGCTelemetry(evaluateMandateReversionRate(workspace, closed))
	return nil
}

// settleGCCycleActivity closes the cycle's activity and releases what its lanes
// prepared. The idempotent start first covers a cycle persisted before its
// activity was ever recorded.
func settleGCCycleActivity(h *history.History, fs *vfs.FS, c *gc.Cycle, outcome history.Outcome) error {
	if err := startGCCycleActivity(h, c); err != nil {
		return err
	}
	for _, d := range h.Project().Executions {
		if !d.Settled && d.Cycle == c.Plan.CycleID {
			if err := fs.CancelPreparedDelegation(d.Root, d.Unit, d.Attempt); err != nil {
				return err
			}
			if err := h.ExecutionFact(d, history.KindExecutionEnded, "", "", nil); err != nil {
				return err
			}
		}
	}
	return protocol.FinishActivity(h, c.Plan.SessionID, c.Plan.CycleID, history.NodeKindMaintenance, outcome)
}

func startGCCycleActivity(h *history.History, c *gc.Cycle) error {
	// History can commit interruption before the coordinator's close is saved.
	// Resume only that exact closure; never reopen or relabel a finished activity.
	prior, exists := h.Project().Activities[c.Plan.CycleID]
	if exists && prior.SessionID == c.Plan.SessionID && prior.NodeKind == history.NodeKindMaintenance &&
		prior.State == history.StateSettled && prior.Outcome == history.OutcomeInterrupted &&
		(c.Phase == "aborting" || c.Phase == "blocked") {
		return nil
	}
	return protocol.StartActivity(h, c.Plan.SessionID, c.Plan.CycleID, history.NodeKindMaintenance)
}
