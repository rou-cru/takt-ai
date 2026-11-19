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
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// coordinationRequest is harness IPC, not a model-selected identity or mandate.
// Ordinary dispatch owns the shared envelope; `gc coordinate` consumes only its cycle fields.
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
	Scope             []string         `json:"scope"`
	Actions           int              `json:"actions"`
	Attempts          int              `json:"attempts"`
	Key               string           `json:"key"`
	Bound             string           `json:"bound"`
	Allowance         int              `json:"allowance"`
	Version           string           `json:"version"`
	Plan              []protocol.PlanUnit `json:"plan"`
	QARound           *history.QARound `json:"qa_round,omitempty"`
	BaseVersion       string           `json:"base_version,omitempty"`
	Withdrawals       []string         `json:"withdrawals,omitempty"`
	AdditionalContext string           `json:"additional_context"`
	ExtraArtifacts    []string         `json:"extra_artifacts"`
	ResultIDs         []int64          `json:"result_ids"`
	// Units are the producer units an inputs declaration consumes.
	Units []string `json:"units,omitempty"`
	// Blocker marks a delivery whose entries record a blocker (PR-HAR-38).
	Blocker bool `json:"blocker,omitempty"`
	// Requirements are the entry IDs a switch declares for the lent session;
	// an empty list states that nothing useful exists (CTX-6).
	Requirements []int64 `json:"requirements"`
	Origin       string  `json:"origin"`
	Caller       string  `json:"caller,omitempty"`
	CreatedAt    int64   `json:"created_at,omitempty"`
	// AuthorKeys distinguishes an absent list (no gates) from an empty one,
	// which a verifier preparation refuses.
	AuthorKeys []vfs.AgentID `json:"author_keys,omitempty"`
	Phase      string        `json:"phase,omitempty"`
	// Owner is the harness instance issuing the request, for execution
	// ownership and lease renewal.
	Owner   string              `json:"owner,omitempty"`
	Stopped []laneStop          `json:"stopped,omitempty"`
	Repair  *vfs.RepairApproval `json:"repair,omitempty"`
	// Model is the provider/model a delegation runs; PreviousModel is the
	// effective native model before an override. SeatReason and Substitute declare
	// a seat restoration (PR-CRW-13); UserConfirmed carries the user's approval.
	Model         string `json:"model,omitempty"`
	PreviousModel string `json:"previous_model,omitempty"`
	SeatReason    string `json:"seat_reason,omitempty"`
	Substitute    bool   `json:"substitute,omitempty"`
	UserConfirmed bool   `json:"user_confirmed,omitempty"`
}

// memoryAdmin connects to the admin path of the running memory service; it never
// starts the owner (PR-MSV-35), so an absent service surfaces as unreachable.
func memoryAdmin(ctx context.Context) (*memory.Admin, error) {
	dir, err := memory.ResolveDir()
	if err != nil {
		return nil, err
	}
	return memory.DialAdmin(ctx, dir)
}

// checkMemory is the harness's one entry validator over its own admin connection.
func checkMemory(ctx context.Context, ids []int64, rule memory.Rule) (_ memory.DescribeResult, err error) {
	admin, err := memoryAdmin(ctx)
	if err != nil {
		return memory.DescribeResult{}, err
	}
	defer func() { err = errors.Join(err, admin.Close()) }()
	return memory.Validate(ctx, admin, ids, rule)
}

// currentMemory carries entry IDs forward to their current versions over the harness's admin connection.
func currentMemory(ctx context.Context, ids []int64) (_ []int64, _ memory.DescribeResult, err error) {
	admin, err := memoryAdmin(ctx)
	if err != nil {
		return nil, memory.DescribeResult{}, err
	}
	defer func() { err = errors.Join(err, admin.Close()) }()
	return memory.Current(ctx, admin, ids)
}

// journalRef is the Action Journal position an execution-history entry is
// recorded against; empty while the journal is empty.
func journalRef(entries []vfs.JournalEntry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[len(entries)-1].Ref()
}

// interlocutorReturn is what the harness delivers when the interface comes
// back: the four-field envelope, the lent session, and for an abort its root. Notice is the text the
// convening agent reads, so every host relays the same words.
type interlocutorReturn struct {
	Envelope protocol.Envelope `json:"envelope"`
	Child    string            `json:"child,omitempty"`
	Root     string            `json:"root,omitempty"`
	Notice   string            `json:"notice"`
}

// withNotice words the return for the convening agent.
func (r interlocutorReturn) withNotice(from string) (interlocutorReturn, error) {
	envelope, e := json.MarshalIndent(r.Envelope, "", "  ")
	if e != nil {
		return r, e
	}
	r.Notice = fmt.Sprintf("The interface is back from %s. Envelope:\n%s", from, envelope)
	return r, nil
}

// handoffOutcome is the negotiated result a handoff request carries.
func handoffOutcome(r coordinationRequest) protocol.HandoffOutcome {
	return protocol.HandoffOutcome{Result: r.Result, AdditionalContext: r.AdditionalContext, ExtraArtifacts: r.ExtraArtifacts, ResultIDs: r.ResultIDs}
}

// handoffReturn assembles the handoff envelope, whose memory entry IDs
// BuildHandoffEnvelope validates.
func handoffReturn(ctx context.Context, h *history.History, ref string, r coordinationRequest) (any, error) {
	envelope, e := protocol.BuildHandoffEnvelope(ctx, h, ref, checkMemory, r.Session, r.Child, handoffOutcome(r))
	if e != nil {
		return nil, e
	}
	return interlocutorReturn{Envelope: envelope, Child: r.Child}.withNotice(r.Agent)
}

// abortReturn assembles the synthesized abort envelope. Without
// r.Session, r.Child names the session the user is in: a lent session aborts
// itself, a convening session its current holder.
func abortReturn(h *history.History, ref string, r coordinationRequest) (interlocutorReturn, error) {
	if r.Session == "" {
		if root, held := h.Project().HeldRoot(r.Child); held {
			r.Session = root
		} else {
			r.Session, r.Child = r.Child, ""
		}
	}
	holder := protocol.CurrentAgent(h, r.Session)
	envelope, child, e := protocol.BuildAbortEnvelope(h, ref, r.Session, r.Child, r.Evidence, r.Origin)
	if e != nil {
		return interlocutorReturn{}, e
	}
	return interlocutorReturn{Envelope: envelope, Child: child, Root: r.Session}.withNotice(holder + ", whose lent session was aborted")
}

// sessionDeletedEvidence is the abort evidence recorded when a holder session
// is deleted by the host.
const sessionDeletedEvidence = "session ended"

// abortDeletedHolder aborts the root the deleted session currently holds, as a
// harness-origin abort_switch would, and reports whether it held one.
func abortDeletedHolder(h *history.History, ref string, r coordinationRequest) (any, error) {
	root, held := h.Project().HeldRoot(r.Child)
	if !held {
		return map[string]any{"aborted": false}, nil
	}
	abort := r
	abort.Session, abort.Evidence, abort.Origin = root, sessionDeletedEvidence, "harness"
	out, e := abortReturn(h, ref, abort)
	if e != nil {
		return nil, e
	}
	return out, nil
}

// acknowledged answers a dispatch action that has nothing to report beyond its success.
var acknowledged = struct {
	OK bool `json:"ok"`
}{true}

// coordinateRoutine admits and finishes ordinary work.
func coordinateRoutine(fs *vfs.FS, h *history.History, entries []vfs.JournalEntry, r coordinationRequest) (any, error) {
	unit, _ := h.Project().Unit(r.Session, r.Event)
	switch r.Action {
	case "admit":
		// A repeated host call yields its recorded disposition; a new dispatch
		// of the same unit is a retry, not another copy of this admission.
		repeated := unit.State != "" && unit.State != history.StatePlanned &&
			(r.Dispatch == "" || unit.Dispatch == r.Dispatch)
		if repeated {
			return acknowledged, nil
		}
		p, e := protocol.LoadAdmissionPolicy()
		if e != nil {
			return nil, e
		}
		req := protocol.AdmissionRequest{Event: r.Event, Session: r.Session, Agent: r.Agent, Dispatch: r.Dispatch}
		return acknowledged, protocol.Admit(h, p, journalRef(entries), req)
	case "finish":
		if unit.State != history.StateInFlight || (r.Dispatch != "" && unit.Dispatch != r.Dispatch) || (r.Attempt != "" && unit.AttemptID != r.Attempt) {
			return acknowledged, nil
		}
		// Settling follows an exact host observation of this dispatch and
		// attempt, whichever harness instance relays it; a repeat is a no-op.
		if err := fs.CancelPreparedDelegation(r.Session, r.Event, unit.AttemptID); err != nil {
			return nil, err
		}
		return acknowledged, protocol.Finish(h, journalRef(entries), r.Event, r.Session)
	}
	return acknowledged, nil
}

// abandonExecution ends an ordinary delegation the host can no longer prove
// terminated, on the user's decision or once its caller session is gone: its
// unit's attempt ends interrupted and its slot is released; staged work stays.
// A repeat is a no-op.
func abandonExecution(fs *vfs.FS, h *history.History, entries []vfs.JournalEntry, r coordinationRequest) (any, error) {
	d, ok := h.Project().Executions[r.Dispatch]
	if !ok || d.Cycle != "" {
		return nil, fmt.Errorf("dispatch: %q names no ordinary delegation; use a dispatch from the unsettled list a refusal reported", r.Dispatch)
	}
	if d.Settled {
		return acknowledged, nil
	}
	u, _ := h.Project().Unit(d.Root, d.Unit)
	if u.State != history.StateInFlight || u.Dispatch != d.Dispatch || u.AttemptID != d.Attempt {
		return acknowledged, h.ExecutionFact(d, history.KindExecutionEnded, "", "", nil)
	}
	if e := protocol.Record(h, journalRef(entries), d.Unit, d.Root, history.KindCancelRequested); e != nil {
		return nil, e
	}
	finish := r
	finish.Action, finish.Event, finish.Session, finish.Dispatch, finish.Attempt = "finish", d.Unit, d.Root, d.Dispatch, d.Attempt
	return coordinateRoutine(fs, h, entries, finish)
}

// ordinaryDispatchActions are the crew actions any orchestrator may take
// through `takt-ai dispatch`: admission, lifecycle, plan commitment, contests,
// recovery, user exceptions, the interlocutor stack, and tick (which re-runs
// the accounting pass every request begins with). Direct activity uses its own
// activity_id and never enters the work-unit path.
var ordinaryDispatchActions = map[string]bool{
	"prepare": true, "inputs": true, "execution": true, "executions": true, "lease": true, "attach_child": true, "deliver": true, "resolve": true,
	"admit": true, "finish": true, "tick": true,
	"activity_start": true, "activity_finish": true, "activity_unmonitored": true,
	"launch": true, "uncertain": true, "unmonitored": true, "reconcile": true, "suspend": true,
	"cancel": true, "stop": true, "escalate": true, "withdraw": true,
	"commit": true, "contest": true, "recovery": true, "recovered": true,
	"restore": true, "exception": true, "abandon": true,
	"switch": true, "handoff": true, "abort_switch": true, "session_deleted": true, "interlocutor": true, "validate_results": true, "validate_handoff": true, "validate_switch": true,
}

// runDispatch admits and drives ordinary crew work through the execution
// history.
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
		return fmt.Errorf("dispatch: %q is a GC cycle action; use `takt-ai gc coordinate`", req.Action)
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
	if out, query, queryErr := executionQuery(context.Background(), h, req); query {
		if queryErr != nil {
			return queryErr
		}
		return json.NewEncoder(stdout).Encode(out)
	}
	before, _ := h.Project().Unit(req.Session, req.Event)
	from := before.State
	result, e := coordinate(context.Background(), fs, h, *workspace, req)
	if e != nil {
		if handled, err := refuseForRecovery(stdout, fs); handled {
			return err
		}
		return e
	}
	// A unit transition reports the states it moved between and the slots now
	// held, which only the history knows, so the plugin's telemetry invents neither.
	if lifecycleActions[req.Action] {
		after := h.Project()
		now, _ := after.Unit(req.Session, req.Event)
		result = lifecycleResponse{true, after.InFlight(), from, now.State}
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

// lifecycleResponse is the transition a lifecycle action recorded.
type lifecycleResponse struct {
	OK        bool          `json:"ok"`
	InFlight  int           `json:"in_flight"`
	FromState history.State `json:"from_state"`
	ToState   history.State `json:"to_state"`
}

// queryActions only read the history, so no pending repair stands before them.
var queryActions = map[string]bool{"resolve": true, "execution": true, "executions": true, "interlocutor": true, "validate_handoff": true, "validate_switch": true}

// Execution queries observe history only, and answer about ordinary
// executions alone: a maintenance lane is never the crew's.
func executionQuery(ctx context.Context, h *history.History, r coordinationRequest) (any, bool, error) {
	switch r.Action {
	case "resolve":
		out, err := resolveExecution(h, r.Child, r.Agent)
		return out, true, err
	case "execution":
		d, ok := h.Project().Executions[r.Dispatch]
		if !ok || d.Cycle != "" {
			return nil, true, nil
		}
		return d, true, nil
	case "executions":
		return listExecutions(h, time.Now()), true, nil
	case "interlocutor":
		// The holder fact for the root session, from its owner.
		return map[string]string{"holder": protocol.CurrentHolder(h, r.Session)}, true, nil
	// The validation-only actions run the checks their committing action runs,
	// so the user is asked only about a request that will be accepted (PR-HAR-23).
	case "validate_handoff":
		return nil, true, protocol.ValidateHandoff(ctx, h, checkMemory, r.Session, r.Child, handoffOutcome(r))
	case "validate_switch":
		return nil, true, errors.Join(validateRequirements(ctx, r.Requirements), protocol.ValidateSwitch(h, r.Session, r.Agent))
	}
	return nil, false, nil
}

// coordinate runs one dispatch action against the history and the VFS, after
// accounting what the journal recorded since the last call. Execution queries
// are answered before it is called.
func coordinate(ctx context.Context, fs *vfs.FS, h *history.History, workspace string, r coordinationRequest) (any, error) {
	entries := journalEntries(fs, "")
	if e := protocol.Account(h, entries); e != nil {
		return nil, e
	}
	routine := func() (any, error) { return coordinateRoutine(fs, h, entries, r) }
	ref := journalRef(entries)
	// record wires the observed facts and declarations that consume no budget.
	record := func(kind history.Kind) func() (any, error) {
		return func() (any, error) { return acknowledged, protocol.Record(h, ref, r.Event, r.Session, kind) }
	}
	// unestablished records launch or capture that could not be established, unless
	// a later attempt under another dispatch already holds the unit. A repeated
	// report of lost capture records nothing new.
	unestablished := func(kind history.Kind) func() (any, error) {
		return func() (any, error) {
			if staleDispatch(h, r) {
				return acknowledged, nil
			}
			if u, _ := h.Project().Unit(r.Session, r.Event); kind == history.KindUnmonitored && u.State == history.StateInFlight && u.Flight == history.FlightUnmonitored {
				return acknowledged, nil
			}
			return record(kind)()
		}
	}
	attempt := r.Attempt
	if attempt == "" {
		attempt = protocol.CurrentAttempt(h.Project(), r.Session, r.Event)
	}
	handlers := map[string]func() (any, error){
		"admit": routine, "finish": routine, "tick": routine,
		"prepare":      func() (any, error) { return prepareExecution(ctx, fs, h, entries, r) },
		"inputs":       func() (any, error) { return declareInputs(ctx, h, r) },
		"lease":        func() (any, error) { return nil, renewLease(h, r.Owner, time.Now()) },
		"attach_child": func() (any, error) { return attachExecutionChild(h, r) },
		"deliver":      func() (any, error) { return deliverExecution(ctx, h, r) },
		"launch": func() (any, error) {
			if staleDispatch(h, r) {
				return acknowledged, nil
			}
			return acknowledged, protocol.Launch(h, ref, r.Event, r.Session)
		},
		"uncertain":   unestablished(history.KindUncertain),
		"unmonitored": unestablished(history.KindUnmonitored),
		"reconcile": func() (any, error) {
			if staleDispatch(h, r) {
				return acknowledged, nil
			}
			// A non-start is the effective termination: release the gates prepared
			// for this attempt first, as finish does, because the unit stops being
			// in flight once reconciled and a retry could no longer reach them.
			if u, _ := h.Project().Unit(r.Session, r.Event); !r.Pass && u.State == history.StateInFlight && protocol.UncertainFlight(u.Flight) {
				if err := fs.CancelPreparedDelegation(r.Session, r.Event, u.AttemptID); err != nil {
					return nil, err
				}
			}
			return acknowledged, protocol.Reconcile(h, ref, r.Event, r.Session, r.Pass)
		},
		"suspend":  record(history.KindSuspended),
		"cancel":   record(history.KindCancelRequested),
		"stop":     record(history.KindStopped),
		"escalate": record(history.KindEscalated),
		"withdraw": func() (any, error) { return acknowledged, protocol.Withdraw(h, ref, r.Event, r.Session) },
		"abandon":  func() (any, error) { return abandonExecution(fs, h, entries, r) },
		"commit": func() (any, error) {
			return acknowledged, protocol.Declare(h, ref, r.Session, r.Version, r.BaseVersion, r.Plan, r.Withdrawals, r.QARound)
		},
		"switch":           func() (any, error) { return nil, switchInterface(ctx, h, ref, workspace, r) },
		"validate_results": func() (any, error) { return nil, validateTopLevelDelivery(ctx, r) },
		"handoff":          func() (any, error) { return handoffReturn(ctx, h, ref, r) },
		"abort_switch":     func() (any, error) { return abortReturn(h, ref, r) },
		// A deleted session that holds a root is aborted in this same locked
		// unit; one that holds nothing writes nothing.
		"session_deleted": func() (any, error) { return abortDeletedHolder(h, ref, r) },
		"contest": func() (any, error) {
			p, e := protocol.LoadAdmissionPolicy()
			if e != nil {
				return nil, e
			}
			return acknowledged, protocol.Contest(h, p, ref, r.Event, r.Session, attempt)
		},
		"recovery": func() (any, error) {
			p, e := protocol.LoadAdmissionPolicy()
			if e != nil {
				return nil, e
			}
			return acknowledged, protocol.DeclareRecovery(h, p, ref, r.Event, r.Session, protocol.RecoveryDeclaration{
				Objective: r.Objective, Result: r.Result,
				Scope: r.Scope, Actions: r.Actions, Attempts: r.Attempts,
			})
		},
		"recovered": func() (any, error) {
			return acknowledged, protocol.CloseRecovery(h, ref, r.Event, r.Session, r.Objective, r.Evidence, r.Pass)
		},
		"restore": func() (any, error) {
			return acknowledged, protocol.Restore(h, fs, ref, r.Session, r.Objective)
		},
		"exception": func() (any, error) {
			return acknowledged, protocol.Except(h, ref, r.Event, r.Session, r.Bound, r.Objective, r.Allowance)
		},
		"activity_start": func() (any, error) {
			if r.NodeKind != history.NodeKindOrchestrator {
				return nil, errors.New("dispatch: activity_start only accepts node_kind orchestrator")
			}
			// The capturing instance is alive as it starts the activity: its lease
			// is renewed first, so no other instance reads the activity orphaned.
			if r.Owner != "" {
				if err := renewLease(h, r.Owner, time.Now()); err != nil {
					return nil, err
				}
			}
			return acknowledged, protocol.StartActivity(h, r.Session, r.ActivityID, r.NodeKind, r.Owner)
		},
		"activity_finish": func() (any, error) {
			if r.NodeKind != history.NodeKindOrchestrator {
				return nil, errors.New("dispatch: activity_finish only accepts node_kind orchestrator")
			}
			return acknowledged, protocol.FinishActivity(h, r.Session, r.ActivityID, r.NodeKind, history.Outcome(r.Outcome))
		},
		// Capture of a non-unit activity was lost: it stays in flight, neither
		// failed nor finished (PR-OBS-BUS-5).
		"activity_unmonitored": func() (any, error) {
			if r.NodeKind != history.NodeKindOrchestrator {
				return nil, errors.New("dispatch: activity_unmonitored only accepts node_kind orchestrator")
			}
			return acknowledged, protocol.MarkActivityUnmonitored(h, r.Session, r.ActivityID)
		},
	}
	if handler, ok := handlers[r.Action]; ok {
		return handler()
	}
	return nil, fmt.Errorf("dispatch: unknown action %q", r.Action)
}
