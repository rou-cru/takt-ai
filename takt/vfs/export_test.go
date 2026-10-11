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
	"fmt"
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
