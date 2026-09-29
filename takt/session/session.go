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

// Package session is the composition root of one crew execution session: it
// owns the session clock, the control bus and the VFS, and wires the VFS's
// Takt-native signals onto the bus. Sensors feed the control plane, which
// never reaches back for them: takt/obs imports no sensor and takt/vfs
// imports no bus; this package imports both. A Session does not execute
// agents; it guarantees that every VFS operation lands on the bus, in order,
// without content.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// Session correlates one crew execution: a clock, a control bus, and the VFS
// whose journal and collisions feed that bus.
type Session struct {
	// Clock is the session clock owned by the control plane.
	Clock *obs.Clock
	// Bus is the single deterministic control point for this session.
	Bus *obs.Bus
	// FS is the transactional execution layer for agent file operations.
	FS *vfs.FS
	// Store persists the bus in the workspace's .takt-ai/events.db, so signals outlive this process.
	Store *obs.Store
}

// Close releases the event store; the FS is closed separately by whoever opened the session's workspace.
func (s *Session) Close() error { return s.Store.Close() }

// NewDurable composes the durable workspace VFS with this session's bus.
func NewDurable(rootDir, stateDir, sessionID string) (*Session, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session: sessionID is required")
	}
	fs, err := vfs.Open(rootDir, stateDir)
	if err != nil {
		return nil, err
	}
	// The store lives in the workspace, not the private state dir, so every invocation for one workspace shares it.
	store, err := obs.OpenStore(rootDir)
	if err != nil {
		_ = fs.Close()
		return nil, err
	}
	return compose(fs, sessionID, store), nil
}

func compose(fs *vfs.FS, sessionID string, store *obs.Store) *Session {
	clock := obs.NewClock()
	s := &Session{Clock: clock, Bus: obs.NewBus(sessionID, clock), FS: fs, Store: store}
	s.Bus.AttachStore(store)

	// The envelope references the journal entry and carries no
	// copy of it, and collisions reach the orchestrator as typed
	// events. Both handlers run in VFS order, so the bus stream stays in order.
	fs.OnJournal(func(entry vfs.JournalEntry) {
		if entry.SessionID != "" && entry.SessionID != sessionID {
			return
		}
		s.report(obs.PublishVFSEvent(s.Bus, clock, string(entry.Agent), entry.Ref(), entry.WorkUnitID,
			map[string]any{"operation": string(entry.Operation)}))
		// PR-MNT-31: a write landing on a path a maintenance cycle already
		// consolidated is a reversion, attributed back to that cycle before it
		// looks like an unattributed edit. The FS computes this under its own
		// lock at journal time (ReeditOf*); this handler only reads the
		// result — it must never call back into fs, which is still locked
		// for the duration of this callback.
		if entry.ReeditOfCycleID != "" {
			s.report(obs.PublishCycleReeditEvent(s.Bus, clock, string(entry.Agent), entry.ReeditOfCycleID, entry.ReeditOfMandateClass, hashPath(entry.Path), entry.WorkUnitID))
		}
	})
	fs.OnCollision(func(e vfs.CollisionEvent) {
		if e.SessionID != "" && e.SessionID != sessionID {
			return
		}
		s.report(obs.PublishCollisionEvent(s.Bus, clock,
			string(e.AttemptingAgent), string(e.OwningAgent), e.Path, e.WorkUnitID))
	})
	return s
}

// hashPath digests path so the reedit event attributes a reversion without
// carrying the raw path as event content.
func hashPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])
}

// report degrades the control plane when an event cannot be recorded. Losing a
// signal is itself observable, never silent.
func (s *Session) report(err error) {
	if err != nil {
		s.Bus.Degrade(fmt.Sprintf("session: telemetry rejected: %v", err))
	}
}
