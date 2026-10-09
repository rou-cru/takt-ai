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

package session_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/session"
	"github.com/rou-cru/takt-ai/takt/vfs"
	// modernc.org/sqlite registers the "sqlite" database/sql driver via init.
	_ "modernc.org/sqlite"
)

func newDurableSession(t *testing.T) (*session.Session, string) {
	t.Helper()
	root := t.TempDir()
	s, err := session.NewDurable(root, filepath.Join(t.TempDir(), "private"), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.FS.Close() })
	return s, root
}

// bind acquires scope through the production entry point: a trusted
// coordinator's dispatch identity, not tool args.
func bind(t *testing.T, s *session.Session, agent, unit, specialist string, scope ...string) vfs.AgentID {
	t.Helper()
	key, err := s.FS.Bind(vfs.Identity{
		SessionID: "session-1", WorkUnitID: unit, AttemptID: "1",
		AgentID: vfs.AgentID(agent), Specialist: specialist, InvariantsHash: "h",
	}, scope)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// queryEvents reads one session's events back with raw SQL in id order.
func queryEvents(t *testing.T, workspace, sessionID string, afterID int64, limit int) []obs.StoredEvent {
	t.Helper()
	path, err := obs.StorePath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatal(err)
	}
	arg := limit
	if arg <= 0 {
		arg = -1
	}
	rows, err := db.Query(`SELECT id, written_at, schema_version, wall_timestamp, source, acting_agent, event_class,
		work_unit_id, attempt_id, journal_entry_ref, attributes FROM events WHERE session_id=? AND id>? ORDER BY id LIMIT ?`,
		sessionID, afterID, arg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close rows: %v", err)
		}
	})
	var out []obs.StoredEvent
	for rows.Next() {
		var (
			ev            obs.StoredEvent
			written, wall int64
			source, class string
			attrs         sql.NullString
		)
		e := &ev.Envelope
		if err := rows.Scan(&ev.ID, &written, &e.SchemaVersion, &wall, &source, &e.Authority, &class,
			&e.Correlations.WorkUnitID, &e.Correlations.AttemptID, &e.Correlations.JournalEntryRef, &attrs); err != nil {
			t.Fatal(err)
		}
		ev.WrittenAt = time.Unix(0, written)
		if wall != 0 {
			e.WallTimestamp = time.Unix(0, wall)
		}
		e.Source, e.EventClass, e.Correlations.SessionID = obs.SourcePlane(source), obs.EventClass(class), sessionID
		if attrs.Valid {
			if err := json.Unmarshal([]byte(attrs.String), &e.Attributes); err != nil {
				t.Fatal(err)
			}
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// envelopesOfClass filters the persisted session stream down to one event class.
func envelopesOfClass(t *testing.T, workspace, sessionID string, class obs.EventClass) []obs.Envelope {
	t.Helper()
	var found []obs.Envelope
	for _, e := range queryEvents(t, workspace, sessionID, 0, 0) {
		if e.Envelope.EventClass == class {
			found = append(found, e.Envelope)
		}
	}
	return found
}

func TestNewDurable_RequiresSessionID(t *testing.T) {
	if _, err := session.NewDurable(t.TempDir(), filepath.Join(t.TempDir(), "private"), ""); err == nil {
		t.Error("NewDurable with empty sessionID = nil error; want a failure")
	}
}

func TestJournalEntryReachesBusAsReference(t *testing.T) {
	// The envelope references the journal entry, never
	// duplicates it, and no file content lands on the bus.
	s, root := newDurableSession(t)
	key := bind(t, s, "agent-a", "wu-1", "dev", "pkg/file.go")
	if _, err := s.FS.Apply(vfs.Operation{Key: key, CallID: "c1", Action: vfs.OpCreate, Path: "pkg/file.go", Content: []byte("package pkg\n")}); err != nil {
		t.Fatal(err)
	}

	events := envelopesOfClass(t, root, "session-1", obs.EventVFSDelta)
	if len(events) != 1 {
		t.Fatalf("vfs_delta events = %d; want 1", len(events))
	}
	e := events[0]

	journal := s.FS.JournalPage("session-1", "wu-1", -1, 10)
	if len(journal) != 1 {
		t.Fatalf("journal entries = %d; want 1", len(journal))
	}
	if e.Correlations.JournalEntryRef != journal[0].Ref() {
		t.Errorf("JournalEntryRef = %q; want %q", e.Correlations.JournalEntryRef, journal[0].Ref())
	}
	if e.Correlations.SessionID != "session-1" || e.Correlations.WorkUnitID != "wu-1" {
		t.Errorf("correlations = %+v; want session-1 / wu-1", e.Correlations)
	}
	if e.Authority != "agent-a" {
		t.Errorf("ActingAgent = %q; want %q", e.Authority, "agent-a")
	}
	if got := e.Attributes["operation"]; got != string(vfs.OpCreate) {
		t.Errorf("operation attribute = %v; want %q", got, vfs.OpCreate)
	}
	for key, value := range e.Attributes {
		if s, ok := value.(string); ok && strings.Contains(s, "package pkg") {
			t.Errorf("attribute %q leaked file content", key)
		}
	}
	// The hashes stay in the journal, which the envelope points at.
	if journal[0].AfterHash == "" {
		t.Error("journal entry has no AfterHash")
	}
}

func TestJournalStreamStaysInOperationOrder(t *testing.T) {
	// Envelope events must reach the stream in operation order.
	s, root := newDurableSession(t)
	key := bind(t, s, "agent-a", "wu-1", "dev", "a.go")
	r, err := s.FS.Apply(vfs.Operation{Key: key, CallID: "c1", Action: vfs.OpCreate, Path: "a.go", Content: []byte("a")})
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.FS.Apply(vfs.Operation{Key: key, CallID: "c2", ExpectedRevision: r.Revision, Action: vfs.OpRead, Path: "a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FS.Apply(vfs.Operation{Key: key, CallID: "c3", ExpectedRevision: r.Revision, Action: vfs.OpDelete, Path: "a.go"}); err != nil {
		t.Fatal(err)
	}

	want := []string{string(vfs.OpCreate), string(vfs.OpRead), string(vfs.OpDelete)}
	events := envelopesOfClass(t, root, "session-1", obs.EventVFSDelta)
	if len(events) != len(want) {
		t.Fatalf("vfs_delta events = %d; want %d", len(events), len(want))
	}
	for i, op := range want {
		if got := events[i].Attributes["operation"]; got != op {
			t.Errorf("event %d operation = %v; want %q", i, got, op)
		}
	}
}

func TestCollisionReachesBus(t *testing.T) {
	// A collision emits a typed event to the orchestrator.
	s, root := newDurableSession(t)
	_ = bind(t, s, "agent-a", "wu-1", "dev", "owned.go")
	if _, err := s.FS.Bind(vfs.Identity{
		SessionID: "session-1", WorkUnitID: "wu-2", AttemptID: "1",
		AgentID: "agent-b", Specialist: "dev", InvariantsHash: "h",
	}, []string{"owned.go"}); !errors.Is(err, vfs.ErrCollision) {
		t.Fatalf("Bind = %v; want ErrCollision", err)
	}

	events := envelopesOfClass(t, root, "session-1", obs.EventCollision)
	if len(events) != 1 {
		t.Fatalf("collision events = %d; want 1", len(events))
	}
	e := events[0]
	if e.Authority != "agent-b" {
		t.Errorf("ActingAgent = %q; want %q", e.Authority, "agent-b")
	}
	if e.Attributes["owning_agent"] != "agent-a" || e.Attributes["path"] != "owned.go" {
		t.Errorf("attributes = %+v; want owning_agent=%q path=%q", e.Attributes, "agent-a", "owned.go")
	}
}

func TestConsolidateReportsFlushOnBus(t *testing.T) {
	// A consolidated delta is the progress evidence a completion
	// claim is corroborated against.
	s, root := newDurableSession(t)
	key := bind(t, s, "agent-a", "wu-1", "dev", "out.txt")
	r, err := s.FS.Apply(vfs.Operation{Key: key, CallID: "c1", Action: vfs.OpCreate, Path: "out.txt", Content: []byte("done")})
	if err != nil {
		t.Fatal(err)
	}
	verifierIdentity := vfs.Identity{
		SessionID: "session-1", WorkUnitID: "wu-1", AttemptID: "1",
		AgentID: "agent-b", Specialist: "verify", InvariantsHash: "h",
	}
	if _, err := s.FS.AssignVerifier(verifierIdentity, key); err != nil {
		t.Fatal(err)
	}
	verifierIdentity.GateAuthorKey = key
	verifier, err := s.FS.Bind(verifierIdentity, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FS.Verify(verifier, key, "gate", r.Revision, r.DeltaHash, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.FS.ConsolidateCheckpoint(key, "checkpoint", r.Revision, false); err != nil {
		t.Fatal(err)
	}

	var flushes int
	for _, e := range envelopesOfClass(t, root, "session-1", obs.EventVFSDelta) {
		if e.Attributes["operation"] == string(vfs.OpFlush) {
			flushes++
		}
	}
	if flushes != 1 {
		t.Errorf("flush events on bus = %d; want 1", flushes)
	}
}

func TestDurableBusCorrelatesConcurrentUnitsPerOperation(t *testing.T) {
	root := t.TempDir()
	s, err := session.NewDurable(root, filepath.Join(t.TempDir(), "private"), "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.FS.Close(); err != nil {
			t.Errorf("close filesystem: %v", err)
		}
	})
	var wg sync.WaitGroup
	for _, unit := range []string{"left", "right"} {
		key, e := s.FS.Bind(vfs.Identity{SessionID: "s", WorkUnitID: unit, AttemptID: "1", AgentID: "same-author", Specialist: "dev", InvariantsHash: "h"}, []string{unit})
		if e != nil {
			t.Fatal(e)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.FS.Apply(vfs.Operation{Key: key, CallID: unit, Action: vfs.OpCreate, Path: unit, Content: []byte("private")}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	events := envelopesOfClass(t, root, "s", obs.EventVFSDelta)
	if len(events) != 2 {
		t.Fatalf("events %d", len(events))
	}
	journal := s.FS.JournalPage("s", "", -1, 100)
	for i, e := range events {
		if e.Correlations.WorkUnitID != journal[i].WorkUnitID || e.Correlations.JournalEntryRef != journal[i].Ref() {
			t.Fatalf("crossed correlation: %+v", e.Correlations)
		}
	}
}

func TestDurableSessionPersistsVFSEventsForLaterInvocations(t *testing.T) {
	root := t.TempDir()
	s, err := session.NewDurable(root, filepath.Join(t.TempDir(), "private"), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	key := bind(t, s, "author", "unit", "dev", "unit")
	if _, err := s.FS.Apply(vfs.Operation{Key: key, CallID: "c1", Action: vfs.OpCreate, Path: "unit", Content: []byte("private")}); err != nil {
		t.Fatal(err)
	}
	live := envelopesOfClass(t, root, "session-1", obs.EventVFSDelta)
	if len(live) == 0 {
		t.Fatal("no vfs event on the bus")
	}
	if err := errors.Join(s.Close(), s.FS.Close()); err != nil {
		t.Fatal(err)
	}

	// A later invocation opens the same workspace store and reads what the first one wrote.
	got := queryEvents(t, root, "session-1", 0, 0)
	var persisted []obs.StoredEvent
	for _, e := range got {
		if e.Envelope.EventClass == obs.EventVFSDelta {
			persisted = append(persisted, e)
		}
	}
	if len(persisted) != len(live) {
		t.Fatalf("persisted %d vfs events; bus had %d", len(persisted), len(live))
	}
	if persisted[0].Envelope.Correlations.JournalEntryRef != live[0].Correlations.JournalEntryRef || persisted[0].Envelope.Correlations.WorkUnitID != "unit" {
		t.Errorf("persisted correlations differ: %+v vs %+v", persisted[0].Envelope.Correlations, live[0].Correlations)
	}
}
