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

package obs_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/obs"
	// modernc.org/sqlite registers the "sqlite" database/sql driver via init.
	_ "modernc.org/sqlite"
)

func openStore(t *testing.T, ws string) *obs.Store {
	t.Helper()
	s, err := obs.OpenStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// queryEvents reads one session's events back with raw SQL (id order, same
// semantics the store's write path is verified against).
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

func TestStorePersistsAcrossReopenInIDOrder(t *testing.T) {
	ws := t.TempDir()
	clock := obs.NewClock()
	s := openStore(t, ws)
	var want []obs.Envelope
	for _, unit := range []string{"a", "b", "c"} {
		e := newEnv(clock, obs.PlaneVFS, "agent-a")
		e.Correlations = obs.CorrelationIDs{SessionID: "s1", WorkUnitID: unit, AttemptID: "2", JournalEntryRef: "j-" + unit}
		e.Attributes = map[string]any{"operation": "create"}
		if _, err := s.AppendEvent(e); err != nil {
			t.Fatal(err)
		}
		want = append(want, e)
	}
	// An invalid envelope must not be written.
	bad := newEnv(clock, obs.PlaneVFS, "agent-a")
	bad.Correlations.SessionID = "s1"
	bad.Attributes = map[string]any{"content": "x"}
	if _, err := s.AppendEvent(bad); !errors.Is(err, obs.ErrContentForbidden) {
		t.Fatalf("err = %v; want ErrContentForbidden", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	got := queryEvents(t, ws, "s1", 0, 0)
	if len(got) != len(want) {
		t.Fatalf("read %d events; want %d", len(got), len(want))
	}
	for i, g := range got {
		w := want[i]
		if i > 0 && g.ID <= got[i-1].ID {
			t.Errorf("ids not increasing: %d then %d", got[i-1].ID, g.ID)
		}
		if g.Envelope.Correlations != w.Correlations || g.Envelope.Authority != w.Authority || g.Envelope.Source != w.Source ||
			g.Envelope.EventClass != w.EventClass || g.Envelope.SchemaVersion != w.SchemaVersion ||
			!g.Envelope.WallTimestamp.Equal(w.WallTimestamp) || !reflect.DeepEqual(g.Envelope.Attributes, w.Attributes) {
			t.Errorf("event %d differs:\n got %+v\nwant %+v", i, g.Envelope, w)
		}
	}
	// Resume from an id, with a limit.
	page := queryEvents(t, ws, "s1", got[0].ID, 1)
	if len(page) != 1 || page[0].ID != got[1].ID {
		t.Fatalf("page = %+v", page)
	}
	// Other sessions are isolated.
	if other := queryEvents(t, ws, "s2", 0, 0); len(other) != 0 {
		t.Fatalf("session s2 saw %d events", len(other))
	}
	// The file lives in the workspace state dir.
	if p, _ := obs.StorePath(ws); p != filepath.Join(ws, ".takt-ai", "events.db") {
		t.Errorf("StorePath = %q", p)
	}
}

func TestStoreTwoOpensShareStream(t *testing.T) {
	ws := t.TempDir()
	clock := obs.NewClock()
	first := openStore(t, ws)
	_ = openStore(t, ws) // a second handle shares the same stream
	e := newEnv(clock, obs.PlaneVFS, "agent-a")
	e.Correlations.SessionID = "s1"
	if _, err := first.AppendEvent(e); err != nil {
		t.Fatal(err)
	}
	if got := queryEvents(t, ws, "s1", 0, 0); len(got) != 1 {
		t.Fatalf("second open saw %d events", len(got))
	}
}

// TestStoreEventsAndCountEventsFilterAndRange checks the read API a rate
// derivation needs: filtering by class, bounding by id range (never wall
// time, per PR-MNT-9/PR-MNT-11), and Count matching what Events returns.
func TestStoreEventsAndCountEventsFilterAndRange(t *testing.T) {
	ws := t.TempDir()
	clock := obs.NewClock()
	s := openStore(t, ws)

	mk := func(class obs.EventClass) obs.Envelope {
		e := newEnv(clock, obs.PlaneVFS, "agent-a")
		e.EventClass = class
		e.Correlations.SessionID = "s1"
		return e
	}
	var ids []int64
	for _, class := range []obs.EventClass{obs.EventVFSDelta, obs.EventDispatch, obs.EventVFSDelta, obs.EventDispatch, obs.EventVFSDelta} {
		id, err := s.AppendEvent(mk(class))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	// Other sessions never leak into a count or a list.
	if _, err := s.AppendEvent(func() obs.Envelope { e := mk(obs.EventVFSDelta); e.Correlations.SessionID = "s2"; return e }()); err != nil {
		t.Fatal(err)
	}

	assertEventCount(t, s, eventCountCase{"s1", obs.EventVFSDelta, 0, 0, 3, "VFSDelta"})
	assertEventCount(t, s, eventCountCase{"s1", obs.EventDispatch, 0, 0, 2, "Dispatch"})
	assertEventCount(t, s, eventCountCase{"s1", "", 0, 0, 5, "any class"})
	// Bounded to a work-registered range (the first two ids only).
	assertEventCount(t, s, eventCountCase{"s1", "", 0, ids[1], 2, "bounded"})
	assertEventCount(t, s, eventCountCase{"s2", obs.EventVFSDelta, 0, 0, 1, "other session"})

	assertEventsAllOfClass(t, s, "s1", obs.EventVFSDelta, 3)
	if page, err := s.Events("s1", "", ids[0], 0, 1); err != nil || len(page) != 1 || page[0].ID != ids[1] {
		t.Fatalf("Events paged from %d limit 1 = %+v, %v", ids[0], page, err)
	}
}

// eventCountCase is one CountEvents assertion: the session and class filter,
// the bounded range, the expected count, and a label identifying which case
// regressed on failure.
type eventCountCase struct {
	Session string
	Class   obs.EventClass
	From    int64
	To      int64
	Want    int64
	Label   string
}

// assertEventCount checks s.CountEvents(c.Session, c.Class, c.From, c.To)
// equals c.Want, failing with c.Label identifying which case regressed.
func assertEventCount(t *testing.T, s *obs.Store, c eventCountCase) {
	t.Helper()
	if n, err := s.CountEvents(c.Session, c.Class, c.From, c.To); err != nil || n != c.Want {
		t.Fatalf("CountEvents %s = %d, %v; want %d, nil", c.Label, n, err, c.Want)
	}
}

// assertEventsAllOfClass checks s.Events returns exactly want rows for
// session and class, and that every returned row is actually that class.
func assertEventsAllOfClass(t *testing.T, s *obs.Store, session string, class obs.EventClass, want int) {
	t.Helper()
	got, err := s.Events(session, class, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != want {
		t.Fatalf("Events %s returned %d rows; want %d", class, len(got), want)
	}
	for _, e := range got {
		if e.Envelope.EventClass != class {
			t.Errorf("Events leaked class %q", e.Envelope.EventClass)
		}
	}
}

func TestNewClassesRejectAttributesOutsideAllowlist(t *testing.T) {
	clock := obs.NewClock()
	for _, class := range []obs.EventClass{obs.EventDispatch, obs.EventUnitLifecycle, obs.EventToolActivity,
		obs.EventGCFindingUnapplied, obs.EventProblemRate} {
		e := obs.NewEnvelope(clock, obs.PlaneOrchestration, "agent-a")
		e.EventClass = class
		e.Attributes = map[string]any{"note": "free text"}
		if err := e.Validate(); err == nil {
			t.Errorf("%s accepted an unlisted attribute", class)
		}
		e.Attributes = map[string]any{"count": map[string]any{"nested": "x"}}
		if err := e.Validate(); err == nil {
			t.Errorf("%s accepted a nested value", class)
		}
	}
	e := obs.NewEnvelope(clock, obs.PlanePlatform, "agent-a")
	e.EventClass = obs.EventToolActivity
	e.Attributes = map[string]any{"tool": "edit", "duration_ms": 12}
	if err := e.Validate(); err != nil {
		t.Errorf("listed attributes rejected: %v", err)
	}
}
