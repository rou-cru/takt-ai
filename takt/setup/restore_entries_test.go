package setup_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// restoreOne restores a single hand-built entry through the public Restore.
func restoreOne(root string, entry setup.OwnershipEntry) error {
	manifest := setup.NewOwnershipManifest()
	manifest.Entries[entry.Path] = entry
	_, err := setup.Restore(root, manifest)
	return err
}

func writeRestoreFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsBadEntries(t *testing.T) {
	root := t.TempDir()
	backup := setup.BackupDir + "/b"
	writeRestoreFile(t, filepath.Join(root, setup.BackupDir, "b"), "old")
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	writeRestoreFile(t, filepath.Join(root, "file"), "x")
	tests := map[string]struct {
		entry   setup.OwnershipEntry
		wantErr string
	}{
		"invalid managed path":             {setup.OwnershipEntry{Path: "../a", BackupPath: backup}, "invalid managed path"},
		"invalid backup path":              {setup.OwnershipEntry{Path: "a", BackupPath: "../b"}, "invalid backup path"},
		"backup through link":              {setup.OwnershipEntry{Path: "a", BackupPath: "link/b"}, "resolve backup"},
		"destination through link":         {setup.OwnershipEntry{Path: "link/a", BackupPath: backup}, "resolve destination"},
		"destination directory is blocked": {setup.OwnershipEntry{Path: "file/a", BackupPath: backup}, "inspect managed file"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := restoreOne(root, tt.entry)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRestoreDefaultsModeWhenUnrecorded(t *testing.T) {
	root := t.TempDir()
	writeRestoreFile(t, filepath.Join(root, setup.BackupDir, "b"), "old")
	if err := restoreOne(root, setup.OwnershipEntry{Path: "defaulted.txt", BackupPath: setup.BackupDir + "/b"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "defaulted.txt"))
	if err != nil || info.Mode().Perm() != setup.ManagedFileMode {
		t.Errorf("mode = %v, %v; want %v", info.Mode().Perm(), err, setup.ManagedFileMode)
	}
}

func TestRestorePropagatesEntryFailure(t *testing.T) {
	err := restoreOne(t.TempDir(), setup.OwnershipEntry{Path: "a", BackupPath: setup.BackupDir + "/missing"})
	if err == nil || !strings.Contains(err.Error(), "read backup") {
		t.Fatalf("error = %v", err)
	}
}

func TestRestoreFailsWhenDestinationDirectoryIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	root := t.TempDir()
	writeRestoreFile(t, filepath.Join(root, setup.BackupDir, "b"), "old")
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	err := restoreOne(root, setup.OwnershipEntry{Path: "locked/a.txt", BackupPath: setup.BackupDir + "/b"})
	if err == nil || !strings.Contains(err.Error(), "stage restore") {
		t.Fatalf("error = %v", err)
	}
}
