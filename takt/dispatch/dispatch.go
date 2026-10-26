// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package dispatch is the generic admission and lifecycle protocol for any
// unit of work an orchestrator dispatches, backed by the execution history
// projection. It owns no process state: every fact it decides is read from and
// appended to the *history.History it is given. A caller with its own reason to
// pause admission (a maintenance barrier, a drain) passes it as held; this
// package need not know why.
package dispatch

import (
	"cmp"
	"errors"
	"fmt"

	"github.com/rou-cru/takt-ai/takt/history"
)

// CauseRepetition denies a second delegation of a unit whose current attempt
// is still in flight: it would duplicate execution rather than retry it
// (PR-HAR-17).
const CauseRepetition = "control/unit-in-flight"

// CauseWithdrawn denies delegating a unit its plan withdrew.
const CauseWithdrawn = "control/unit-withdrawn"

// CurrentAttempt is the attempt a lifecycle fact belongs to: the one session's
// unit's latest admission opened.
func CurrentAttempt(p history.Projection, session, event string) string {
	if u, _ := p.Unit(session, event); u.AttemptID != "" {
		return u.AttemptID
	}
	return history.FirstAttempt
}

// AdmissionRequest identifies the unit an admission decision is made for: the
// work unit and session identities, the agent it would delegate to, and the
// dispatch identity to record against it.
type AdmissionRequest struct {
	Event    string
	Session  string
	Agent    string
	Dispatch string
}

// Admit is called before the host task tool. It reserves one concurrency slot
// indivisibly against the projection (PR-HAR-16); a refusal is recorded and
// stays visible.
func Admit(h *history.History, p AdmissionPolicy, journalRef string, req AdmissionRequest) error {
	if req.Event == "" || req.Session == "" {
		return errors.New("dispatch: identity required")
	}
	entry, e := decide(h.Project(), p, req)
	entry.JournalRef = journalRef
	return errors.Join(h.Append(entry), e)
}

// Admissible reports the refusal Admit would record for req, without recording
// anything: a caller that must undo other work before admitting (a maintenance
// cycle in flight) does so only for an admission that will go through.
func Admissible(h *history.History, p AdmissionPolicy, req AdmissionRequest) error {
	if req.Event == "" || req.Session == "" {
		return errors.New("dispatch: identity required")
	}
	_, e := decide(h.Project(), p, req)
	return e
}

// decide is the admission decision over one projection: the entry to record
// and, for a denial, why.
func decide(projection history.Projection, p AdmissionPolicy, req AdmissionRequest) (history.Entry, error) {
	unit, known := projection.Unit(req.Session, req.Event)
	entry := history.Entry{
		Author: history.AuthorHarness, Kind: history.KindAdmitted, SessionID: req.Session,
		WorkUnitID: req.Event, NodeKind: history.NodeKindDelegated, AttemptID: projection.NextAttempt(req.Session, req.Event), Dispatch: req.Dispatch, Agent: req.Agent,
		Cause: history.CauseUncaptured, PolicyRef: AdmissionPolicyRef,
	}
	budgets := projection.Budgets(req.Session)
	// Only a unit never seen before is new work: a planned unit is covered by
	// its commitment, and a settled one is retried under its own identity, so
	// neither adds to the uncovered count (PR-HAR-19, PR-DAG-MUT-10).
	newUnit := !known
	// Scope membership is declared, so an admission inside a recovery scope is an
	// attempt of that recovery and records which one it belongs to.
	objective, recovery := budgets.Scope(req.Event)
	entry.Objective = objective
	ceiling := p.Concurrency.Specialists + budgets.Allowance[history.BoundConcurrency]
	attempts := recovery.Attempts + budgets.Allowance[history.AllowanceKey(history.BoundRecoveryAttempts, objective)]
	actions := recovery.Actions + budgets.Allowance[history.AllowanceKey(history.BoundRecoveryActions, objective)]
	qaVersion, qaRound := budgets.QARoundFor(req.Event)
	qaCeiling := p.Recovery.MaxAttempts + budgets.Allowance[history.AllowanceKey(history.BoundQARounds, qaRound.Objective)]
	var e error
	switch {
	case projection.InFlight() >= ceiling:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundConcurrency
		e = fmt.Errorf("harness: concurrent specialist ceiling of %d reached; admission denied (bound %s); delegate again once a delegation returns; a delegation that will never return is ended, with the user's agreement, by dispatch_abandon on its dispatch; or ask the user to raise the ceiling with dispatch_exception on %s", ceiling, history.BoundConcurrency, history.BoundConcurrency)
	case known && unit.State == history.StateInFlight:
		entry.Kind, entry.Cause = history.KindDenied, CauseRepetition
		e = fmt.Errorf("harness: work unit %q is already in flight under dispatch %s; a retry starts once that delegation returns; if it will never return, ask the user and call dispatch_abandon with dispatch %s", req.Event, unit.Dispatch, unit.Dispatch)
	case known && unit.State == history.StateWithdrawn:
		entry.Kind, entry.Cause = history.KindDenied, CauseWithdrawn
		e = fmt.Errorf("harness: work unit %q was withdrawn from the plan; delegate the work under a new unit name", req.Event)
	case newUnit && budgets.Unplanned >= p.Budgets.UnplannedUnits+budgets.Allowance[history.BoundUnplanned]:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundUnplanned
		e = fmt.Errorf("harness: unplanned delegation bound of %d reached; commit a plan covering this unit with dispatch_commit and delegate it under that identity, or propose dispatch_exception (the harness asks the user) on bound %s", p.Budgets.UnplannedUnits, history.BoundUnplanned)
	case qaVersion != "" && !qaRound.Started && budgets.StrengtheningUsed(qaRound.Objective) >= qaCeiling:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundQARounds
		e = fmt.Errorf("harness: strengthening round budget of %d consumed for objective %q; Close/Report remain available: close with residual risk, or propose dispatch_exception (the harness asks the user) on %s with this objective and a finite allowance", qaCeiling, qaRound.Objective, history.BoundQARounds)
	case objective != "" && !recovery.Open:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundRecoveryAttempts
		e = fmt.Errorf("harness: recovery %q is abandoned, its %s spent; %s", objective, recovery.Cause, scopeExit(objective, recovery.Cause))
	case objective != "" && recovery.Unreconciled:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundRecoveryActions
		e = fmt.Errorf("harness: action consumption of this recovery is unestablished; admission waits for reconciliation, which only a dispatch_exception the user agrees to on %s for objective %q provides", history.BoundRecoveryActions, objective)
	case objective != "" && len(recovery.Attempted) >= attempts:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundRecoveryAttempts
		e = fmt.Errorf("harness: attempt budget of %d consumed for this recovery; the admitted attempt may still finish; for more attempts propose dispatch_exception (the harness asks the user) on %s for objective %q", attempts, history.BoundRecoveryAttempts, objective)
	case objective != "" && recovery.Used >= actions:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundRecoveryActions
		e = fmt.Errorf("harness: action budget of %d consumed for this recovery; for more actions propose dispatch_exception (the harness asks the user) on %s for objective %q", actions, history.BoundRecoveryActions, objective)
	}
	return entry, e
}

// Finish records the observed effective termination of a dispatched unit,
// releasing its reserved slot. Cancellation-pending or uncertain execution ends
// without a work result, so its outcome is interrupted (PR-DAG-TMP-1).
func Finish(h *history.History, journalRef, event, session string) error {
	outcome := history.OutcomeCompleted
	projection := h.Project()
	unit, _ := projection.Unit(session, event)
	switch unit.Flight {
	case history.FlightCancelling, history.FlightUncertain:
		outcome = history.OutcomeInterrupted
	}
	// Termination of work in an abandoned recovery scope is the second fact of
	// forced backtracking: the outcome is backtracked, and restoration is still
	// a later fact of its own (PR-DAG-TMP-5).
	objective, recovery := projection.Budgets(session).Scope(event)
	if recovery.Backtracked {
		outcome = history.OutcomeBacktracked
	}
	return h.Append(history.Entry{
		Author: history.AuthorHarness, Kind: history.KindTerminated, SessionID: session,
		WorkUnitID: event, AttemptID: CurrentAttempt(projection, session, event), Cause: history.CauseUncaptured,
		JournalRef: journalRef, Outcome: outcome, Objective: objective,
	})
}

// Record appends an observed lifecycle fact or an orchestrator declaration that
// consumes no budget and needs no disposition: launch, uncertainty, suspension,
// a cancellation request, a withdrawal, a stop declaration, an escalation.
func Record(h *history.History, journalRef, event, session string, kind history.Kind) error {
	return h.Append(history.Entry{
		Author: history.AuthorOf(kind), Kind: kind, SessionID: session, WorkUnitID: event,
		AttemptID: CurrentAttempt(h.Project(), session, event), Cause: history.CauseUncaptured, JournalRef: journalRef,
	})
}

// Launch records execution observed running. A repeat of an observed launch
// yields its recorded disposition, and a launch left uncertain by a
// communication failure must be reconciled before any repeat that could
// duplicate execution (PR-HAR-17).
func Launch(h *history.History, journalRef, event, session string) error {
	u, _ := h.Project().Unit(session, event)
	switch {
	case u.State != history.StateInFlight:
		return errors.New("dispatch: launch requires an admitted unit in flight")
	case u.Launched:
		return nil
	case u.Flight == history.FlightUncertain:
		return errors.New("harness: uncertain launch must be reconciled before it may be repeated")
	}
	return Record(h, journalRef, event, session, history.KindLaunched)
}

// Reconcile establishes what an uncertain launch did: execution observed
// running, or non-start, which is the effective termination that finally
// releases the reservation. Uncertainty alone never releases it (PR-HAR-18).
func Reconcile(h *history.History, journalRef, event, session string, running bool) error {
	u, _ := h.Project().Unit(session, event)
	// A restarted host can retain a delegation record for a unit that is no longer
	// in flight (settled, or never admitted): there is nothing left to reconcile,
	// and it must not block new dispatches. Only session's own unit is looked at:
	// another root's unit of the same name is never touched.
	if !running && u.State != history.StateInFlight {
		return nil
	}
	if u.Flight != history.FlightUncertain {
		return errors.New("dispatch: nothing uncertain to reconcile")
	}
	if running {
		return Record(h, journalRef, event, session, history.KindLaunched)
	}
	return Finish(h, journalRef, event, session)
}

// Withdraw revokes still-planned work. Admitted work's contract and
// prerequisites are frozen, so it can no longer be withdrawn (PR-DAG-TMP-3).
func Withdraw(h *history.History, journalRef, event, session string) error {
	if u, _ := h.Project().Unit(session, event); u.State != history.StatePlanned {
		return errors.New("dispatch: only still-planned work may be withdrawn")
	}
	return Record(h, journalRef, event, session, history.KindWithdrawn)
}

// Except records a scoped user exception before it enables further work: the
// bound, the scope and a finite additional allowance (consumption already
// recorded stands, PR-HAR-22). An exception that could enable nothing is
// refused with the one that would. event only labels the record, defaulting to
// the objective or the bound when there is none.
func Except(h *history.History, journalRef, event, session, bound, objective string, allowance int) error {
	if e := checkException(h.Project().Budgets(session), bound, objective); e != nil {
		return e
	}
	if event == "" {
		event = cmp.Or(objective, bound)
	}
	return h.Append(history.Entry{
		Author: history.AuthorHarness, Kind: history.KindException, SessionID: session,
		WorkUnitID: event, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured,
		JournalRef: journalRef, PolicyRef: AdmissionPolicyRef,
		Bound: bound, Objective: objective, Allowance: allowance,
	})
}

// checkException refuses an exception that would be recorded and read by
// nothing: a recovery bound without its objective, or the wrong recovery bound
// for the one that abandoned a scope.
func checkException(budgets history.Budgets, bound, objective string) error {
	switch bound {
	case history.BoundRecovery, history.BoundRecoveryActions, history.BoundRecoveryAttempts, history.BoundQARounds:
		if objective == "" {
			return fmt.Errorf("dispatch: an exception on %s names the objective it applies to", bound)
		}
	}
	recovery := budgets.Recoveries[objective]
	spentLimit := bound == history.BoundRecoveryActions || bound == history.BoundRecoveryAttempts
	if spentLimit && recovery.Backtracked && !recovery.Restored && bound != recovery.Cause {
		return fmt.Errorf("dispatch: recovery %q was abandoned because its %s was spent; the exception that lifts it is on %s", objective, recovery.Cause, recovery.Cause)
	}
	return nil
}
