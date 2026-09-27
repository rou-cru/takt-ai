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
	"context"
	"errors"
	"fmt"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
)

// interlocutorRole resolves agent's catalog role. Duplicated from
// takt/memory.authorRole (~10 lines) rather than importing takt/memory from
// takt/dispatch, which would add a cross-package dependency for so little.
func interlocutorRole(agent string) (model.RoleClass, error) {
	content, e := catalog.LoadNativeContent()
	if e != nil {
		return "", fmt.Errorf("load crew catalog: %w", e)
	}
	entry, ok := content[agent]
	if !ok {
		return "", fmt.Errorf("dispatch: %q is not a Takt crew member", agent)
	}
	return entry.Role, nil
}

// Switch registers a new temporary interlocutor-stack holder for root. Only
// a targetAgent whose catalog role resolves to model.RoleDirectInterlocutor
// is eligible (IR-1); a root that already has an active holder refuses a
// second one (IR-19: no chaining). expectedArtifact is the standard artifact
// path this switch declares, fixed here and never renegotiated (PR-HAR-24).
func Switch(h *history.History, journalRef, root, childSession, targetAgent, expectedArtifact string) error {
	role, roleErr := interlocutorRole(targetAgent)
	entry := history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched,
		SessionID: root, WorkUnitID: childSession, AttemptID: history.FirstAttempt,
		Agent: targetAgent, Artifact: expectedArtifact, Cause: history.CauseUncaptured,
		JournalRef: journalRef,
	}
	var e error
	switch {
	case roleErr != nil || !role.HoldsInterface():
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, history.CauseInterlocutorRole
		e = fmt.Errorf("harness: %q does not resolve to the direct-interlocutor role", targetAgent)
	case h.Project().Budgets(root).InterlocutorHolder != "":
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, history.CauseInterlocutorHeld
		e = errors.New("harness: the interlocutor interface is already held; no chaining")
	}
	return errors.Join(h.Append(entry), e)
}

// Handoff ends the current temporary holder's turn and returns the
// interface to root. Only the session that is currently the holder may call
// this (IR-24: the convening agent never emits it). result is one of
// "Standard", "EarlyHandoff", "TechFault", "Outraged" (IR-23).
func Handoff(h *history.History, journalRef, root, callerSession, result string) error {
	entry := history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorHandoff,
		SessionID: root, WorkUnitID: callerSession, AttemptID: history.FirstAttempt,
		Result: result, Cause: history.CauseUncaptured, JournalRef: journalRef,
	}
	var e error
	if callerSession != h.Project().Budgets(root).InterlocutorHolder {
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, history.CauseInterlocutorHolder
		e = errors.New("harness: only the current interlocutor holder may hand off")
	}
	return errors.Join(h.Append(entry), e)
}

// Abort ends the temporary holder's turn for childSession without
// negotiation. origin must be exactly "user" or "harness" (IR-24: never the
// convening agent nor the temporary holder itself).
func Abort(h *history.History, journalRef, root, childSession, reason, origin string) error {
	entry := history.Entry{
		Author: history.AuthorHarness, Kind: history.KindInterlocutorAborted,
		SessionID: root, WorkUnitID: childSession, AttemptID: history.FirstAttempt,
		Result: "Aborted", Cause: history.CauseUncaptured, JournalRef: journalRef,
	}
	var e error
	if origin != "user" && origin != "harness" {
		entry.Kind, entry.Cause = history.KindDenied, history.CauseInterlocutorOrigin
		e = fmt.Errorf("harness: an abortion may only originate from the user or the harness, not %q", origin)
	}
	return errors.Join(h.Append(entry), e)
}

// ExpectedArtifact returns the standard artifact path declared by the
// active switch for root, or "" if there is none.
func ExpectedArtifact(h *history.History, root string) string {
	return h.Project().Budgets(root).InterlocutorArtifact
}

// ArtifactMisses returns how many times a handoff for root has already been
// denied for a missing standard artifact since the current holder took the
// interface (PR-HAR-24: 0 means the next miss is the first).
func ArtifactMisses(h *history.History, root string) int {
	return h.Project().Budgets(root).InterlocutorArtifactMisses
}

// DenyArtifactMissing records one miss of the standard artifact declared for
// root's active switch, for a caller that already confirmed via os.Stat (or
// equivalent) that the file is absent.
func DenyArtifactMissing(h *history.History, journalRef, root, childSession, agent string) error {
	return h.Append(history.Entry{
		Author: history.AuthorHarness, Kind: history.KindDenied, Cause: history.CauseInterlocutorArtifact,
		SessionID: root, WorkUnitID: childSession, AttemptID: history.FirstAttempt,
		Agent: agent, JournalRef: journalRef,
	})
}

// BuildHandoffEnvelope requires existing Engram IDs for delivered results;
// filesystem copies are optional and do not establish a handoff.
func BuildHandoffEnvelope(h *history.History, journalRef, workspace, root, callerSession, agent, result, additionalContext string, extraArtifacts []string, resultIDs []int64) (map[string]any, error) {
	if result == "Standard" || len(resultIDs) > 0 {
		if e := memory.ValidateResultIDs(context.Background(), memory.Config{}, resultIDs); e != nil {
			return nil, e
		}
	}
	if e := Handoff(h, journalRef, root, callerSession, result); e != nil {
		return nil, e
	}
	return map[string]any{
		"result": result, "additional_context": additionalContext,
		"extra_artifacts": extraArtifacts, "memory": resultIDs,
	}, nil
}

// BuildAbortEnvelope ends the temporary holder's turn (IR-24) and assembles
// the same IR-22 envelope shape a handoff produces: no negotiated result, the
// abort's evidence as additional context, and childSession's memory entries.
func BuildAbortEnvelope(h *history.History, journalRef, workspace, root, childSession, reason, origin string) (map[string]any, error) {
	if e := Abort(h, journalRef, root, childSession, reason, origin); e != nil {
		return nil, e
	}
	ids, e := memory.EntryIDsForSession(workspace, childSession)
	if e != nil {
		return nil, e
	}
	return map[string]any{
		"result": "Aborted", "additional_context": reason,
		"extra_artifacts": []string{}, "memory": ids,
	}, nil
}
