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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// faultRoot is a workspace holding a regular file, a directory, a non-empty
// directory and a symlink, so each helper can be handed a path that fails in
// a distinct way.
func faultRoot(t *testing.T) (*FS, string) {
	t.Helper()
	f, root, _ := durable(t)
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("f"), newFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "full", "child"), recoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "real"), recoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	return f, root
}

func TestJournalHandlerSeesCommittedEntriesInOrder(t *testing.T) {
	f, _, _ := durable(t)
	var seen []JournalEntry
	f.OnJournal(func(e JournalEntry) { seen = append(seen, e) })
	key := bind(t, f, "author", "unit", "dev", "a.go", "b.go")
	r := apply(t, f, applyCase{key, "one", 0, OpCreate, "a.go", "1"})
	apply(t, f, applyCase{key, "two", r.Revision, OpCreate, "b.go", "2"})
	if len(seen) != 2 || seen[0].Path != "a.go" || seen[1].Path != "b.go" || seen[1].Seq != seen[0].Seq+1 {
		t.Fatalf("handler saw %+v; want a.go then b.go in sequence", seen)
	}
	if seen[0].CallID != "one" || seen[0].WorkUnitID != "unit" {
		t.Fatalf("handler saw an uncorrelated entry: %+v", seen[0])
	}
}

func TestLockedHelpersRefuseUnboundOrUnauthorizedAgents(t *testing.T) {
	f, _, _ := durable(t)
	if err := f.stageLocked("ghost", "a.go", []byte("x")); !errors.Is(err, ErrIdentity) {
		t.Errorf("stageLocked(unbound) = %v; want ErrIdentity", err)
	}
	if _, err := f.readLockedAs("ghost", "a.go", false); !errors.Is(err, ErrIdentity) {
		t.Errorf("readLockedAs(unbound) = %v; want ErrIdentity", err)
	}
	f.bindings["mute"] = Identity{SessionID: "s", WorkUnitID: "u", AgentID: "mute", Specialist: noGrantInstance}
	if _, err := f.readLockedAs("mute", "a.go", false); !errors.Is(err, ErrScopeDenied) {
		t.Errorf("readLockedAs(no read grant) = %v; want ErrScopeDenied", err)
	}
	if _, err := f.readLockedAs("ghost", escapingPath, false); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("readLockedAs(escaping path) = %v; want ErrInvalidPath", err)
	}
}

func TestConsolidateLockedRefusals(t *testing.T) {
	f, _, _ := durable(t)
	if err := f.consolidateLocked("nobody"); err != nil {
		t.Fatalf("consolidateLocked(no delta) = %v; want the idempotent no-op", err)
	}
	const agent = AgentID("author")
	d := f.ensureDelta(agent)
	d.files["a.go"] = []byte("x")
	f.owners["a.go"] = "someone-else"
	if err := f.consolidateLocked(agent); !errors.Is(err, ErrScopeDenied) {
		t.Errorf("consolidateLocked(path owned elsewhere) = %v; want ErrScopeDenied", err)
	}
	f.owners["a.go"] = agent
}

func TestReadMergedRefusesUnsafePhysicalPath(t *testing.T) {
	f, _ := faultRoot(t)
	if _, _, err := f.readMergedLocked("nobody", "link"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("readMergedLocked(symlink) = %v; want ErrInvalidPath", err)
	}
}

func TestClaimRefusesUnreadableScopePath(t *testing.T) {
	f, _ := faultRoot(t)
	if _, err := f.claimLocked(id("a", "u", "dev"), []string{"ok.go", escapingPath}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("claimLocked() = %v; want ErrInvalidPath", err)
	}
	if len(f.owners) != 0 || len(f.bindings) != 0 {
		t.Fatalf("a refused claim registered %d owners and %d bindings", len(f.owners), len(f.bindings))
	}
}

func TestFlushPlanningRefusals(t *testing.T) {
	f, _ := faultRoot(t)
	const agent = AgentID("author")
	t.Run("unsafe path", func(t *testing.T) {
		d := &agentDelta{files: map[string][]byte{"link": []byte("x")}, bases: map[string]baseFile{}}
		if _, err := f.planFlush(agent, d); !errors.Is(err, ErrFlushPartial) {
			t.Fatalf("planFlush() = %v; want ErrFlushPartial", err)
		}
	})
	t.Run("base diverged", func(t *testing.T) {
		d := &agentDelta{files: map[string][]byte{"file.txt": []byte("x")}, bases: map[string]baseFile{"file.txt": {Content: []byte("stale"), Present: true, Mode: newFileMode}}}
		if _, err := f.planFlush(agent, d); !errors.Is(err, ErrBaseChanged) {
			t.Fatalf("planFlush() = %v; want ErrBaseChanged", err)
		}
	})
	t.Run("parent is a file", func(t *testing.T) {
		m := &recoveryManifest{}
		if err := f.noteMissingDirs(m, "file.txt/sub/x"); err == nil {
			t.Fatal("noteMissingDirs accepted a path below a regular file")
		}
	})
	t.Run("missing parents are listed once, shallowest last", func(t *testing.T) {
		m := &recoveryManifest{}
		if err := f.noteMissingDirs(m, "a/b/x"); err != nil {
			t.Fatal(err)
		}
		if err := f.noteMissingDirs(m, "a/c"); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(m.Dirs, ","); got != "a/b,a" {
			t.Fatalf("Dirs = %q; want a/b,a with no duplicate", got)
		}
	})
	t.Run("directory below a file", func(t *testing.T) {
		if err := f.createDirs([]string{"file.txt/sub"}); !errors.Is(err, ErrFlushPartial) {
			t.Fatalf("createDirs() = %v; want ErrFlushPartial", err)
		}
	})
	t.Run("existing directory tolerated", func(t *testing.T) {
		if err := f.createDirs([]string{"real"}); err != nil {
			t.Fatalf("createDirs(existing) = %v", err)
		}
	})
}

func TestReplaceItemsRefusals(t *testing.T) {
	f, _ := faultRoot(t)
	present := baseFile{Content: []byte("f"), Present: true, Mode: newFileMode}
	t.Run("before-image no longer matches", func(t *testing.T) {
		m := &recoveryManifest{Items: []recoveryItem{{Path: "file.txt", Temp: "tmp", Before: baseFile{Content: []byte("other"), Present: true, Mode: newFileMode}, After: present}}}
		if err := f.replaceItems(m); !errors.Is(err, ErrBaseChanged) {
			t.Fatalf("replaceItems() = %v; want ErrBaseChanged", err)
		}
		if m.Intent != 0 {
			t.Fatalf("intent advanced to %d before any replacement", m.Intent)
		}
	})
	t.Run("replacement cannot be written", func(t *testing.T) {
		m := &recoveryManifest{Items: []recoveryItem{{Path: "missing/x.txt", Temp: "missing/.tmp", After: present}}}
		if err := f.replaceItems(m); !errors.Is(err, ErrFlushPartial) {
			t.Fatalf("replaceItems() = %v; want ErrFlushPartial", err)
		}
		if m.Intent != 1 {
			t.Fatalf("intent = %d; want it durably advanced before the attempt", m.Intent)
		}
	})
}

func TestReplaceLockedRefusals(t *testing.T) {
	f, root := faultRoot(t)
	value := baseFile{Content: []byte("new"), Present: true, Mode: newFileMode}
	if err := f.replaceLocked(escapingPath, "tmp", value); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("replaceLocked(escaping) = %v; want ErrInvalidPath", err)
	}
	if err := f.replaceLocked("missing/x.txt", "missing/.tmp", value); err == nil {
		t.Error("replaceLocked below a missing directory succeeded")
	}
	parent, err := f.openParent("top.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := parent.Close(); err != nil {
			t.Error(err)
		}
	}()
	fd := int(parent.Fd())
	if err := removeLocked(fd, "real", parent); err == nil {
		t.Error("removeLocked unlinked a directory as if it were a file")
	}
	if err := removeLocked(fd, "already-gone.txt", parent); err != nil {
		t.Errorf("removeLocked(absent) = %v; want it tolerated", err)
	}
	if err := writeLocked(fd, "top.txt", "file.txt", value, parent); err == nil {
		t.Error("writeLocked reused an existing temporary name")
	}
	if err := writeLocked(fd, "real", ".takt-vfs-tmp", value, parent); err == nil {
		t.Error("writeLocked replaced a directory with a file")
	}
	if _, err := os.Stat(filepath.Join(root, ".takt-vfs-tmp")); err != nil {
		t.Errorf("the failed rename should leave its temporary for recovery to remove: %v", err)
	}
}

func TestRemoveLeftoversStopsOnUndeletableEntries(t *testing.T) {
	f, root := faultRoot(t)
	if err := f.removeIfPresentLocked("full"); err == nil {
		t.Error("removeIfPresentLocked removed a non-empty directory")
	}
	if err := f.removeIfPresentLocked("never-existed"); err != nil {
		t.Errorf("removeIfPresentLocked(absent) = %v", err)
	}
	if err := f.removeLeftovers(&recoveryManifest{Items: []recoveryItem{{Path: "x", Temp: "full"}}}); err == nil {
		t.Error("removeLeftovers ignored an undeletable temporary")
	}
	if err := f.removeLeftovers(&recoveryManifest{Dirs: []string{"full"}}); err == nil {
		t.Error("removeLeftovers ignored an undeletable directory")
	}
	if err := os.Mkdir(filepath.Join(root, "a"), recoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a", "b"), recoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := f.removeLeftovers(&recoveryManifest{Dirs: []string{"a", "a/b"}}); err != nil {
		t.Fatalf("removeLeftovers(nested) = %v; want deepest first", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("directories left behind: %v", err)
	}
}

func TestSiblingAndEntryChecksRefuseNonDirectoryParents(t *testing.T) {
	f, _ := faultRoot(t)
	if _, err := f.checkSiblings("file.txt", "x"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("checkSiblings(file parent) = %v; want ErrInvalidPath", err)
	}
	if _, err := f.checkSiblings("file.txt/sub", "x"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("checkSiblings(below a file) = %v; want ErrInvalidPath", err)
	}
	if exists, err := f.checkSiblings("nowhere", "x"); exists || err != nil {
		t.Errorf("checkSiblings(missing parent) = %v, %v; want absent, no error", exists, err)
	}
	if _, err := f.checkEntry("file.txt/x", true); err == nil {
		t.Error("checkEntry accepted an entry below a regular file")
	}
	if exists, err := f.checkEntry("real", false); !exists || err != nil {
		t.Errorf("checkEntry(directory, not last) = %v, %v; want accepted", exists, err)
	}
}

func TestStoreWritesFailWithoutPublishing(t *testing.T) {
	t.Run("journal insert rejected", func(t *testing.T) {
		f, _, _ := durable(t)
		if _, err := f.db.Exec(`CREATE TRIGGER fail_journal BEFORE INSERT ON journal BEGIN SELECT RAISE(ABORT,'disk full'); END;`); err != nil {
			t.Fatal(err)
		}
		f.appendJournalLocked(JournalEntry{Operation: OpRead})
		if err := f.persistLocked(); err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("persistLocked() = %v; want the insert failure", err)
		}
		var rows int
		if err := f.db.QueryRow("SELECT COUNT(*) FROM journal").Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("journal rows = %d, %v; want the failed transaction rolled back", rows, err)
		}
	})
	t.Run("state table missing on load", func(t *testing.T) {
		f, _, _ := durable(t)
		if _, err := f.db.Exec("DROP TABLE state"); err != nil {
			t.Fatal(err)
		}
		if err := f.loadState(); err == nil || !strings.Contains(err.Error(), "no such table") {
			t.Fatalf("loadState() = %v", err)
		}
	})
	t.Run("journal table missing on replay", func(t *testing.T) {
		f, _, _ := durable(t)
		if _, err := f.db.Exec("DROP TABLE journal"); err != nil {
			t.Fatal(err)
		}
		if err := f.replayJournal(0); err == nil || !strings.Contains(err.Error(), "no such table") {
			t.Fatalf("replayJournal() = %v", err)
		}
	})
	t.Run("transaction cannot begin", func(t *testing.T) {
		f, _, _ := durable(t)
		if err := f.db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.persistLocked(); err == nil {
			t.Fatal("persistLocked() succeeded on a closed database")
		}
	})
	t.Run("private file in a missing directory", func(t *testing.T) {
		if err := ensurePrivateFile(filepath.Join(t.TempDir(), "missing", "vfs.sqlite")); err == nil || errors.Is(err, ErrInvalidPath) {
			t.Fatalf("ensurePrivateFile() = %v; want the creation error", err)
		}
	})
}
