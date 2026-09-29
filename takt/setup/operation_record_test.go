package setup

import (
	"strings"
	"testing"
	"time"
)

func TestBackupsSince(t *testing.T) {
	root := t.TempDir()
	start := time.Now()
	if BackupsSince(root, start) != "" {
		t.Fatal("no backup dir must report none")
	}
	if _, err := backupContent(root, "x.md", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if BackupsSince(root, start) == "" || BackupsSince(root, start.Add(time.Hour)) != "" {
		t.Fatal("BackupsSince must report only copies written since start")
	}
}

func TestOperationRecordNoticePointsToRestoreWhenBackedUp(t *testing.T) {
	root := t.TempDir()
	record := OperationRecord{Action: "install"}

	notice := record.Notice(root)
	if len(notice) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(notice))
	}
	if !strings.Contains(notice[1], "no replaced content was backed up") {
		t.Fatalf("without backups, notice must say nothing was backed up, got %q", notice[1])
	}
	if strings.Contains(notice[1], "restore") {
		t.Fatalf("without backups, notice must not point to restore, got %q", notice[1])
	}

	if _, err := backupContent(root, "x.md", []byte("x")); err != nil {
		t.Fatal(err)
	}

	notice = record.Notice(root)
	if !strings.Contains(notice[1], "takt-ai restore --root "+root) {
		t.Fatalf("with a backup present, notice must point to the restore command, got %q", notice[1])
	}
}
