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
	"database/sql"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/obs"
)

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
