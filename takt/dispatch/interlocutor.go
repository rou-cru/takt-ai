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
	"slices"

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

// Switch registers a new temporary interlocutor-stack holder for root. Only a
// targetAgent whose catalog role resolves to model.RoleDirectInterlocutor is
// eligible (IR-1); a root with an active holder refuses a second one (IR-19: no
// chaining). expectedArtifact is the standard artifact path this switch
// declares, fixed here and never renegotiated (PR-HAR-24).
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
		held := h.Project().Budgets(root)
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, history.CauseInterlocutorHeld
		e = fmt.Errorf("harness: the interlocutor interface is already held by %s (session %s); no chaining; to replace it, ask the user to run /takt-abort, then call dispatch_switch again", held.InterlocutorAgent, held.InterlocutorHolder)
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
// convening agent nor the temporary holder itself). An empty childSession
// aborts whoever currently holds root, resolved here under the caller's lock.
func Abort(h *history.History, journalRef, root, childSession, reason, origin string) error {
	entry := history.Entry{
		Author: history.AuthorHarness, Kind: history.KindInterlocutorAborted,
		SessionID: root, WorkUnitID: childSession, AttemptID: history.FirstAttempt,
		Result: "Aborted", Cause: history.CauseUncaptured, JournalRef: journalRef,
	}
	var e error
	holder := h.Project().Budgets(root).InterlocutorHolder
	if childSession == "" {
		childSession = holder
		entry.WorkUnitID = holder
	}
	switch {
	case origin != "user" && origin != "harness":
		entry.Kind, entry.Cause = history.KindDenied, history.CauseInterlocutorOrigin
		e = fmt.Errorf("harness: an abortion may only originate from the user or the harness, not %q", origin)
	case holder == "":
		entry.Kind, entry.Cause = history.KindDenied, history.CauseInterlocutorHolder
		e = errors.New("harness: no interlocutor switch is active for this session; nothing to abort")
	case holder != childSession:
		entry.Kind, entry.Cause = history.KindDenied, history.CauseInterlocutorHolder
		e = fmt.Errorf("harness: session %s is not the current interlocutor holder; abort the holder session instead", childSession)
	}
	return errors.Join(h.Append(entry), e)
}

// ExpectedArtifact returns the standard document path declared by the active
// switch for root, or "" when it declares none.
func ExpectedArtifact(h *history.History, root string) string {
	return h.Project().Budgets(root).InterlocutorArtifact
}

// CurrentAgent returns the specialist holding root's interlocutor role, read
// from its owner; empty when no switch is active.
func CurrentAgent(h *history.History, root string) string {
	return h.Project().Budgets(root).InterlocutorAgent
}

// CurrentHolder returns the session holding root's interlocutor role, read
// from its owner; empty when no switch is active.
func CurrentHolder(h *history.History, root string) string {
	return h.Project().Budgets(root).InterlocutorHolder
}

// HandoffOutcome is the negotiated result a handoff envelope carries: the
// IR-23 result, any additional context, extra filesystem artifacts, and the
// Engram IDs the holder recorded for delivered results.
type HandoffOutcome struct {
	Result            string
	AdditionalContext string
	ExtraArtifacts    []string
	ResultIDs         []int64
}

// MemoryIndex is the bounded IR-22 index of the entries a lent session
// recorded. NextAfter is the last listed entry when more remain, the cursor
// LentEntries pages from; zero when the index is complete.
type MemoryIndex struct {
	Entries   []int64 `json:"entries"`
	NextAfter int64   `json:"next_after,omitempty"`
}

// Envelope is the IR-22 output envelope: exactly these four fields.
type Envelope struct {
	Result            string      `json:"result"`
	AdditionalContext string      `json:"additional_context"`
	ExtraArtifacts    []string    `json:"extra_artifacts"`
	Memory            MemoryIndex `json:"memory"`
}

// BuildHandoffEnvelope requires Engram IDs the holder itself recorded in this
// session for delivered results; extra filesystem artifacts are optional and do
// not establish a handoff. memoryRoot is where the memory session index lives.
func BuildHandoffEnvelope(h *history.History, journalRef, memoryRoot, root, callerSession, agent string, outcome HandoffOutcome) (Envelope, error) {
	if outcome.Result == "Standard" || len(outcome.ResultIDs) > 0 {
		if e := memory.ValidateSessionResultIDs(context.Background(), memory.Config{Root: memoryRoot}, root, agent, outcome.ResultIDs); e != nil {
			return Envelope{}, e
		}
	}
	// A caller that does not hold the turn has no index to read: Handoff
	// records and returns its denial.
	if callerSession != h.Project().Budgets(root).InterlocutorHolder {
		return Envelope{}, Handoff(h, journalRef, root, callerSession, outcome.Result)
	}
	// The index is read before the handoff is recorded: a failed read leaves
	// the holder in place, so the same handoff can be retried.
	index, e := lentIndex(memoryRoot, root, callerSession, 0)
	if e != nil {
		return Envelope{}, e
	}
	if e := Handoff(h, journalRef, root, callerSession, outcome.Result); e != nil {
		return Envelope{}, e
	}
	extra := outcome.ExtraArtifacts
	if extra == nil {
		extra = []string{}
	}
	return Envelope{Result: outcome.Result, AdditionalContext: outcome.AdditionalContext, ExtraArtifacts: extra, Memory: index}, nil
}

// BuildAbortEnvelope ends the temporary holder's turn (IR-24) and synthesizes
// its envelope: no negotiated result, the abort's evidence as additional
// context, and the entries the lent session recorded. It also returns the
// aborted session.
func BuildAbortEnvelope(h *history.History, journalRef, memoryRoot, root, childSession, reason, origin string) (Envelope, string, error) {
	aborted := h.Project().Budgets(root).InterlocutorHolder
	if childSession != "" {
		aborted = childSession
	}
	// As for a handoff, the index is read before the abort is recorded. With
	// no holder there is nothing to index, and Abort states the refusal.
	var index MemoryIndex
	if aborted != "" {
		var e error
		if index, e = lentIndex(memoryRoot, root, aborted, 0); e != nil {
			return Envelope{}, "", e
		}
	}
	if e := Abort(h, journalRef, root, childSession, reason, origin); e != nil {
		return Envelope{}, "", e
	}
	return Envelope{Result: "Aborted", AdditionalContext: reason, ExtraArtifacts: []string{}, Memory: index}, aborted, nil
}

// LentEntries pages the memory index of child, a session that held root's
// interface, from the entry after. Any other session is refused: the index
// never carries unrelated root-session history (IR-22).
func LentEntries(h *history.History, memoryRoot, root, child string, after int64) (MemoryIndex, error) {
	lent := false
	for _, e := range h.Entries() {
		if e.Kind == history.KindInterlocutorSwitched && e.SessionID == root && e.WorkUnitID == child {
			lent = true
			break
		}
	}
	if !lent {
		return MemoryIndex{}, fmt.Errorf("harness: session %s never held the interface of %s; page a lent session named by its switch", child, root)
	}
	return lentIndex(memoryRoot, root, child, after)
}

// lentIndex lists one policy page of the entries child recorded in root after
// the entry after (all from the start when after is zero).
func lentIndex(memoryRoot, root, child string, after int64) (MemoryIndex, error) {
	p, e := LoadAdmissionPolicy()
	if e != nil {
		return MemoryIndex{}, e
	}
	ids, e := memory.EntryIDsByCaller(memoryRoot, root, child)
	if e != nil {
		return MemoryIndex{}, e
	}
	if after != 0 {
		i := slices.Index(ids, after)
		if i < 0 {
			return MemoryIndex{}, fmt.Errorf("harness: entry #%d is not in the lent session's index; page from its next_after", after)
		}
		ids = ids[i+1:]
	}
	index := MemoryIndex{Entries: ids}
	if len(ids) > p.Interlocutor.MemoryPage {
		index.Entries = ids[:p.Interlocutor.MemoryPage]
		index.NextAfter = index.Entries[len(index.Entries)-1]
	}
	return index, nil
}
