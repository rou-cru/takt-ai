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

// coordinateRoutine admits and finishes ordinary work. Maintenance never gates
// either: an admission ends a cycle in flight first, so the orchestrator is
// never made to wait for one (PR-MNT-3). Whether a cycle is due is decided only
// when the session is idle, by `gc coordinate`'s tick.
func coordinateRoutine(fs *vfs.FS, h *history.History, workspace string, c *gc.Coordinator, entries []vfs.JournalEntry, r coordinationRequest) (any, error) {
	unit := h.Project().Units[r.Event]
	switch r.Action {
	case "admit":
		// A repeated host call yields its recorded disposition; a new dispatch
		// of the same unit is a retry, not another copy of this admission.
		repeated := unit.State != "" && unit.State != history.StatePlanned &&
			(r.Dispatch == "" || unit.Dispatch == r.Dispatch)
		if repeated {
			return c, nil
		}
		if c.Cycle != nil {
			// A cycle that cannot be unwound records why on itself (blocked,
			// with its reason); that never refuses the orchestrator's dispatch.
			_ = abortGCCycle(h, fs, workspace, c, "yielded: ordinary dispatch resumed")
		}
		return c, c.Admit(h, journalRef(entries), r.Event, r.Session, r.Agent, r.Dispatch)
	case "finish":
		if unit.State != history.StateInFlight || (r.Dispatch != "" && unit.Dispatch != r.Dispatch) {
			return c, nil
		}
		return c, protocol.Finish(h, journalRef(entries), r.Event, r.Session)
	}
	return c, nil
}

// ordinaryDispatchActions are the crew dispatch actions any orchestrator may
// take through `takt-ai dispatch`: admission, lifecycle, plan commitment,
// contests, recovery, user exceptions, and the interlocutor stack
// (switch/handoff/abort_switch). tick carries no GC cycle awareness of its
// own; it only re-runs the accounting pass every request already begins with,
// so it belongs here too. Direct activity has
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
	"switch": true, "handoff": true, "abort_switch": true, "validate_results": true, "validate_inputs": true,
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
		attempt = protocol.CurrentAttempt(h.Project(), r.Event)
	}
	handlers := map[string]func() (any, error){
		"admit": routine, "finish": routine, "tick": routine,
		"launch":    func() (any, error) { return c, protocol.Launch(h, ref, r.Event, r.Session) },
		"uncertain": record(history.KindUncertain),
		"reconcile": func() (any, error) { return c, protocol.Reconcile(h, ref, r.Event, r.Session, r.Pass) },
		"suspend":   record(history.KindSuspended),
		"cancel":    record(history.KindCancelRequested),
		"stop":      record(history.KindStopped),
		"escalate":  record(history.KindEscalated),
		"withdraw":  func() (any, error) { return c, protocol.Withdraw(h, ref, r.Event, r.Session) },
		"commit": func() (any, error) {
			return c, protocol.Declare(h, ref, r.Session, r.Version, r.BaseVersion, r.Plan, r.Withdrawals)
		},
		"switch": func() (any, error) { return nil, protocol.Switch(h, ref, r.Session, r.Child, r.Agent, r.Artifact) },
		"validate_results": func() (any, error) {
			return nil, memory.ValidateSessionResultIDs(ctx, memory.Config{Root: memoryRoot()}, r.Session, r.Agent, r.ResultIDs)
		},
		// A delegation's consumed invariants must name existing entries; who
		// recorded them is irrelevant to the consumer.
		"validate_inputs": func() (any, error) {
			return nil, memory.ValidateResultIDs(ctx, memory.Config{Root: memoryRoot()}, r.ResultIDs)
		},
		"handoff":      func() (any, error) { return handoffWithCopyReport(h, ref, workspace, r) },
		"abort_switch": func() (any, error) { return abortWithArtifactReport(h, ref, workspace, r) },
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
func observeCoordination(fs *vfs.FS, h *history.History, c *gc.Coordinator, session string) ([]vfs.JournalEntry, error) {
	c.Bind(session)
	entries := journalEntries(fs, "")
	c.Observe(entries)
	if e := protocol.Account(h, entries); e != nil {
		return nil, e
	}
	return entries, nil
}
