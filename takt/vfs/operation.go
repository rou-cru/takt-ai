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

package vfs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rou-cru/takt-ai/takt/model"
)

// Sentinel errors for operation-level rejections.
var (
	// ErrStaleRevision is returned when the caller's expected revision no longer matches staged state.
	ErrStaleRevision = errors.New("vfs: revision changed")
	// ErrIdentity is returned when an operation lacks a valid binding or identity.
	ErrIdentity = errors.New("vfs: invalid operation identity")
	// ErrDuplicateCall is returned when a call ID is reused.
	ErrDuplicateCall = errors.New("vfs: call already consumed")
	// ErrNothingStaged is returned when consolidation names work that has no staged change.
	ErrNothingStaged = errors.New("vfs: nothing is staged for that author key")
	// ErrStagedWork is returned when a claim to release still holds staged work: a
	// delta without an owner could never be consolidated.
	ErrStagedWork = errors.New("vfs: the claim holds staged work; consolidate it, reassign it with its author key, or discard it")
)

// Operation carries adapter-controlled identity and a revision from the last result.
type Operation struct {
	Key              AgentID
	CallID           string
	ExpectedRevision uint64
	Action           OperationType
	Path             string
	Content          []byte
}

// OperationResult returns the merged view content an operation produced.
type OperationResult struct {
	Content   []byte
	Revision  uint64
	DeltaHash string
	// OutdatedPaths lists, for a gate read, the paths the gate read under an
	// earlier state of its author's staged work.
	OutdatedPaths []string
}

// correlateLocked stamps journal entries from start with key's identity.
func (f *FS) correlateLocked(start int, key, view AgentID, call string) {
	id := f.bindings[key]
	d := f.staged[view]
	if d == nil {
		d = f.staged[key]
	}
	for i := start; i < len(f.journal); i++ {
		e := &f.journal[i]
		stamp(e, id)
		e.CallID = call
		if d != nil {
			e.Revision = d.revision
		}
	}
}

// consumeCallLocked marks call as used in session; a replay is rejected.
func (f *FS) consumeCallLocked(session, call string) error {
	key := session + "\x00" + call
	if f.calls[key] {
		return fmt.Errorf("%w: %q was already consumed by a prior request; issue a fresh call", ErrDuplicateCall, call)
	}
	f.calls[key] = true
	return nil
}

// Apply validates identity, revision and replay, then persists the result.
func (f *FS) Apply(op Operation) (OperationResult, error) { return f.apply(op, op.Key) }

func (f *FS) apply(op Operation, view AgentID) (result OperationResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return result, err
	}
	return f.applyLocked(op, view, true)
}

// applyLocked admits and performs op through view's staged state. A trusted
// in-process caller states the revision it expects; a resolved caller's
// operation applies at the current revision, which the lock serializes.
func (f *FS) applyLocked(op Operation, view AgentID, checkRevision bool) (result OperationResult, err error) {
	start := len(f.journal)
	defer func() {
		if err != nil && len(f.journal) == start {
			f.appendJournalLocked(JournalEntry{Agent: op.Key, Operation: op.Action, Outcome: "denied"})
		}
		f.correlateLocked(start, op.Key, view, op.CallID)
		f.finishLocked(&err)
		if err != nil {
			result.Content = nil
		}
	}()
	identity, err := f.admitLocked(op, view, checkRevision)
	if err != nil {
		return result, err
	}
	result.Content, err = f.dispatchLocked(op, view, identity)
	result.Revision = f.revisionOf(view)
	result.DeltaHash = f.deltaHashLocked(view)
	return result, err
}

// admitLocked runs the checks that precede any effect: a bound identity, a
// legitimate view, a fresh call and the caller's revision. The call is consumed even when a later check rejects it.
func (f *FS) admitLocked(op Operation, view AgentID, checkRevision bool) (Identity, error) {
	identity, ok := f.bindings[op.Key]
	if !ok {
		return identity, errNoBinding(op.Key)
	}
	if op.CallID == "" {
		return identity, fmt.Errorf("%w: an operation names its call", ErrIdentity)
	}
	if view != op.Key {
		target, ok := f.bindings[view]
		if !ok {
			return identity, fmt.Errorf("%w: view %q is not an author binding", ErrIdentity, view)
		}
		if op.Action != OpRead {
			return identity, fmt.Errorf("%w: another author's staged view is read-only", ErrIdentity)
		}
		if err := RequireVFSCapability(identity.Specialist, model.VFSCapabilityVerify); err != nil {
			return identity, err
		}
		if !identity.verifies(view, target) {
			return identity, fmt.Errorf("%w: this gate is not assigned to the author whose view %q it reads", ErrIdentity, view)
		}
	}
	if err := f.consumeCallLocked(identity.SessionID, op.CallID); err != nil {
		return identity, err
	}
	if current := f.revisionOf(view); checkRevision && current != op.ExpectedRevision {
		return identity, fmt.Errorf("%w: current revision %d; re-read and issue a fresh call, or bind again to learn the current revision", ErrStaleRevision, current)
	}
	return identity, nil
}

// dispatchLocked performs an operation only when the bound catalog instance
// has its exact operation capability.
func (f *FS) dispatchLocked(op Operation, view AgentID, identity Identity) ([]byte, error) {
	switch op.Action {
	case OpRead:
		if err := RequireVFSCapability(identity.Specialist, model.VFSCapabilityRead); err != nil {
			return nil, err
		}
		return f.readLockedAs(view, op.Path, view != op.Key)
	case OpCreate, OpPatch:
		content := op.Content
		if content == nil {
			content = []byte{} // nil content would stage a deletion
		}
		return nil, f.stageLocked(op.Key, op.Path, content)
	case OpDelete:
		return nil, f.stageLocked(op.Key, op.Path, nil)
	case OpRollback:
		// Discarding a delta is the orchestrator's backtracking decision
		// (PR-VFS-CSL-5), never the author's.
		if err := RequireVFSCapability(OrchestratorInstance, model.VFSCapabilityDiscard); err != nil {
			return nil, err
		}
		f.rollbackLocked(op.Key)
		return nil, nil
	default:
		return nil, errors.New("vfs: unsupported operation")
	}
}

// ConsolidateCheckpoint is a trusted coordinator operation that consolidates
// authorized staged changes. Verdicts inform: a current failing one refuses
// unless acceptFailing carries the user's explicit acceptance, and a stale one
// is ignored. A maintenance cycle's current verdicts stay a gate that must pass.
// An empty checkpoint label defaults to the author key and revision.
func (f *FS) ConsolidateCheckpoint(key AgentID, checkpoint string, expected uint64, acceptFailing bool) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	return f.consolidateCheckpointLocked(key, checkpoint, expected, acceptFailing)
}

// consolidateCheckpointLocked consolidates key's staged work at the expected
// revision under checkpoint.
func (f *FS) consolidateCheckpointLocked(key AgentID, checkpoint string, expected uint64, acceptFailing bool) (err error) {
	if strings.TrimSpace(checkpoint) == "" {
		checkpoint = fmt.Sprintf("%s@%d", key, expected)
	}
	if _, ok := f.bindings[key]; !ok {
		return fmt.Errorf("%w: %q is not a known author key", ErrIdentity, key)
	}
	d := f.staged[key]
	if d == nil || len(d.files) == 0 {
		return ErrNothingStaged
	}
	if d.revision != expected {
		return fmt.Errorf("%w: the staged work is at revision %d, not %d; delegate a Verify of the author key again and consolidate against its revision", ErrStaleRevision, d.revision, expected)
	}
	// A maintenance cycle's verdict gates in consolidateLocked; ordinary
	// work asks for acceptance only over a current failing verdict.
	if !f.verdictRequiredLocked(key) {
		if err = f.consolidationAcceptedLocked(key, acceptFailing); err != nil {
			return err
		}
	}
	start := len(f.journal)
	defer func() {
		f.correlateLocked(start, key, key, "checkpoint/"+hashOf([]byte(checkpoint), true))
		f.finishLocked(&err)
	}()
	return f.consolidateLocked(key)
}
