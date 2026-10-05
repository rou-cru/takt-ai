// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history

import (
	"slices"
	"strconv"
	"strings"
)

// State is the temporal state of PR-DAG-TMP-1. Every work unit holds exactly one.
type State string

// States are the closed set of temporal states (PR-DAG-TMP-1).
const (
	// StatePlanned is declared work that has not been admitted.
	StatePlanned State = "planned"
	// StateInFlight is admitted work that has not effectively terminated.
	StateInFlight State = "in flight"
	// StateSettled is work whose effective termination is recorded.
	StateSettled State = "settled"
	// StateWithdrawn is work revoked while planned, never admitted.
	StateWithdrawn State = "withdrawn"
)

// Flight distinguishes the in-flight conditions PR-DAG-TMP-1 keeps visible.
// Reservation is not proof of execution.
type Flight string

// Flights are the closed set of in-flight conditions.
const (
	// FlightPendingLaunch holds a reserved slot without observed execution.
	FlightPendingLaunch Flight = "admitted pending launch"
	// FlightRunning is execution observed running.
	FlightRunning Flight = "observed running"
	// FlightSuspended is execution held while it retains the ability to act.
	FlightSuspended Flight = "suspended"
	// FlightCancelling is cancellation requested but not yet effective.
	FlightCancelling Flight = "cancellation pending"
	// FlightUncertain is launch or liveness that could not be established.
	FlightUncertain Flight = "launch uncertain"
)

// flightOf maps the in-flight transitions.
var flightOf = map[Kind]Flight{
	KindAdmitted:        FlightPendingLaunch,
	KindLaunched:        FlightRunning,
	KindSuspended:       FlightSuspended,
	KindCancelRequested: FlightCancelling,
	KindUncertain:       FlightUncertain,
}

// Outcome is the prevailing outcome of settled work.
type Outcome string

// Outcomes are the closed set of settled results.
const (
	// OutcomeCompleted is work that ended with a result.
	OutcomeCompleted Outcome = "completed"
	// OutcomeFailed is work whose terminal failure is established.
	OutcomeFailed Outcome = "failed"
	// OutcomeBacktracked is work whose line was abandoned.
	OutcomeBacktracked Outcome = "backtracked"
	// OutcomeInterrupted is work cancelled or closed without a result.
	OutcomeInterrupted Outcome = "interrupted"
)

// Bound names a deterministic budget: the cause recorded on a denial, and the
// limit a user exception scopes (PR-HAR-22).
const (
	// BoundConcurrency caps concurrently admitted specialists.
	BoundConcurrency = "ceiling/concurrent-specialists"
	// BoundUnplanned caps units admitted without plan coverage.
	BoundUnplanned = "budget/unplanned-units"
	// BoundContests caps contests of terminal failures.
	BoundContests = "budget/contests"
	// BoundRecovery caps consecutive recoveries closed without their result.
	BoundRecovery = "budget/recovery-failures"
	// BoundRecoveryActions and BoundRecoveryAttempts are the two distinct
	// budgets of one declared recovery (PR-HAR-6, PR-ORQ-13).
	BoundRecoveryActions  = "budget/recovery-actions"
	BoundRecoveryAttempts = "budget/recovery-attempts"
)

// bounds enumerates the limits a user exception may scope.
var bounds = map[string]bool{
	BoundConcurrency: true, BoundUnplanned: true, BoundContests: true,
	BoundRecovery: true, BoundRecoveryActions: true, BoundRecoveryAttempts: true,
}

// AllowanceKey names the scope an exception's allowance applies to: a bound,
// narrowed by objective where the exception is objective-scoped, so an
// exception never authorizes unrelated work (PR-HAR-22).
func AllowanceKey(bound, objective string) string {
	if objective == "" {
		return bound
	}
	return bound + "/" + objective
}

// Unit is a work unit as the replay derives it.
type Unit struct {
	SessionID string `json:"session_id"`
	// NodeKind is delegated for explicitly tagged work. Empty means a legacy
	// entry; snapshots map that case to NodeKindDelegated as well.
	NodeKind NodeKind `json:"node_kind,omitempty"`
	// AttemptID identifies the attempt; FirstAttempt for the first one.
	AttemptID string `json:"attempt_id"`
	// Dispatch is the host delegation that admitted the current attempt.
	Dispatch string `json:"dispatch,omitempty"`
	// State is the unit's temporal state.
	State State `json:"state"`
	// Flight is the in-flight condition while State is in flight.
	Flight Flight `json:"flight,omitempty"`
	// Outcome is the prevailing outcome once settled.
	Outcome Outcome `json:"outcome,omitempty"`
	// Launched says whether execution was ever observed, so a pending admission
	// that terminates without launch never appears executed.
	Launched bool `json:"launched"`
	// Committed says a plan commitment declared this unit. Work admitted
	// without plan coverage is never committed.
	Committed bool `json:"committed,omitempty"`
	// Contract and Prerequisites are what a plan commitment declared for this
	// unit; both are empty for work admitted without plan coverage.
	Contract      string   `json:"contract,omitempty"`
	Prerequisites []string `json:"prerequisites,omitempty"`
}

// Activity is a non-work-unit execution record. Its ActivityID is a real
// activity identity and is never inserted into Projection.Units.
type Activity struct {
	ActivityID string   `json:"activity_id"`
	SessionID  string   `json:"session_id"`
	NodeKind   NodeKind `json:"node_kind"`
	State      State    `json:"state"`
	Flight     Flight   `json:"flight,omitempty"`
	Outcome    Outcome  `json:"outcome,omitempty"`
}

// Recovery is one objective's bounded-recovery accounting.
type Recovery struct {
	// Open marks a declared recovery that has not closed.
	Open bool `json:"open"`
	// Failures counts consecutive recoveries closed without the declared
	// result; only a demonstrated recovery clears the streak (PR-HAR-21).
	Failures int `json:"failures"`
	// Unit is the work unit the declaration was recorded against, so a forced
	// closure names it instead of inventing one.
	Unit string `json:"unit,omitempty"`
	// Result, Point and Scope are what the declaration recorded before the
	// uncertain work began; empty means the declaration was absent.
	Result string   `json:"result,omitempty"`
	Point  string   `json:"point,omitempty"`
	Scope  []string `json:"scope,omitempty"`
	// Actions and Attempts are the declared allowances, Used and Attempted the
	// consumption derived from recorded facts alone. Neither budget is reset by
	// a runtime restart, because both are replayed rather than remembered.
	Actions   int             `json:"actions,omitempty"`
	Attempts  int             `json:"attempts,omitempty"`
	Used      int             `json:"used"`
	Attempted map[string]bool `json:"attempted,omitempty"`
	// Cursor is the bus position through which action consumption is
	// established; an interval with no recorded action consumes nothing.
	Cursor int `json:"cursor"`
	// Unreconciled marks consumption that could not be established: the
	// uncertainty stays visible and budgeted admission waits for an enabling
	// decision instead of receiving a fresh allowance (PR-OBS-PRG-2).
	Unreconciled bool `json:"unreconciled,omitempty"`
	// Backtracked and Restored are the separate observable facts of the
	// abandonment decision and of confirmed restoration (PR-DAG-TMP-5).
	Backtracked bool `json:"backtracked,omitempty"`
	Restored    bool `json:"restored,omitempty"`
}

// Unresolved reports a recovery that still governs its declared scope: open, or
// abandoned by forced backtracking and not yet restored.
func (r Recovery) Unresolved() bool { return r.Open || (r.Backtracked && !r.Restored) }

// Scope returns the objective of the unresolved recovery whose declared scope
// contains unit. Membership is declared, never inferred (PR-DAG-MUT-9), and an
// overlapping declaration is refused, so at most one recovery matches.
func (b Budgets) Scope(unit string) (string, Recovery) {
	for objective, r := range b.Recoveries {
		if r.Unresolved() && slices.Contains(r.Scope, unit) {
			return objective, r
		}
	}
	return "", Recovery{}
}

// refSeq reads the bus position a JournalRef names; an empty reference precedes
// every recorded action.
func refSeq(ref string) int {
	seq, err := strconv.Atoi(strings.TrimPrefix(ref, "journal/"))
	if ref == "" || err != nil {
		return -1
	}
	return seq
}

// Budgets is a session's derived consumption of the bounds of PR-HAR-19..21.
// Consumption only grows: a recorded exception adds allowance beside it rather
// than erasing it (PR-HAR-22).
type Budgets struct {
	// Unplanned counts distinct work units admitted without plan coverage,
	// including work never launched and work later cancelled (PR-DAG-MUT-10).
	Unplanned int `json:"unplanned"`
	// Contests is the set of contested failures, keyed by work unit and
	// attempt, so a transport duplicate collapses instead of counting again.
	Contests map[string]bool `json:"contests"`
	// Recoveries is the recovery accounting per objective identity.
	Recoveries map[string]Recovery `json:"recoveries"`
	// Allowance holds what recorded exceptions granted, by AllowanceKey.
	Allowance map[string]int `json:"allowance"`
	// Stopped and Escalated stay distinguishable from an enabling decision.
	Stopped   int `json:"stopped"`
	Escalated int `json:"escalated"`
	// PlanVersion is the session's currently committed or revised plan
	// version, so a later revision's declared base can be checked directly
	// against it without a separate plan cache (PR-DAG-MUT-6).
	PlanVersion string `json:"plan_version,omitempty"`
	// InterlocutorHolder is the session id currently holding the interface on
	// behalf of this root; empty means the base itself holds it (IR-14..17).
	InterlocutorHolder string `json:"interlocutor_holder,omitempty"`
	// InterlocutorAgent is the specialist agent id of the current holder, so a
	// caller's own agent identity can be checked against it directly.
	InterlocutorAgent string `json:"interlocutor_agent,omitempty"`
	// InterlocutorArtifact is the optional filesystem copy path declared at switch time.
	InterlocutorArtifact string `json:"interlocutor_artifact,omitempty"`
}

// Projection is the derived execution DAG state. It is never edited in place:
// it is recomputed from the record.
type Projection struct {
	Units map[string]Unit `json:"units"`
	// Activities contains direct orchestrator and maintenance activity, outside
	// the work-unit map and its dispatch budgets.
	Activities map[string]Activity `json:"activities"`
	// Sessions holds each session's consumed budgets; the session-scoped bounds
	// are per session, never global.
	Sessions map[string]*Budgets `json:"sessions"`
}

// Budgets returns a session's consumption; a session with nothing recorded has
// consumed nothing.
func (p Projection) Budgets(session string) Budgets {
	if b := p.Sessions[session]; b != nil {
		return *b
	}
	return Budgets{}
}

// budgets returns the session's mutable accounting, creating it on first sight.
func (p Projection) budgets(session string) *Budgets {
	b := p.Sessions[session]
	if b == nil {
		b = &Budgets{Contests: map[string]bool{}, Recoveries: map[string]Recovery{}, Allowance: map[string]int{}}
		p.Sessions[session] = b
	}
	return b
}

// InFlight counts the units holding a concurrency slot: admitted pending
// launch, running, suspended, cancellation-pending and uncertain alike
// (PR-HAR-16, PR-HAR-18).
func (p Projection) InFlight() int {
	n := 0
	for _, u := range p.Units {
		if u.State == StateInFlight {
			n++
		}
	}
	return n
}

// Project derives the projection from a recorded prefix. It reads no external
// state and no clock, so the same prefix always yields the same projection.
func Project(entries []Entry) Projection {
	p := newProjection()
	for _, e := range entries {
		p.fold(e)
	}
	return p
}

func newProjection() Projection {
	return Projection{Units: map[string]Unit{}, Activities: map[string]Activity{}, Sessions: map[string]*Budgets{}}
}

// fold applies one recorded entry. Project is the fold of every entry in
// order; a caller that needs the state between entries folds them itself.
func (p *Projection) fold(e Entry) {
	if nodeKind, activity := activityKindOf(e.Kind); activity {
		p.foldActivity(e, nodeKind)
		return
	}
	b := p.budgets(e.SessionID)
	// A commitment's own units carry the version it establishes; tracked
	// per session so a later revision's declared base can be checked
	// against it (PR-DAG-MUT-6). A valid revision advances it in turn,
	// handled alongside its session accounting in budgeted below.
	if e.Kind == KindPlanned {
		b.PlanVersion = e.PlanVersion
	}
	if budgeted(b, e) {
		return
	}
	u, known := p.Units[e.WorkUnitID]
	if !accepts(u, known, e.Kind) {
		return
	}
	// The first explicit classification sticks to the real work-unit identity.
	// Missing values remain compatible with old records and never rewrite it.
	if u.NodeKind == "" && e.NodeKind == NodeKindDelegated {
		u.NodeKind = e.NodeKind
	}
	// Admission with no prior planned identity is uncovered delegation; it
	// is counted once per unit and never given back (PR-HAR-19).
	if e.Kind == KindAdmitted && !known {
		p.budgets(e.SessionID).Unplanned++
	}
	// An admission that declares its recovery scope membership consumes one
	// attempt of that recovery; a repeated request never reaches here
	// (PR-HAR-6, PR-HAR-17).
	if e.Kind == KindAdmitted && e.Objective != "" {
		if r := p.budgets(e.SessionID).Recoveries[e.Objective]; r.Attempted != nil {
			r.Attempted[e.WorkUnitID+"/"+e.AttemptID] = true
		}
	}
	u.SessionID, u.AttemptID = e.SessionID, e.AttemptID
	switch e.Kind {
	case KindAdmitted:
		// Each attempt starts unlaunched and without an outcome; the unit's
		// contract and prerequisites carry over unchanged.
		u.State, u.Flight, u.Outcome, u.Launched, u.Dispatch = StateInFlight, flightOf[e.Kind], "", false, e.Dispatch
	case KindPlanned:
		u.State = StatePlanned
		u.Committed = true
		u.Contract, u.Prerequisites = e.Contract, e.Prerequisites
	case KindWithdrawn:
		u.State = StateWithdrawn
	case KindTerminated:
		u.State, u.Flight, u.Outcome = StateSettled, "", e.Outcome
	default:
		u.State, u.Flight = StateInFlight, flightOf[e.Kind]
		u.Launched = u.Launched || e.Kind == KindLaunched
	}
	p.Units[e.WorkUnitID] = u
}

func (p *Projection) foldActivity(e Entry, nodeKind NodeKind) {
	a, exists := p.Activities[e.ActivityID]
	if isActivityStarted(e.Kind) {
		if !exists {
			p.Activities[e.ActivityID] = Activity{
				ActivityID: e.ActivityID, SessionID: e.SessionID, NodeKind: nodeKind,
				State: StateInFlight, Flight: FlightRunning,
			}
		}
		return
	}
	if !exists || a.SessionID != e.SessionID || a.NodeKind != nodeKind || a.State != StateInFlight {
		return
	}
	a.State, a.Flight, a.Outcome = StateSettled, "", e.Outcome
	p.Activities[e.ActivityID] = a
}

// budgeted folds the kinds that carry session accounting rather than a work
// state, and reports that the entry changes no unit. They stay recorded and
// visible; a denial in particular consumes nothing.
func budgeted(b *Budgets, e Entry) bool {
	switch e.Kind {
	case KindDenied:
	case KindContested:
		b.Contests[e.WorkUnitID+"/"+e.AttemptID] = true
	case KindException:
		b.Allowance[AllowanceKey(e.Bound, e.Objective)] += e.Allowance
		if e.Bound == BoundRecoveryActions && e.Objective != "" {
			// The enabling decision is also what reconciles unestablished
			// consumption: the harness never clears that uncertainty itself. It
			// re-establishes the bus position counting resumes from, and what was
			// already consumed stands.
			r := b.Recoveries[e.Objective]
			r.Unreconciled, r.Cursor = false, refSeq(e.JournalRef)
			b.Recoveries[e.Objective] = r
		}
	case KindStopped:
		b.Stopped++
	case KindEscalated:
		b.Escalated++
	case KindRecoveryDeclared:
		// A new declaration is a new budget origin, but the objective's failure
		// streak is carried: redeclaring never resets it (PR-HAR-22).
		b.Recoveries[e.Objective] = Recovery{
			Open: true, Failures: b.Recoveries[e.Objective].Failures, Unit: e.WorkUnitID,
			Result: e.Result, Point: e.Point, Scope: e.Scope,
			Actions: e.Actions, Attempts: e.Attempts,
			Attempted: map[string]bool{}, Cursor: refSeq(e.JournalRef),
		}
	case KindActions:
		r := b.Recoveries[e.Objective]
		r.Used += e.Actions
		r.Cursor = refSeq(e.JournalRef)
		b.Recoveries[e.Objective] = r
	case KindActionsUncertain:
		r := b.Recoveries[e.Objective]
		r.Unreconciled = true
		b.Recoveries[e.Objective] = r
	case KindRestored:
		r := b.Recoveries[e.Objective]
		r.Restored = true
		b.Recoveries[e.Objective] = r
	case KindRecoveryClosed:
		r := b.Recoveries[e.Objective]
		r.Open = false
		r.Backtracked = r.Backtracked || e.Outcome == OutcomeBacktracked
		if e.Outcome == OutcomeCompleted {
			r.Failures = 0
		} else {
			r.Failures++
		}
		b.Recoveries[e.Objective] = r
	case KindInterlocutorSwitched:
		b.InterlocutorHolder, b.InterlocutorAgent, b.InterlocutorArtifact = e.WorkUnitID, e.Agent, e.Artifact
	case KindInterlocutorHandoff, KindInterlocutorAborted:
		b.InterlocutorHolder, b.InterlocutorAgent, b.InterlocutorArtifact = "", "", ""
	case KindRevised:
		// A valid revision advances the tracked plan version; the per-unit
		// changes it produced are recorded as ordinary KindPlanned/KindWithdrawn
		// entries alongside it and fold through the normal unit path.
		b.PlanVersion = e.PlanVersion
	case KindInvalidRevision:
		// Recorded and visible, but touches no state: the last valid plan and
		// its tracked version stay untouched (PR-DAG-MUT-6).
	default:
		return false
	}
	return true
}

// accepts says whether the kind applies to the unit's current state. A later
// entry never rewrites an earlier one, so an act the lifecycle has passed is
// recorded without changing the projection.
func accepts(u Unit, known bool, k Kind) bool {
	switch k {
	case KindPlanned:
		// A still-planned identity may be redeclared: a valid revision's
		// changed unit reuses this same shape to carry its new contract or
		// prerequisites (PR-DAG-MUT-6). Anything past planned keeps its
		// identity frozen, same as before.
		return !known || u.State == StatePlanned
	case KindWithdrawn:
		return u.State == StatePlanned
	case KindAdmitted:
		// A settled unit is admitted again as its next attempt: a retry
		// continues the same unit instead of creating one (PR-DAG-MUT-8).
		return !known || u.State == StatePlanned || u.State == StateSettled
	default:
		return u.State == StateInFlight
	}
}
