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

package vfs_test

import (
	"errors"
	"github.com/rou-cru/takt-ai/takt/vfs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// faultRoot is a workspace holding a regular file, a directory, a non-empty
// directory and a symlink, so each helper can be handed a path that fails in
// a distinct way.
func faultRoot(t *testing.T) (*vfs.FS, string) {
	t.Helper()
	f, root, _ := newStore(t)
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("f"), newFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "full", "child"), vfs.RecoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "real"), vfs.RecoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	return f, root
}

func TestJournalHandlerSeesCommittedEntriesInOrder(t *testing.T) {
	f, _, _ := newStore(t)
	var seen []vfs.JournalEntry
	f.OnJournal(func(e vfs.JournalEntry) { seen = append(seen, e) })
	key := bind(t, f, "author", "unit", "dev", "a.go", "b.go")
	r := apply(t, f, applyCase{key, "one", 0, vfs.OpCreate, "a.go", "1"})
	apply(t, f, applyCase{key, "two", r.Revision, vfs.OpCreate, "b.go", "2"})
	if len(seen) != 2 || seen[0].Path != "a.go" || seen[1].Path != "b.go" || seen[1].Seq != seen[0].Seq+1 {
		t.Fatalf("handler saw %+v; want a.go then b.go in sequence", seen)
	}
	if seen[0].CallID != "one" || seen[0].WorkUnitID != "unit" {
		t.Fatalf("handler saw an uncorrelated entry: %+v", seen[0])
	}
}

func TestLockedHelpersRefuseUnboundOrUnauthorizedAgents(t *testing.T) {
	f, _, _ := newStore(t)
	if err := f.StageLocked("ghost", "a.go", []byte("x")); !errors.Is(err, vfs.ErrIdentity) {
		t.Errorf("stageLocked(unbound) = %v; want ErrIdentity", err)
	}
	if _, err := f.ReadLockedAs("ghost", "a.go", false); !errors.Is(err, vfs.ErrIdentity) {
		t.Errorf("readLockedAs(unbound) = %v; want ErrIdentity", err)
	}
	f.SetBinding("mute", vfs.Identity{SessionID: "s", WorkUnitID: "u", AgentID: "mute", Specialist: noGrantInstance})
	if _, err := f.ReadLockedAs("mute", "a.go", false); !errors.Is(err, vfs.ErrScopeDenied) {
		t.Errorf("readLockedAs(no read grant) = %v; want ErrScopeDenied", err)
	}
	if _, err := f.ReadLockedAs("ghost", escapingPath, false); !errors.Is(err, vfs.ErrInvalidPath) {
		t.Errorf("readLockedAs(escaping path) = %v; want ErrInvalidPath", err)
	}
}

func TestConsolidateLockedRefusals(t *testing.T) {
	f, _, _ := newStore(t)
	if err := f.ConsolidateLocked("nobody"); err != nil {
		t.Fatalf("consolidateLocked(no delta) = %v; want the idempotent no-op", err)
	}
	const agent = vfs.AgentID("author")
	f.StageFile(agent, "a.go", []byte("x"))
	f.SetOwner("a.go", "someone-else")
	if err := f.ConsolidateLocked(agent); !errors.Is(err, vfs.ErrScopeDenied) {
		t.Errorf("consolidateLocked(path owned elsewhere) = %v; want ErrScopeDenied", err)
	}
	f.SetOwner("a.go", agent)
}

func TestReadMergedRefusesUnsafePhysicalPath(t *testing.T) {
	f, _ := faultRoot(t)
	if _, _, err := f.ReadMergedLocked("nobody", "link"); !errors.Is(err, vfs.ErrInvalidPath) {
		t.Fatalf("readMergedLocked(symlink) = %v; want ErrInvalidPath", err)
	}
}

func TestClaimRefusesUnreadableScopePath(t *testing.T) {
	f, _ := faultRoot(t)
	if _, err := f.ClaimLocked(ident("a", "u", "dev"), []string{"ok.go", escapingPath}); !errors.Is(err, vfs.ErrInvalidPath) {
		t.Fatalf("claimLocked() = %v; want ErrInvalidPath", err)
	}
	if owners, bindings := f.Held(); owners != 0 || bindings != 0 {
		t.Fatalf("a refused claim registered %d owners and %d bindings", owners, bindings)
	}
}

func TestFlushPlanningRefusals(t *testing.T) {
	f, _ := faultRoot(t)
	const agent = vfs.AgentID("author")
	t.Run("unsafe path", func(t *testing.T) {
		d := vfs.NewDelta(map[string][]byte{"link": []byte("x")}, map[string]vfs.BaseFile{})
		if _, err := f.PlanFlush(agent, d); !errors.Is(err, vfs.ErrFlushPartial) {
			t.Fatalf("planFlush() = %v; want ErrFlushPartial", err)
		}
	})
	t.Run("base diverged", func(t *testing.T) {
		d := vfs.NewDelta(map[string][]byte{"file.txt": []byte("x")}, map[string]vfs.BaseFile{"file.txt": {Content: []byte("stale"), Present: true, Mode: newFileMode}})
		if _, err := f.PlanFlush(agent, d); !errors.Is(err, vfs.ErrBaseChanged) {
			t.Fatalf("planFlush() = %v; want ErrBaseChanged", err)
		}
	})
	t.Run("parent is a file", func(t *testing.T) {
		m := &vfs.RecoveryManifest{}
		if err := f.NoteMissingDirs(m, "file.txt/sub/x"); err == nil {
			t.Fatal("noteMissingDirs accepted a path below a regular file")
		}
	})
	t.Run("missing parents are listed once, shallowest last", func(t *testing.T) {
		m := &vfs.RecoveryManifest{}
		if err := f.NoteMissingDirs(m, "a/b/x"); err != nil {
			t.Fatal(err)
		}
		if err := f.NoteMissingDirs(m, "a/c"); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(m.Dirs, ","); got != "a/b,a" {
			t.Fatalf("Dirs = %q; want a/b,a with no duplicate", got)
		}
	})
	t.Run("directory below a file", func(t *testing.T) {
		if err := f.CreateDirs([]string{"file.txt/sub"}); !errors.Is(err, vfs.ErrFlushPartial) {
			t.Fatalf("createDirs() = %v; want ErrFlushPartial", err)
		}
	})
	t.Run("existing directory tolerated", func(t *testing.T) {
		if err := f.CreateDirs([]string{"real"}); err != nil {
			t.Fatalf("createDirs(existing) = %v", err)
		}
	})
}

func TestReplaceItemsRefusals(t *testing.T) {
	f, _ := faultRoot(t)
	present := vfs.BaseFile{Content: []byte("f"), Present: true, Mode: newFileMode}
	t.Run("before-image no longer matches", func(t *testing.T) {
		m := &vfs.RecoveryManifest{Items: []vfs.RecoveryItem{{Path: "file.txt", Temp: "tmp", Before: vfs.BaseFile{Content: []byte("other"), Present: true, Mode: newFileMode}, After: present}}}
		if err := f.ReplaceItems(m); !errors.Is(err, vfs.ErrBaseChanged) {
			t.Fatalf("replaceItems() = %v; want ErrBaseChanged", err)
		}
		if m.Intent != 0 {
			t.Fatalf("intent advanced to %d before any replacement", m.Intent)
		}
	})
	t.Run("replacement cannot be written", func(t *testing.T) {
		m := &vfs.RecoveryManifest{Items: []vfs.RecoveryItem{{Path: "missing/x.txt", Temp: "missing/.tmp", After: present}}}
		if err := f.ReplaceItems(m); !errors.Is(err, vfs.ErrFlushPartial) {
			t.Fatalf("replaceItems() = %v; want ErrFlushPartial", err)
		}
		if m.Intent != 1 {
			t.Fatalf("intent = %d; want it durably advanced before the attempt", m.Intent)
		}
	})
}

func TestReplaceLockedRefusals(t *testing.T) {
	f, root := faultRoot(t)
	value := vfs.BaseFile{Content: []byte("new"), Present: true, Mode: newFileMode}
	if err := f.ReplaceLocked(escapingPath, "tmp", value); !errors.Is(err, vfs.ErrInvalidPath) {
		t.Errorf("replaceLocked(escaping) = %v; want ErrInvalidPath", err)
	}
	if err := f.ReplaceLocked("missing/x.txt", "missing/.tmp", value); err == nil {
		t.Error("replaceLocked below a missing directory succeeded")
	}
	parent, err := f.OpenParent("top.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := parent.Close(); err != nil {
			t.Error(err)
		}
	}()
	fd := int(parent.Fd())
	if err := vfs.RemoveLocked(fd, "real", parent); err == nil {
		t.Error("removeLocked unlinked a directory as if it were a file")
	}
	if err := vfs.RemoveLocked(fd, "already-gone.txt", parent); err != nil {
		t.Errorf("removeLocked(absent) = %v; want it tolerated", err)
	}
	if err := vfs.WriteLocked(fd, "top.txt", "file.txt", value, parent); err == nil {
		t.Error("writeLocked reused an existing temporary name")
	}
	if err := vfs.WriteLocked(fd, "real", ".takt-vfs-tmp", value, parent); err == nil {
		t.Error("writeLocked replaced a directory with a file")
	}
	if _, err := os.Stat(filepath.Join(root, ".takt-vfs-tmp")); err != nil {
		t.Errorf("the failed rename should leave its temporary for recovery to remove: %v", err)
	}
}

func TestRemoveLeftoversStopsOnUndeletableEntries(t *testing.T) {
	f, root := faultRoot(t)
	if err := f.RemoveIfPresentLocked("full"); err == nil {
		t.Error("removeIfPresentLocked removed a non-empty directory")
	}
	if err := f.RemoveIfPresentLocked("never-existed"); err != nil {
		t.Errorf("removeIfPresentLocked(absent) = %v", err)
	}
	if err := f.RemoveLeftovers(&vfs.RecoveryManifest{Items: []vfs.RecoveryItem{{Path: "x", Temp: "full"}}}); err == nil {
		t.Error("removeLeftovers ignored an undeletable temporary")
	}
	if err := f.RemoveLeftovers(&vfs.RecoveryManifest{Dirs: []string{"full"}}); err == nil {
		t.Error("removeLeftovers ignored an undeletable directory")
	}
	if err := os.Mkdir(filepath.Join(root, "a"), vfs.RecoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a", "b"), vfs.RecoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := f.RemoveLeftovers(&vfs.RecoveryManifest{Dirs: []string{"a", "a/b"}}); err != nil {
		t.Fatalf("removeLeftovers(nested) = %v; want deepest first", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("directories left behind: %v", err)
	}
}

func TestSiblingAndEntryChecksRefuseNonDirectoryParents(t *testing.T) {
	f, _ := faultRoot(t)
	if _, err := f.CheckSiblings("file.txt", "x"); !errors.Is(err, vfs.ErrInvalidPath) {
		t.Errorf("checkSiblings(file parent) = %v; want ErrInvalidPath", err)
	}
	if _, err := f.CheckSiblings("file.txt/sub", "x"); !errors.Is(err, vfs.ErrInvalidPath) {
		t.Errorf("checkSiblings(below a file) = %v; want ErrInvalidPath", err)
	}
	if exists, err := f.CheckSiblings("nowhere", "x"); exists || err != nil {
		t.Errorf("checkSiblings(missing parent) = %v, %v; want absent, no error", exists, err)
	}
	if _, err := f.CheckEntry("file.txt/x", true); err == nil {
		t.Error("checkEntry accepted an entry below a regular file")
	}
	if exists, err := f.CheckEntry("real", false); !exists || err != nil {
		t.Errorf("checkEntry(directory, not last) = %v, %v; want accepted", exists, err)
	}
}

func TestStoreWritesFailWithoutPublishing(t *testing.T) {
	t.Run("journal insert rejected", func(t *testing.T) {
		f, _, _ := newStore(t)
		if err := f.ExecState(`CREATE TRIGGER fail_journal BEFORE INSERT ON journal BEGIN SELECT RAISE(ABORT,'disk full'); END;`); err != nil {
			t.Fatal(err)
		}
		f.AppendJournalLocked(vfs.JournalEntry{Operation: vfs.OpRead})
		if err := f.PersistLocked(); err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("persistLocked() = %v; want the insert failure", err)
		}
		if rows, err := f.QueryInt("SELECT COUNT(*) FROM journal"); err != nil || rows != 0 {
			t.Fatalf("journal rows = %d, %v; want the failed transaction rolled back", rows, err)
		}
	})
	t.Run("state table missing on load", func(t *testing.T) {
		f, _, _ := newStore(t)
		if err := f.ExecState("DROP TABLE state"); err != nil {
			t.Fatal(err)
		}
		if err := f.LoadState(); err == nil || !strings.Contains(err.Error(), "no such table") {
			t.Fatalf("loadState() = %v", err)
		}
	})
	t.Run("journal table missing on replay", func(t *testing.T) {
		f, _, _ := newStore(t)
		if err := f.ExecState("DROP TABLE journal"); err != nil {
			t.Fatal(err)
		}
		if err := f.ReplayJournal(0); err == nil || !strings.Contains(err.Error(), "no such table") {
			t.Fatalf("replayJournal() = %v", err)
		}
	})
	t.Run("transaction cannot begin", func(t *testing.T) {
		f, _, _ := newStore(t)
		if err := f.CloseDB(); err != nil {
			t.Fatal(err)
		}
		if err := f.PersistLocked(); err == nil {
			t.Fatal("persistLocked() succeeded on a closed database")
		}
	})
	t.Run("private file in a missing directory", func(t *testing.T) {
		if err := vfs.EnsurePrivateFile(filepath.Join(t.TempDir(), "missing", "vfs.sqlite")); err == nil || errors.Is(err, vfs.ErrInvalidPath) {
			t.Fatalf("EnsurePrivateFile() = %v; want the creation error", err)
		}
	})
}
