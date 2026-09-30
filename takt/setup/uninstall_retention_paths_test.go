package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// retentionClock is a fixed instant so retained directory names are deterministic.
var retentionClock = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// installOne deploys a single file and returns the deployment root.
func installOne(t *testing.T, path, content string) string {
	t.Helper()
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{path: content}))
	return root
}

func TestEngramDataDirFailsWithoutHome(t *testing.T) {
	t.Setenv("ENGRAM_DATA_DIR", "")
	t.Setenv("HOME", "")
	if _, err := EngramDataDir(); err == nil || !strings.Contains(err.Error(), "resolve Engram data directory") {
		t.Fatalf("error = %v, want resolution failure", err)
	}
}

func TestRestorablePreExisting(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "original")
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "takt"}))
	manifest, err := LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entries["a.txt"]
	if !restorablePreExisting(root, entry) {
		t.Fatal("untouched takeover with its backup should be restorable")
	}

	noBackup := entry
	noBackup.BackupPath = ""
	escaping := entry
	escaping.BackupPath = "../outside"
	missing := entry
	missing.BackupPath = BackupDir + "/absent"
	for name, candidate := range map[string]OwnershipEntry{"no backup path": noBackup, "escaping backup": escaping, "backup file absent": missing} {
		if restorablePreExisting(root, candidate) {
			t.Errorf("%s: reported restorable", name)
		}
	}

	writeFile(t, filepath.Join(root, "a.txt"), "user edit")
	if restorablePreExisting(root, entry) {
		t.Error("edited file reported restorable")
	}
}

func TestRetentionRequiresManifestForFileChoices(t *testing.T) {
	_, err := ApplyUninstallRetention(context.Background(), t.TempDir(), RetentionChoices{Keep: []string{"a.txt"}}, retentionClock)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want missing manifest", err)
	}
}

func TestRetentionCancelledLeavesChoicesNotApplied(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "a", "b.txt": "b"}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := ApplyUninstallRetention(ctx, root, RetentionChoices{Remove: []string{"a.txt"}, Keep: []string{"b.txt"}}, retentionClock)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !slices.Equal(result.NotApplied, []string{"a.txt", "b.txt"}) {
		t.Errorf("NotApplied = %v", result.NotApplied)
	}
	if readString(t, filepath.Join(root, "a.txt")) != "a" {
		t.Error("cancelled retention touched a file")
	}
}

func TestRetentionKeepsDirectoryLayoutAndPrunesEmptyParents(t *testing.T) {
	root := installOne(t, "deep/dir/a.txt", "a")
	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{Keep: []string{"deep/dir/a.txt"}}, retentionClock)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(root, RetainedDirName, "20260102T030405Z")
	if result.RetainedDir != wantDir {
		t.Errorf("RetainedDir = %q, want %q", result.RetainedDir, wantDir)
	}
	if readString(t, filepath.Join(wantDir, "deep", "dir", "a.txt")) != "a" {
		t.Error("kept file missing from retained layout")
	}
	if _, err := os.Stat(filepath.Join(root, "deep")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("emptied source directories survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, OwnershipManifestFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("manifest survived after its last entry left: %v", err)
	}
}

func TestRetentionRemoveToleratesAlreadyDeletedFile(t *testing.T) {
	root := installOne(t, "a.txt", "a")
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{Remove: []string{"a.txt"}}, retentionClock)
	if err != nil || !slices.Equal(result.Removed, []string{"a.txt"}) {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestRetentionKeepOfMissingFileIsIncomplete(t *testing.T) {
	root := installOne(t, "a.txt", "a")
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{Keep: []string{"a.txt"}}, retentionClock)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Incomplete) != 1 || !strings.HasPrefix(result.Incomplete[0], "a.txt: ") || len(result.Retained) != 0 {
		t.Errorf("result = %+v, want one incomplete entry for a.txt", result)
	}
}

func TestRetentionBlockedRetainedDirectoryIsIncomplete(t *testing.T) {
	root := installOne(t, "a.txt", "a")
	// A regular file where the retained directory must go blocks every reservation.
	writeFile(t, filepath.Join(root, RetainedDirName), "in the way")

	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{Keep: []string{"a.txt"}, MemoryFrom: engramFixture(t)}, retentionClock)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Incomplete) != 2 || !strings.HasPrefix(result.Incomplete[1], memoryNotApplied+": ") {
		t.Errorf("Incomplete = %v, want the file and the memory choice", result.Incomplete)
	}
	if readString(t, filepath.Join(root, "a.txt")) != "a" {
		t.Error("file moved although the retained directory could not be reserved")
	}
}

func TestRetainFileFailsWhenDestinationDirectoryIsBlocked(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "src.txt")
	writeFile(t, source, "x")
	writeFile(t, filepath.Join(dir, "blocker"), "file")
	if err := retainFile(source, filepath.Join(dir, "blocker", "child", "src.txt")); err == nil {
		t.Fatal("retainFile succeeded under a regular file")
	}
}

func TestSnapshotDatabaseFailsForUnusableDestinationAndSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "blocker"), "file")
	if err := snapshotDatabase(context.Background(), filepath.Join(dir, "src.db"), filepath.Join(dir, "blocker", "out.db")); err == nil {
		t.Error("snapshot into a path under a regular file succeeded")
	}
	notADatabase := filepath.Join(dir, "garbage.db")
	writeFile(t, notADatabase, strings.Repeat("not sqlite ", 20))
	destination := filepath.Join(dir, "out", "copy.db")
	if err := snapshotDatabase(context.Background(), notADatabase, destination); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Errorf("error = %v, want snapshot failure", err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("partial snapshot left behind: %v", err)
	}
}

func TestRetentionRemoveOfNonEmptyDirectoryIsIncomplete(t *testing.T) {
	root := installOne(t, "a.txt", "a")
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "a.txt", "child"), "x")

	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{Remove: []string{"a.txt"}}, retentionClock)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 0 || len(result.Incomplete) != 1 || !strings.HasPrefix(result.Incomplete[0], "a.txt: ") {
		t.Errorf("result = %+v, want the removal reported incomplete", result)
	}
}
