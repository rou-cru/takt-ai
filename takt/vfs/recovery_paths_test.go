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
	"testing"
)

const (
	// externalContent stands in for an edit made outside the coordinator.
	externalContent = "edited elsewhere"
	// newFileMode is the mode a flush gives a file it creates.
	newFileMode os.FileMode = 0o644
	// executableMode is a base mode a flush must preserve.
	executableMode os.FileMode = 0o751
)

var errInjected = errors.New("injected failure")

// stagedAndGated stages content for rel under a fresh binding and passes its gate.
func stagedAndGated(t *testing.T, f *FS, rel, content string, action OperationType) (AgentID, OperationResult) {
	t.Helper()
	key := bind(t, f, "author", "unit", "dev", rel)
	r := apply(t, f, applyCase{key, "stage", 0, action, rel, content})
	gate(t, f, key, r)
	return key, r
}

// failAt makes the flush fail at the named boundary, once.
func failAt(f *FS, point string) {
	f.failpoint = func(p string) error {
		if p == point {
			return errInjected
		}
		return nil
	}
}

func TestConsolidateCreatesMissingDirectoriesAndDeletesFiles(t *testing.T) {
	f, root, _ := durable(t)
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("old"), executableMode); err != nil {
		t.Fatal(err)
	}
	key := bind(t, f, "author", "unit", "dev", "deep/er/new.txt", "old.txt")
	r := apply(t, f, applyCase{key, "create", 0, OpCreate, "deep/er/new.txt", "fresh"})
	r = apply(t, f, applyCase{key, "delete", r.Revision, OpDelete, "old.txt", ""})
	gate(t, f, key, r)
	if err := f.ConsolidateCheckpoint(key, "cp", r.Revision, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "deep/er/new.txt"))
	if err != nil || info.Mode().Perm() != newFileMode {
		t.Fatalf("created file: %v, %v; want mode %v", info, err, newFileMode)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "deep/er/new.txt")); string(data) != "fresh" {
		t.Fatalf("created content = %q", data)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file still on disk: %v", err)
	}
	flushed := map[string]string{}
	for _, e := range f.JournalPage("", "", -1, MaxJournalPageSize) {
		if e.Operation == OpFlush {
			flushed[e.Path] = e.AfterHash
		}
	}
	if len(flushed) != 2 || flushed["old.txt"] != "" || flushed["deep/er/new.txt"] != hashOf([]byte("fresh"), true) {
		t.Fatalf("flush journal = %v; want a hash for the new file and none for the deletion", flushed)
	}
}

func TestConsolidateRefusesWhenBaseChangedAfterStaging(t *testing.T) {
	f, root, _ := durable(t)
	key, r := stagedAndGated(t, f, "a.txt", "staged", OpCreate)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte(externalContent), newFileMode); err != nil {
		t.Fatal(err)
	}
	if err := f.ConsolidateCheckpoint(key, "cp", r.Revision, false); !errors.Is(err, ErrBaseChanged) {
		t.Fatalf("ConsolidateCheckpoint() = %v; want ErrBaseChanged", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(data) != externalContent {
		t.Fatalf("the refused flush overwrote the external edit: %q", data)
	}
	if f.recovery != nil {
		t.Fatal("a flush refused before any write demanded recovery")
	}
	if stagedCount(f, key) != 1 {
		t.Fatal("the refused flush discarded staged work")
	}
}

func TestFlushDetectsForeignContentWrittenDuringFlush(t *testing.T) {
	f, root, _ := durable(t)
	key, r := stagedAndGated(t, f, "a.txt", "staged", OpCreate)
	f.failpoint = func(p string) error {
		if p == "after/a.txt" {
			return os.WriteFile(filepath.Join(root, "a.txt"), []byte(externalContent), newFileMode)
		}
		return nil
	}
	if err := f.ConsolidateCheckpoint(key, "cp", r.Revision, false); !errors.Is(err, ErrBaseChanged) {
		t.Fatalf("ConsolidateCheckpoint() = %v; want ErrBaseChanged", err)
	}
	if _, err := readForTest(f, key, "a.txt"); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("work continued after a half-verified flush: %v", err)
	}
	f.failpoint = nil
	if err := f.Recover(); !errors.Is(err, ErrBaseChanged) {
		t.Fatalf("Recover() = %v; want ErrBaseChanged, never an overwrite of foreign content", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(data) != externalContent {
		t.Fatalf("recovery overwrote foreign content: %q", data)
	}
}

func TestCheckpointPersistenceFailureFaultsTheStore(t *testing.T) {
	f, root, _ := durable(t)
	key, r := stagedAndGated(t, f, "a.txt", "staged", OpCreate)
	if _, err := f.db.Exec(`CREATE TRIGGER fail_state BEFORE UPDATE ON state BEGIN SELECT RAISE(ABORT,'disk full'); END;`); err != nil {
		t.Fatal(err)
	}
	err := f.ConsolidateCheckpoint(key, "cp", r.Revision, false)
	if !errors.Is(err, ErrStoreFailed) || !errors.Is(err, ErrFlushPartial) {
		t.Fatalf("ConsolidateCheckpoint() = %v; want ErrStoreFailed and ErrFlushPartial", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "a.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a flush that could not record its intent wrote to disk: %v", statErr)
	}
	if _, err := f.Apply(Operation{Key: key, CallID: "after", Action: OpRead, Path: "a.txt"}); !errors.Is(err, ErrStoreFailed) {
		t.Fatalf("Apply() after a store fault = %v; want ErrStoreFailed", err)
	}
}

func TestRecoverJournalsTheRestoration(t *testing.T) {
	f, root, _ := durable(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("base"), executableMode); err != nil {
		t.Fatal(err)
	}
	key, r := stagedAndGated(t, f, "a.txt", "staged", OpPatch)
	failAt(f, "verified")
	if err := f.ConsolidateCheckpoint(key, "cp", r.Revision, false); !errors.Is(err, errInjected) {
		t.Fatalf("ConsolidateCheckpoint() = %v; want the injected failure", err)
	}
	f.failpoint = nil
	if err := f.Recover(); err != nil {
		t.Fatal(err)
	}
	if f.recovery != nil {
		t.Fatal("recovery manifest survived a completed restoration")
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	info, _ := os.Stat(filepath.Join(root, "a.txt"))
	if string(data) != "base" || info.Mode().Perm() != executableMode {
		t.Fatalf("restored %q with mode %v; want the original", data, info.Mode().Perm())
	}
	var restored bool
	for _, e := range f.JournalPage("", "", -1, MaxJournalPageSize) {
		restored = restored || (e.Operation == "recovery" && e.Outcome == "restored")
	}
	if !restored {
		t.Fatal("journal does not record the restoration")
	}
	if err := f.ConsolidateCheckpoint(key, "retry", r.Revision, false); err != nil {
		t.Fatalf("staged work could not be flushed again after recovery: %v", err)
	}
}

func TestRecoverRefusesATouchedPathTurnedSymlink(t *testing.T) {
	f, root, _ := durable(t)
	key, r := stagedAndGated(t, f, "a.txt", "staged", OpCreate)
	failAt(f, "after/a.txt")
	if err := f.ConsolidateCheckpoint(key, "cp", r.Revision, false); !errors.Is(err, errInjected) {
		t.Fatalf("ConsolidateCheckpoint() = %v", err)
	}
	f.failpoint = nil
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("safe"), newFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := f.Recover(); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Recover() = %v; want ErrInvalidPath", err)
	}
	if f.recovery == nil {
		t.Fatal("refused recovery dropped the manifest")
	}
	if data, _ := os.ReadFile(target); string(data) != "safe" {
		t.Fatalf("recovery wrote through the symlink: %q", data)
	}
}

func TestRecoverRetriesAfterACheckpointFailure(t *testing.T) {
	f, root, _ := durable(t)
	key, r := stagedAndGated(t, f, "a.txt", "staged", OpCreate)
	failAt(f, "after/a.txt")
	if err := f.ConsolidateCheckpoint(key, "cp", r.Revision, false); !errors.Is(err, errInjected) {
		t.Fatalf("ConsolidateCheckpoint() = %v", err)
	}
	failAt(f, "restored/a.txt")
	if err := f.Recover(); !errors.Is(err, errInjected) {
		t.Fatalf("Recover() = %v; want the injected failure", err)
	}
	if f.recovery == nil {
		t.Fatal("a failed restoration dropped the manifest")
	}
	f.failpoint = nil
	if err := f.Recover(); err != nil {
		t.Fatalf("second Recover() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created file survived recovery: %v", err)
	}
}

func TestValidatePathAcceptsMissingAndPlainPaths(t *testing.T) {
	f, root, _ := durable(t)
	if err := os.MkdirAll(filepath.Join(root, "dir"), recoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "file.txt"), nil, newFileMode); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"dir/file.txt", "dir/absent.txt", "no/such/deep/file.txt", "plain.txt"} {
		if err := f.validatePath(p); err != nil {
			t.Errorf("validatePath(%q) = %v; want accepted", p, err)
		}
	}
}

func TestValidatePathRejectsUnsafeExistingEntries(t *testing.T) {
	f, root, _ := durable(t)
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, recoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), nil, newFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"file used as a directory":  "file.txt/child",
		"directory as the file":     "real",
		"symlinked directory":       "link/child",
		"decomposed unicode name":   "é.txt",
		"case alias of a directory": "REAL/x",
	} {
		if err := f.validatePath(p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("%s: validatePath(%q) = %v; want ErrInvalidPath", name, p, err)
		}
	}
}

func TestPhysicalReadsBaseFiles(t *testing.T) {
	f, root, _ := durable(t)
	if err := os.WriteFile(filepath.Join(root, "exec.sh"), []byte("#!/bin/sh"), executableMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "exec.sh"), executableMode); err != nil {
		t.Fatal(err)
	}
	got, err := f.physical("exec.sh")
	if err != nil || !got.Present || string(got.Content) != "#!/bin/sh" || got.Mode != executableMode {
		t.Fatalf("physical(existing) = %+v, %v", got, err)
	}
	if got, err := f.physical("absent.sh"); err != nil || got.Present || got.Content != nil {
		t.Fatalf("physical(absent) = %+v, %v; want an absent base", got, err)
	}
	if _, err := f.physical(escapingPath); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("physical(escaping) = %v; want ErrInvalidPath", err)
	}
}

func TestCaptureBaseKeepsTheFirstSnapshot(t *testing.T) {
	f, root, _ := durable(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("first"), newFileMode); err != nil {
		t.Fatal(err)
	}
	const agent = AgentID("capturer")
	if err := f.captureBaseLocked(agent, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("second"), newFileMode); err != nil {
		t.Fatal(err)
	}
	if err := f.captureBaseLocked(agent, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if base := f.staged[agent].bases["a.txt"]; string(base.Content) != "first" || !base.Present {
		t.Fatalf("base = %+v; want the first snapshot kept", base)
	}
	if err := f.captureBaseLocked(agent, escapingPath); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("captureBaseLocked(escaping) = %v; want ErrInvalidPath", err)
	}
	if _, kept := f.staged[agent].bases[escapingPath]; kept {
		t.Fatal("a refused path was recorded as a base")
	}
}

func TestParentHelpersRefuseUnsafeParents(t *testing.T) {
	f, root, _ := durable(t)
	if err := os.Mkdir(filepath.Join(root, "real"), recoveryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), nil, newFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := f.syncParent("real/x"); err != nil {
		t.Errorf("syncParent(existing parent) = %v", err)
	}
	if err := f.syncParent("missing/x"); err == nil {
		t.Error("syncParent(missing parent) succeeded")
	}
	for _, rel := range []string{"file.txt/x", "link/x", "missing/x"} {
		if dir, err := f.openParent(rel); err == nil {
			_ = dir.Close()
			t.Errorf("openParent(%q) succeeded; want a refusal to traverse it", rel)
		}
	}
	for _, rel := range []string{"top.txt", "real/x"} {
		dir, err := f.openParent(rel)
		if err != nil {
			t.Errorf("openParent(%q) = %v", rel, err)
			continue
		}
		if err := dir.Close(); err != nil {
			t.Error(err)
		}
	}
}
