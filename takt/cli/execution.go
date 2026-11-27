package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/catalog"
	protocol "github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// declaredInput is one entry a unit consumes, at its current version; Unit names
// the producer unit it came from, empty for an entry named by ID.
type declaredInput struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Unit  string `json:"unit,omitempty"`
}

// declareInputs records what a unit consumes: the entries named by ID and what
// each named producer unit delivered, every one at its current version. A
// producer that has delivered nothing is recorded as such, never refused.
func declareInputs(ctx context.Context, h *history.History, r coordinationRequest) (any, error) {
	if strings.TrimSpace(r.Event) == "" {
		return nil, errors.New("dispatch_inputs: name the unit the delegation will be named after")
	}
	p := h.Project()
	ids := slices.Clone(r.ResultIDs)
	from := make([]string, len(ids))
	var undelivered []string
	for _, unit := range r.Units {
		delivered, ok := p.Delivered(r.Session, unit)
		if !ok {
			undelivered = append(undelivered, unit)
			continue
		}
		for _, id := range delivered {
			ids, from = append(ids, id), append(from, unit)
		}
	}
	var current []int64
	inputs := []declaredInput{}
	if len(ids) > 0 {
		heads, described, err := currentMemory(ctx, ids)
		if errors.Is(err, memory.ErrMissingEntries) {
			return nil, fmt.Errorf("%w; call dispatch_inputs again without them", err)
		}
		if err != nil {
			return nil, fmt.Errorf("%w; retry dispatch_inputs with the same declaration", err)
		}
		for _, d := range described.Entries {
			current = append(current, d.ID)
			inputs = append(inputs, declaredInput{ID: d.ID, Title: d.Title, Unit: from[slices.Index(heads, d.ID)]})
		}
	}
	err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInputs, SessionID: r.Session, WorkUnitID: r.Event, ResultIDs: current, Undelivered: undelivered, None: len(current) == 0 && len(undelivered) == 0, Cause: history.CauseNone})
	return map[string]any{"inputs": inputs, "undelivered": undelivered}, err
}

// prepareExecution is one locked decision and effect. Inputs/claims/authors are
// read now, not accepted from a plugin preflight. Native execution is outside
// this command's lifetime and therefore outside the workspace lock.
func prepareExecution(ctx context.Context, fs *vfs.FS, h *history.History, entries []vfs.JournalEntry, r coordinationRequest) (any, error) {
	if resp, done, err := checkPriorExecution(h.Project(), r); err != nil || done {
		return resp, err
	}
	seat := protocol.Seat{Agent: r.Agent, Model: r.Model, Previous: r.PreviousModel, Reason: r.SeatReason, Substitute: r.Substitute, UserConfirmed: r.UserConfirmed}
	consumed, err := validateExecutionInputs(ctx, h, r, seat)
	if err != nil {
		return nil, err
	}
	u, _ := h.Project().Unit(r.Session, r.Event)
	identity := vfs.Identity{SessionID: r.Session, WorkUnitID: r.Event, AgentID: vfs.AgentID(r.Agent), Specialist: r.Agent}
	for _, id := range consumed {
		identity.Invariants = append(identity.Invariants, vfs.MemoryInvariant(id))
	}
	needsClaim := vfs.RequireVFSCapability(r.Agent, model.VFSCapabilityWrite) == nil
	if prior, exists := h.Project().Executions[r.Dispatch]; exists {
		if !slices.Equal(prior.Inputs, consumed) {
			return nil, compensateExecutionPreparation(fs, h, entries, r, u.AttemptID, fmt.Errorf("inputs changed during interrupted preparation for unit %s; dispatch_inputs now declares a different set", r.Event))
		}
	}
	admit := func() (string, error) {
		return admitExecutionAttempt(fs, h, entries, r, seat, &u)
	}
	if err := fs.PrepareDelegation(identity, r.AuthorKeys, needsClaim, admit); err != nil {
		// Invalid declarations never spend an attempt. Only compensate an
		// admission issued by this exact preparation, not an unrelated run.
		if u.Dispatch == r.Dispatch && u.State == history.StateInFlight {
			return nil, compensateExecutionPreparation(fs, h, entries, r, u.AttemptID, err)
		}
		return nil, err
	}
	if err := h.ExecutionFact(h.Project().Executions[r.Dispatch], history.KindPrepared, "", "", nil); err != nil {
		return nil, compensateExecutionPreparation(fs, h, entries, r, u.AttemptID, err)
	}
	if r.Child != "" {
		if _, err := h.AttachChild(r.Dispatch, r.Child, r.Agent); err != nil {
			return nil, compensateExecutionPreparation(fs, h, entries, r, u.AttemptID, err)
		}
	}
	return struct {
		history.Execution
		Launchable bool `json:"launchable"`
	}{h.Project().Executions[r.Dispatch], true}, nil
}

func checkPriorExecution(p history.Projection, r coordinationRequest) (any, bool, error) {
	prior, ok := p.Executions[r.Dispatch]
	if !ok {
		return nil, false, nil
	}
	if err := priorExecutionMismatch(prior, r); err != nil {
		return nil, false, err
	}
	if prior.Ready || prior.Settled {
		return map[string]any{"execution": prior, "launchable": false}, true, nil
	}
	return nil, false, nil
}

func priorExecutionMismatch(prior history.Execution, r coordinationRequest) error {
	if prior.Root != r.Session || prior.Unit != r.Event || prior.Caller != r.Caller || prior.Agent != r.Agent {
		return fmt.Errorf("dispatch %s already identifies root %s unit %s agent %s; retry the intended delegation with a new subagent call", r.Dispatch, prior.Root, prior.Unit, prior.Agent)
	}
	// The seat is part of what the execution record names: a retry may not
	// run another model or swap in a substitute under it. An execution
	// recorded before models were recorded names none, so any model matches.
	if (prior.Model != "" && prior.Model != r.Model) || prior.Substitute != r.Substitute {
		return fmt.Errorf("dispatch %s is recorded with model %q and substitute %t; to run another seat, make a new subagent call", r.Dispatch, prior.Model, prior.Substitute)
	}
	return nil
}

func validateExecutionInputs(ctx context.Context, h *history.History, r coordinationRequest, seat protocol.Seat) ([]int64, error) {
	if r.Dispatch == "" || r.Caller == "" || r.Agent == "" || r.Owner == "" {
		return nil, errors.New("dispatch: exact host call identity and harness instance required; retry the same subagent call, or run takt-ai setup sync to re-deploy the plugin")
	}
	consumed, ok := h.Project().Inputs[history.UnitKey(r.Session, r.Event)]
	if !ok {
		return nil, fmt.Errorf("launch refused: inputs absent for root %s unit %s; call dispatch_inputs, then retry the same subagent call", r.Session, r.Event)
	}
	// One describe validates the inputs.
	described, err := checkMemory(ctx, consumed, memory.Rule{})
	if err != nil {
		return nil, fmt.Errorf("%w; redeclare valid IDs with dispatch_inputs for unit %s, then retry delegation", err, r.Event)
	}
	if err := refuseRecycledProductManager(r, described); err != nil {
		return nil, err
	}
	if err := refuseUnblockedContinuation(h.Project(), r); err != nil {
		return nil, err
	}
	if err := protocol.ValidateSeat(h, r.Session, seat); err != nil {
		return nil, err
	}
	return consumed, nil
}

func compensateExecutionPreparation(fs *vfs.FS, h *history.History, entries []vfs.JournalEntry, r coordinationRequest, attemptID string, cause error) error {
	cleanup := fs.CancelPreparedDelegation(r.Session, r.Event, attemptID)
	finish := protocol.Finish(h, journalRef(entries), r.Event, r.Session)
	continuation := "prepared gates released and attempt closed; retry this unit with a new subagent call"
	if cleanup != nil || finish != nil {
		continuation = "preparation compensation could not complete; retry this unit after the reported store failure is resolved"
	}
	return fmt.Errorf("launch preparation refused for root %s unit %s dispatch %s: %w; %s (use dispatch_inputs or claim_assign to correct a declaration if named)", r.Session, r.Event, r.Dispatch, errors.Join(cause, cleanup, finish), continuation)
}

func admitExecutionAttempt(fs *vfs.FS, h *history.History, entries []vfs.JournalEntry, r coordinationRequest, seat protocol.Seat, u *history.Unit) (string, error) {
	req := r
	req.Action = "admit"
	if _, err := coordinateRoutine(fs, h, entries, req); err != nil {
		return "", err
	}
	*u, _ = h.Project().Unit(r.Session, r.Event)
	if u.State != history.StateInFlight || u.Dispatch != r.Dispatch || u.Agent != r.Agent {
		return "", fmt.Errorf("dispatch %s is not running unit %s; retry the unit with a new subagent call", r.Dispatch, r.Event)
	}
	if _, exists := h.Project().Executions[r.Dispatch]; !exists {
		if r.CreatedAt == 0 {
			r.CreatedAt = time.Now().UnixMilli()
		}
		batch := []history.Entry{{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: r.Session, WorkUnitID: r.Event, AttemptID: u.AttemptID, Dispatch: r.Dispatch, Agent: r.Agent, Caller: r.Caller, CreatedAt: r.CreatedAt, Owner: r.Owner, Model: r.Model, Substitute: r.Substitute, Cause: history.CauseNone}}
		if seat.Restores() {
			batch = append(batch, protocol.SeatEntry(r.Session, r.Event, u.AttemptID, seat))
		}
		if err := h.AppendBatch(batch); err != nil {
			return "", err
		}
	}
	return u.AttemptID, nil
}

func attachExecutionChild(h *history.History, r coordinationRequest) (any, error) {
	if r.Dispatch != "" {
		prepared, ok := h.Project().Executions[r.Dispatch]
		if !ok || (r.Attempt != "" && r.Attempt != prepared.Attempt) {
			return nil, errors.New("native callback no longer names its prepared attempt; resume the current delegation")
		}
		// A dispatch prepared without a child meets the continuation gate here.
		r.Event = prepared.Unit
		if err := refuseUnblockedContinuation(h.Project(), r); err != nil {
			return nil, err
		}
		d, err := h.AttachChild(r.Dispatch, r.Child, r.Agent)
		if err == nil {
			err = observeExecutionRunning(h, d)
		}
		return attached(d, err)
	}
	p := h.Project()
	if d, ok := p.ExecutionForChild(r.Child, r.Agent); ok {
		return attached(d, observeExecutionRunning(h, d))
	}
	// The host proposes the first link using native parent/title/time. Neither
	// titles nor timestamps authorize any operation after this association.
	for _, old := range p.Executions {
		if old.Child == r.Child {
			return nil, nil
		}
	}
	var candidate *history.Execution
	for _, d := range p.Executions {
		if d.Settled || d.Child != "" || d.Root != r.Session || d.Unit != r.Event || d.Agent != r.Agent || d.Caller != r.Caller || (r.CreatedAt > 0 && r.CreatedAt < d.AdmittedAt-1000) {
			continue
		}
		if candidate != nil {
			return nil, errors.New("dispatch: ambiguous native child; resume the exact sessionID in the subagent call")
		}
		copy := d
		candidate = &copy
	}
	if candidate == nil {
		return nil, nil
	}
	d, err := h.AttachChild(candidate.Dispatch, r.Child, r.Agent)
	if err == nil {
		err = observeExecutionRunning(h, d)
	}
	return attached(d, err)
}

// attachedExecution is a linked execution; a substitute's carries the
// identity texts its composed prompt leaves out (PR-CRW-13).
type attachedExecution struct {
	history.Execution
	Identity []string `json:"identity,omitempty"`
}

func attached(d history.Execution, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	out := attachedExecution{Execution: d}
	if d.Substitute {
		if out.Identity, err = protocol.SubstituteIdentity(d.Agent); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// A native context/event is evidence of this linked execution, even when a
// prior transport interruption left launch uncertain. It never repeats launch.
func observeExecutionRunning(h *history.History, d history.Execution) error {
	if d.Cycle != "" {
		return nil
	}
	u, _ := h.Project().Unit(d.Root, d.Unit)
	if u.AttemptID != d.Attempt || u.Dispatch != d.Dispatch || u.State != history.StateInFlight {
		return errors.New("native execution no longer holds this dispatch; resume its current delegation")
	}
	if protocol.UncertainFlight(u.Flight) {
		return protocol.Reconcile(h, "", d.Unit, d.Root, true)
	}
	return protocol.Launch(h, "", d.Unit, d.Root)
}

func resolveExecution(h *history.History, child, agent string) (any, error) {
	if d, ok := h.Project().ExecutionForChild(child, agent); ok && d.Cycle == "" {
		return d, nil
	}
	if root, held := h.Project().HeldRoot(child); held {
		return map[string]any{"root": root}, nil
	}
	return nil, nil
}

// deliverRetry is the call a refused execution delivery continues with.
const deliverRetry = "deliver_result"

func deliverExecution(ctx context.Context, h *history.History, r coordinationRequest) (any, error) {
	d, ok := h.Project().ExecutionForChild(r.Child, r.Agent)
	if !ok || d.Cycle != "" || (r.Dispatch != "" && r.Dispatch != d.Dispatch) || (r.Attempt != "" && r.Attempt != d.Attempt) {
		return nil, errors.New("deliver_result belongs to a running delegation; nothing credited; report the IDs in your reply")
	}
	if err := protocol.ValidateDelivery(ctx, checkMemory, r.Child, r.ResultIDs); err != nil {
		return nil, protocol.DeliveryRefusal(err, deliverRetry)
	}
	if err := h.Deliver(d, r.ResultIDs, r.Blocker); err != nil {
		return nil, err
	}
	return map[string]any{"result_ids": h.Project().Executions[d.Dispatch].Delivery}, nil
}

// renewLease records that harness instance owner is alive at now.
func renewLease(h *history.History, owner string, now time.Time) error {
	if owner == "" {
		return errors.New("dispatch lease names the harness instance; run takt-ai setup sync to re-deploy the plugin")
	}
	return h.Append(history.Entry{Author: history.AuthorHarness, Kind: history.KindLease, Owner: owner, CreatedAt: now.UnixMilli(), Cause: history.CauseNone})
}

// listExecutions answers every execution with the dispatches whose owner's
// lease lapsed by now, which any harness instance may reconcile, and likewise
// the orchestrator activities still in flight with those whose capturing
// instance's lease lapsed: their capture is lost (PR-OBS-BUS-6).
func listExecutions(h *history.History, now time.Time) map[string]any {
	p := h.Project()
	// Admission can survive a crash before KindExecution. Expose that exact
	// attempt for host reconciliation without manufacturing a launchable record.
	for _, u := range p.Units {
		if u.State != history.StateInFlight || u.Dispatch == "" {
			continue
		}
		if _, exists := p.Executions[u.Dispatch]; exists {
			continue
		}
		caller, _, ok := strings.Cut(u.Dispatch, ":")
		if !ok {
			caller = u.SessionID
		}
		p.Executions[u.Dispatch] = history.Execution{Dispatch: u.Dispatch, Root: u.SessionID, Unit: u.WorkUnitID, Attempt: u.AttemptID, Agent: u.Agent, Caller: caller}
	}
	for dispatch, d := range p.Executions {
		if d.Cycle != "" {
			delete(p.Executions, dispatch)
		}
	}
	orphaned := []string{}
	for dispatch, d := range p.Executions {
		if !d.Settled && p.Orphaned(d, now) {
			orphaned = append(orphaned, dispatch)
		}
	}
	slices.Sort(orphaned)
	activities, orphanedActivities := map[string]history.Activity{}, []string{}
	for id, a := range p.Activities {
		if a.NodeKind != history.NodeKindOrchestrator || a.State != history.StateInFlight {
			continue
		}
		activities[id] = a
		if p.LeaseLapsed(a.Owner, now) {
			orphanedActivities = append(orphanedActivities, id)
		}
	}
	slices.Sort(orphanedActivities)
	return map[string]any{"executions": p.Executions, "orphaned": orphaned, "activities": activities, "orphaned_activities": orphanedActivities}
}

// productManagerID names the catalog definition whose sessions author user
// stories; another session of it formalizes them (PR-CRW-20).
const productManagerID = "takt-pm"

// refuseRecycledProductManager refuses resuming a pm session to consume an
// invariant that same session recorded. A fresh delegation is always admitted.
func refuseRecycledProductManager(r coordinationRequest, consumed memory.DescribeResult) error {
	if r.Child == "" || !isProductManager(r.Agent) {
		return nil
	}
	for _, e := range consumed.Entries {
		if e.WriterHostSession == r.Child {
			return fmt.Errorf("launch refused: pm session %s recorded consumed invariant #%d; delegate a fresh pm unit to formalize it", r.Child, e.ID)
		}
	}
	return nil
}

// refuseUnblockedContinuation refuses continuing a returned session unless its
// delivery recorded a blocker: every other retry runs in a new session
// (PR-ORQ-39, PR-HAR-38).
func refuseUnblockedContinuation(p history.Projection, r coordinationRequest) error {
	d, returned := p.ReturnedExecution(r.Child)
	if !returned || d.Blocker {
		return nil
	}
	return fmt.Errorf("launch refused: session %s returned unit %s without a blocker, so it is not continued; delegate unit %s again without sessionID to start a new session", r.Child, d.Unit, r.Event)
}

func isProductManager(agent string) bool {
	packages, err := catalog.LoadPackages()
	if err != nil {
		return false
	}
	for _, def := range packages.Agents {
		if def.ID == productManagerID {
			return slices.Contains(def.Instances, agent)
		}
	}
	return false
}

// validateTopLevelDelivery checks the entries a top-level agent delivers: each written by the
// calling host session, and current.
func validateTopLevelDelivery(ctx context.Context, r coordinationRequest) error {
	if err := protocol.ValidateDelivery(ctx, checkMemory, r.Session, r.ResultIDs); err != nil {
		return protocol.DeliveryRefusal(err, deliverRetry)
	}
	return nil
}

// validateRequirements checks the requirements of a switch: every ID is current, and an
// empty list is the explicit statement that nothing useful exists (CTX-6). An absent list is refused.
func validateRequirements(ctx context.Context, requirements []int64) error {
	if requirements == nil {
		return errors.New("dispatch_switch: name the Takt memory entry IDs the specialist needs as requirements, or an empty list when nothing useful exists")
	}
	if len(requirements) == 0 {
		return nil
	}
	if _, err := checkMemory(ctx, requirements, memory.Rule{}); err != nil {
		return fmt.Errorf("%w; name current IDs as requirements, then call dispatch_switch again", err)
	}
	return nil
}

// switchInterface lends the interface to a specialist session: it validates the requirements, binds the
// lent session to the convening root before it is prompted (only once the harness would accept the switch), and records the switch.
// A failed bind fails the switch and records nothing.
func switchInterface(ctx context.Context, h *history.History, ref, workspace string, r coordinationRequest) (err error) {
	if err := validateRequirements(ctx, r.Requirements); err != nil {
		return err
	}
	admin, err := memoryAdmin(ctx)
	if err != nil {
		return fmt.Errorf("switch refused: %w; nothing was recorded; call dispatch_switch again", err)
	}
	defer func() { err = errors.Join(err, admin.Close()) }()
	return protocol.Switch(h, ref, r.Session, r.Child, r.Agent, func() error {
		if err := admin.Bind(ctx, memory.BindArgs{HostSession: r.Child, Agent: r.Agent, ClaimedRoot: r.Session, Workspace: memory.WorkspaceKey(ctx, workspace)}); err != nil {
			return fmt.Errorf("switch refused: the lent session could not be bound to its conversation: %w; nothing was recorded; call dispatch_switch again", err)
		}
		return nil
	})
}
