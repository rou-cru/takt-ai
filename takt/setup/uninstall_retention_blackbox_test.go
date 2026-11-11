package setup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func installedManifestFile(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	manifest := setup.NewOwnershipManifest()
	entry, err := setup.NewOwnershipEntry(path, []byte(content), 0o644, false, "", "", setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("NewOwnershipEntry() error = %v", err)
	}
	if err := manifest.Add(entry); err != nil {
		t.Fatalf("manifest.Add() error = %v", err)
	}
	if err := manifest.Save(root); err != nil {
		t.Fatalf("manifest.Save() error = %v", err)
	}
}

func TestApplyUninstallRetentionEmptyChoicesIsNoop(t *testing.T) {
	result, err := setup.ApplyUninstallRetention(context.Background(), t.TempDir(), setup.RetentionChoices{}, time.Now())
	if err != nil {
		t.Fatalf("ApplyUninstallRetention() error = %v", err)
	}
	if len(result.Retained) != 0 || len(result.Removed) != 0 {
		t.Errorf("ApplyUninstallRetention() with no choices = %+v, want the zero result", result)
	}
}

func TestApplyUninstallRetentionKeepsFile(t *testing.T) {
	root := t.TempDir()
	installedManifestFile(t, root, "a.txt", "keep me")

	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Keep: []string{"a.txt"}}, time.Now())
	if err != nil {
		t.Fatalf("ApplyUninstallRetention() error = %v", err)
	}
	if len(result.Retained) != 1 || result.Retained[0] != "a.txt" {
		t.Fatalf("ApplyUninstallRetention() Retained = %v, want [a.txt]", result.Retained)
	}
	if result.RetainedDir == "" {
		t.Fatal("ApplyUninstallRetention() RetainedDir is empty")
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Error("a.txt still at its original path, want it moved into the retained directory")
	}
	moved, err := os.ReadFile(filepath.Join(result.RetainedDir, "a.txt"))
	if err != nil || string(moved) != "keep me" {
		t.Errorf("retained file content = %q, %v, want %q", moved, err, "keep me")
	}
}

func TestApplyUninstallRetentionRemovesFile(t *testing.T) {
	root := t.TempDir()
	installedManifestFile(t, root, "a.txt", "delete me")

	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Remove: []string{"a.txt"}}, time.Now())
	if err != nil {
		t.Fatalf("ApplyUninstallRetention() error = %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "a.txt" {
		t.Fatalf("ApplyUninstallRetention() Removed = %v, want [a.txt]", result.Removed)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Error("a.txt still exists, want it removed")
	}
}

func TestApplyUninstallRetentionUnknownPathIsIncomplete(t *testing.T) {
	root := t.TempDir()
	if err := setup.NewOwnershipManifest().Save(root); err != nil {
		t.Fatalf("manifest.Save() error = %v", err)
	}
	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Keep: []string{"never-recorded.txt"}}, time.Now())
	if err != nil {
		t.Fatalf("ApplyUninstallRetention() error = %v", err)
	}
	if len(result.Incomplete) != 1 {
		t.Fatalf("ApplyUninstallRetention() Incomplete = %v, want one entry for the unrecorded path", result.Incomplete)
	}
}

// TestApplyUninstallRetentionRefusesSymlinkedRetainedDir checks that a planted
// takt-retained symlink cannot move retained output outside the root.
func TestApplyUninstallRetentionRefusesSymlinkedRetainedDir(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	installedManifestFile(t, root, "a.txt", "keep me")
	if err := os.Symlink(outside, filepath.Join(root, setup.RetainedDirName)); err != nil {
		t.Fatal(err)
	}
	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Keep: []string{"a.txt"}}, time.Now())
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("retention wrote outside the root: %v (err %v)", entries, err)
	}
	if len(result.Incomplete) != 1 {
		t.Fatalf("Incomplete = %v, want the refused file reported", result.Incomplete)
	}
}

// retentionClock is a fixed instant so retained directory names are deterministic.
var retentionClock = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func retentionWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func retentionRead(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

// retentionDeploy deploys the files to root as managed OpenCode artifacts.
func retentionDeploy(t *testing.T, root string, files map[string]string) {
	t.Helper()
	plan := setup.TargetPlan{Target: "opencode"}
	for path, content := range files {
		plan.ManagedPaths = append(plan.ManagedPaths, path)
		plan.Artifacts = append(plan.Artifacts, setup.Artifact{Path: path, Content: []byte(content)})
	}
	slices.Sort(plan.ManagedPaths)
	if _, err := setup.ApplyContext(context.Background(), root, []setup.TargetPlan{plan}, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
}

// retentionInstallOne deploys a single file and returns the deployment root.
func retentionInstallOne(t *testing.T, path, content string) string {
	t.Helper()
	root := t.TempDir()
	retentionDeploy(t, root, map[string]string{path: content})
	return root
}

func TestUninstallRestoresOnlyUntouchedPreExistingFiles(t *testing.T) {
	for _, edited := range []bool{false, true} {
		root := t.TempDir()
		retentionWrite(t, filepath.Join(root, "a.txt"), "original")
		retentionDeploy(t, root, map[string]string{"a.txt": "takt"})
		if edited {
			retentionWrite(t, filepath.Join(root, "a.txt"), "user edit")
		}
		result, err := setup.UninstallContext(context.Background(), root, setup.TargetOpenCode)
		if err != nil {
			t.Fatal(err)
		}
		if restored := slices.Contains(result.Restored, "a.txt"); restored == edited {
			t.Errorf("edited = %v: Restored = %v", edited, result.Restored)
		}
		want := "original"
		if edited {
			want = "user edit"
		}
		if got := retentionRead(t, filepath.Join(root, "a.txt")); got != want {
			t.Errorf("edited = %v: a.txt = %q, want %q", edited, got, want)
		}
	}
}

func TestRetentionRequiresManifestForFileChoices(t *testing.T) {
	_, err := setup.ApplyUninstallRetention(context.Background(), t.TempDir(), setup.RetentionChoices{Keep: []string{"a.txt"}}, retentionClock)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want missing manifest", err)
	}
}

func TestRetentionCancelledLeavesChoicesNotApplied(t *testing.T) {
	root := t.TempDir()
	retentionDeploy(t, root, map[string]string{"a.txt": "a", "b.txt": "b"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := setup.ApplyUninstallRetention(ctx, root, setup.RetentionChoices{Remove: []string{"a.txt"}, Keep: []string{"b.txt"}}, retentionClock)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !slices.Equal(result.NotApplied, []string{"a.txt", "b.txt"}) {
		t.Errorf("NotApplied = %v", result.NotApplied)
	}
	if retentionRead(t, filepath.Join(root, "a.txt")) != "a" {
		t.Error("cancelled retention touched a file")
	}
}

func TestRetentionKeepsDirectoryLayoutAndPrunesEmptyParents(t *testing.T) {
	root := retentionInstallOne(t, "deep/dir/a.txt", "a")
	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Keep: []string{"deep/dir/a.txt"}}, retentionClock)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(root, setup.RetainedDirName, "20260102T030405Z")
	if result.RetainedDir != wantDir {
		t.Errorf("RetainedDir = %q, want %q", result.RetainedDir, wantDir)
	}
	if retentionRead(t, filepath.Join(wantDir, "deep", "dir", "a.txt")) != "a" {
		t.Error("kept file missing from retained layout")
	}
	if _, err := os.Stat(filepath.Join(root, "deep")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("emptied source directories survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, setup.OwnershipManifestFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("manifest survived after its last entry left: %v", err)
	}
}

func TestRetentionRemoveToleratesAlreadyDeletedFile(t *testing.T) {
	root := retentionInstallOne(t, "a.txt", "a")
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Remove: []string{"a.txt"}}, retentionClock)
	if err != nil || !slices.Equal(result.Removed, []string{"a.txt"}) {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestRetentionKeepOfMissingFileIsIncomplete(t *testing.T) {
	root := retentionInstallOne(t, "a.txt", "a")
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Keep: []string{"a.txt"}}, retentionClock)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Incomplete) != 1 || !strings.HasPrefix(result.Incomplete[0], "a.txt: ") || len(result.Retained) != 0 {
		t.Errorf("result = %+v, want one incomplete entry for a.txt", result)
	}
}

func TestRetentionBlockedRetainedDirectoryIsIncomplete(t *testing.T) {
	root := retentionInstallOne(t, "a.txt", "a")
	// A regular file where the retained directory must go blocks every reservation.
	retentionWrite(t, filepath.Join(root, setup.RetainedDirName), "in the way")

	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Keep: []string{"a.txt"}}, retentionClock)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Incomplete) != 1 || !strings.HasPrefix(result.Incomplete[0], "a.txt: ") {
		t.Errorf("Incomplete = %v, want the file reported", result.Incomplete)
	}
	if retentionRead(t, filepath.Join(root, "a.txt")) != "a" {
		t.Error("file moved although the retained directory could not be reserved")
	}
}

func TestRetentionFailsWhenDestinationDirectoryIsBlocked(t *testing.T) {
	root := retentionInstallOne(t, "child/a.txt", "a")
	reserved := t.TempDir()
	retentionWrite(t, filepath.Join(reserved, "child"), "file")
	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Keep: []string{"child/a.txt"}, RetainedDir: reserved}, retentionClock)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Incomplete) != 1 || len(result.Retained) != 0 {
		t.Errorf("result = %+v, want the file reported incomplete", result)
	}
	if retentionRead(t, filepath.Join(root, "child", "a.txt")) != "a" {
		t.Error("file moved although its destination was blocked")
	}
}

func TestRetentionRemoveOfNonEmptyDirectoryIsIncomplete(t *testing.T) {
	root := retentionInstallOne(t, "a.txt", "a")
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	retentionWrite(t, filepath.Join(root, "a.txt", "child"), "x")

	result, err := setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{Remove: []string{"a.txt"}}, retentionClock)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 0 || len(result.Incomplete) != 1 || !strings.HasPrefix(result.Incomplete[0], "a.txt: ") {
		t.Errorf("result = %+v, want the removal reported incomplete", result)
	}
}

func TestReserveRetentionDirSkipsEveryExistingCandidate(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, setup.RetainedDirName, "20260102T030405Z")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+"-1", []byte("existing file"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{base + "-2", base + "-3"} {
		got, err := setup.ReserveRetentionDir(root, retentionClock)
		if err != nil || got != want {
			t.Fatalf("ReserveRetentionDir = %q, %v; want %q", got, err, want)
		}
	}
	if got, err := os.ReadFile(base + "-1"); err != nil || string(got) != "existing file" {
		t.Fatalf("collision changed: %q, %v", got, err)
	}
}
