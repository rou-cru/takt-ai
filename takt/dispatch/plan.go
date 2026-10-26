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

package dispatch

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/history"
)

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
func Commit(h *history.History, journalRef, session, version string, units []PlanUnit, qaRound ...*history.QARound) error {
	if session == "" || version == "" || len(units) == 0 {
		return errors.New("dispatch: a commitment records an identified baseline version and its work")
	}
	if e := validPlan(h.Project(), session, units); e != nil {
		return e
	}
	entries, err := qaRoundEntries(h.Project(), journalRef, session, version, units, qaRound)
	if err != nil {
		return err
	}
	for _, u := range units {
		entries = append(entries, plannedEntry(journalRef, session, version, u))
	}
	return h.AppendBatch(entries)
}

// Declare is the orchestrator's one plan declaration: the first one commits
// the baseline; once a plan stands, a declaration revises it and must name
// the version it revises, so a stale or silent rewrite of the plan is refused
// instead of recorded as a fresh commitment (PR-DAG-MUT-6).
func Declare(h *history.History, journalRef, session, version, baseVersion string, units []PlanUnit, withdrawals []string, qaRound ...*history.QARound) error {
	standing := h.Project().Budgets(session).PlanVersion
	if standing == "" {
		if baseVersion != "" || len(withdrawals) > 0 {
			return errors.New("dispatch: no plan stands in this session yet; commit one before revising it")
		}
		return Commit(h, journalRef, session, version, units, qaRound...)
	}
	if baseVersion == "" {
		return fmt.Errorf("dispatch: plan version %s already stands; name it as the base version to revise it", standing)
	}
	return Revise(h, journalRef, session, baseVersion, version, units, withdrawals, qaRound...)
}

// validPlan rejects the declarations PR-DAG-MUT-6 calls invalid: duplicate or
// unknown identities and cycles. An invalid commitment covers nothing.
func validPlan(p history.Projection, session string, units []PlanUnit) error {
	pending, dependents, err := planGraph(p, session, units)
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
func planGraph(p history.Projection, session string, units []PlanUnit) (map[string]int, map[string][]string, error) {
	pending, err := indexPendingUnits(units)
	if err != nil {
		return nil, nil, err
	}
	dependents, err := linkPrerequisites(p, session, units, pending)
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
// an already-executed unit of session, and otherwise counts it against pending
// and records the in-plan reverse edge in dependents.
func linkPrerequisites(p history.Projection, session string, units []PlanUnit, pending map[string]int) (map[string][]string, error) {
	dependents := map[string][]string{}
	for _, u := range units {
		for _, need := range u.Prerequisites {
			if _, inPlan := pending[need]; !inPlan {
				if _, executed := p.Unit(session, need); !executed {
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
func Revise(h *history.History, journalRef, session, baseVersion, newVersion string, adds []PlanUnit, withdrawals []string, qaRound ...*history.QARound) error {
	if session == "" || baseVersion == "" || newVersion == "" {
		return errors.New("dispatch: a revision identifies its session, expected base version and new version")
	}
	projection := h.Project()
	if projection.Budgets(session).PlanVersion != baseVersion {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion,
			"dispatch: revision declares a stale base version")
	}
	for _, u := range adds {
		if slices.Contains(withdrawals, u.Unit) {
			return invalidRevision(h, journalRef, session, baseVersion, newVersion,
				fmt.Sprintf("dispatch: %s is both added and withdrawn in one revision", u.Unit))
		}
	}
	current := currentPlan(projection, session)
	resulting := reviseUnits(current, adds, withdrawals)
	if e := validPlan(projection, session, resulting); e != nil {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion, e.Error())
	}
	if e := checkWithdrawalsPlanned(projection, session, withdrawals); e != nil {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion, e.Error())
	}
	if e := checkAdmittedUnchanged(projection, session, adds); e != nil {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion, e.Error())
	}
	if e := checkAddedPrerequisitesSettled(projection, session, resulting, adds); e != nil {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion, e.Error())
	}
	entries, err := qaRoundEntries(projection, journalRef, session, newVersion, resulting, qaRound)
	if err != nil {
		return invalidRevision(h, journalRef, session, baseVersion, newVersion, err.Error())
	}
	entries = append(entries, history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindRevised, SessionID: session,
		WorkUnitID: session, AttemptID: history.FirstAttempt, Cause: history.CauseNone,
		JournalRef: journalRef, BaseVersion: baseVersion, PlanVersion: newVersion, Classification: classify(current, resulting),
	})
	for _, u := range adds {
		entries = append(entries, plannedEntry(journalRef, session, newVersion, u))
	}
	for _, w := range withdrawals {
		entries = append(entries, history.Entry{
			Author: history.AuthorOrchestrator, Kind: history.KindWithdrawn, SessionID: session,
			WorkUnitID: w, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured, JournalRef: journalRef,
		})
	}
	return h.AppendBatch(entries)
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

// currentPlan reconstructs the plan session's last valid commitment or
// revision left standing: every unit a commitment declared that is still
// planned or admitted, in deterministic order.
func currentPlan(p history.Projection, session string) []PlanUnit {
	var units []PlanUnit
	for _, u := range p.Units {
		if u.SessionID == session && u.Contract != "" && (u.State == history.StatePlanned || u.State == history.StateInFlight) {
			units = append(units, PlanUnit{Unit: u.WorkUnitID, Contract: u.Contract, Prerequisites: u.Prerequisites})
		}
	}
	slices.SortFunc(units, func(a, b PlanUnit) int { return strings.Compare(a.Unit, b.Unit) })
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
func checkWithdrawalsPlanned(p history.Projection, session string, withdrawals []string) error {
	for _, w := range withdrawals {
		if u, _ := p.Unit(session, w); u.State != history.StatePlanned {
			return fmt.Errorf("dispatch: %s is not planned; only still-planned work may be withdrawn", w)
		}
	}
	return nil
}

// checkAdmittedUnchanged rejects a revision that would alter the contract or
// prerequisites of a unit no longer planned: a genuinely unchanged
// redeclaration of already-admitted work is tolerated, a change is not
// (PR-DAG-MUT-6).
func checkAdmittedUnchanged(p history.Projection, session string, adds []PlanUnit) error {
	for _, u := range adds {
		existing, known := p.Unit(session, u.Unit)
		if !known || existing.State == history.StatePlanned {
			continue
		}
		if existing.Contract != u.Contract || !slices.Equal(existing.Prerequisites, u.Prerequisites) {
			return fmt.Errorf("dispatch: %s is no longer planned; its contract and prerequisites cannot change; omit it from the revision", u.Unit)
		}
	}
	return nil
}

// checkAddedPrerequisitesSettled rejects a declared unit that depends on work
// outside the resulting plan which is not settled: a retained unit keeps its
// reference to withdrawn work visibly unsatisfied (PR-DAG-MUT-6), but new
// declarations may depend only on executed history.
func checkAddedPrerequisitesSettled(p history.Projection, session string, resulting []PlanUnit, adds []PlanUnit) error {
	inPlan := make(map[string]bool, len(resulting))
	for _, u := range resulting {
		inPlan[u.Unit] = true
	}
	for _, u := range adds {
		for _, need := range u.Prerequisites {
			if existing, _ := p.Unit(session, need); !inPlan[need] && existing.State != history.StateSettled {
				return fmt.Errorf("dispatch: %s depends on %s, which is withdrawn or not settled; a new declaration depends only on planned or settled work", u.Unit, need)
			}
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

// plannedEntry is the sole plan-effect shape for both commitment and revision.
func plannedEntry(ref, session, version string, u PlanUnit) history.Entry {
	return history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: session,
		WorkUnitID: u.Unit, NodeKind: history.NodeKindDelegated, AttemptID: history.FirstAttempt, Cause: history.CauseNone,
		JournalRef: ref, PlanVersion: version, Contract: u.Contract, Prerequisites: u.Prerequisites}
}

// qaRoundEntries validates membership before any plan effect is persisted. A
// unit never migrates to a new round; retries retain its original membership.
func qaRoundEntries(p history.Projection, ref, session, version string, plan []PlanUnit, rounds []*history.QARound) ([]history.Entry, error) {
	if len(rounds) > 1 {
		return nil, errors.New("dispatch: declare at most one qa_round per plan version")
	}
	if len(rounds) == 0 || rounds[0] == nil {
		return nil, nil
	}
	r := rounds[0]
	if strings.TrimSpace(r.Objective) == "" || len(r.Units) == 0 {
		return nil, errors.New("dispatch: qa_round requires a stable acceptance objective and authoring units")
	}
	budgets := p.Budgets(session)
	if _, exists := budgets.QARounds[version]; exists {
		return nil, errors.New("dispatch: this plan version already declares a QA round; its membership is immutable; use a new version for a new round")
	}
	seen := map[string]bool{}
	for _, id := range r.Units {
		if strings.TrimSpace(id) == "" || seen[id] {
			return nil, errors.New("dispatch: qa_round units must be distinct and nonempty")
		}
		seen[id] = true
		if !slices.ContainsFunc(plan, func(u PlanUnit) bool { return u.Unit == id }) {
			return nil, fmt.Errorf("dispatch: qa_round unit %q is absent from the resulting plan; declare its contract with dispatch_commit", id)
		}
		if u, known := p.Unit(session, id); known && u.State != history.StatePlanned {
			return nil, fmt.Errorf("dispatch: qa_round unit %q has already been admitted or withdrawn; declare new authoring units before delegation", id)
		}
		if prior, _ := budgets.QARoundFor(id); prior != "" {
			return nil, fmt.Errorf("dispatch: qa_round unit %q already belongs to round %s; membership is immutable", id, prior)
		}
	}
	round := &history.QARound{Objective: r.Objective, Units: slices.Clone(r.Units)}
	return []history.Entry{{Author: history.AuthorOrchestrator, Kind: history.KindQARound, SessionID: session,
		WorkUnitID: session, AttemptID: history.FirstAttempt, Cause: history.CauseNone, JournalRef: ref, PlanVersion: version, QARound: round}}, nil
}
