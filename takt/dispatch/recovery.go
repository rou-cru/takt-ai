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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// scopeExit is how an abandoned recovery scope goes on: the user decides, and
// each decision is one call (PR-ORQ-13, PR-HAR-22). bound is the allowance the
// abandonment spent.
func scopeExit(objective, bound string) string {
	return fmt.Sprintf("ask the user: to map the real state first, the blind review pair over objective %[1]q; to continue, propose dispatch_exception on %[2]s for objective %[1]q with a finite allowance, which the harness confirms with them; to drop the scope, dispatch_restore for objective %[1]q", objective, bound)
}

// RecoveryDeclaration is what PR-ORQ-13 requires recorded before uncertain work
// begins: the binary expected result, both budgets, the objective identity and
// the explicit scope. The prior recoverable point is the harness's own: the
// journal position the declaration is recorded at.
type RecoveryDeclaration struct {
	Objective string `json:"objective"`
	Result    string `json:"result"`

	Scope    []string `json:"scope"`
	Actions  int      `json:"actions"`
	Attempts int      `json:"attempts"`
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
		Result: d.Result, Scope: d.Scope, Actions: d.Actions, Attempts: d.Attempts,
	}
	var e error
	deny := func(cause string, reason error) {
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, cause
		e = reason
	}
	allowed := p.Budgets.RecoveryFailures + budgets.Allowance[history.AllowanceKey(history.BoundRecovery, d.Objective)]
	owner, owned := scopeOwner(budgets, d.Scope)
	switch {
	case d.Objective == "" || d.Result == "" || len(d.Scope) == 0 || d.Actions <= 0 || d.Attempts <= 0:
		deny(causeIncomplete, errors.New("harness: a recovery declares a binary result, both budgets, an objective and its scope; declare it again with every field set and both budgets above zero"))
	case d.Actions > p.Recovery.MaxActions || d.Attempts > p.Recovery.MaxAttempts:
		deny(history.BoundRecoveryActions, fmt.Errorf("harness: declared budgets exceed the admissible ceiling of %d actions and %d attempts; declare it again within them", p.Recovery.MaxActions, p.Recovery.MaxAttempts))
	case owner != "":
		deny(history.BoundRecoveryAttempts, overlapError(owner, owned))
	case budgets.Recoveries[d.Objective].Failures >= allowed:
		deny(history.BoundRecovery, fmt.Errorf("harness: %d consecutive failed recoveries of this objective require escalation; present the evidence and alternatives to the user; to continue, propose dispatch_exception on %s for objective %q, which the harness confirms with them, then declare the recovery again", allowed, history.BoundRecovery, d.Objective))
	}
	return errors.Join(h.Append(entry), e)
}

// scopeOwner names the unresolved recovery a declared scope's work already
// belongs to, which would make scope membership ambiguous; empty when none.
func scopeOwner(budgets history.Budgets, scope []string) (string, history.Recovery) {
	for _, unit := range scope {
		if objective, recovery := budgets.Scope(unit); objective != "" {
			return objective, recovery
		}
	}
	return "", history.Recovery{}
}

// overlapError says what resolves the recovery a declared scope collides with.
func overlapError(objective string, recovery history.Recovery) error {
	if recovery.Open {
		return fmt.Errorf("harness: the declared scope belongs to recovery %q, which is still open; close it with dispatch_close_recovery or declare disjoint units", objective)
	}
	return fmt.Errorf("harness: the declared scope belongs to recovery %q, abandoned with its %s spent; %s", objective, recovery.Cause, scopeExit(objective, recovery.Cause))
}

// CloseRecovery closes a declared recovery, recording whether its result was
// demonstrated and linking the evidence. Only a demonstrated recovery breaks the
// objective's streak, and a claimed result without evidence is not one.
func CloseRecovery(h *history.History, journalRef, event, session, objective, evidence string, demonstrated bool) error {
	if recovery := h.Project().Budgets(session).Recoveries[objective]; !recovery.Open {
		if recovery.Backtracked && !recovery.Restored {
			return fmt.Errorf("dispatch: recovery %q was abandoned, not left open; %s", objective, scopeExit(objective, recovery.Cause))
		}
		return fmt.Errorf("dispatch: no declared recovery %q is open; check the objective identity", objective)
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
	if n := scopeActions(entries, scope.Session, scope.Recovery); n > 0 {
		scope.Recovery.Used += n
		if e := recordActions(h, scope.Session, scope.Objective, scope.Recovery, history.KindActions, n, head); e != nil {
			return e
		}
	}
	return forceBacktrackIfExhausted(h, scope.Session, scope.Objective, scope.Recovery, scope.Budgets, scope.Projection, head)
}

// scopeActions counts the tool executions of session recorded past the
// established cursor whose work unit the recovery scope declares, whichever
// attempt performed them.
func scopeActions(entries []vfs.JournalEntry, session string, recovery history.Recovery) int {
	calls := map[string]bool{}
	for _, entry := range entries {
		if entry.Seq > recovery.Cursor && entry.CallID != "" && entry.SessionID == session && slices.Contains(recovery.Scope, entry.WorkUnitID) {
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
		u, _ := projection.Unit(session, unit)
		return u.State == history.StateInFlight
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
		Outcome: history.OutcomeBacktracked, Scope: recovery.Scope,
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

// restoreCallBytes is the entropy of a rollback's call identity: every attempt
// gets a fresh one, so a rollback that failed after consuming its call can be
// repeated.
const restoreCallBytes = 8

// restoreCall names one rollback of a recovery's staged work.
func restoreCall(objective string) (string, error) {
	random := make([]byte, restoreCallBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "recovery-restore/" + objective + "/" + hex.EncodeToString(random), nil
}

// Restore confirms restoration of an abandoned recovery scope's virtual state:
// it releases every author claim, rolling back staged work, and records the
// restoration only once all operations succeed, leaving unrelated progress
// untouched (PR-VFS-STG-6, PR-HAR-18). A failed rollback leaves the scope
// unresolved and a repeat call retries what remains.
func Restore(h *history.History, fs *vfs.FS, journalRef, session, objective string) error {
	projection := h.Project()
	recovery := projection.Budgets(session).Recoveries[objective]
	switch {
	case recovery.Open:
		return fmt.Errorf("dispatch: recovery %q is still open; close it with dispatch_close_recovery before anything is restored", objective)
	case recovery.Restored:
		return fmt.Errorf("dispatch: recovery %q is already restored; declare a new recovery to go on", objective)
	case !recovery.Backtracked:
		return fmt.Errorf("dispatch: no abandoned recovery %q exists; check the objective identity", objective)
	case slices.ContainsFunc(recovery.Scope, func(unit string) bool {
		u, _ := projection.Unit(session, unit)
		return u.State == history.StateInFlight
	}):
		return fmt.Errorf("harness: restoration waits until the affected executions can no longer act; call dispatch_restore for %q again once the delegations of %s have returned (in flight: %s); one that will never return is ended, with the user's agreement, by dispatch_abandon on its dispatch", objective, strings.Join(recovery.Scope, ", "), strings.Join(inFlightDispatches(projection, session, recovery.Scope), ", "))
	}
	var restored []string
	for _, claim := range fs.OwnershipClaims(session) {
		if claim.AuthorKey != "" || claim.RootSessionID != session || !slices.Contains(recovery.Scope, claim.WorkUnitID) {
			continue
		}
		if claim.Staged {
			call, e := restoreCall(objective)
			if e != nil {
				return e
			}
			// An unconfirmed rollback leaves the failure visible and the scope
			// unresolved; abandonment never claims restoration.
			result, e := fs.Apply(vfs.Operation{
				Key: claim.Key, CallID: call, Action: vfs.OpRollback, ExpectedRevision: fs.InspectDelta(claim.Key).Revision,
			})
			if e != nil {
				return fmt.Errorf("dispatch: restoring %s of recovery %q failed: %w; call dispatch_restore again to retry what remains", claim.Key, objective, e)
			}
			restored = append(restored, fmt.Sprintf("%s@%d", claim.Key, result.Revision))
		}
		if e := fs.RevokeOwnership(claim.Key); e != nil {
			return fmt.Errorf("dispatch: releasing %s of recovery %q failed: %w; call dispatch_restore again to retry what remains", claim.Key, objective, e)
		}
	}
	return h.Append(history.Entry{
		Author: history.AuthorHarness, Kind: history.KindRestored, SessionID: session,
		WorkUnitID: recovery.Unit, AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured,
		JournalRef: journalRef, PolicyRef: AdmissionPolicyRef, Objective: objective,
		Scope:    recovery.Scope,
		Evidence: "restored/" + strings.Join(restored, ","),
	})
}

// inFlightDispatches names the dispatch of each unit among units still in flight.
func inFlightDispatches(projection history.Projection, session string, units []string) []string {
	var dispatches []string
	for _, unit := range units {
		if u, _ := projection.Unit(session, unit); u.State == history.StateInFlight {
			dispatches = append(dispatches, unit+" "+u.Dispatch)
		}
	}
	return dispatches
}
