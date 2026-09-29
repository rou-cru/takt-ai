package setup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestRestoreRequiresManifest(t *testing.T) {
	if _, err := setup.Restore(t.TempDir(), nil); err == nil {
		t.Fatal("Restore(nil manifest) error = nil, want an error")
	}
}

func TestRestoreSkipsEntriesWithoutBackup(t *testing.T) {
	root := t.TempDir()
	manifest := setup.NewOwnershipManifest()
	entry, err := setup.NewOwnershipEntry("a.txt", []byte("content"), 0o644, false, "", "", setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("NewOwnershipEntry() error = %v", err)
	}
	if err := manifest.Add(entry); err != nil {
		t.Fatalf("manifest.Add() error = %v", err)
	}

	result, err := setup.Restore(root, manifest)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if len(result.Restored) != 0 {
		t.Errorf("Restore() Restored = %v, want none: the entry has no backup", result.Restored)
	}
}

func TestRestorePutsBackBackedUpContent(t *testing.T) {
	root := t.TempDir()
	backupRel := filepath.Join(".takt-backups", "a.txt")
	if err := os.MkdirAll(filepath.Join(root, ".takt-backups"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, backupRel), []byte("original content"), 0o644); err != nil {
		t.Fatalf("WriteFile() backup error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("takt content"), 0o644); err != nil {
		t.Fatalf("WriteFile() managed error = %v", err)
	}

	manifest := setup.NewOwnershipManifest()
	priorSHA := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	entry, err := setup.NewOwnershipEntry("a.txt", []byte("takt content"), 0o644, false, priorSHA, filepath.ToSlash(backupRel), setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("NewOwnershipEntry() error = %v", err)
	}
	if err := manifest.Add(entry); err != nil {
		t.Fatalf("manifest.Add() error = %v", err)
	}

	result, err := setup.Restore(root, manifest)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if len(result.Restored) != 1 || result.Restored[0] != "a.txt" {
		t.Fatalf("Restore() Restored = %v, want [a.txt]", result.Restored)
	}
	content, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(content) != "original content" {
		t.Errorf("a.txt content = %q, %v, want the restored backup content", content, err)
	}
}

func TestRestoreMissingBackupFileErrors(t *testing.T) {
	root := t.TempDir()
	manifest := setup.NewOwnershipManifest()
	entry, err := setup.NewOwnershipEntry("a.txt", []byte("takt content"), 0o644, false,
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", ".takt-backups/a.txt", setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("NewOwnershipEntry() error = %v", err)
	}
	if err := manifest.Add(entry); err != nil {
		t.Fatalf("manifest.Add() error = %v", err)
	}

	if _, err := setup.Restore(root, manifest); err == nil {
		t.Fatal("Restore() with a missing backup file error = nil, want an error")
	}
}
