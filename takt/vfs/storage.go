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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
	// modernc.org/sqlite registers the "sqlite" database/sql driver via init.
	_ "modernc.org/sqlite"
)

const (
	// stateDirectoryMode prevents group and other users from inspecting VFS state.
	stateDirectoryMode os.FileMode = 0700
	// stateFileMode prevents group and other users from reading the VFS database.
	stateFileMode os.FileMode = 0600
)

// MaxJournalPageSize bounds the number of entries JournalPage returns.
const MaxJournalPageSize = 500

type storedDelta struct {
	Files    map[string][]byte
	Bases    map[string]baseFile
	Verdict  *VerificationVerdict
	Revision uint64
}

type storedState struct {
	JournalCount int
	Version      int
	Workspace    string
	Deltas       map[AgentID]storedDelta
	Owners       map[string]AgentID
	Collisions   []CollisionEvent
	Bindings     map[AgentID]Identity
	Calls        map[string]bool
	Recovery     *recoveryManifest
	// Cycles is absent in stores written before maintenance cycles retained
	// what they overwrote; such a store simply has nothing to discard.
	Cycles map[string]*cycleSnapshot
}

// Open opens durable staging under an advisory lock on the workspace directory.
func Open(rootDir, stateDir string) (_ *FS, err error) {
	f, err := newFS(rootDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, f.Close())
		}
	}()
	if err = f.lockWorkspace(); err != nil {
		return nil, err
	}
	if stateDir, err = resolveStateDir(stateDir, f.rootDir); err != nil {
		return nil, err
	}
	if err = f.openStateDB(stateDir); err != nil {
		return nil, err
	}
	if err = f.loadState(); err != nil {
		return nil, err
	}
	return f, nil
}

// lockWorkspace canonicalizes the workspace root and takes the exclusive advisory lock.
func (f *FS) lockWorkspace() (err error) {
	canonical, err := filepath.EvalSymlinks(f.rootDir)
	if err != nil {
		return err
	}
	f.rootDir = canonical
	if f.workspaceLock, err = f.root.Open("."); err != nil {
		return err
	}
	if err = unix.Flock(int(f.workspaceLock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("vfs: workspace already governed: %w", err)
	}
	return nil
}

// resolveStateDir returns the private state directory with symlinks resolved.
func resolveStateDir(stateDir, workspace string) (string, error) {
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		return "", err
	}
	// Resolve the existing ancestor before creating anything, so a state path
	// through a symlink into the workspace cannot mutate it during setup.
	ancestor, err := nearestExistingAncestor(abs)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	suffix, err := filepath.Rel(ancestor, abs)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(resolved, suffix)
	if err := ensureOutsideWorkspace(workspace, dir); err != nil {
		return "", err
	}
	if err = os.MkdirAll(dir, stateDirectoryMode); err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("vfs: state directory must be private (0700)")
	}
	return dir, nil
}

func ensureOutsideWorkspace(workspace, dir string) error {
	if rel, err := filepath.Rel(workspace, dir); err == nil && (rel == "." || filepath.IsLocal(rel)) {
		return fmt.Errorf("vfs: state directory must be outside workspace")
	}
	return nil
}

func nearestExistingAncestor(p string) (string, error) {
	for {
		if _, err := os.Lstat(p); err == nil {
			return p, nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", ErrInvalidPath
		}
		p = parent
	}
}

const stateSchema = `PRAGMA journal_mode=DELETE; PRAGMA synchronous=EXTRA;
 CREATE TABLE IF NOT EXISTS state (id INTEGER PRIMARY KEY CHECK(id=1), data BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS journal (seq INTEGER PRIMARY KEY, data BLOB NOT NULL);
 CREATE TRIGGER IF NOT EXISTS journal_no_update BEFORE UPDATE ON journal BEGIN SELECT RAISE(ABORT,'append-only journal'); END;
 CREATE TRIGGER IF NOT EXISTS journal_no_delete BEFORE DELETE ON journal BEGIN SELECT RAISE(ABORT,'append-only journal'); END;`

// openStateDB opens the private SQLite store, creating it if absent.
func (f *FS) openStateDB(stateDir string) (err error) {
	dbPath := filepath.Join(stateDir, "vfs.sqlite")
	if err = ensurePrivateFile(dbPath); err != nil {
		return err
	}
	if f.db, err = sql.Open("sqlite", dbPath); err != nil {
		return err
	}
	f.db.SetMaxOpenConns(1)
	_, err = f.db.Exec(stateSchema)
	return err
}

// ensurePrivateFile creates path with mode 0600, or accepts an existing
// regular file that is not accessible to group or others.
func ensurePrivateFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, stateFileMode)
	if err == nil {
		return file.Close()
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return ErrInvalidPath
	}
	return nil
}

// loadState adopts the stored state and journal, or persists empty state on first use.
func (f *FS) loadState() error {
	var data []byte
	err := f.db.QueryRow("SELECT data FROM state WHERE id=1").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return f.persistLocked()
	}
	if err != nil {
		return err
	}
	var s storedState
	if err = json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s.Version != 1 || s.Workspace != f.rootDir {
		return fmt.Errorf("vfs: incompatible store or workspace")
	}
	f.staged = make(map[AgentID]*agentDelta)
	for key, d := range s.Deltas {
		f.staged[key] = &agentDelta{files: d.Files, bases: d.Bases, verdict: d.Verdict, revision: d.Revision}
	}
	f.owners = s.Owners
	// Saved collisions are observations, not pending work or notifications.
	f.collisions = nil
	f.bindings = s.Bindings
	f.calls = s.Calls
	f.recovery = s.Recovery
	f.cycles = s.Cycles
	if f.cycles == nil {
		f.cycles = make(map[string]*cycleSnapshot)
	}
	if err = f.replayJournal(s.JournalCount); err != nil {
		return err
	}
	f.persisted = len(f.journal)
	f.notifiedCollisions = 0
	return nil
}

// replayJournal reads the append-only journal.
func (f *FS) replayJournal(count int) (err error) {
	rows, err := f.db.Query("SELECT data FROM journal ORDER BY seq")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		var e JournalEntry
		if err = json.Unmarshal(raw, &e); err != nil {
			return err
		}
		if e.Seq != len(f.journal) {
			return fmt.Errorf("vfs: journal sequence corrupt")
		}
		f.journal = append(f.journal, e)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(f.journal) != count {
		return fmt.Errorf("vfs: journal incomplete")
	}
	return nil
}

func (f *FS) persistLocked() (err error) {
	s := storedState{JournalCount: len(f.journal), Version: 1, Workspace: f.rootDir, Deltas: make(map[AgentID]storedDelta), Owners: f.owners, Collisions: f.collisions, Bindings: f.bindings, Calls: f.calls, Recovery: f.recovery, Cycles: f.cycles}
	for key, d := range f.staged {
		s.Deltas[key] = storedDelta{Files: d.files, Bases: d.bases, Verdict: d.verdict, Revision: d.revision}
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tx, err := f.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	if _, err = tx.Exec("INSERT INTO state(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", data); err != nil {
		return err
	}
	for _, e := range f.journal[f.persisted:] {
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO journal(seq,data) VALUES(?,?)", e.Seq, raw); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (f *FS) readyLocked() error {
	if f.fault != nil {
		return fmt.Errorf("%w: %v", ErrStoreFailed, f.fault)
	}
	if f.recovery != nil {
		return ErrRecoveryRequired
	}
	return nil
}

func (f *FS) finishLocked(result *error) {
	if f.fault != nil {
		*result = errors.Join(*result, ErrStoreFailed)
		return
	}
	if err := f.persistLocked(); err != nil {
		f.fault = err
		*result = errors.Join(*result, ErrStoreFailed, err)
		return
	}
	if f.journalHandler != nil {
		for _, e := range f.journal[f.persisted:] {
			f.journalHandler(e)
		}
	}
	if f.collisionHandler != nil {
		for _, e := range f.collisions[min(f.notifiedCollisions, len(f.collisions)):] {
			f.collisionHandler(e)
		}
	}
	f.persisted = len(f.journal)
	f.collisions = nil
	f.notifiedCollisions = 0
}

// Close releases all handles. It never discards staged state or recovery data.
func (f *FS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var err error
	if f.db != nil {
		err = errors.Join(err, f.db.Close())
		f.db = nil
	}
	if f.workspaceLock != nil {
		err = errors.Join(err, f.workspaceLock.Close())
		f.workspaceLock = nil
	}
	if f.root != nil {
		err = errors.Join(err, f.root.Close())
		f.root = nil
	}
	f.fault = os.ErrClosed
	return err
}

// JournalPage returns bounded, content-free journal entries.
func (f *FS) JournalPage(sessionID, unitID string, after, limit int) []JournalEntry {
	f.mu.RLock()
	defer f.mu.RUnlock()
	limit = max(1, min(limit, MaxJournalPageSize))
	result := make([]JournalEntry, 0, limit)
	for _, e := range f.journal[:f.persisted] {
		if e.Seq > after && (sessionID == "" || e.SessionID == sessionID) && (unitID == "" || e.WorkUnitID == unitID) {
			result = append(result, e)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}
