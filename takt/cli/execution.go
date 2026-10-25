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
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func declareInputs(ctx context.Context, h *history.History, r coordinationRequest) (any, error) {
	if strings.TrimSpace(r.Event) == "" {
		return nil, errors.New("dispatch_inputs: name the unit the delegation will be named after")
	}
	if len(r.ResultIDs) > 0 {
		if err := memory.ValidateResultIDs(ctx, memory.Config{Root: memoryRoot()}, r.ResultIDs); err != nil {
			return nil, err
		}
	}
	err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInputs, SessionID: r.Session, WorkUnitID: r.Event, ResultIDs: r.ResultIDs, None: len(r.ResultIDs) == 0, Cause: history.CauseNone})
	return map[string]any{"result_ids": r.ResultIDs}, err
}

// prepareExecution is one locked decision and effect. Inputs/claims/authors are
// read now, not accepted from a plugin preflight. Native execution is outside
// this command's lifetime and therefore outside the workspace lock.
func prepareExecution(ctx context.Context, fs *vfs.FS, h *history.History, workspace string, c *gc.Coordinator, entries []vfs.JournalEntry, r coordinationRequest) (any, error) {
	if prior, ok := h.Project().Executions[r.Dispatch]; ok && (prior.Ready || prior.Settled) {
		return map[string]any{"execution": prior, "launchable": false}, nil
	}
	if r.Dispatch == "" || r.Caller == "" || r.Agent == "" || r.Owner == "" {
		return nil, errors.New("dispatch: exact host call identity and harness instance required; retry the same subagent call, or run takt-ai setup sync to re-deploy the plugin")
	}
	consumed, ok := h.Project().Inputs[history.UnitKey(r.Session, r.Event)]
	if !ok {
		return nil, fmt.Errorf("launch refused: inputs absent for root %s unit %s; call dispatch_inputs, then retry the same subagent call", r.Session, r.Event)
	}
	if len(consumed) > 0 {
		if err := memory.ValidateResultIDs(ctx, memory.Config{Root: memoryRoot()}, consumed); err != nil {
			return nil, fmt.Errorf("%w; redeclare valid IDs with dispatch_inputs for unit %s, then retry delegation", err, r.Event)
		}
	}
	if err := refuseRecycledProductManager(r, consumed); err != nil {
		return nil, err
	}
	u, _ := h.Project().Unit(r.Session, r.Event)
	identity := vfs.Identity{SessionID: r.Session, WorkUnitID: r.Event, AgentID: vfs.AgentID(r.Agent), Specialist: r.Agent}
	for _, id := range consumed {
		identity.Invariants = append(identity.Invariants, vfs.EngramInvariant(id))
	}
	needsClaim := vfs.RequireVFSCapability(r.Agent, model.VFSCapabilityWrite) == nil
	compensate := func(cause error) (any, error) {
		cleanup := fs.CancelPreparedDelegation(r.Session, r.Event, u.AttemptID)
		finish := protocol.Finish(h, journalRef(entries), r.Event, r.Session)
		continuation := "prepared gates released and attempt closed; retry this unit with a new subagent call"
		if cleanup != nil || finish != nil {
			continuation = "preparation compensation could not complete; retry this unit after the reported store failure is resolved"
		}
		return nil, fmt.Errorf("launch preparation refused for root %s unit %s dispatch %s: %w; %s (use dispatch_inputs or claim_assign to correct a declaration if named)", r.Session, r.Event, r.Dispatch, errors.Join(cause, cleanup, finish), continuation)
	}
	if prior, exists := h.Project().Executions[r.Dispatch]; exists {
		if prior.Root != r.Session || prior.Unit != r.Event || prior.Caller != r.Caller || prior.Agent != r.Agent || prior.Background != r.Background {
			return nil, fmt.Errorf("dispatch %s already identifies root %s unit %s agent %s; retry the intended delegation with a new subagent call", r.Dispatch, prior.Root, prior.Unit, prior.Agent)
		}
		if !slices.Equal(prior.Inputs, consumed) {
			return compensate(fmt.Errorf("inputs changed during interrupted preparation for unit %s; dispatch_inputs now declares a different set", r.Event))
		}
	}
	admit := func() (string, error) {
		req := r
		req.Action = "admit"
		if _, err := coordinateRoutine(fs, h, workspace, c, entries, req); err != nil {
			return "", err
		}
		u, _ = h.Project().Unit(r.Session, r.Event)
		if u.State != history.StateInFlight || u.Dispatch != r.Dispatch || u.Agent != r.Agent {
			return "", fmt.Errorf("dispatch %s is not running unit %s; retry the unit with a new subagent call", r.Dispatch, r.Event)
		}
		if _, exists := h.Project().Executions[r.Dispatch]; !exists {
			if r.CreatedAt == 0 {
				r.CreatedAt = time.Now().UnixMilli()
			}
			if err := h.Append(history.Entry{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: r.Session, WorkUnitID: r.Event, AttemptID: u.AttemptID, Dispatch: r.Dispatch, Agent: r.Agent, Caller: r.Caller, Background: r.Background, CreatedAt: r.CreatedAt, Owner: r.Owner, Cause: history.CauseNone}); err != nil {
				return "", err
			}
		}
		return u.AttemptID, nil
	}
	if err := fs.PrepareDelegation(identity, r.AuthorKeys, needsClaim, admit); err != nil {
		// Invalid declarations never spend an attempt. Only compensate an
		// admission issued by this exact preparation, not an unrelated run.
		if u.Dispatch == r.Dispatch && u.State == history.StateInFlight {
			return compensate(err)
		}
		return nil, err
	}
	if err := h.ExecutionFact(h.Project().Executions[r.Dispatch], history.KindPrepared, "", "", nil); err != nil {
		return compensate(err)
	}
	if r.Child != "" {
		if _, err := h.AttachChild(r.Dispatch, r.Child, r.Agent); err != nil {
			return compensate(err)
		}
	}
	return struct {
		history.Execution
		Launchable bool `json:"launchable"`
	}{h.Project().Executions[r.Dispatch], true}, nil
}

func attachExecutionChild(h *history.History, r coordinationRequest) (any, error) {
	if r.Dispatch != "" {
		if d, ok := h.Project().Executions[r.Dispatch]; !ok || (r.Attempt != "" && r.Attempt != d.Attempt) {
			return nil, errors.New("native callback no longer names its prepared attempt; resume the current delegation")
		}
		d, err := h.AttachChild(r.Dispatch, r.Child, r.Agent)
		if err == nil {
			err = observeExecutionRunning(h, d)
		}
		return d, err
	}
	p := h.Project()
	if d, ok := p.ExecutionForChild(r.Child, r.Agent); ok {
		return d, observeExecutionRunning(h, d)
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
	return d, err
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
	if u.Flight == history.FlightUncertain {
		return protocol.Reconcile(h, "", d.Unit, d.Root, true)
	}
	return protocol.Launch(h, "", d.Unit, d.Root)
}
func resolveExecution(h *history.History, child, agent string) (any, error) {
	if d, ok := h.Project().ExecutionForChild(child, agent); ok {
		return d, nil
	}
	if root, held := h.Project().HeldRoot(child); held {
		return map[string]any{"root": root}, nil
	}
	return nil, nil
}
func deliverExecution(ctx context.Context, h *history.History, r coordinationRequest) (any, error) {
	d, ok := h.Project().ExecutionForChild(r.Child, r.Agent)
	if !ok || d.Cycle != "" || (r.Dispatch != "" && r.Dispatch != d.Dispatch) || (r.Attempt != "" && r.Attempt != d.Attempt) {
		return nil, errors.New("deliver_result belongs to a running delegation; nothing credited; return the IDs in your reply or resume its exact session")
	}
	if err := memory.ValidateSessionResultIDs(ctx, memory.Config{Root: memoryRoot()}, d.Root, d.Agent, r.ResultIDs); err != nil {
		return nil, err
	}
	if err := h.ExecutionFact(d, history.KindDelivery, "", "", r.ResultIDs); err != nil {
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
// lease lapsed by now, which any harness instance may reconcile.
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
	orphaned := []string{}
	for dispatch, d := range p.Executions {
		if !d.Settled && p.Orphaned(d, now) {
			orphaned = append(orphaned, dispatch)
		}
	}
	slices.Sort(orphaned)
	return map[string]any{"executions": p.Executions, "orphaned": orphaned}
}

// productManagerID names the catalog definition whose sessions author user
// stories; another session of it formalizes them (PR-CRW-20).
const productManagerID = "takt-pm"

// refuseRecycledProductManager refuses resuming a pm session to consume an
// invariant that same session recorded. A fresh delegation is always admitted.
func refuseRecycledProductManager(r coordinationRequest, consumed []int64) error {
	if r.Child == "" || len(consumed) == 0 || !isProductManager(r.Agent) {
		return nil
	}
	own, err := memory.EntryIDsByCaller(memoryRoot(), r.Session, r.Child)
	if err != nil {
		return err
	}
	for _, id := range consumed {
		if slices.Contains(own, id) {
			return fmt.Errorf("launch refused: pm session %s recorded consumed invariant #%d; delegate a fresh pm unit to formalize it", r.Child, id)
		}
	}
	return nil
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
