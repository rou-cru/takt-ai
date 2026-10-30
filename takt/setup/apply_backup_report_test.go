package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// TestApplyReportsContentWrittenAfterValidation checks that a file an external
// actor creates between the conflict preview and the application is backed up
// and named in the result, never replaced silently (PR-INS-18, PR-INS-47).
func TestApplyReportsContentWrittenAfterValidation(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("hello")
	conflicts, err := setup.DetectConflicts(root, plans)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("DetectConflicts() = %v, %v, want no conflict on an empty root", conflicts, err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("concurrent edit"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	if !slices.Equal(result.BackedUp, []string{"a.txt"}) {
		t.Fatalf("BackedUp = %v, want [a.txt]", result.BackedUp)
	}
	backup, err := os.ReadFile(filepath.Join(root, setup.BackupDir, "a.txt"))
	if err != nil || string(backup) != "concurrent edit" {
		t.Errorf("backup = %q, %v, want the concurrent edit preserved in full", backup, err)
	}
}

// TestApplyReportsNoBackupForUneditedRedeploy checks that redeploying content
// Takt still owns unchanged reports nothing as backed up.
func TestApplyReportsNoBackupForUneditedRedeploy(t *testing.T) {
	root := t.TempDir()
	if _, err := setup.ApplyContext(context.Background(), root, simplePlan("hello"), setup.ProviderRuntime{}); err != nil {
		t.Fatalf("first ApplyContext() error = %v", err)
	}
	result, err := setup.ApplyContext(context.Background(), root, simplePlan("hello again"), setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("second ApplyContext() error = %v", err)
	}
	if len(result.BackedUp) != 0 {
		t.Errorf("BackedUp = %v, want none for an unedited managed file", result.BackedUp)
	}
}
