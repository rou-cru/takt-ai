package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestBeginOperationRecordsAndClears(t *testing.T) {
	root := t.TempDir()
	if _, ok := setup.IncompleteOperation(root); ok {
		t.Fatal("IncompleteOperation() before BeginOperation = ok, want none recorded")
	}

	done, err := setup.BeginOperation(root, "install")
	if err != nil {
		t.Fatalf("BeginOperation() error = %v", err)
	}
	record, ok := setup.IncompleteOperation(root)
	if !ok {
		t.Fatal("IncompleteOperation() while an operation is in progress = not found, want found")
	}
	if record.Action != "install" {
		t.Errorf("record.Action = %q, want %q", record.Action, "install")
	}

	done()
	if _, ok := setup.IncompleteOperation(root); ok {
		t.Error("IncompleteOperation() after the operation's done() = found, want none")
	}
}

func TestIncompleteOperationOnFreshRoot(t *testing.T) {
	if _, ok := setup.IncompleteOperation(t.TempDir()); ok {
		t.Error("IncompleteOperation() on a fresh root = found, want none")
	}
}

// takeOverExistingFile deploys over a pre-existing file, which backs it up.
func takeOverExistingFile(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.ApplyContext(context.Background(), root, simplePlan("takt"), setup.ProviderRuntime{}); err != nil {
		t.Fatal(err)
	}
}

func TestBackupsSince(t *testing.T) {
	root := t.TempDir()
	start := time.Now()
	if setup.BackupsSince(root, start) != "" {
		t.Fatal("no backup dir must report none")
	}
	takeOverExistingFile(t, root)
	if setup.BackupsSince(root, start) == "" || setup.BackupsSince(root, start.Add(time.Hour)) != "" {
		t.Fatal("BackupsSince must report only copies written since start")
	}
}

func TestOperationRecordNoticePointsToRestoreWhenBackedUp(t *testing.T) {
	root := t.TempDir()
	record := setup.OperationRecord{Action: "install"}

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

	takeOverExistingFile(t, root)

	notice = record.Notice(root)
	if !strings.Contains(notice[1], "takt-ai restore --root "+root) {
		t.Fatalf("with a backup present, notice must point to the restore command, got %q", notice[1])
	}
}
