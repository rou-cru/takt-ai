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
// projection. It owns no process state of its own: every fact it decides is
// read from and appended to the *history.History it is given. A caller that
// holds its own reason to pause admission (a maintenance barrier, a drain)
// passes that as held; this package does not know why admission might be
// held, only that it can be.
package dispatch

import (
	// Required blank import: the embed directive below depends on this package.
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
	"gopkg.in/yaml.v2"
)

//go:embed admission_policy.yaml
var admissionPolicyYAML []byte

// loadAdmissionPolicy shares one parsed policy because the embedded content never changes at runtime.
var loadAdmissionPolicy = sync.OnceValues(func() (AdmissionPolicy, error) {
	return parseAdmissionPolicy(admissionPolicyYAML)
})

// AdmissionPolicyRef names the admission rule on every record.
const AdmissionPolicyRef = "harness.admission"

// AdmissionPolicy holds the ceiling and the session budgets so every fixed
// value is canonical policy data rather than a literal in an execution path
// (PR-HAR-16, PR-HAR-19..22).
type AdmissionPolicy struct {
	Version     int `yaml:"version"`
	Concurrency struct {
		Specialists int `yaml:"specialists"`
	} `yaml:"concurrency"`
	Budgets struct {
		UnplannedUnits   int `yaml:"unplanned_units"`
		Contests         int `yaml:"contests"`
		RecoveryFailures int `yaml:"recovery_failures"`
	} `yaml:"budgets"`
	// Recovery holds the admissible ceilings of the two budgets the orchestrator
	// declares per recovery (PR-ORQ-13).
	Recovery struct {
		MaxActions  int `yaml:"max_actions"`
		MaxAttempts int `yaml:"max_attempts"`
	} `yaml:"recovery"`
}

// LoadAdmissionPolicy returns the shared embedded policy; callers must not mutate the result.
func LoadAdmissionPolicy() (AdmissionPolicy, error) { return loadAdmissionPolicy() }

// parseAdmissionPolicy rejects unknown fields and a non-positive ceiling so a
// broken policy fails early instead of silently admitting without a limit.
func parseAdmissionPolicy(data []byte) (AdmissionPolicy, error) {
	var p AdmissionPolicy
	if err := yaml.UnmarshalStrict(data, &p); err != nil {
		return AdmissionPolicy{}, fmt.Errorf("admission policy YAML: %w", err)
	}
	if p.Version != 1 {
		return AdmissionPolicy{}, fmt.Errorf("admission policy: unsupported version %d", p.Version)
	}
	if p.Recovery.MaxActions <= 0 || p.Recovery.MaxAttempts <= 0 {
		return AdmissionPolicy{}, errors.New("admission policy: every declared recovery budget needs a positive ceiling")
	}
	if p.Concurrency.Specialists <= 0 || p.Budgets.UnplannedUnits <= 0 || p.Budgets.Contests <= 0 || p.Budgets.RecoveryFailures <= 0 {
		return AdmissionPolicy{}, errors.New("admission policy: every ceiling and budget must be positive")
	}
	return p, nil
}

// CauseHeld records an admission an external barrier held, rather than any budget.
const CauseHeld = "control/admission-barrier"

// CauseRepetition denies a second delegation of a unit whose current attempt
// is still in flight: it would duplicate execution rather than retry it
// (PR-HAR-17).
const CauseRepetition = "control/unit-in-flight"

// CauseWithdrawn denies delegating a unit its plan withdrew.
const CauseWithdrawn = "control/unit-withdrawn"

// nextAttempt is the attempt an admission opens: the first for work never
// admitted, the following one when a settled unit is retried (PR-DAG-MUT-8).
func nextAttempt(u history.Unit, known bool) string {
	if !known || u.State != history.StateSettled {
		return history.FirstAttempt
	}
	n, err := strconv.Atoi(u.AttemptID)
	if err != nil {
		return history.FirstAttempt
	}
	return strconv.Itoa(n + 1)
}

// CurrentAttempt is the attempt a lifecycle fact belongs to: the one the unit's
// latest admission opened.
func CurrentAttempt(p history.Projection, event string) string {
	if a := p.Units[event].AttemptID; a != "" {
		return a
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
// stays visible. held is the caller's own reason (if any) to pause every
// admission regardless of budget, such as a maintenance cycle in flight;
// this package enforces it without knowing why it applies.
func Admit(h *history.History, p AdmissionPolicy, journalRef string, req AdmissionRequest, held bool) error {
	if req.Event == "" || req.Session == "" {
		return errors.New("dispatch: identity required")
	}
	projection := h.Project()
	unit, known := projection.Units[req.Event]
	entry := history.Entry{
		Author: history.AuthorHarness, Kind: history.KindAdmitted, SessionID: req.Session,
		WorkUnitID: req.Event, AttemptID: nextAttempt(unit, known), Dispatch: req.Dispatch, Agent: req.Agent,
		Cause: history.CauseUncaptured, JournalRef: journalRef, PolicyRef: AdmissionPolicyRef,
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
	attempts := recovery.Attempts + budgets.Allowance[history.AllowanceKey(history.BoundRecoveryAttempts, objective)]
	actions := recovery.Actions + budgets.Allowance[history.AllowanceKey(history.BoundRecoveryActions, objective)]
	var e error
	switch {
	case held:
		entry.Kind, entry.Cause = history.KindDenied, CauseHeld
		e = errors.New("dispatch: an admission barrier holds new dispatches")
	case projection.InFlight() >= p.Concurrency.Specialists:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundConcurrency
		e = fmt.Errorf("harness: concurrent specialist ceiling of %d reached; admission denied (bound %s)", p.Concurrency.Specialists, history.BoundConcurrency)
	case known && unit.State == history.StateInFlight:
		entry.Kind, entry.Cause = history.KindDenied, CauseRepetition
		e = fmt.Errorf("harness: work unit %q is already in flight; a retry starts only once its current attempt settles", req.Event)
	case known && unit.State == history.StateWithdrawn:
		entry.Kind, entry.Cause = history.KindDenied, CauseWithdrawn
		e = fmt.Errorf("harness: work unit %q was withdrawn from the plan", req.Event)
	case newUnit && budgets.Unplanned >= p.Budgets.UnplannedUnits+budgets.Allowance[history.BoundUnplanned]:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundUnplanned
		e = fmt.Errorf("harness: unplanned delegation bound of %d reached; commit a plan covering this unit and delegate it under that identity, or record the user's enabling decision on bound %s", p.Budgets.UnplannedUnits, history.BoundUnplanned)
	case objective != "" && !recovery.Open:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundRecoveryAttempts
		e = errors.New("harness: this recovery scope is abandoned; no further attempt is admitted")
	case objective != "" && recovery.Unreconciled:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundRecoveryActions
		e = errors.New("harness: action consumption of this recovery is unestablished; admission waits for reconciliation")
	case objective != "" && len(recovery.Attempted) >= attempts:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundRecoveryAttempts
		e = fmt.Errorf("harness: attempt budget of %d consumed for this recovery; the admitted attempt may still finish", attempts)
	case objective != "" && recovery.Used >= actions:
		entry.Kind, entry.Cause = history.KindDenied, history.BoundRecoveryActions
		e = fmt.Errorf("harness: action budget of %d consumed for this recovery", actions)
	}
	return errors.Join(h.Append(entry), e)
}

// Finish records the observed effective termination of a dispatched unit,
// releasing its reserved slot. Cancellation-pending or uncertain execution ends
// without a work result, so its outcome is interrupted (PR-DAG-TMP-1).
func Finish(h *history.History, journalRef, event, session string) error {
	outcome := history.OutcomeCompleted
	projection := h.Project()
	switch projection.Units[event].Flight {
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
		WorkUnitID: event, AttemptID: CurrentAttempt(projection, event), Cause: history.CauseUncaptured,
		JournalRef: journalRef, Outcome: outcome, Objective: objective,
	})
}

// StartActivity records non-work-unit orchestrator or maintenance activity.
// Activity identity stays in ActivityID, never in WorkUnitID; delegated work
// continues to use Admit and the ordinary unit lifecycle.
func StartActivity(h *history.History, session, activityID string, nodeKind history.NodeKind) error {
	if session == "" || activityID == "" {
		return errors.New("dispatch: activity start requires session and activity_id")
	}
	kind, err := activityRecordKind(nodeKind, true)
	if err != nil {
		return err
	}
	if prior, exists := h.Project().Activities[activityID]; exists {
		if prior.SessionID == session && prior.NodeKind == nodeKind && prior.State == history.StateInFlight {
			return nil
		}
		return fmt.Errorf("dispatch: activity identity %q is already recorded", activityID)
	}
	return h.Append(history.Entry{
		Author: history.AuthorOf(kind), Kind: kind, SessionID: session, ActivityID: activityID,
		NodeKind: nodeKind, Cause: history.CauseNone,
	})
}

// FinishActivity settles a prior non-work-unit activity without creating a
// work-unit identity or affecting delegated admission accounting.
func FinishActivity(h *history.History, session, activityID string, nodeKind history.NodeKind, outcome history.Outcome) error {
	if session == "" || activityID == "" {
		return errors.New("dispatch: activity finish requires session and activity_id")
	}
	prior, exists := h.Project().Activities[activityID]
	if !exists || prior.SessionID != session || prior.NodeKind != nodeKind {
		return fmt.Errorf("dispatch: activity %q is not active for this session and kind", activityID)
	}
	if prior.State == history.StateSettled {
		if prior.Outcome == outcome {
			return nil
		}
		return fmt.Errorf("dispatch: activity %q already finished with outcome %q", activityID, prior.Outcome)
	}
	kind, err := activityRecordKind(nodeKind, false)
	if err != nil {
		return err
	}
	return h.Append(history.Entry{
		Author: history.AuthorOf(kind), Kind: kind, SessionID: session, ActivityID: activityID,
		NodeKind: nodeKind, Cause: history.CauseNone, Outcome: outcome,
	})
}

func activityRecordKind(nodeKind history.NodeKind, start bool) (history.Kind, error) {
	switch {
	case nodeKind == history.NodeKindOrchestrator && start:
		return history.KindActivityStarted, nil
	case nodeKind == history.NodeKindOrchestrator:
		return history.KindActivityFinished, nil
	case nodeKind == history.NodeKindMaintenance && start:
		return history.KindMaintenanceStarted, nil
	case nodeKind == history.NodeKindMaintenance:
		return history.KindMaintenanceFinished, nil
	default:
		return "", fmt.Errorf("dispatch: node kind %q is not a non-work-unit activity", nodeKind)
	}
}

// Record appends an observed lifecycle fact or an orchestrator declaration that
// consumes no budget and needs no disposition: launch, uncertainty, suspension,
// a cancellation request, a withdrawal, a stop declaration, an escalation.
func Record(h *history.History, journalRef, event, session string, kind history.Kind) error {
	return h.Append(history.Entry{
		Author: history.AuthorOf(kind), Kind: kind, SessionID: session, WorkUnitID: event,
		AttemptID: CurrentAttempt(h.Project(), event), Cause: history.CauseUncaptured, JournalRef: journalRef,
	})
}

// Launch records execution observed running. A repeat of an observed launch
// yields its recorded disposition, and a launch left uncertain by a
// communication failure must be reconciled before any repeat that could
// duplicate execution (PR-HAR-17).
func Launch(h *history.History, journalRef, event, session string) error {
	u := h.Project().Units[event]
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
	u := h.Project().Units[event]
	// A restarted host can retain a delegation record for a unit that is no longer
	// in flight (settled, or never admitted): there is nothing left to reconcile,
	// and it must not block new dispatches. Another session's unit is not ours to
	// reconcile.
	if !running && u.State != history.StateInFlight && (u.SessionID == "" || u.SessionID == session) {
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
	if h.Project().Units[event].State != history.StatePlanned {
		return errors.New("dispatch: only still-planned work may be withdrawn")
	}
	return Record(h, journalRef, event, session, history.KindWithdrawn)
}

// PlanUnit is one unit of a plan commitment: its identity, its contract and its
// explicit prerequisite identities.
type PlanUnit struct {
	Unit          string   `json:"unit"`
	Contract      string   `json:"contract"`
	Prerequisites []string `json:"prerequisites"`
}

// Commit records a plan commitment: the units it covers become planned, so a
// later admission of one of them is covered delegation (PR-DAG-MUT-1,
// PR-HAR-19).
func Commit(h *history.History, journalRef, session, version string, units []PlanUnit) error {
	if session == "" || version == "" || len(units) == 0 {
		return errors.New("dispatch: a commitment records an identified baseline version and its work")
	}
	if e := validPlan(h.Project(), units); e != nil {
		return e
	}
	for _, u := range units {
		e := h.Append(history.Entry{
			Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: session,
			WorkUnitID: u.Unit, AttemptID: history.FirstAttempt, Cause: history.CauseNone,
			JournalRef: journalRef, PlanVersion: version, Contract: u.Contract, Prerequisites: u.Prerequisites,
		})
		if e != nil {
			return e
		}
	}
	return nil
}

// Declare is the orchestrator's one plan declaration: the first one commits
// the baseline; once a plan stands, a declaration revises it and must name
// the version it revises, so a stale or silent rewrite of the plan is refused
// instead of recorded as a fresh commitment (PR-DAG-MUT-6).
func Declare(h *history.History, journalRef, session, version, baseVersion string, units []PlanUnit, withdrawals []string) error {
	standing := h.Project().Budgets(session).PlanVersion
	if standing == "" {
		if baseVersion != "" || len(withdrawals) > 0 {
			return errors.New("dispatch: no plan stands in this session yet; commit one before revising it")
		}
		return Commit(h, journalRef, session, version, units)
	}
	if baseVersion == "" {
		return fmt.Errorf("dispatch: plan version %s already stands; name it as the base version to revise it", standing)
	}
	return Revise(h, journalRef, session, baseVersion, version, units, withdrawals)
}

// validPlan rejects the declarations PR-DAG-MUT-6 calls invalid: duplicate or
// unknown identities and cycles. An invalid commitment covers nothing.
func validPlan(p history.Projection, units []PlanUnit) error {
	pending, dependents, err := planGraph(p, units)
	if err != nil {
		return err
	}
	if !acyclic(pending, dependents) {
		return errors.New("dispatch: committed prerequisites form a cycle")
	}
	return nil
}

// planGraph indexes the committed units, rejecting missing or duplicate
// identities and unknown prerequisites. pending counts each unit's in-plan
// prerequisites; dependents maps a unit to the units waiting on it.
func planGraph(p history.Projection, units []PlanUnit) (map[string]int, map[string][]string, error) {
	pending, err := indexPendingUnits(units)
	if err != nil {
		return nil, nil, err
	}
	dependents, err := linkPrerequisites(p, units, pending)
	if err != nil {
		return nil, nil, err
	}
	return pending, dependents, nil
}

// indexPendingUnits validates that every committed unit has an identity and
// a contract and appears once, seeding pending's in-plan-prerequisite count
// at zero for each.
func indexPendingUnits(units []PlanUnit) (map[string]int, error) {
	pending := make(map[string]int, len(units))
	for _, u := range units {
		if u.Unit == "" || u.Contract == "" {
			return nil, errors.New("dispatch: every committed unit needs an identity and a contract")
		}
		if _, dup := pending[u.Unit]; dup {
			return nil, fmt.Errorf("dispatch: duplicate identity in commitment: %s", u.Unit)
		}
		pending[u.Unit] = 0
	}
	return pending, nil
}

// linkPrerequisites rejects a prerequisite that names neither an in-plan nor
// an already-executed unit, and otherwise counts it against pending and
// records the in-plan reverse edge in dependents.
func linkPrerequisites(p history.Projection, units []PlanUnit, pending map[string]int) (map[string][]string, error) {
	dependents := map[string][]string{}
	for _, u := range units {
		for _, need := range u.Prerequisites {
			if _, inPlan := pending[need]; !inPlan {
				if _, executed := p.Units[need]; !executed {
					return nil, fmt.Errorf("dispatch: unknown prerequisite identity: %s", need)
				}
				continue
			}
			pending[u.Unit]++
			dependents[need] = append(dependents[need], u.Unit)
		}
	}
	return dependents, nil
}

// acyclic reports whether every unit settles once its prerequisites do
// (Kahn's algorithm). It consumes pending.
func acyclic(pending map[string]int, dependents map[string][]string) bool {
	ready := make([]string, 0, len(pending))
	for unit, blocked := range pending {
		if blocked == 0 {
			ready = append(ready, unit)
		}
	}
	settled := 0
	for len(ready) > 0 {
		unit := ready[len(ready)-1]
		ready = ready[:len(ready)-1]
		settled++
		for _, next := range dependents[unit] {
			pending[next]--
			if pending[next] == 0 {
				ready = append(ready, next)
			}
		}
	}
	return settled == len(pending)
}

// Revise records a plan revision against its expected prior valid version
// (PR-DAG-MUT-6). A valid revision is classified strategic or tactical by
// comparing the old and new plans (PR-DAG-MUT-4) and applies as a whole; an
// invalid one is rejected with its reason recorded, touching no per-unit
// state, so the last valid plan is untouched by construction.
func Revise(h *history.History, journalRef, session, baseVersion, newVersion string, adds []PlanUnit, withdrawals []string) error {
	if session == "" || baseVersion == "" || newVersion == "" {
		return errors.New("dispatch: a revision identifies its session, expected base version and new version")
	}
	projection := h.Project()
	if projection.Budgets(session).PlanVersion != baseVersion {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion,
			"dispatch: revision declares a stale base version")
	}
	current := currentPlan(projection)
	resulting := reviseUnits(current, adds, withdrawals)
	if e := validPlan(projection, resulting); e != nil {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion, e.Error())
	}
	if e := checkWithdrawalsPlanned(projection, withdrawals); e != nil {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion, e.Error())
	}
	if e := checkAdmittedUnchanged(projection, adds); e != nil {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion, e.Error())
	}
	classification := classify(current, resulting)
	if e := h.Append(history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindRevised, SessionID: session,
		WorkUnitID: session, AttemptID: history.FirstAttempt, Cause: history.CauseNone,
		JournalRef: journalRef, BaseVersion: baseVersion, PlanVersion: newVersion, Classification: classification,
	}); e != nil {
		return e
	}
	for _, u := range adds {
		e := h.Append(history.Entry{
			Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: session,
			WorkUnitID: u.Unit, AttemptID: history.FirstAttempt, Cause: history.CauseNone,
			JournalRef: journalRef, PlanVersion: newVersion, Contract: u.Contract, Prerequisites: u.Prerequisites,
		})
		if e != nil {
			return e
		}
	}
	for _, w := range withdrawals {
		e := h.Append(history.Entry{
			Author: history.AuthorOrchestrator, Kind: history.KindWithdrawn, SessionID: session,
			WorkUnitID: w, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured, JournalRef: journalRef,
		})
		if e != nil {
			return e
		}
	}
	return nil
}

// invalidRevision records a rejected revision and its reason as a single
// declaration, never a per-unit entry, so the last valid plan and its
// tracked version are untouched by construction (PR-DAG-MUT-6).
func invalidRevision(h *history.History, journalRef, session, baseVersion, newVersion, reason string) error {
	entry := history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindInvalidRevision, SessionID: session,
		WorkUnitID: session, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured,
		JournalRef: journalRef, BaseVersion: baseVersion, PlanVersion: newVersion, Reason: reason,
	}
	return errors.Join(h.Append(entry), errors.New(reason))
}

// currentPlan reconstructs the plan the last valid commitment or revision
// left standing: every unit a commitment declared that is still planned or
// admitted, in deterministic order.
func currentPlan(p history.Projection) []PlanUnit {
	ids := make([]string, 0, len(p.Units))
	for id, u := range p.Units {
		if u.Contract != "" && (u.State == history.StatePlanned || u.State == history.StateInFlight) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	units := make([]PlanUnit, len(ids))
	for i, id := range ids {
		u := p.Units[id]
		units[i] = PlanUnit{Unit: id, Contract: u.Contract, Prerequisites: u.Prerequisites}
	}
	return units
}

// reviseUnits applies a revision's declared changes to the current plan as a
// whole: withdrawals remove, adds insert or replace (PR-DAG-MUT-6).
func reviseUnits(current, adds []PlanUnit, withdrawals []string) []PlanUnit {
	withdrawn := make(map[string]bool, len(withdrawals))
	for _, w := range withdrawals {
		withdrawn[w] = true
	}
	byID := make(map[string]PlanUnit, len(current)+len(adds))
	order := make([]string, 0, len(current)+len(adds))
	for _, u := range current {
		if withdrawn[u.Unit] {
			continue
		}
		byID[u.Unit] = u
		order = append(order, u.Unit)
	}
	for _, u := range adds {
		if _, exists := byID[u.Unit]; !exists {
			order = append(order, u.Unit)
		}
		byID[u.Unit] = u
	}
	result := make([]PlanUnit, len(order))
	for i, id := range order {
		result[i] = byID[id]
	}
	return result
}

// checkWithdrawalsPlanned rejects withdrawing work that is no longer planned:
// a revision may only revoke still-planned work, same as Withdraw
// (PR-DAG-MUT-6, PR-DAG-TMP-3).
func checkWithdrawalsPlanned(p history.Projection, withdrawals []string) error {
	for _, w := range withdrawals {
		if p.Units[w].State != history.StatePlanned {
			return fmt.Errorf("dispatch: %s is not planned; only still-planned work may be withdrawn", w)
		}
	}
	return nil
}

// checkAdmittedUnchanged rejects a revision that would alter the contract or
// prerequisites of a unit no longer planned: a genuinely unchanged
// redeclaration of already-admitted work is tolerated, a change is not
// (PR-DAG-MUT-6).
func checkAdmittedUnchanged(p history.Projection, adds []PlanUnit) error {
	for _, u := range adds {
		existing, known := p.Units[u.Unit]
		if !known || existing.State == history.StatePlanned {
			continue
		}
		if existing.Contract != u.Contract || !slices.Equal(existing.Prerequisites, u.Prerequisites) {
			return fmt.Errorf("dispatch: %s is no longer planned; its contract and prerequisites cannot change", u.Unit)
		}
	}
	return nil
}

// classify derives strategic or tactical by diffing retained units' declared
// prerequisites between the old and new plan; nothing else (grouping, a
// focal unit, a rationale) changes the label (PR-DAG-MUT-4).
func classify(current, resulting []PlanUnit) string {
	old := make(map[string][]string, len(current))
	for _, u := range current {
		old[u.Unit] = u.Prerequisites
	}
	for _, u := range resulting {
		if prev, retained := old[u.Unit]; retained && !slices.Equal(prev, u.Prerequisites) {
			return history.ClassificationStrategic
		}
	}
	return history.ClassificationTactical
}

// Contest admits at most the policy's distinct contests per session, upheld
// ones included. A transport duplicate of the same targeted failure returns its
// recorded disposition without consuming again (PR-HAR-17, PR-HAR-20).
func Contest(h *history.History, p AdmissionPolicy, journalRef, event, session, attempt string) error {
	if event == "" || session == "" {
		return errors.New("dispatch: contest identity required")
	}
	budgets := h.Project().Budgets(session)
	if budgets.Contests[event+"/"+attempt] {
		return nil
	}
	entry := history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindContested, SessionID: session,
		WorkUnitID: event, AttemptID: attempt, Cause: history.CauseUncaptured,
		JournalRef: journalRef, PolicyRef: AdmissionPolicyRef,
	}
	var e error
	if len(budgets.Contests) >= p.Budgets.Contests+budgets.Allowance[history.BoundContests] {
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, history.BoundContests
		e = fmt.Errorf("harness: contest allowance of %d consumed; a scoped user exception is required", p.Budgets.Contests)
	}
	return errors.Join(h.Append(entry), e)
}

// RecoveryDeclaration is what PR-ORQ-13 requires recorded before uncertain work
// begins: the binary expected result, both budgets, the objective identity, the
// prior recoverable point, and the explicit scope.
type RecoveryDeclaration struct {
	Objective string   `json:"objective"`
	Result    string   `json:"result"`
	Point     string   `json:"point"`
	Scope     []string `json:"scope"`
	Actions   int      `json:"actions"`
	Attempts  int      `json:"attempts"`
}

// causeIncomplete records a recovery declaration that did not declare everything
// PR-ORQ-13 requires: recorded as missing, and no proof of bounded recovery.
const causeIncomplete = "declaration/recovery-incomplete"

// DeclareRecovery admits bounded recovery of an objective. Consecutive failed
// recoveries close autonomous recovery for it until an enabling user decision
// is recorded; the objective identity carries that history (PR-HAR-21,
// PR-HAR-22).
func DeclareRecovery(h *history.History, p AdmissionPolicy, journalRef, event, session string, d RecoveryDeclaration) error {
	budgets := h.Project().Budgets(session)
	if budgets.Recoveries[d.Objective].Open {
		return nil
	}
	entry := history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindRecoveryDeclared, SessionID: session,
		WorkUnitID: event, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured,
		JournalRef: journalRef, PolicyRef: AdmissionPolicyRef, Objective: d.Objective,
		Result: d.Result, Point: d.Point, Scope: d.Scope, Actions: d.Actions, Attempts: d.Attempts,
	}
	var e error
	deny := func(cause string, reason error) {
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, cause
		e = reason
	}
	allowed := p.Budgets.RecoveryFailures + budgets.Allowance[history.AllowanceKey(history.BoundRecovery, d.Objective)]
	switch {
	case d.Objective == "" || d.Result == "" || d.Point == "" || len(d.Scope) == 0 || d.Actions <= 0 || d.Attempts <= 0:
		deny(causeIncomplete, errors.New("harness: a recovery declares a binary result, both budgets, an objective, a recoverable point and its scope"))
	case d.Actions > p.Recovery.MaxActions || d.Attempts > p.Recovery.MaxAttempts:
		deny(history.BoundRecoveryActions, fmt.Errorf("harness: declared budgets exceed the admissible ceiling of %d actions and %d attempts", p.Recovery.MaxActions, p.Recovery.MaxAttempts))
	case overlaps(budgets, d.Scope):
		deny(history.BoundRecoveryAttempts, errors.New("harness: the declared scope belongs to an unresolved recovery"))
	case budgets.Recoveries[d.Objective].Failures >= allowed:
		deny(history.BoundRecovery, fmt.Errorf("harness: %d consecutive failed recoveries of this objective require escalation", p.Budgets.RecoveryFailures))
	}
	return errors.Join(h.Append(entry), e)
}

// overlaps reports a declared scope whose work already belongs to an unresolved
// recovery, which would make scope membership ambiguous.
func overlaps(budgets history.Budgets, scope []string) bool {
	return slices.ContainsFunc(scope, func(unit string) bool {
		objective, _ := budgets.Scope(unit)
		return objective != ""
	})
}

// CloseRecovery closes a declared recovery, recording whether its result was
// demonstrated and linking the evidence. Only a demonstrated recovery breaks the
// objective's streak, and a claimed result without evidence is not one.
func CloseRecovery(h *history.History, journalRef, event, session, objective, evidence string, demonstrated bool) error {
	if !h.Project().Budgets(session).Recoveries[objective].Open {
		return errors.New("dispatch: no declared recovery of that objective is open")
	}
	if demonstrated && evidence == "" {
		return errors.New("harness: a demonstrated recovery result must link its evidence")
	}
	outcome := history.OutcomeFailed
	if demonstrated {
		outcome = history.OutcomeCompleted
	}
	return h.Append(history.Entry{
		Author: history.AuthorHarness, Kind: history.KindRecoveryClosed, SessionID: session,
		WorkUnitID: event, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured,
		JournalRef: journalRef, PolicyRef: AdmissionPolicyRef, Objective: objective,
		Outcome: outcome, Evidence: evidence,
	})
}

// Account attributes recorded bus actions to the recovery scope that declared
// them and forces backtracking at exhaustion of either budget (PR-OBS-PRG-2,
// PR-HAR-6).
func Account(h *history.History, entries []vfs.JournalEntry) error {
	head := -1
	if len(entries) > 0 {
		head = entries[len(entries)-1].Seq
	}
	projection := h.Project()
	for _, session := range slices.Sorted(maps.Keys(projection.Sessions)) {
		budgets := projection.Budgets(session)
		for _, objective := range slices.Sorted(maps.Keys(budgets.Recoveries)) {
			recovery := budgets.Recoveries[objective]
			if !recovery.Open || recovery.Unreconciled {
				continue
			}
			scope := recoveryScope{Session: session, Objective: objective, Recovery: recovery, Budgets: budgets, Projection: projection}
			if e := accountRecovery(h, entries, scope, head); e != nil {
				return e
			}
		}
	}
	return nil
}

// recoveryScope is one open, reconciled recovery scope being accounted: the
// session and objective it belongs to, its current recovery and budget
// state, and the session projection its exhaustion check reads.
type recoveryScope struct {
	Session    string
	Objective  string
	Recovery   history.Recovery
	Budgets    history.Budgets
	Projection history.Projection
}

// accountRecovery records the consumption of one open, reconciled recovery
// scope observed on the bus up to head, then forces backtracking if that
// exhausts its budget.
func accountRecovery(h *history.History, entries []vfs.JournalEntry, scope recoveryScope, head int) error {
	if head < scope.Recovery.Cursor {
		// The bus no longer accounts for consumption already observed.
		return recordActions(h, scope.Session, scope.Objective, scope.Recovery, history.KindActionsUncertain, 0, head)
	}
	if n := scopeActions(entries, scope.Recovery); n > 0 {
		scope.Recovery.Used += n
		if e := recordActions(h, scope.Session, scope.Objective, scope.Recovery, history.KindActions, n, head); e != nil {
			return e
		}
	}
	return forceBacktrackIfExhausted(h, scope.Session, scope.Objective, scope.Recovery, scope.Budgets, scope.Projection, head)
}

// scopeActions counts the tool executions recorded past the established cursor
// whose work unit the recovery scope declares, whichever attempt performed them.
func scopeActions(entries []vfs.JournalEntry, recovery history.Recovery) int {
	calls := map[string]bool{}
	for _, entry := range entries {
		if entry.Seq > recovery.Cursor && entry.CallID != "" && slices.Contains(recovery.Scope, entry.WorkUnitID) {
			calls[entry.SessionID+"/"+entry.CallID] = true
		}
	}
	return len(calls)
}

// forceBacktrackIfExhausted abandons the recovery scope when the action budget is
// spent, or when no attempt allowance remains and every admitted attempt has
// finished without the declared result. It needs no renewed orchestrator
// approval (PR-DAG-TMP-5).
func forceBacktrackIfExhausted(h *history.History, session, objective string, recovery history.Recovery, budgets history.Budgets, projection history.Projection, head int) error {
	actions := recovery.Actions + budgets.Allowance[history.AllowanceKey(history.BoundRecoveryActions, objective)]
	attempts := recovery.Attempts + budgets.Allowance[history.AllowanceKey(history.BoundRecoveryAttempts, objective)]
	inFlight := slices.ContainsFunc(recovery.Scope, func(unit string) bool {
		return projection.Units[unit].State == history.StateInFlight
	})
	cause := history.BoundRecoveryActions
	switch {
	case recovery.Used >= actions:
	case len(recovery.Attempted) >= attempts && !inFlight:
		cause = history.BoundRecoveryAttempts
	default:
		// The last admitted attempt may still finish and present evidence while
		// action allowance remains.
		return nil
	}
	// At exhaustion missing evidence is not success: the scope is abandoned with
	// its result undemonstrated, and no evidence is linked.
	return h.Append(history.Entry{
		Author: history.AuthorHarness, Kind: history.KindRecoveryClosed, SessionID: session,
		WorkUnitID: recovery.Unit, AttemptID: history.FirstAttempt, Cause: cause,
		JournalRef: journalPosition(head), PolicyRef: AdmissionPolicyRef, Objective: objective,
		Outcome: history.OutcomeBacktracked, Point: recovery.Point, Scope: recovery.Scope,
	})
}

// recordActions appends one action-accounting observation. Its JournalRef is the bus
// position through which consumption is established, so replay reads these
// records and never the consumer's current state.
func recordActions(h *history.History, session, objective string, recovery history.Recovery, kind history.Kind, actions, head int) error {
	return h.Append(history.Entry{
		Author: history.AuthorHarness, Kind: kind, SessionID: session,
		WorkUnitID: recovery.Unit, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured,
		JournalRef: journalPosition(head), PolicyRef: AdmissionPolicyRef,
		Objective: objective, Actions: actions,
	})
}

// journalPosition names a bus position the way vfs.JournalEntry.Ref does; an
// empty reference precedes every recorded action.
func journalPosition(seq int) string {
	if seq < 0 {
		return ""
	}
	return "journal/" + strconv.Itoa(seq)
}

// Restore confirms restoration of an abandoned recovery scope's virtual state:
// the rollback records the resulting state on the Action Journal and leaves
// unrelated progress untouched (PR-VFS-STG-6, PR-HAR-18).
func Restore(h *history.History, fs *vfs.FS, journalRef, session, objective string, key vfs.AgentID) error {
	projection := h.Project()
	recovery := projection.Budgets(session).Recoveries[objective]
	switch {
	case !recovery.Backtracked || recovery.Restored:
		return errors.New("dispatch: no abandoned recovery of that objective awaits restoration")
	case slices.ContainsFunc(recovery.Scope, func(unit string) bool {
		return projection.Units[unit].State == history.StateInFlight
	}):
		return errors.New("harness: restoration waits until the affected executions can no longer act")
	}
	identity, bound := fs.BindingIdentity(key)
	if !bound || !slices.Contains(recovery.Scope, identity.WorkUnitID) {
		return errors.New("harness: the restored delta must belong to the declared recovery scope")
	}
	result, e := fs.Apply(vfs.Operation{
		Key: key, CallID: "recovery-restore/" + objective, Action: vfs.OpRollback,
		ExpectedRevision: fs.InspectDelta(key).Revision,
	})
	if e != nil {
		// Unconfirmed restoration leaves the failure visible and the scope
		// unresolved; abandonment never claims restoration.
		return e
	}
	return h.Append(history.Entry{
		Author: history.AuthorHarness, Kind: history.KindRestored, SessionID: session,
		WorkUnitID: recovery.Unit, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured,
		JournalRef: journalRef, PolicyRef: AdmissionPolicyRef, Objective: objective,
		Point: recovery.Point, Scope: recovery.Scope,
		Evidence: fmt.Sprintf("revision/%d", result.Revision),
	})
}

// Except records a scoped user exception before it enables any further work. It
// names the bound, the scope, and a finite additional allowance; consumption
// already recorded stands (PR-HAR-22).
func Except(h *history.History, journalRef, event, session, bound, objective string, allowance int) error {
	return h.Append(history.Entry{
		Author: history.AuthorHarness, Kind: history.KindException, SessionID: session,
		WorkUnitID: event, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured,
		JournalRef: journalRef, PolicyRef: AdmissionPolicyRef,
		Bound: bound, Objective: objective, Allowance: allowance,
	})
}
