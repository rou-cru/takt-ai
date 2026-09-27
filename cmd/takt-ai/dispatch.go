package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

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
	Artifact          string           `json:"artifact"`
	AdditionalContext string           `json:"additional_context"`
	ExtraArtifacts    []string         `json:"extra_artifacts"`
	ResultIDs         []int64          `json:"result_ids"`
	Origin            string           `json:"origin"`
}

// journalRef is the Action Journal position an execution-history entry is
// recorded against; empty while the journal is empty.
func journalRef(entries []vfs.JournalEntry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[len(entries)-1].Ref()
}

func coordinateRoutineAction(h *history.History, c *gc.Coordinator, entries []vfs.JournalEntry, r coordinationRequest) (any, bool, error) {
	unit := h.Project().Units[r.Event]
	switch r.Action {
	case "admit":
		// A repeated host call yields its recorded disposition; a new dispatch
		// of the same unit is a retry, not another copy of this admission.
		repeated := unit.State != "" && unit.State != history.StatePlanned &&
			(r.Dispatch == "" || unit.Dispatch == r.Dispatch)
		if repeated {
			return c, true, nil
		}
		return nil, false, c.Admit(h, journalRef(entries), r.Event, r.Session, r.Agent, r.Dispatch)
	case "finish":
		if unit.State != history.StateInFlight || (r.Dispatch != "" && unit.Dispatch != r.Dispatch) {
			return c, true, nil
		}
		return nil, false, protocol.Finish(h, journalRef(entries), r.Event, r.Session)
	}
	return nil, false, nil
}

func coordinateRoutine(ctx context.Context, fs *vfs.FS, h *history.History, workspace string, c *gc.Coordinator, entries []vfs.JournalEntry, r coordinationRequest) (any, error) {
	result, handled, actionErr := coordinateRoutineAction(h, c, entries, r)
	if handled {
		return result, nil
	}
	// A denied admission is still a real tick: advance the GC barrier and its
	// deferral limit. A repeated host call, above, consumes neither.
	advanceResult, advanceErr := coordinateAdvance(ctx, fs, h, workspace, c, entries, r)
	if actionErr != nil {
		return nil, errors.Join(actionErr, advanceErr)
	}
	return advanceResult, advanceErr
}

// ordinaryDispatchActions are the crew dispatch actions any orchestrator may
// take through `takt-ai dispatch`: admission, lifecycle, plan commitment,
// contests, recovery, user exceptions, and the interlocutor stack
// (switch/handoff/abort_switch). tick carries no GC cycle awareness of its
// own; it only re-runs the same admission/recovery accounting pass admit and
// finish already fall through to, so it belongs here too. Direct activity has
// its own activity_id and never enters the work-unit path. They are not GC's;
// GC's own maintenance-cycle phases (prepare, baseline, findings,
// investigate, authorize, collected, delta, verdict, acceptance, no-change,
// attach, recover, abort) and its own trigger bookkeeping (request, status)
// stay under `takt-ai gc coordinate`.
var ordinaryDispatchActions = map[string]bool{
	"admit": true, "finish": true, "tick": true,
	"activity_start": true, "activity_finish": true,
	"launch": true, "uncertain": true, "reconcile": true, "suspend": true,
	"cancel": true, "stop": true, "escalate": true, "withdraw": true,
	"commit": true, "contest": true, "recovery": true, "recovered": true,
	"restore": true, "exception": true,
	"switch": true, "handoff": true, "abort_switch": true, "validate_results": true,
}

// runDispatch admits and drives ordinary crew work through the execution
// history: the same underlying protocol `gc coordinate` uses to gate its own
// cycle, exposed under its own name so the orchestrator does not need to look
// under `gc` to find it.
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
	c, e := gc.LoadCoordinator(*state)
	if e != nil {
		return e
	}
	response, e := coordinate(context.Background(), fs, h, *workspace, *state, c, req)
	// Persist admission/lifecycle state even if the effect failed, exactly as
	// `gc coordinate` does: the barrier and the recorded disposition stay held.
	if save := gc.SaveCoordinator(*state, c); save != nil {
		return errors.Join(e, save)
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(stdout).Encode(response)
}

func coordinate(ctx context.Context, fs *vfs.FS, h *history.History, workspace, state string, c *gc.Coordinator, r coordinationRequest) (any, error) {
	entries, e := observeCoordination(fs, h, c)
	if e != nil {
		return nil, e
	}
	routine := func() (any, error) { return coordinateRoutine(ctx, fs, h, workspace, c, entries, r) }
	ref := journalRef(entries)
	// record wires the observed facts and declarations that consume no budget.
	record := func(kind history.Kind) func() (any, error) {
		return func() (any, error) { return c, protocol.Record(h, ref, r.Event, r.Session, kind) }
	}
	attempt := r.Attempt
	if attempt == "" {
		attempt = protocol.CurrentAttempt(h.Project(), r.Event)
	}
	handlers := map[string]func() (any, error){
		"admit": routine, "finish": routine, "tick": routine,
		"launch":           func() (any, error) { return c, protocol.Launch(h, ref, r.Event, r.Session) },
		"uncertain":        record(history.KindUncertain),
		"reconcile":        func() (any, error) { return c, protocol.Reconcile(h, ref, r.Event, r.Session, r.Pass) },
		"suspend":          record(history.KindSuspended),
		"cancel":           record(history.KindCancelRequested),
		"stop":             record(history.KindStopped),
		"escalate":         record(history.KindEscalated),
		"withdraw":         func() (any, error) { return c, protocol.Withdraw(h, ref, r.Event, r.Session) },
		"commit":           func() (any, error) { return c, protocol.Commit(h, ref, r.Session, r.Version, r.Plan) },
		"switch":           func() (any, error) { return nil, protocol.Switch(h, ref, r.Session, r.Child, r.Agent, r.Artifact) },
		"validate_results": func() (any, error) { return nil, memory.ValidateResultIDs(ctx, memory.Config{}, r.ResultIDs) },
		"handoff": func() (any, error) {
			return protocol.BuildHandoffEnvelope(h, ref, workspace, r.Session, r.Child, r.Agent, r.Result, r.AdditionalContext, r.ExtraArtifacts, r.ResultIDs)
		},
		"abort_switch": func() (any, error) {
			return protocol.BuildAbortEnvelope(h, ref, workspace, r.Session, r.Child, r.Evidence, r.Origin)
		},
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
			return c, protocol.Restore(h, fs, ref, r.Session, r.Objective, vfs.AgentID(r.Key))
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
// before either entry performs its own actions.
func observeCoordination(fs *vfs.FS, h *history.History, c *gc.Coordinator) ([]vfs.JournalEntry, error) {
	entries := journalEntries(fs, "")
	c.Observe(entries)
	if e := protocol.Account(h, entries); e != nil {
		return nil, e
	}
	return entries, nil
}
