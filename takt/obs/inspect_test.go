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
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
)

func TestInspectStoreAbsentStoreIsNotCreated(t *testing.T) {
	ws := t.TempDir()
	health, err := obs.InspectStore(ws)
	if err != nil {
		t.Fatalf("InspectStore() error = %v, want none for a workspace without a store", err)
	}
	if health.Exists {
		t.Error("Exists = true for a workspace without a store")
	}
	if _, err := os.Stat(filepath.Join(ws, obs.StateDirName)); !os.IsNotExist(err) {
		t.Errorf("inspecting created %s: stat error = %v", obs.StateDirName, err)
	}
}

func TestInspectStoreReportsRecordedEvents(t *testing.T) {
	ws := t.TempDir()
	s := openStore(t, ws)
	e := newEnv(obs.NewClock(), obs.PlaneVFS, "agent-a")
	e.Correlations.SessionID = "s1"
	for range 2 {
		if _, err := s.AppendEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	health, err := obs.InspectStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	if !health.Exists || !health.Private || health.Events != 2 || health.LastEventAt.IsZero() {
		t.Errorf("health = %+v, want an existing private store with 2 events and a last event time", health)
	}
}

func TestInspectStoreEmptyStoreHasNoLastEvent(t *testing.T) {
	ws := t.TempDir()
	openStore(t, ws)
	health, err := obs.InspectStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	if !health.Exists || health.Events != 0 || !health.LastEventAt.IsZero() {
		t.Errorf("health = %+v, want an existing store with no events", health)
	}
}

func TestInspectStoreFlagsLooseMode(t *testing.T) {
	ws := t.TempDir()
	openStore(t, ws)
	path, err := obs.StorePath(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	health, err := obs.InspectStore(ws)
	if err != nil {
		t.Fatal(err)
	}
	if health.Private {
		t.Error("Private = true for a world-readable store")
	}
}

func TestInspectStoreRejectsUnusableStores(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, path string)
	}{
		{"not a database", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("this is not a sqlite database, not even close to one"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing table", func(t *testing.T, path string) {
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Errorf("close database: %v", err)
				}
			}()
			if _, err := db.Exec(`CREATE TABLE events (id INTEGER PRIMARY KEY, written_at INTEGER)`); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws := t.TempDir()
			path, err := obs.StorePath(ws)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), obs.PrivateTelemetryDirectoryMode); err != nil {
				t.Fatal(err)
			}
			tc.write(t, path)
			if _, err := obs.InspectStore(ws); err == nil {
				t.Error("InspectStore() error = nil, want a failure for an unusable store")
			}
		})
	}
}
