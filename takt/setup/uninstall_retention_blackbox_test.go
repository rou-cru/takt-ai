package setup_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
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
	root, outside, memory := t.TempDir(), t.TempDir(), t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(memory, setup.EngramDatabaseName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE observations (id INTEGER PRIMARY KEY, title TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, setup.RetainedDirName)); err != nil {
		t.Fatal(err)
	}
	_, err = setup.ApplyUninstallRetention(context.Background(), root, setup.RetentionChoices{MemoryFrom: memory}, time.Now())
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("retention wrote outside the root: %v (err %v)", entries, err)
	}
}
