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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"time"

	"github.com/rou-cru/takt-ai/takt/history"
)

// Accessors the black-box tests inspect the store through; production reads
// these facts only inside the locked commands that act on them.

// SetWorkspaceLockWait shortens the bounded wait on the workspace lock and
// returns the function that restores it.
func SetWorkspaceLockWait(wait time.Duration) (restore func()) {
	previous := workspaceLockWait
	workspaceLockWait = wait
	return func() { workspaceLockWait = previous }
}

// PendingGate returns the unadopted, current gate identity's judge already
// holds over authorKey in the same unit and attempt; an empty attempt matches
// any reservation.
func (f *FS) PendingGate(identity Identity, authorKey AgentID) (AgentID, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.pendingGateLocked(identity, authorKey)
}

// StagedState reports the revision and delta hash of the work staged under key,
// and whether any exists, so a binding that adopts existing work starts from it.
func (f *FS) StagedState(key AgentID) (revision uint64, deltaHash string, staged bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.revisionOf(key), f.deltaHashLocked(key), f.stagedLocked(key)
}

// ReadAs reads through the staged view of the author a verifier judges, so the
// gate assesses exactly the delta its verdict will cover. op.ExpectedRevision
// is the author's revision; any other caller or action is refused.
func (f *FS) ReadAs(op Operation, author AgentID) (OperationResult, error) {
	if op.Action != OpRead {
		return OperationResult{}, fmt.Errorf("%w: another author's staged view is read-only", ErrIdentity)
	}
	return f.apply(op, author)
}

// AdmittedAttempt is the attempt the execution history admitted for session's
// unit, or empty when no delegation of it is in flight. The history is read
// through this handle, which holds the workspace lock that serializes it.
func (f *FS) AdmittedAttempt(session, unit string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	projection, err := history.ReadProjection(f.stateDir)
	if err != nil {
		return "", err
	}
	if u, _ := projection.Unit(session, unit); u.State == history.StateInFlight {
		return u.AttemptID, nil
	}
	return "", nil
}

// GuardMaintenance applies the maintenance guard the locked commands apply.
func (f *FS) GuardMaintenance(command, attempt, action, path string, key AgentID) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.guardMaintenanceLocked(command, attempt, action, path, key)
}

// SetFailpoint installs fault injection at the store's durable flush
// boundaries; nil removes it.
func (f *FS) SetFailpoint(fn func(string) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failpoint = fn
}

// HasVerdict reports whether key's staged work carries any verdict.
func (f *FS) HasVerdict(key AgentID) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	d, ok := f.staged[key]
	return ok && len(d.verdicts) > 0
}

// ReadUnderGate reads path as agent through the same locked gate
// Apply(OpRead) passes, without the binding bookkeeping: the readiness check
// before and the store-failure check after.
func (f *FS) ReadUnderGate(agent AgentID, path string) (data []byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return nil, err
	}
	defer func() {
		f.finishLocked(&err)
		if err != nil {
			data = nil
		}
	}()
	return f.readLockedAs(agent, path, false)
}

// UnresolvedCollisions counts the collisions recorded as unresolved work.
func (f *FS) UnresolvedCollisions() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.collisions)
}

// RecoveryPending reports whether an incomplete consolidation's manifest is
// still retained.
func (f *FS) RecoveryPending() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.recovery != nil
}

// ExecState runs statement against the store's own database.
func (f *FS) ExecState(statement string) error {
	_, err := f.db.Exec(statement)
	return err
}

// ValidatePath applies the store's canonical workspace-path check.
func (f *FS) ValidatePath(path string) error { return f.validatePath(path) }

// ValidatePathIn applies the canonical workspace-path check over root without
// opening a store.
func ValidatePathIn(root, path string) (err error) {
	f, err := newFS(root)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return f.validatePath(path)
}

// InterruptedEvidence is what an approved interrupted closure retained.
type InterruptedEvidence = interruptedEvidence

// StoredState is the store's persisted state record.
type StoredState = storedState

// Interrupted lists the retained interrupted-closure evidence by its id.
func (f *FS) Interrupted() map[string]InterruptedEvidence {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return maps.Clone(f.interrupted)
}

// SeedConsolidatedCycle marks key's binding as cycle's collector and records
// that cycle's consolidation of path from before to after, both with mode.
func (f *FS) SeedConsolidatedCycle(cycle string, key AgentID, path string, before, after []byte, mode os.FileMode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	identity := f.bindings[key]
	identity.CycleID = cycle
	f.bindings[key] = identity
	f.cycles[cycle] = &cycleSnapshot{Agent: key, Items: map[string]recoveryItem{path: {Path: path,
		Before: baseFile{Present: true, Content: before, Mode: mode}, After: baseFile{Present: true, Content: after, Mode: mode}}}}
}

// Held counts the owned paths and the bindings the store holds.
func (f *FS) Held() (owners, bindings int) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.owners), len(f.bindings)
}

// RootDir is the canonical workspace root the store governs.
func (f *FS) RootDir() string { return f.rootDir }

// SavedState reads the state record as the store last persisted it.
func (f *FS) SavedState() (saved StoredState, err error) {
	var data []byte
	if err = f.db.QueryRow("SELECT data FROM state WHERE id=1").Scan(&data); err != nil {
		return saved, err
	}
	err = json.Unmarshal(data, &saved)
	return saved, err
}

// NotifiedCollisions counts the collision events already delivered.
func (f *FS) NotifiedCollisions() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.notifiedCollisions
}
