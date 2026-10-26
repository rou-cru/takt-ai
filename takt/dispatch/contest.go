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

	"github.com/rou-cru/takt-ai/takt/history"
)

// CauseNoFailure denies a contest of work with no recorded terminal failure.
const CauseNoFailure = "control/no-terminal-failure"

// Contest records the contest of a unit attempt that ended without a result,
// admitting at most the policy's distinct contests per session, upheld ones
// included. Judging the contested work is the orchestrator's Verify delegation.
// A transport duplicate of the same targeted failure returns its recorded
// disposition without consuming again (PR-HAR-17, PR-HAR-20).
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
	switch {
	case !terminalFailure(h.Entries(), session, event, attempt):
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, CauseNoFailure
		e = fmt.Errorf("harness: work unit %q has no recorded terminal failure to contest; to correct its delivery, claim_assign its author_key and delegate the unit again, or delegate a Verify of that author_key to judge it", event)
	case len(budgets.Contests) >= p.Budgets.Contests+budgets.Allowance[history.BoundContests]:
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, history.BoundContests
		e = fmt.Errorf("harness: contest allowance of %d consumed; propose dispatch_exception (the harness asks the user) on %s", p.Budgets.Contests, history.BoundContests)
	}
	return errors.Join(h.Append(entry), e)
}

// terminalFailure reports whether session's unit's attempt ended without a
// result: failed, backtracked or interrupted. A completed attempt has nothing
// to contest.
func terminalFailure(entries []history.Entry, session, event, attempt string) bool {
	return slices.ContainsFunc(entries, func(e history.Entry) bool {
		return e.Kind == history.KindTerminated && e.SessionID == session && e.WorkUnitID == event && e.AttemptID == attempt &&
			e.Outcome != history.OutcomeCompleted
	})
}
