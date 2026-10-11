package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	protocol "github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// coordinationRequest is harness IPC, not a model-selected identity or mandate.
// Ordinary dispatch owns the shared envelope; GC consumes only its cycle fields.
type coordinationRequest struct {
	Action            string           `json:"action"`
	Event             string           `json:"event"`
	ActivityID        string           `json:"activity_id,omitempty"`
	NodeKind          history.NodeKind `json:"node_kind,omitempty"`
	Dispatch          string           `json:"dispatch,omitempty"`
	Session           string           `json:"session"`
	Agent             string           `json:"agent"`
	Role              string           `json:"role"`
	Child             string           `json:"child"`
	Finding           string           `json:"finding"`
	Outcome           string           `json:"outcome"`
	Evidence          string           `json:"evidence"`
	Pass              bool             `json:"pass"`
	Attempt           string           `json:"attempt"`
	Objective         string           `json:"objective"`
	Result            string           `json:"result"`
	Point             string           `json:"point"`
	Scope             []string         `json:"scope"`
	Actions           int              `json:"actions"`
	Attempts          int              `json:"attempts"`
	Key               string           `json:"key"`
	Bound             string           `json:"bound"`
	Allowance         int              `json:"allowance"`
	Version           string           `json:"version"`
	Plan              []gc.PlanUnit    `json:"plan"`
	BaseVersion       string           `json:"base_version,omitempty"`
	Withdrawals       []string         `json:"withdrawals,omitempty"`
	Artifact          string           `json:"artifact"`
	AdditionalContext string           `json:"additional_context"`
	ExtraArtifacts    []string         `json:"extra_artifacts"`
	ResultIDs         []int64          `json:"result_ids"`
	Origin            string           `json:"origin"`
	Caller            string           `json:"caller,omitempty"`
	Background        bool             `json:"background,omitempty"`
	CreatedAt         int64            `json:"created_at,omitempty"`
	// AuthorKeys distinguishes an absent list (no gates) from an empty one,
	// which a verifier preparation refuses.
	AuthorKeys []vfs.AgentID `json:"author_keys,omitempty"`
	None       bool          `json:"none,omitempty"`
	Phase      string        `json:"phase,omitempty"`
	// Owner is the harness instance issuing the request, for execution
	// ownership and lease renewal.
	Owner   string              `json:"owner,omitempty"`
	Stopped []laneStop          `json:"stopped,omitempty"`
	Repair  *vfs.RepairApproval `json:"repair,omitempty"`
}

// memoryRoot is where `takt-ai memory` keeps its session index: the user's
// home, not the workspace, so results are resolved where they were recorded.
func memoryRoot() string {
	home, _ := os.UserHomeDir()
	return home
}

// journalRef is the Action Journal position an execution-history entry is
// recorded against; empty while the journal is empty.
func journalRef(entries []vfs.JournalEntry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[len(entries)-1].Ref()
}

// checkArtifact reports whether the optional filesystem copy the active
// interlocutor switch for session declared exists under workspace. Nothing
// declared (no active switch, or none named) reports verified: PR-HAR-24
// checks existence only when a copy was named.
func checkArtifact(h *history.History, workspace, session string) bool {
	path := protocol.ExpectedArtifact(h, session)
	if path == "" {
		return true
	}
	_, err := os.Stat(filepath.Join(workspace, path))
	return err == nil
}

// handoffWithCopyReport assembles the handoff envelope, whose Engram result
// IDs BuildHandoffEnvelope validates, and reports whether the optional
// filesystem copy declared at switch time exists; that copy never gates it.
func handoffWithCopyReport(h *history.History, ref, workspace string, r coordinationRequest) (any, error) {
	verified := checkArtifact(h, workspace, r.Session)
	envelope, e := protocol.BuildHandoffEnvelope(h, ref, memoryRoot(), r.Session, r.Child, r.Agent, protocol.HandoffOutcome{
		Result: r.Result, AdditionalContext: r.AdditionalContext,
		ExtraArtifacts: r.ExtraArtifacts, ResultIDs: r.ResultIDs,
	})
	if e != nil {
		return nil, e
	}
	envelope["artifact_verified"] = verified
	return envelope, nil
}

// abortWithArtifactReport assembles the abort envelope: the standard
// artifact is never required to abort (PR-HAR-24), but its presence is still
// checked and reported so what survived the abandoned switch is known.
func abortWithArtifactReport(h *history.History, ref, workspace string, r coordinationRequest) (any, error) {
	verified := checkArtifact(h, workspace, r.Session)
	envelope, e := protocol.BuildAbortEnvelope(h, ref, memoryRoot(), r.Session, r.Child, r.Evidence, r.Origin)
	if e != nil {
		return nil, e
	}
	envelope["artifact_verified"] = verified
	return envelope, nil
}

// sessionDeletedEvidence is the abort evidence recorded when a holder session
// is deleted by the host.
const sessionDeletedEvidence = "session ended"

// abortDeletedHolder aborts the root the deleted session currently holds, as a
// harness-origin abort_switch would, and reports whether it held one.
func abortDeletedHolder(h *history.History, ref, workspace string, r coordinationRequest) (any, error) {
	root, held := h.Project().HeldRoot(r.Child)
	if !held {
		return map[string]any{"aborted": false}, nil
	}
	abort := r
	abort.Session, abort.Evidence, abort.Origin = root, sessionDeletedEvidence, "harness"
	out, e := abortWithArtifactReport(h, ref, workspace, abort)
	if e != nil {
		return nil, e
	}
	envelope := out.(map[string]any)
	envelope["aborted"], envelope["root"] = true, root
	return envelope, nil
}

// coordinateRoutine admits and finishes ordinary work. Maintenance never gates
// either: an admission ends a cycle in flight first (PR-MNT-3). Whether a cycle
// is due is decided only when the session is idle, by `gc coordinate`'s tick.
func coordinateRoutine(fs *vfs.FS, h *history.History, workspace string, c *gc.Coordinator, entries []vfs.JournalEntry, r coordinationRequest) (any, error) {
	unit, _ := h.Project().Unit(r.Session, r.Event)
	switch r.Action {
	case "admit":
		// A repeated host call yields its recorded disposition; a new dispatch
		// of the same unit is a retry, not another copy of this admission.
		repeated := unit.State != "" && unit.State != history.StatePlanned &&
			(r.Dispatch == "" || unit.Dispatch == r.Dispatch)
		if repeated {
			return c, nil
		}
		if e := yieldGCCycle(h, fs, workspace, c, r); e != nil {
			return c, e
		}
		return c, c.Admit(h, journalRef(entries), r.Event, r.Session, r.Agent, r.Dispatch)
	case "finish":
		if unit.State != history.StateInFlight || (r.Dispatch != "" && unit.Dispatch != r.Dispatch) || (r.Attempt != "" && unit.AttemptID != r.Attempt) {
			return c, nil
		}
		// Settling follows an exact host observation of this dispatch and
		// attempt, whichever harness instance relays it; a repeat is a no-op.
		if err := fs.CancelPreparedDelegation(r.Session, r.Event, unit.AttemptID); err != nil {
			return nil, err
		}
		return c, protocol.Finish(h, journalRef(entries), r.Event, r.Session)
	}
	return c, nil
}

// abandonExecution ends an ordinary delegation the host can no longer prove
// terminated, on the user's decision or once its caller session is gone: its
// unit's attempt ends interrupted and its slot is released; staged work stays.
// A repeat is a no-op.
func abandonExecution(fs *vfs.FS, h *history.History, workspace string, c *gc.Coordinator, entries []vfs.JournalEntry, r coordinationRequest) (any, error) {
	d, ok := h.Project().Executions[r.Dispatch]
	if !ok || d.Cycle != "" {
		return nil, fmt.Errorf("dispatch: %q names no ordinary delegation; use a dispatch from the unsettled list a refusal reported", r.Dispatch)
	}
	if d.Settled {
		return c, nil
	}
	u, _ := h.Project().Unit(d.Root, d.Unit)
	if u.State != history.StateInFlight || u.Dispatch != d.Dispatch || u.AttemptID != d.Attempt {
		return c, h.ExecutionFact(d, history.KindExecutionEnded, "", "", nil)
	}
	if e := protocol.Record(h, journalRef(entries), d.Unit, d.Root, history.KindCancelRequested); e != nil {
		return nil, e
	}
	finish := r
	finish.Action, finish.Event, finish.Session, finish.Dispatch, finish.Attempt = "finish", d.Unit, d.Root, d.Dispatch, d.Attempt
	return coordinateRoutine(fs, h, workspace, c, entries, finish)
}

// yieldGCCycle ends a maintenance cycle in flight for a delegation that will be
// admitted, discarding the cycle's delta in full (PR-MNT-3, PR-MNT-16). A
// delegation the budgets deny leaves the cycle running (Admit records that
// denial). A cycle whose delta cannot be discarded refuses the admission, so
// new work never runs over live maintenance changes; it stays recorded as
// blocked and the next admission retries.
func yieldGCCycle(h *history.History, fs *vfs.FS, workspace string, c *gc.Coordinator, r coordinationRequest) error {
	if c.Cycle == nil {
		return nil
	}
	p, e := protocol.LoadAdmissionPolicy()
	if e != nil {
		return e
	}
	req := protocol.AdmissionRequest{Event: r.Event, Session: r.Session, Agent: r.Agent, Dispatch: r.Dispatch}
	if protocol.Admissible(h, p, req) != nil {
		return nil
	}
	cycle := c.Cycle.Plan.CycleID
	if e := recordLaneStops(h, c, r.Stopped); e != nil {
		return e
	}
	if e := abortGCCycle(h, fs, workspace, c, "yielded: ordinary dispatch resumed", r.Repair); e != nil {
		return fmt.Errorf("dispatch: maintenance cycle %s could not be unwound, so its changes may still be live; admission waits until it is; retry the same subagent call, which unwinds it again; if the same failure repeats, ask the user to run: takt-ai gc coordinate --workspace %s --state %s --request '{\"action\":\"abort\",\"event\":\"%s\",\"evidence\":\"user unwind\"}': %w", cycle, workspace, fs.StateDirectory(), cycle, e)
	}
	return nil
}

// ordinaryDispatchActions are the crew actions any orchestrator may take
// through `takt-ai dispatch`: admission, lifecycle, plan commitment, contests,
// recovery, user exceptions, the interlocutor stack, and tick (which re-runs
// the accounting pass every request begins with). Direct activity uses its own
// activity_id and never enters the work-unit path. GC's own cycle phases and
// trigger bookkeeping stay under `takt-ai gc coordinate`.
var ordinaryDispatchActions = map[string]bool{
	"prepare": true, "inputs": true, "execution": true, "executions": true, "lease": true, "attach_child": true, "deliver": true, "resolve": true,
	"admit": true, "finish": true, "tick": true,
	"activity_start": true, "activity_finish": true,
	"launch": true, "uncertain": true, "reconcile": true, "suspend": true,
	"cancel": true, "stop": true, "escalate": true, "withdraw": true,
	"commit": true, "contest": true, "recovery": true, "recovered": true,
	"restore": true, "exception": true, "abandon": true,
	"switch": true, "handoff": true, "abort_switch": true, "session_deleted": true, "interlocutor": true, "validate_results": true,
}

// runDispatch admits and drives ordinary crew work through the execution
// history: the protocol `gc coordinate` uses for its own cycle, exposed under
// its own name so the orchestrator need not look under `gc`.
func runDispatch(args []string, stdout, stderr io.Writer) (err error) {
	flags := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspace := flags.String("workspace", "", "workspace")
	state := flags.String("state", "", "private state")
	raw := flags.String("request", "", "harness JSON request")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if *workspace == "" || *state == "" || flags.NArg() != 0 {
		return errors.New("dispatch: --workspace --state --request required")
	}
	var req coordinationRequest
	if e := json.Unmarshal([]byte(*raw), &req); e != nil {
		return e
	}
	if !ordinaryDispatchActions[req.Action] {
		return fmt.Errorf("dispatch: %q is a maintenance-cycle action; use `takt-ai gc coordinate`", req.Action)
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
	if !queryActions[req.Action] {
		if handled, err := admitRepair(stdout, fs, &req.Repair, true); handled {
			return err
		}
	}
	if out, query, queryErr := executionQuery(h, req); query {
		if queryErr != nil {
			return queryErr
		}
		return json.NewEncoder(stdout).Encode(out)
	}
	c, e := gc.LoadCoordinator(fs)
	if e != nil {
		return e
	}
	before, _ := h.Project().Unit(req.Session, req.Event)
	from := before.State
	result, e := coordinate(context.Background(), fs, h, *workspace, *state, c, req)
	if e != nil {
		if handled, err := refuseForRecovery(stdout, fs); handled {
			return err
		}
	}
	// Persist admission/lifecycle state even if the effect failed, exactly as
	// `gc coordinate` does: the barrier and the recorded disposition stay held.
	if save := gc.SaveCoordinator(fs, c); save != nil {
		return errors.Join(e, save)
	}
	if e != nil {
		if out := repairRefusal(e); out != nil {
			return json.NewEncoder(stdout).Encode(out)
		}
		return e
	}
	// A unit transition reports the states it moved between and the slots now
	// held, which only the history knows, so the plugin's telemetry invents neither.
	if lifecycleActions[req.Action] && result == c {
		after := h.Project()
		now, _ := after.Unit(req.Session, req.Event)
		result = lifecycleResponse{c, after.InFlight(), from, now.State}
	}
	return json.NewEncoder(stdout).Encode(result)
}

// staleDispatch reports whether r names a host delegation that no longer holds
// its unit: a later attempt admitted under another dispatch is not that
// delegation's to mark uncertain or reconcile.
func staleDispatch(h *history.History, r coordinationRequest) bool {
	if r.Dispatch == "" {
		return false
	}
	u, _ := h.Project().Unit(r.Session, r.Event)
	return u.State != history.StateInFlight || u.Dispatch != r.Dispatch
}

// lifecycleActions are the dispatch actions that move a work unit between states.
var lifecycleActions = map[string]bool{"admit": true, "launch": true, "finish": true}

// lifecycleResponse is the coordinator state plus the transition it recorded.
type lifecycleResponse struct {
	*gc.Coordinator
	InFlight  int           `json:"in_flight"`
	FromState history.State `json:"from_state"`
	ToState   history.State `json:"to_state"`
}

// queryActions only read the history, so no pending repair stands before them.
var queryActions = map[string]bool{"resolve": true, "execution": true, "executions": true, "interlocutor": true}

// Execution queries observe history only; a model question must not rebind GC.
func executionQuery(h *history.History, r coordinationRequest) (any, bool, error) {
	switch r.Action {
	case "resolve":
		out, err := resolveExecution(h, r.Child, r.Agent)
		return out, true, err
	case "execution":
		d, ok := h.Project().Executions[r.Dispatch]
		if !ok {
			return nil, true, nil
		}
		return d, true, nil
	case "executions":
		return listExecutions(h, time.Now()), true, nil
	case "interlocutor":
		// The holder fact for the root session, from its owner.
		return map[string]string{"holder": protocol.CurrentHolder(h, r.Session)}, true, nil
	}
	return nil, false, nil
}

// coordinate runs one dispatch action against the history, the VFS and the
// coordinator, after observing what the journal recorded since the last call.
// Execution queries are answered before it is called.
func coordinate(ctx context.Context, fs *vfs.FS, h *history.History, workspace, state string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	entries, e := observeCoordination(fs, h, c, r.Session)
	if e != nil {
		return nil, e
	}
	routine := func() (any, error) { return coordinateRoutine(fs, h, workspace, c, entries, r) }
	ref := journalRef(entries)
	// record wires the observed facts and declarations that consume no budget.
	record := func(kind history.Kind) func() (any, error) {
		return func() (any, error) { return c, protocol.Record(h, ref, r.Event, r.Session, kind) }
	}
	attempt := r.Attempt
	if attempt == "" {
		attempt = protocol.CurrentAttempt(h.Project(), r.Session, r.Event)
	}
	handlers := map[string]func() (any, error){
		"admit": routine, "finish": routine, "tick": routine,
		"prepare":      func() (any, error) { return prepareExecution(ctx, fs, h, workspace, c, entries, r) },
		"inputs":       func() (any, error) { return declareInputs(ctx, h, r) },
		"lease":        func() (any, error) { return nil, renewLease(h, r.Owner, time.Now()) },
		"attach_child": func() (any, error) { return attachExecutionChild(h, r) },
		"deliver":      func() (any, error) { return deliverExecution(ctx, h, r) },
		"launch": func() (any, error) {
			if staleDispatch(h, r) {
				return c, nil
			}
			return c, protocol.Launch(h, ref, r.Event, r.Session)
		},
		"uncertain": func() (any, error) {
			if staleDispatch(h, r) {
				return c, nil
			}
			return record(history.KindUncertain)()
		},
		"reconcile": func() (any, error) {
			if staleDispatch(h, r) {
				return c, nil
			}
			return c, protocol.Reconcile(h, ref, r.Event, r.Session, r.Pass)
		},
		"suspend":  record(history.KindSuspended),
		"cancel":   record(history.KindCancelRequested),
		"stop":     record(history.KindStopped),
		"escalate": record(history.KindEscalated),
		"withdraw": func() (any, error) { return c, protocol.Withdraw(h, ref, r.Event, r.Session) },
		"abandon":  func() (any, error) { return abandonExecution(fs, h, workspace, c, entries, r) },
		"commit": func() (any, error) {
			return c, protocol.Declare(h, ref, r.Session, r.Version, r.BaseVersion, r.Plan, r.Withdrawals)
		},
		"switch": func() (any, error) { return nil, protocol.Switch(h, ref, r.Session, r.Child, r.Agent, r.Artifact) },
		"validate_results": func() (any, error) {
			return nil, memory.ValidateSessionResultIDs(ctx, memory.Config{Root: memoryRoot()}, r.Session, r.Agent, r.ResultIDs)
		},
		"handoff":      func() (any, error) { return handoffWithCopyReport(h, ref, workspace, r) },
		"abort_switch": func() (any, error) { return abortWithArtifactReport(h, ref, workspace, r) },
		// A deleted session that holds a root is aborted in this same locked
		// unit; one that holds nothing writes nothing.
		"session_deleted": func() (any, error) { return abortDeletedHolder(h, ref, workspace, r) },
		"contest": func() (any, error) {
			p, e := protocol.LoadAdmissionPolicy()
			if e != nil {
				return nil, e
			}
			return c, protocol.Contest(h, p, ref, r.Event, r.Session, attempt)
		},
		"recovery": func() (any, error) {
			p, e := protocol.LoadAdmissionPolicy()
			if e != nil {
				return nil, e
			}
			return c, protocol.DeclareRecovery(h, p, ref, r.Event, r.Session, gc.RecoveryDeclaration{
				Objective: r.Objective, Result: r.Result, Point: r.Point,
				Scope: r.Scope, Actions: r.Actions, Attempts: r.Attempts,
			})
		},
		"recovered": func() (any, error) {
			return c, protocol.CloseRecovery(h, ref, r.Event, r.Session, r.Objective, r.Evidence, r.Pass)
		},
		"restore": func() (any, error) {
			return c, protocol.Restore(h, fs, ref, r.Session, r.Objective)
		},
		"exception": func() (any, error) {
			return c, protocol.Except(h, ref, r.Event, r.Session, r.Bound, r.Objective, r.Allowance)
		},
		"activity_start": func() (any, error) {
			if r.NodeKind != history.NodeKindOrchestrator {
				return nil, errors.New("dispatch: activity_start only accepts node_kind orchestrator")
			}
			return c, protocol.StartActivity(h, r.Session, r.ActivityID, r.NodeKind)
		},
		"activity_finish": func() (any, error) {
			if r.NodeKind != history.NodeKindOrchestrator {
				return nil, errors.New("dispatch: activity_finish only accepts node_kind orchestrator")
			}
			return c, protocol.FinishActivity(h, r.Session, r.ActivityID, r.NodeKind, history.Outcome(r.Outcome))
		},
	}
	if handler, ok := handlers[r.Action]; ok {
		return handler()
	}
	return nil, fmt.Errorf("dispatch: unknown action %q", r.Action)
}

// observeCoordination is the common journal observation and budget accounting
// before either entry performs its own actions. While a cycle is held, its
// lanes call with their own sessions but act for the root the cycle covers.
func observeCoordination(fs *vfs.FS, h *history.History, c *gc.Coordinator, session string) ([]vfs.JournalEntry, error) {
	if c.Cycle != nil {
		session = c.Cycle.Plan.SessionID
	}
	c.Bind(session)
	entries := journalEntries(fs, "")
	c.Observe(entries)
	c.ObserveExecutions(h)
	if e := protocol.Account(h, entries); e != nil {
		return nil, e
	}
	return entries, nil
}
