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
	"strconv"
	"strings"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
)

// interlocutorRole resolves agent's catalog role.
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

// switchRefusal is the one place the switch checks live: the history cause that
// records the refusal and the refusal itself, both empty when the switch is allowed.
func switchRefusal(h *history.History, root, targetAgent string) (string, error) {
	role, roleErr := interlocutorRole(targetAgent)
	switch {
	case roleErr != nil || !role.HoldsInterface():
		return history.CauseInterlocutorRole, fmt.Errorf("harness: %q does not resolve to the direct-interlocutor role", targetAgent)
	case h.Project().Budgets(root).InterlocutorHolder != "":
		held := h.Project().Budgets(root)
		return history.CauseInterlocutorHeld, fmt.Errorf("harness: the interlocutor interface is already held by %s (session %s); no chaining; to replace it, ask the user to run /takt-abort, then call dispatch_switch again", held.InterlocutorAgent, held.InterlocutorHolder)
	}
	return "", nil
}

// ValidateSwitch runs, read-only, the checks Switch runs before it binds and
// records: the role of targetAgent and the active holder of root (PR-HAR-23).
func ValidateSwitch(h *history.History, root, targetAgent string) error {
	_, err := switchRefusal(h, root, targetAgent)
	return err
}

// Switch registers a new temporary interlocutor-stack holder for root. Only a
// targetAgent whose catalog role resolves to model.RoleDirectInterlocutor is
// eligible (IR-1); a root with an active holder refuses a second one (IR-19: no
// chaining). A refused switch is recorded as a denial and never calls bind;
// bind runs once the checks pass, and its failure fails the switch and records
// nothing.
func Switch(h *history.History, journalRef, root, childSession, targetAgent string, bind func() error) error {
	entry := history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched,
		SessionID: root, WorkUnitID: childSession, AttemptID: history.FirstAttempt,
		Agent: targetAgent, Cause: history.CauseUncaptured,
		JournalRef: journalRef,
	}
	cause, refusal := switchRefusal(h, root, targetAgent)
	if refusal != nil {
		entry.Author, entry.Kind, entry.Cause = history.AuthorHarness, history.KindDenied, cause
		return errors.Join(h.Append(entry), refusal)
	}
	if err := bind(); err != nil {
		return err
	}
	return h.Append(entry)
}

// errNotHolder refuses a handoff from a session that does not hold the interface.
var errNotHolder = errors.New("only the temporary interlocutor holder may hand off; in a delegation deliver with deliver_result, otherwise answer the user directly")

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
		e = errNotHolder
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

// completedResult is the handoff result that delivers the holder's work (IR-23); every other
// result closes early and needs no entry.
const completedResult = "Standard"

// handoffResults is the closed set of IR-23 handoff results.
var handoffResults = []string{completedResult, "EarlyHandoff", "TechFault", "Outraged"}

// entryRefPrefix marks an extra-artifact reference as a memory entry reference (#<id>).
const entryRefPrefix = "#"

// validArtifactRef reports whether ref is syntactically resolvable (PR-HAR-25): an entry
// reference "#<positive integer>" or a non-empty path that does not start with "#".
func validArtifactRef(ref string) bool {
	if id, ok := strings.CutPrefix(ref, entryRefPrefix); ok {
		n, err := strconv.ParseUint(id, 10, 63)
		return err == nil && n > 0
	}
	return ref != ""
}

// HandoffOutcome is the negotiated result a handoff envelope carries: the
// IR-23 result, any additional context, extra filesystem artifacts, and the
// memory entry IDs the holder recorded for delivered results.
type HandoffOutcome struct {
	Result            string
	AdditionalContext string
	ExtraArtifacts    []string
	ResultIDs         []int64
}

// Envelope is the IR-22 output envelope: exactly these four fields. Memory is
// the delivered, validated entry IDs; a synthesized abort leaves it empty.
type Envelope struct {
	Result            string   `json:"result"`
	AdditionalContext string   `json:"additional_context"`
	ExtraArtifacts    []string `json:"extra_artifacts"`
	Memory            []int64  `json:"memory"`
}

// MemoryCheck validates entry IDs against a rule through the memory service's
// one validator.
type MemoryCheck func(ctx context.Context, ids []int64, rule memory.Rule) (memory.DescribeResult, error)

// DeliveryRefusal words a refused delivery: the cause and the continuation that
// makes a delivery valid (PR-HAR-24). retry is the call that delivers.
func DeliveryRefusal(cause error, retry string) error {
	return fmt.Errorf("%w; call %s again with the current IDs of Takt memory entries recorded in this session, recording with %s first only a result not yet recorded here", cause, retry, memory.PrefixedToolName(memory.ToolRecord))
}

// ValidateDelivery checks the entry IDs a session delivers: written by that exact
// session and current. A completed delivery without IDs is refused.
func ValidateDelivery(ctx context.Context, check MemoryCheck, session string, ids []int64) error {
	if len(ids) == 0 {
		return errors.New("refused: a completed result delivers at least one Takt memory entry ID")
	}
	_, e := check(ctx, ids, memory.Rule{WrittenBy: session})
	return e
}

// handoffRetry is the call a refused handoff continues with.
const handoffRetry = "dispatch_handoff"

// ValidateHandoff is the check a handoff passes before it is recorded, and
// before the user is asked (PR-HAR-23): only the holder may hand off; a
// result is within its enumeration and every extra artifact is a resolvable reference
// (PR-HAR-25); a completed result delivers valid IDs; an early close with no result passes
// without a placeholder, and any IDs it declares are still validated (PR-HAR-24).
func ValidateHandoff(ctx context.Context, h *history.History, check MemoryCheck, root, callerSession string, outcome HandoffOutcome) error {
	b := h.Project().Budgets(root)
	if callerSession != b.InterlocutorHolder {
		return errNotHolder
	}
	if !slices.Contains(handoffResults, outcome.Result) {
		return fmt.Errorf("harness: handoff result %q is not valid; valid values: %s; call %s again with one of them", outcome.Result, strings.Join(handoffResults, ", "), handoffRetry)
	}
	for _, ref := range outcome.ExtraArtifacts {
		if !validArtifactRef(ref) {
			return fmt.Errorf("harness: extra artifact %q is not a valid reference; use #<positive Takt memory entry ID> or a non-empty path not starting with %q; call %s again with valid references", ref, entryRefPrefix, handoffRetry)
		}
	}
	if outcome.Result != completedResult && len(outcome.ResultIDs) == 0 {
		return nil
	}
	if e := ValidateDelivery(ctx, check, callerSession, outcome.ResultIDs); e != nil {
		return DeliveryRefusal(e, handoffRetry)
	}
	return nil
}

// BuildHandoffEnvelope requires memory IDs the holder itself recorded in this
// lent session for delivered results; extra filesystem artifacts are optional and do
// not establish a handoff.
func BuildHandoffEnvelope(ctx context.Context, h *history.History, journalRef string, check MemoryCheck, root, callerSession string, outcome HandoffOutcome) (Envelope, error) {
	if e := ValidateHandoff(ctx, h, check, root, callerSession, outcome); e != nil {
		// A caller that does not hold the turn is refused by Handoff, which records its denial.
		if errors.Is(e, errNotHolder) {
			return Envelope{}, Handoff(h, journalRef, root, callerSession, outcome.Result)
		}
		return Envelope{}, e
	}
	if e := Handoff(h, journalRef, root, callerSession, outcome.Result); e != nil {
		return Envelope{}, e
	}
	extra := outcome.ExtraArtifacts
	if extra == nil {
		extra = []string{}
	}
	delivered := outcome.ResultIDs
	if delivered == nil {
		delivered = []int64{}
	}
	return Envelope{Result: outcome.Result, AdditionalContext: outcome.AdditionalContext, ExtraArtifacts: extra, Memory: delivered}, nil
}

// BuildAbortEnvelope ends the temporary holder's turn (IR-24) and synthesizes
// its envelope: no negotiated result, the abort's evidence as additional
// context, and no memory entries. It also returns the aborted session.
func BuildAbortEnvelope(h *history.History, journalRef, root, childSession, reason, origin string) (Envelope, string, error) {
	aborted := h.Project().Budgets(root).InterlocutorHolder
	if childSession != "" {
		aborted = childSession
	}
	if e := Abort(h, journalRef, root, childSession, reason, origin); e != nil {
		return Envelope{}, "", e
	}
	return Envelope{Result: "Aborted", AdditionalContext: reason, ExtraArtifacts: []string{}, Memory: []int64{}}, aborted, nil
}
