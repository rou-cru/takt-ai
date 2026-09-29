package setup_test

import (
	"testing"

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
