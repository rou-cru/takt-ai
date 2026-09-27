package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	protocol "github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

var gcCoordinateActions = map[string]bool{
	"prepare": true, "status": true, "request": true, "tick": true,
	"attach": true, "recover": true, "abort": true,
	"baseline": true, "findings": true, "investigate": true,
	"authorize": true, "collected": true, "delta": true,
	"verdict": true, "acceptance": true, "no-change": true,
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
	c, e := gc.LoadCoordinator(*state)
	if e != nil {
		return e
	}
	response, e := coordinateGC(context.Background(), fs, h, *workspace, *state, c, req)
	// Persist abort/escalation state even if the effect failed. The barrier stays held.
	if save := gc.SaveCoordinator(*state, c); save != nil {
		return errors.Join(e, save)
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(stdout).Encode(response)
}

func coordinatePrepare(ctx context.Context, workspace, state string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if c.Held() {
		return nil, errors.New("gc: preparation is ordinary work, not permitted during a cycle")
	}
	return gc.Prepare(ctx, workspace, state, r.Session)
}

func coordinateGC(ctx context.Context, fs *vfs.FS, h *history.History, workspace, state string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	entries, err := observeCoordination(fs, h, c)
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
		return c, c.Attach(r.Role, r.Child)
	case "recover", "abort":
		return coordinateAbort(h, fs, workspace, c, r)
	case "baseline":
		return coordinateBaseline(ctx, workspace, state, c, r)
	case "findings":
		return coordinateFindings(c, r)
	case "investigate":
		return coordinateInvestigation(c, r)
	case "authorize":
		return coordinateAuthorization(fs, c, r)
	case "collected":
		return coordinateCollected(fs, c, r)
	case "delta":
		return coordinateDelta(fs, c, r)
	case "verdict":
		return coordinateVerdict(h, fs, workspace, state, c, r)
	case "acceptance":
		return coordinateAcceptance(ctx, h, fs, workspace, state, c, r)
	case "no-change":
		return coordinateNoChange(h, workspace, c, r)
	default:
		return nil, fmt.Errorf("gc: unknown coordination action %q", r.Action)
	}
}

func coordinateAbort(h *history.History, fs *vfs.FS, workspace string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if c.Cycle == nil {
		return c, nil
	}
	return c, abortGCCycle(h, fs, workspace, c, "aborted: "+r.Evidence)
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

func coordinateAuthorization(fs *vfs.FS, c *gc.Coordinator, r coordinationRequest) (any, error) {
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
	c.Cycle.Scope = scope
	c.Cycle.AuthorKey, e = fs.Bind(cycleIdentity(c.Cycle, gc.CollectorSpecialistID), scope)
	if e != nil {
		return nil, e
	}
	// The verifier is a harness-controlled child: reserve its exact binding
	// against the collector's author key before the verifier is prompted.
	verifier := cycleIdentity(c.Cycle, gc.VerifierSpecialistID)
	verifier.WorkUnitID += "/verify"
	c.Cycle.VerifierKey, e = fs.AssignVerifier(verifier, c.Cycle.AuthorKey)
	if e != nil {
		return nil, e
	}
	c.Cycle.Phase = "collect"
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
	return fs.InspectDelta(c.Cycle.AuthorKey), nil
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
	if e := fs.Verify(c.Cycle.VerifierKey, c.Cycle.AuthorKey, "gc-verdict/"+c.Cycle.Plan.CycleID, delta.Revision, delta.Hash, r.Pass, r.Evidence); e != nil {
		return nil, e
	}
	if !r.Pass {
		return c, abortGCCycle(h, fs, workspace, c, "independent verifier rejected delta")
	}
	// Persist intent before the physical effect; restart always discards uncertain work.
	c.Cycle.Phase = "consolidating"
	if e := gc.SaveCoordinator(state, c); e != nil {
		return nil, e
	}
	if e := fs.ConsolidateCheckpoint(c.Cycle.AuthorKey, c.Cycle.Plan.CycleID, delta.Revision); e != nil {
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
		return c, errors.Join(e, abortGCCycle(h, fs, workspace, c, "preparation changed"))
	}
	c.Cycle.Acceptance = gc.RunChecks(ctx, workspace, p)
	if !gc.ChecksPass(c.Cycle.Acceptance) {
		return c, abortGCCycle(h, fs, workspace, c, "acceptance regression or missing evidence")
	}
	if e = cycleRecord(workspace, c.Cycle, "applied"); e != nil {
		return nil, e
	}
	if e = settleGCCycleActivity(h, c.Cycle, history.OutcomeCompleted); e != nil {
		return nil, e
	}
	// Closure is durably recorded before releasing retained rollback state.
	id := c.Cycle.Plan.CycleID
	closed := c.Cycle
	c.Close("applied")
	if e = evaluateMandateReversionRate(workspace, closed); e != nil {
		return nil, e
	}
	if e = gc.SaveCoordinator(state, c); e != nil {
		return nil, e
	}
	return c, fs.CompleteCycle(id)
}

func coordinateNoChange(h *history.History, workspace string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if e := c.Require(r.Session, "collector"); e != nil {
		return nil, e
	}
	if c.Cycle.Phase != "investigate" {
		return nil, errors.New("gc: no-change is only allowed before mutation")
	}
	reason := "no-change"
	if len(c.Cycle.Report.Findings) > 0 {
		reason = "proposals-or-refuted"
	}
	if e := cycleRecord(workspace, c.Cycle, reason); e != nil {
		return nil, e
	}
	if e := settleGCCycleActivity(h, c.Cycle, history.OutcomeCompleted); e != nil {
		return nil, e
	}
	closed := c.Cycle
	c.Close(reason)
	if e := evaluateMandateReversionRate(workspace, closed); e != nil {
		return nil, e
	}
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
		if e = recordControlAction(workspace, rec); e != nil {
			return nil, e
		}
	}
	if rec, ok := b.ControlRecord(harnessAgent); ok {
		rec.Correlations = obs.CorrelationIDs{SessionID: r.Session}
		if e = recordControlAction(workspace, rec); e != nil {
			return nil, e
		}
	}
	return c, nil
}
func cycleIdentity(c *gc.Cycle, agent string) vfs.Identity {
	// The invariant a maintenance delta is judged against is the reviewed project
	// configuration the cycle already froze: it declares the acceptance checks and
	// each analyzer with its tool and exact version, which is the declared norm a
	// tool can check conformance against (PR-MNT-15, PR-MNT-17, PR-VFS-CSL-3).
	return vfs.Identity{SessionID: c.Plan.SessionID, WorkUnitID: c.Plan.CycleID, AgentID: vfs.AgentID(agent), Specialist: agent, Invariants: vfs.InvariantSet{gc.ProjectConfigPath}, CycleID: c.Plan.CycleID, MandateClass: string(c.Plan.Mandate)}
}
func cycleRecord(workspace string, c *gc.Cycle, outcome string) error {
	return recordControlAction(workspace, obs.ControlRecord{ActionClass: obs.ActionObserve, TriggeringCondition: fmt.Sprintf("gc cycle %s (cycle=%s mandate=%s)", outcome, c.Plan.CycleID, c.Plan.Mandate), PolicyRef: gc.AcceptancePolicyRef, ActingAgent: harnessAgent, Correlations: obs.CorrelationIDs{SessionID: c.Plan.SessionID}})
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
	_, err = gc.EvaluateMandateReversionRate(store, bus, clock, harnessAgent, c.Plan.SessionID, c.Plan.Mandate, 0, 0, c.Plan.CycleID)
	return err
}
func abortGCCycle(h *history.History, fs *vfs.FS, workspace string, c *gc.Coordinator, reason string) error {
	c.Cycle.Phase = "aborting"
	c.Cycle.Reason = reason
	if _, e := fs.DiscardCycle(c.Cycle.Plan.CycleID); e != nil && !errors.Is(e, vfs.ErrUnknownCycle) {
		c.Cycle.Phase = "blocked"
		c.Cycle.Reason = e.Error()
		return errors.Join(e, cycleRecord(workspace, c.Cycle, "blocked-external-divergence"))
	}
	if e := fs.DropCycleStaging(c.Cycle.Plan.CycleID); e != nil {
		return e
	}
	if e := cycleRecord(workspace, c.Cycle, reason); e != nil {
		return e
	}
	if e := settleGCCycleActivity(h, c.Cycle, history.OutcomeInterrupted); e != nil {
		return e
	}
	closed := c.Cycle
	c.Close(reason)
	return evaluateMandateReversionRate(workspace, closed)
}

// settleGCCycleActivity closes the cycle's activity. The idempotent start first
// covers a cycle persisted before its activity was ever recorded.
func settleGCCycleActivity(h *history.History, c *gc.Cycle, outcome history.Outcome) error {
	if err := startGCCycleActivity(h, c); err != nil {
		return err
	}
	return protocol.FinishActivity(h, c.Plan.SessionID, c.Plan.CycleID, history.NodeKindMaintenance, outcome)
}

func startGCCycleActivity(h *history.History, c *gc.Cycle) error {
	return protocol.StartActivity(h, c.Plan.SessionID, c.Plan.CycleID, history.NodeKindMaintenance)
}
