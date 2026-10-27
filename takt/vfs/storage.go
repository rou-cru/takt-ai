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
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/internal/flock"
	// modernc.org/sqlite registers the "sqlite" database/sql driver via init.
	_ "modernc.org/sqlite"
)

const (
	// stateDirectoryMode prevents group and other users from inspecting VFS state.
	stateDirectoryMode os.FileMode = 0700
	// stateFileMode prevents group and other users from reading the VFS database.
	stateFileMode os.FileMode = 0600
	// stateDBFile is the VFS database's name inside the private state directory.
	stateDBFile = "vfs.sqlite"
)

// MaxJournalPageSize bounds the number of entries JournalPage returns.
const MaxJournalPageSize = history.MaxPageSize

type storedDelta struct {
	Files map[string][]byte
	Bases map[string]baseFile
	// Verdict is the single verdict older stores kept; it loads under its verifier.
	Verdict  *VerificationVerdict `json:",omitempty"`
	Verdicts map[AgentID]*VerificationVerdict
	Revision uint64
}

type storedState struct {
	Maintenance  json.RawMessage `json:",omitempty"`
	JournalCount int
	Version      int
	Workspace    string
	Deltas       map[AgentID]storedDelta
	Owners       map[string]AgentID
	Collisions   []CollisionEvent
	Bindings     map[AgentID]Identity
	Calls        map[string]bool
	Recovery     *recoveryManifest
	Interrupted  map[string]interruptedEvidence `json:",omitempty"`
	// Cycles is absent in stores written before maintenance cycles retained
	// what they overwrote; such a store simply has nothing to discard.
	Cycles map[string]*cycleSnapshot
	// ShellCalls is absent in stores written before shell preparation recorded
	// its binding; such a store has no command awaiting import.
	ShellCalls map[string]shellCall `json:",omitempty"`
}

// workspaceLockWait bounds how long Open waits for another process holding the
// workspace lock: long enough to queue behind any single harness command,
// short enough that a stuck holder surfaces as a retryable refusal.
var workspaceLockWait = 30 * time.Second

// ErrWorkspaceBusy is returned when another process held the workspace lock
// for longer than the bounded wait.
var ErrWorkspaceBusy = errors.New("workspace busy")

// Open opens durable staging under an advisory lock on the workspace directory,
// waiting a bounded time for another process holding it.
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
	f.stateDir = stateDir
	if err = f.openStateDB(stateDir); err != nil {
		return nil, err
	}
	if err = f.loadState(); err != nil {
		return nil, err
	}
	return f, nil
}

// lockWorkspace canonicalizes the workspace root and takes the exclusive
// advisory lock, waiting workspaceLockWait for another holder to release it.
func (f *FS) lockWorkspace() (err error) {
	canonical, err := filepath.EvalSymlinks(f.rootDir)
	if err != nil {
		return err
	}
	f.rootDir = canonical
	if f.workspaceLock, err = f.root.Open("."); err != nil {
		return err
	}
	return lockWithin(f.workspaceLock)
}

// lockWithin takes the exclusive advisory lock on held, waiting
// workspaceLockWait for another holder to release it.
func lockWithin(held *os.File) error {
	err := flock.Exclusive(context.Background(), held, workspaceLockWait)
	if errors.Is(err, flock.ErrTimeout) {
		seconds := strconv.FormatFloat(workspaceLockWait.Seconds(), 'f', -1, 64)
		return fmt.Errorf("%w: another harness command held the lock for longer than %s s; retry the same call", ErrWorkspaceBusy, seconds)
	}
	return err
}

// LockWorkspace takes the workspace lock Open takes, without opening the
// store, for a harness command that decides from the execution history under
// the same serialization point. Closing the returned file releases it.
func LockWorkspace(rootDir string) (*os.File, error) {
	canonical, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		return nil, err
	}
	held, err := os.Open(canonical)
	if err != nil {
		return nil, err
	}
	if err = lockWithin(held); err != nil {
		return nil, errors.Join(err, held.Close())
	}
	return held, nil
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
	if dir != history.StateDir(workspace) {
		if err := ensureOutsideWorkspace(workspace, dir); err != nil {
			return "", err
		}
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

// busy_timeout comes first so a write waits, bounded, for another process's
// reader to release the file instead of failing the store.
const stateSchema = `PRAGMA busy_timeout=5000; PRAGMA journal_mode=DELETE; PRAGMA synchronous=EXTRA;
 CREATE TABLE IF NOT EXISTS state (id INTEGER PRIMARY KEY CHECK(id=1), data BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS journal (seq INTEGER PRIMARY KEY, data BLOB NOT NULL);
 CREATE TRIGGER IF NOT EXISTS journal_no_update BEFORE UPDATE ON journal BEGIN SELECT RAISE(ABORT,'append-only journal'); END;
 CREATE TRIGGER IF NOT EXISTS journal_no_delete BEFORE DELETE ON journal BEGIN SELECT RAISE(ABORT,'append-only journal'); END;`

// openStateDB opens the private SQLite store, creating it if absent.
func (f *FS) openStateDB(stateDir string) (err error) {
	dbPath := filepath.Join(stateDir, stateDBFile)
	if err = ensurePrivateFile(dbPath); err != nil {
		return err
	}
	if f.db, err = sql.Open("sqlite", (&url.URL{Scheme: "file", Path: dbPath}).String()); err != nil {
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
		verdicts := d.Verdicts
		if d.Verdict != nil {
			if verdicts == nil {
				verdicts = make(map[AgentID]*VerificationVerdict)
			}
			verdicts[d.Verdict.VerifierID] = d.Verdict
		}
		f.staged[key] = &agentDelta{files: d.Files, bases: d.Bases, verdicts: verdicts, revision: d.Revision}
	}
	f.owners = s.Owners
	// Saved collisions are observations, not pending work or notifications.
	f.collisions = nil
	f.bindings = s.Bindings
	f.maintenance = s.Maintenance
	f.shellCalls = s.ShellCalls
	if f.shellCalls == nil {
		f.shellCalls = make(map[string]shellCall)
	}
	f.calls = s.Calls
	f.recovery = s.Recovery
	f.interrupted = s.Interrupted
	if f.interrupted == nil {
		f.interrupted = make(map[string]interruptedEvidence)
	}
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
	// A negative count is a read-only reader, which has no recorded count to check.
	if count >= 0 && len(f.journal) != count {
		return fmt.Errorf("vfs: journal incomplete")
	}
	return nil
}

func (f *FS) persistLocked() (err error) {
	s := storedState{JournalCount: len(f.journal), Version: 1, Workspace: f.rootDir, Deltas: make(map[AgentID]storedDelta), Owners: f.owners, Collisions: f.collisions, Bindings: f.bindings, Calls: f.calls, Recovery: f.recovery, Interrupted: f.interrupted, Cycles: f.cycles, ShellCalls: f.shellCalls, Maintenance: f.maintenance}
	for key, d := range f.staged {
		s.Deltas[key] = storedDelta{Files: d.files, Bases: d.bases, Verdicts: d.verdicts, Revision: d.revision}
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
	return journalPage(f.journal[:f.persisted], sessionID, unitID, after, limit)
}

// journalPage is the one definition of what a page of journal entries holds.
func journalPage(entries []JournalEntry, sessionID, unitID string, after, limit int) []JournalEntry {
	limit = max(1, min(limit, MaxJournalPageSize))
	result := make([]JournalEntry, 0, limit)
	for _, e := range entries {
		if e.Seq > after && (sessionID == "" || e.SessionID == sessionID) && (unitID == "" || e.WorkUnitID == unitID) {
			result = append(result, e)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

// ReadJournal pages the recorded journal read-only, without taking the
// workspace lock or creating a store, so a reader never blocks or writes. An
// absent store has no entries; a malformed existing journal is an error.
func ReadJournal(stateDir, sessionID, unitID string, after, limit int) (_ []JournalEntry, err error) {
	path, err := filepath.Abs(filepath.Join(stateDir, stateDBFile))
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return []JournalEntry{}, nil
	} else if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String())
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	db.SetMaxOpenConns(1)
	f := &FS{db: db}
	if err = f.replayJournal(-1); err != nil {
		return nil, err
	}
	return journalPage(f.journal, sessionID, unitID, after, limit), nil
}
