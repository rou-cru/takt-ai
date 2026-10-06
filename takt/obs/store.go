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

package obs

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	// modernc.org/sqlite registers the "sqlite" database/sql driver via init.
	_ "modernc.org/sqlite"
)

// Telemetry modes keep the event database owner-only.
const (
	// PrivateTelemetryDirectoryMode protects the workspace telemetry directory.
	PrivateTelemetryDirectoryMode os.FileMode = 0o700
	// PrivateTelemetryFileMode protects the workspace telemetry database.
	PrivateTelemetryFileMode os.FileMode = 0o600
)

// StateDirName is the workspace-local state directory, shared with the execution history so all Takt state lives in one place.
const StateDirName = ".takt-ai"

// StoreFileName is the single event database, so every session and process of a workspace shares one stream.
const StoreFileName = "events.db"

const storeSchema = `
PRAGMA busy_timeout=5000;
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  written_at INTEGER NOT NULL,
  schema_version TEXT NOT NULL,
  wall_timestamp INTEGER NOT NULL,
  source TEXT NOT NULL,
  acting_agent TEXT NOT NULL,
  event_class TEXT NOT NULL,
  work_unit_id TEXT NOT NULL,
  attempt_id TEXT NOT NULL,
  journal_entry_ref TEXT NOT NULL,
  attributes TEXT
);
CREATE INDEX IF NOT EXISTS events_session ON events(session_id, id);
CREATE TABLE IF NOT EXISTS control_actions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  written_at INTEGER NOT NULL,
  action_class TEXT NOT NULL,
  triggering_condition TEXT NOT NULL,
  policy_ref TEXT NOT NULL,
  acting_agent TEXT NOT NULL,
  work_unit_id TEXT NOT NULL,
  attempt_id TEXT NOT NULL,
  journal_entry_ref TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS control_actions_session ON control_actions(session_id, id);`

// Store persists events and control actions in one workspace-local SQLite file so
// separate process invocations share a single stream. Row id is the order.
type Store struct {
	db *sql.DB
}

// StoredEvent is one persisted event with its row id and write time, so readers can resume by id.
type StoredEvent struct {
	ID        int64
	WrittenAt time.Time
	Envelope  Envelope
}

// StoredAction is one persisted control action with its row id and write time.
type StoredAction struct {
	ID        int64
	WrittenAt time.Time
	Record    ControlRecord
}

// StorePath returns where a workspace keeps its event database, the only location the store ever uses.
func StorePath(workspace string) (string, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, StateDirName, StoreFileName), nil
}

// OpenStore opens (creating if absent) the workspace event database in WAL mode with a busy timeout, so
// concurrent invocations wait for each other instead of failing.
func OpenStore(workspace string) (*Store, error) {
	path, err := StorePath(workspace)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), PrivateTelemetryDirectoryMode); err != nil {
		return nil, err
	}
	// modernc creates files 0644; pre-create private so telemetry is owner-only like the rest of the state.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, PrivateTelemetryFileMode)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// busy_timeout first so the WAL switch itself waits when another invocation holds the file.
	if _, err = db.Exec(storeSchema); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// AppendEvent validates then writes one event under its own session and returns its id. The session clock is
// not stored: it restarts each process, so row id is the order.
func (s *Store) AppendEvent(e Envelope) (int64, error) {
	if err := e.Validate(); err != nil {
		return 0, err
	}
	if e.Correlations.SessionID == "" {
		return 0, errors.New("obs: event Correlations.SessionID is required")
	}
	var attrs any
	if len(e.Attributes) > 0 {
		b, err := json.Marshal(e.Attributes)
		if err != nil {
			return 0, err
		}
		attrs = string(b)
	}
	var wall int64
	if !e.WallTimestamp.IsZero() {
		wall = e.WallTimestamp.UnixNano()
	}
	res, err := s.db.Exec(`INSERT INTO events (session_id, written_at, schema_version, wall_timestamp, source, acting_agent,
		event_class, work_unit_id, attempt_id, journal_entry_ref, attributes) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.Correlations.SessionID, time.Now().UnixNano(), e.SchemaVersion, wall, string(e.Source), e.Authority,
		string(e.EventClass), e.Correlations.WorkUnitID, e.Correlations.AttemptID, e.Correlations.JournalEntryRef, attrs)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// eventRowColumns lists the columns Events/CountEvents scan, in the order
// scanRow expects them, so the two queries below stay in lockstep.
const eventRowColumns = `id, written_at, schema_version, wall_timestamp, source, acting_agent, event_class,
	work_unit_id, attempt_id, journal_entry_ref, attributes`

// Events returns up to limit persisted events for sessionID with id in
// (afterID, beforeID], in id order (the order AppendEvent assigns, i.e. the
// order work was registered — never wall-clock time, per PR-MNT-9/PR-MNT-11).
// class filters to one EventClass; the zero value matches every class.
// beforeID of 0 means no upper bound; limit of 0 means no limit.
func (s *Store) Events(sessionID string, class EventClass, afterID, beforeID int64, limit int) (out []StoredEvent, err error) {
	query := `SELECT ` + eventRowColumns + ` FROM events WHERE session_id=? AND id>?`
	args := []any{sessionID, afterID}
	if beforeID > 0 {
		query += ` AND id<=?`
		args = append(args, beforeID)
	}
	if class != "" {
		query += ` AND event_class=?`
		args = append(args, string(class))
	}
	query += ` ORDER BY id LIMIT ?`
	if limit <= 0 {
		limit = -1
	}
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		ev, err := scanEventRow(rows, sessionID)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// scanEventRow decodes one row shaped like eventRowColumns into a StoredEvent.
func scanEventRow(rows *sql.Rows, sessionID string) (StoredEvent, error) {
	var (
		ev            StoredEvent
		written, wall int64
		source, class string
		attrs         sql.NullString
	)
	e := &ev.Envelope
	if err := rows.Scan(&ev.ID, &written, &e.SchemaVersion, &wall, &source, &e.Authority, &class,
		&e.Correlations.WorkUnitID, &e.Correlations.AttemptID, &e.Correlations.JournalEntryRef, &attrs); err != nil {
		return StoredEvent{}, err
	}
	ev.WrittenAt = time.Unix(0, written)
	if wall != 0 {
		e.WallTimestamp = time.Unix(0, wall)
	}
	e.Source, e.EventClass, e.Correlations.SessionID = SourcePlane(source), EventClass(class), sessionID
	if attrs.Valid {
		if err := json.Unmarshal([]byte(attrs.String), &e.Attributes); err != nil {
			return StoredEvent{}, err
		}
	}
	return ev, nil
}

// CountEvents reports how many events Events would return for the same
// filters, without loading them, so a consumer can derive a rate's numerator
// or denominator (a count of registered work, per PR-MNT-9/PR-MNT-11) over
// one row-id range cheaply.
func (s *Store) CountEvents(sessionID string, class EventClass, afterID, beforeID int64) (int64, error) {
	query := `SELECT COUNT(*) FROM events WHERE session_id=? AND id>?`
	args := []any{sessionID, afterID}
	if beforeID > 0 {
		query += ` AND id<=?`
		args = append(args, beforeID)
	}
	if class != "" {
		query += ` AND event_class=?`
		args = append(args, string(class))
	}
	var count int64
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// AppendAction validates then writes one control action and returns its id.
func (s *Store) AppendAction(r ControlRecord) (int64, error) {
	if err := r.Validate(); err != nil {
		return 0, err
	}
	if r.Correlations.SessionID == "" {
		return 0, errors.New("obs: action Correlations.SessionID is required")
	}
	res, err := s.db.Exec(`INSERT INTO control_actions (session_id, written_at, action_class, triggering_condition, policy_ref,
		acting_agent, work_unit_id, attempt_id, journal_entry_ref) VALUES (?,?,?,?,?,?,?,?,?)`,
		r.Correlations.SessionID, time.Now().UnixNano(), string(r.ActionClass), r.TriggeringCondition, r.PolicyRef,
		r.ActingAgent, r.Correlations.WorkUnitID, r.Correlations.AttemptID, r.Correlations.JournalEntryRef)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
