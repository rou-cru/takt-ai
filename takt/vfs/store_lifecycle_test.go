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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// worldReadableFileMode is a file mode the private store must refuse.
	worldReadableFileMode os.FileMode = 0o644
	// unsupportedStoreVersion is a stored-state version this build cannot read.
	unsupportedStoreVersion = 2
	// currentStoreVersion is the stored-state version this build writes.
	currentStoreVersion = 1
	// foreignWorkspace is a workspace path that is not the one under test.
	foreignWorkspace = "/elsewhere"
	// undecodableJSON is a record no JSON decoder accepts.
	undecodableJSON = "{not json"
	// gappedJournalEntry claims a sequence position the journal never reached.
	gappedJournalEntry = `{"Seq":7}`
	// recordedJournalCount is a journal length larger than what a store holds.
	recordedJournalCount = 3
	// foreignFileBody pads a non-database file past the SQLite header size.
	foreignFileBody = "not a sqlite database, only text; "
	foreignFileReps = 100
)

// withRawStore edits the closed store's tables directly, bypassing the FS.
func withRawStore(t *testing.T, state string, edit func(*sql.DB)) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(state, "vfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close raw store: %v", err)
		}
	}()
	edit(db)
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// assertOpenSucceeds proves a failed Open left the workspace lock released.
func assertOpenSucceeds(t *testing.T, root string) {
	t.Helper()
	f, err := Open(root, filepath.Join(t.TempDir(), "fresh"))
	if err != nil {
		t.Fatalf("workspace stayed locked after a failed Open: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRejectsUnusableWorkspaceRoot(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := Open(missing, state); err == nil || !strings.Contains(err.Error(), "workspace root does not exist") {
		t.Fatalf("Open(missing root) = %v", err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, stateFileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(file, state); err == nil {
		t.Fatal("Open accepted a regular file as the workspace root")
	}
}

func TestOpenRejectsUnusableStateLocation(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare func(t *testing.T, state string)
		want    string
		wantErr error
	}{
		"a regular file": {
			prepare: func(t *testing.T, state string) {
				if err := os.WriteFile(state, nil, stateFileMode); err != nil {
					t.Fatal(err)
				}
			},
		},
		"directory readable by others": {
			prepare: func(t *testing.T, state string) {
				if err := os.Mkdir(state, stateDirectoryMode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(state, recoveryDirectoryMode); err != nil {
					t.Fatal(err)
				}
			},
			want: "must be private",
		},
		"database readable by others": {
			prepare: func(t *testing.T, state string) {
				mustMkdirPrivate(t, state)
				path := filepath.Join(state, "vfs.sqlite")
				if err := os.WriteFile(path, nil, worldReadableFileMode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, worldReadableFileMode); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: ErrInvalidPath,
		},
		"database is a directory": {
			prepare: func(t *testing.T, state string) {
				mustMkdirPrivate(t, state)
				if err := os.Mkdir(filepath.Join(state, "vfs.sqlite"), stateDirectoryMode); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: ErrInvalidPath,
		},
		"database is a symlink": {
			prepare: func(t *testing.T, state string) {
				mustMkdirPrivate(t, state)
				if err := os.Symlink(os.DevNull, filepath.Join(state, "vfs.sqlite")); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: ErrInvalidPath,
		},
		"database is not sqlite": {
			prepare: func(t *testing.T, state string) {
				mustMkdirPrivate(t, state)
				body := strings.Repeat(foreignFileBody, foreignFileReps)
				if err := os.WriteFile(filepath.Join(state, "vfs.sqlite"), []byte(body), stateFileMode); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			state := filepath.Join(t.TempDir(), "state")
			tc.prepare(t, state)
			f, err := Open(root, state)
			if err == nil {
				_ = f.Close()
				t.Fatal("Open accepted an unusable state location")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Open error = %v; want %q", err, tc.want)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("Open error = %v; want %v", err, tc.wantErr)
			}
			assertOpenSucceeds(t, root)
		})
	}
}

func mustMkdirPrivate(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, stateDirectoryMode); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRefusesCorruptStore(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(t *testing.T, db *sql.DB, workspace string)
		want string
	}{
		"undecodable state": {
			edit: func(t *testing.T, db *sql.DB, _ string) {
				mustExec(t, db, "UPDATE state SET data=? WHERE id=1", []byte(undecodableJSON))
			},
			want: "invalid character",
		},
		"unsupported version": {
			edit: func(t *testing.T, db *sql.DB, workspace string) {
				setState(t, db, storedState{Version: unsupportedStoreVersion, Workspace: workspace})
			},
			want: "incompatible store or workspace",
		},
		"another workspace": {
			edit: func(t *testing.T, db *sql.DB, _ string) {
				setState(t, db, storedState{Version: currentStoreVersion, Workspace: foreignWorkspace})
			},
			want: "incompatible store or workspace",
		},
		"journal shorter than recorded": {
			edit: func(t *testing.T, db *sql.DB, workspace string) {
				setState(t, db, storedState{Version: currentStoreVersion, Workspace: workspace, JournalCount: recordedJournalCount})
			},
			want: "journal incomplete",
		},
		"journal sequence gap": {
			edit: func(t *testing.T, db *sql.DB, workspace string) {
				setState(t, db, storedState{Version: currentStoreVersion, Workspace: workspace, JournalCount: 1})
				mustExec(t, db, "INSERT INTO journal(seq,data) VALUES(0,?)", []byte(gappedJournalEntry))
			},
			want: "journal sequence corrupt",
		},
		"undecodable journal entry": {
			edit: func(t *testing.T, db *sql.DB, workspace string) {
				setState(t, db, storedState{Version: currentStoreVersion, Workspace: workspace, JournalCount: 1})
				mustExec(t, db, "INSERT INTO journal(seq,data) VALUES(0,?)", []byte(undecodableJSON))
			},
			want: "invalid character",
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, root, state := durable(t)
			workspace := f.rootDir
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			withRawStore(t, state, func(db *sql.DB) { tc.edit(t, db, workspace) })
			g, err := Open(root, state)
			if err == nil {
				_ = g.Close()
				t.Fatal("Open adopted a corrupt store instead of failing")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Open error = %v; want %q", err, tc.want)
			}
		})
	}
}

func setState(t *testing.T, db *sql.DB, s storedState) {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE state SET data=? WHERE id=1", data)
}

func TestMutatorsRefuseOnceClosed(t *testing.T) {
	f, _, _ := durable(t)
	key := bind(t, f, "author", "unit", "dev", "a.go")
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	identity := Identity{SessionID: "s", WorkUnitID: "other", AgentID: "late", Specialist: "dev"}
	read := Operation{Key: key, CallID: "late-read", Action: OpRead, Path: "a.go"}
	for name, call := range map[string]func() error{
		"Bind":         func() error { _, err := f.Bind(identity, []string{"b.go"}); return err },
		"AssignScope":  func() error { _, err := f.AssignScope(identity, []string{"b.go"}); return err },
		"AssignVerify": func() error { _, err := f.AssignVerifier(identity, key); return err },
		"Revoke":       func() error { return f.RevokeOwnership(key) },
		"Apply":        func() error { _, err := f.Apply(read); return err },
		"ReadAs":       func() error { _, err := f.ReadAs(read, key); return err },
		"Verify":       func() error { return f.Verify(key, key, "late", 0, "", true, "") },
		"Consolidate":  func() error { return f.ConsolidateCheckpoint(key, "late", 0, false) },
		"Recover":      func() error { return f.Recover() },
	} {
		if err := call(); !errors.Is(err, ErrStoreFailed) {
			t.Errorf("%s on a closed store = %v; want ErrStoreFailed", name, err)
		}
	}
}

func TestRecoverWithoutPendingFlushChangesNothing(t *testing.T) {
	f, _, _ := durable(t)
	if err := f.Recover(); err != nil {
		t.Fatalf("Recover() = %v", err)
	}
	if page := f.JournalPage("", "", -1, MaxJournalPageSize); len(page) != 0 {
		t.Fatalf("Recover with nothing pending journaled %+v", page)
	}
}

func TestJournalPageIsBoundedAndFiltered(t *testing.T) {
	f, _, _ := durable(t)
	for _, unit := range []string{"u1", "u2", "u3"} {
		key := bind(t, f, "agent-"+unit, unit, "dev", unit+".go")
		apply(t, f, applyCase{key, "write-" + unit, 0, OpCreate, unit + ".go", unit})
	}
	all := f.JournalPage("", "", -1, MaxJournalPageSize)
	if len(all) != 3 {
		t.Fatalf("journal has %d entries; want one per staged write", len(all))
	}
	if got := f.JournalPage("", "", -1, 0); len(got) != 1 {
		t.Fatalf("a zero limit returned %d entries; want the minimum page of 1", len(got))
	}
	if got := f.JournalPage("", "", all[0].Seq, MaxJournalPageSize); len(got) != 2 || got[0].Seq != all[1].Seq {
		t.Fatalf("page after seq %d = %+v", all[0].Seq, got)
	}
	if got := f.JournalPage("s", "u2", -1, MaxJournalPageSize); len(got) != 1 || got[0].WorkUnitID != "u2" {
		t.Fatalf("unit filter = %+v", got)
	}
	if got := f.JournalPage("other-session", "", -1, MaxJournalPageSize); len(got) != 0 {
		t.Fatalf("session filter leaked %+v", got)
	}
}

func TestReopenDiscardsSavedCollisions(t *testing.T) {
	f, root, state := durable(t)
	bind(t, f, "owner", "u1", "dev", "a.go")
	intruder := bind(t, f, "intruder", "u2", "dev", "b.go")
	if _, err := f.Apply(Operation{Key: intruder, CallID: "before", Action: OpRead, Path: "a.go"}); !errors.Is(err, ErrCollision) {
		t.Fatalf("foreign read = %v; want ErrCollision", err)
	}
	// finishLocked persists before notifying, so this also covers stores
	// written by versions that retained collision history.
	var data []byte
	if err := f.db.QueryRow("SELECT data FROM state WHERE id=1").Scan(&data); err != nil {
		t.Fatal(err)
	}
	var saved storedState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Collisions) != 1 {
		t.Fatalf("saved collisions = %+v; want one historical event", saved.Collisions)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if len(reopened.collisions) != 0 || reopened.notifiedCollisions != 0 {
		t.Fatalf("reopen retained collision notifications: %+v, %d", reopened.collisions, reopened.notifiedCollisions)
	}
	var seen []CollisionEvent
	reopened.OnCollision(func(e CollisionEvent) { seen = append(seen, e) })
	apply(t, reopened, applyCase{intruder, "own", 0, OpCreate, "b.go", "ok"})
	if len(seen) != 0 {
		t.Fatalf("replayed saved collisions: %+v", seen)
	}
	if _, err := reopened.Apply(Operation{Key: intruder, CallID: "after", ExpectedRevision: 1, Action: OpRead, Path: "a.go"}); !errors.Is(err, ErrCollision) {
		t.Fatalf("foreign read after reopen = %v; want ErrCollision", err)
	}
	if len(seen) != 1 || seen[0].AttemptingAgent != "intruder" || seen[0].OwningAgent != "owner" || seen[0].Path != "a.go" {
		t.Fatalf("new collision notification = %+v", seen)
	}
}
