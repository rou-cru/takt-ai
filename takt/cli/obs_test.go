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

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/obs"
	// modernc.org/sqlite registers the "sqlite" database/sql driver via init.
	_ "modernc.org/sqlite"
)

// obsIngest drives the public CLI entry once, as a separate invocation, and returns the assigned id.
func obsIngest(t *testing.T, workspace, payload string) (int64, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	if err := run([]string{"obs", "ingest", "--workspace", workspace}, strings.NewReader(payload), &out, &stderr); err != nil {
		return 0, err
	}
	var resp struct {
		OK bool  `json:"ok"`
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil || !resp.OK {
		t.Fatalf("response %q: %v", out.String(), err)
	}
	return resp.ID, nil
}

func obsEvents(t *testing.T, workspace, session string) []obs.StoredEvent {
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
	rows, err := db.Query(`SELECT id, written_at, schema_version, wall_timestamp, source, acting_agent, event_class,
		work_unit_id, attempt_id, journal_entry_ref, attributes FROM events WHERE session_id=? AND id>? ORDER BY id LIMIT -1`,
		session, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close rows: %v", err)
		}
	})
	var events []obs.StoredEvent
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
		e.Source, e.EventClass, e.Correlations.SessionID = obs.SourcePlane(source), obs.EventClass(class), session
		if attrs.Valid {
			if err := json.Unmarshal([]byte(attrs.String), &e.Attributes); err != nil {
				t.Fatal(err)
			}
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

// queryStoredActions reads one session's control actions back with raw SQL, oldest first.
func queryStoredActions(t *testing.T, workspace, session string) []obs.StoredAction {
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
	rows, err := db.Query(`SELECT id, written_at, action_class, triggering_condition, policy_ref, acting_agent,
		work_unit_id, attempt_id, journal_entry_ref FROM control_actions WHERE session_id=? ORDER BY id`, session)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close rows: %v", err)
		}
	})
	var actions []obs.StoredAction
	for rows.Next() {
		var (
			a       obs.StoredAction
			written int64
			class   string
		)
		r := &a.Record
		if err := rows.Scan(&a.ID, &written, &class, &r.TriggeringCondition, &r.PolicyRef, &r.ActingAgent,
			&r.Correlations.WorkUnitID, &r.Correlations.AttemptID, &r.Correlations.JournalEntryRef); err != nil {
			t.Fatal(err)
		}
		a.WrittenAt = time.Unix(0, written)
		r.ActionClass, r.Correlations.SessionID = obs.ActionClass(class), session
		actions = append(actions, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return actions
}

func TestObsIngestPersistsAndKeepsOrderAcrossInvocations(t *testing.T) {
	ws := t.TempDir()
	first, err := obsIngest(t, ws, `{"ipc_version":1,"session_id":"s1","work_unit_id":"u1","attempt_id":"a1","agent":"orch","source":"takt.orchestration","event_class":"dispatch","attributes":{"decision":"dispatch","specialist":"dev","parallelism":2}}`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := obsIngest(t, ws, `{"ipc_version":1,"session_id":"s1","agent":"author","source":"takt.platform","event_class":"tool_activity","attributes":{"platform":"opencode","tool":"bash","phase":"end","duration_ms":12}}`)
	if err != nil {
		t.Fatal(err)
	}
	if second <= first {
		t.Fatalf("ids not increasing: %d then %d", first, second)
	}
	events := obsEvents(t, ws, "s1")
	if len(events) != 2 || events[0].ID != first || events[1].ID != second {
		t.Fatalf("stream = %+v", events)
	}
	e := events[0].Envelope
	if e.EventClass != obs.EventDispatch || e.Source != obs.PlaneOrchestration || e.Authority != "orch" ||
		e.Correlations.WorkUnitID != "u1" || e.Correlations.AttemptID != "a1" || e.Attributes["specialist"] != "dev" {
		t.Fatalf("first event = %+v", e)
	}
	if events[1].Envelope.EventClass != obs.EventToolActivity || events[1].Envelope.Source != obs.PlanePlatform {
		t.Fatalf("second event = %+v", events[1].Envelope)
	}
}

func TestObsIngestRejectsInvalidWithoutWriting(t *testing.T) {
	ws := t.TempDir()
	base := func(class, source, attrs string) string {
		return `{"ipc_version":1,"session_id":"s1","agent":"a","source":"` + source + `","event_class":"` + class + `","attributes":` + attrs + `}`
	}
	cases := map[string]string{
		"vfs class":             base("vfs_delta", "takt.vfs", `{}`),
		"control class":         base("control_gate", "takt.control", `{"policy_ref":"p","triggering_condition":"c"}`),
		"source/class mismatch": base("dispatch", "takt.platform", `{}`),
		"attribute not listed":  base("dispatch", "takt.orchestration", `{"decision":"x","note":"y"}`),
		"content attribute":     base("tool_activity", "takt.platform", `{"content":"secret text"}`),
		"nested attribute":      base("dispatch", "takt.orchestration", `{"decision":{"a":1}}`),
		"unknown field":         `{"ipc_version":1,"session_id":"s1","agent":"a","source":"takt.orchestration","event_class":"dispatch","extra":1}`,
		"missing session_id":    `{"ipc_version":1,"agent":"a","source":"takt.orchestration","event_class":"dispatch"}`,
		"missing agent":         `{"ipc_version":1,"session_id":"s1","source":"takt.orchestration","event_class":"dispatch"}`,
		"wrong ipc_version":     `{"ipc_version":2,"session_id":"s1","agent":"a","source":"takt.orchestration","event_class":"dispatch"}`,
		"two values":            base("dispatch", "takt.orchestration", `{}`) + base("dispatch", "takt.orchestration", `{}`),
	}
	for name, payload := range cases {
		if _, err := obsIngest(t, ws, payload); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, session := range []string{"s1", ""} {
		if n := len(obsEvents(t, ws, session)); n != 0 {
			t.Errorf("session %q: %d events written by rejected requests", session, n)
		}
	}
}
