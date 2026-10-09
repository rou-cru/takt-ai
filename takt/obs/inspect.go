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
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"
)

// otherAccessBits are the permission bits that let anyone but the owner reach the store.
const otherAccessBits os.FileMode = 0o077

// storeTables are the tables a usable event store holds.
var storeTables = []string{"events", "control_actions"}

// StoreHealth is what a read-only look at a workspace's event store found.
type StoreHealth struct {
	// Exists is false when the workspace has no event store yet, which is normal before its first session.
	Exists bool
	// Private reports that only the owner can reach the file, as OpenStore creates it.
	Private bool
	// Events counts the recorded events across all sessions.
	Events int64
	// LastEventAt is when the latest event was written; zero when none was.
	LastEventAt time.Time
}

// InspectStore checks the workspace event store without changing anything, so a
// health check never creates the store or its directory. It fails when the file
// is not readable as a database, is missing a table, or fails SQLite's
// integrity check: such a store cannot be relied on.
func InspectStore(workspace string) (health StoreHealth, err error) {
	path, err := StorePath(workspace)
	if err != nil {
		return StoreHealth{}, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return StoreHealth{}, nil
	}
	if err != nil {
		return StoreHealth{}, err
	}
	health = StoreHealth{Exists: true, Private: info.Mode().Perm()&otherAccessBits == 0}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String())
	if err != nil {
		return health, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var verdict string
	if err = db.QueryRow(`PRAGMA quick_check`).Scan(&verdict); err != nil {
		return health, err
	}
	if verdict != "ok" {
		return health, fmt.Errorf("obs: event store failed its integrity check: %s", verdict)
	}
	for _, table := range storeTables {
		var found string
		if err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found); err != nil {
			return health, fmt.Errorf("obs: event store has no %q table: %w", table, err)
		}
	}
	var last sql.NullInt64
	if err = db.QueryRow(`SELECT COUNT(*), MAX(written_at) FROM events`).Scan(&health.Events, &last); err != nil {
		return health, err
	}
	if last.Valid {
		health.LastEventAt = time.Unix(0, last.Int64)
	}
	return health, nil
}
