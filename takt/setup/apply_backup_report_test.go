package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	want := []setup.Backup{{Path: "a.txt", Copy: setup.BackupDir + "/a.txt"}}
	if !slices.Equal(result.BackedUp, want) {
		t.Fatalf("BackedUp = %v, want %v", result.BackedUp, want)
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

// TestApplyReportsTheCopyWrittenForEachReplacement checks that a second
// backup of the same path, kept apart from the first by a hash suffix, is
// the copy reported for it.
func TestApplyReportsTheCopyWrittenForEachReplacement(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.ApplyContext(context.Background(), root, simplePlan("hello"), setup.ProviderRuntime{}); err != nil {
		t.Fatalf("first ApplyContext() error = %v", err)
	}
	if err := os.WriteFile(target, []byte("second edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := setup.ApplyContext(context.Background(), root, simplePlan("hello"), setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("second ApplyContext() error = %v", err)
	}
	if len(result.BackedUp) != 1 || result.BackedUp[0].Path != "a.txt" {
		t.Fatalf("BackedUp = %v, want one copy for a.txt", result.BackedUp)
	}
	copied := result.BackedUp[0].Copy
	if !strings.HasPrefix(copied, setup.BackupDir+"/a.txt.") {
		t.Fatalf("Copy = %q, want the hash-suffixed copy beside the first backup", copied)
	}
	if got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(copied))); err != nil || string(got) != "second edit" {
		t.Errorf("reported copy = %q, %v, want the second edit", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(root, setup.BackupDir, "a.txt")); err != nil || string(got) != "first" {
		t.Errorf("first backup = %q, %v, want it untouched", got, err)
	}
}

// TestApplyRefusesAnEditMadeWhileApplying checks that a destination edited
// after inspection is never replaced: the batch stops, everything already
// installed is rolled back, and the edit stays on disk.
func TestApplyRefusesAnEditMadeWhileApplying(t *testing.T) {
	root := t.TempDir()
	plans := []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{"a.txt", "b.txt"},
		Artifacts:    []setup.Artifact{{Path: "a.txt", Content: []byte("A")}, {Path: "b.txt", Content: []byte("B")}},
	}}
	edit := func(p setup.DeploymentProgress) {
		if p.Stage == "applied" && p.Path == "a.txt" {
			if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("user edit"), 0o644); err != nil {
				t.Error(err)
			}
		}
	}
	_, err := setup.ApplyContextProgress(context.Background(), root, plans, setup.ProviderRuntime{}, edit)
	if err == nil || !strings.Contains(err.Error(), `"b.txt" changed`) {
		t.Fatalf("ApplyContextProgress() error = %v, want the concurrent edit reported", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(root, "b.txt")); readErr != nil || string(got) != "user edit" {
		t.Errorf("b.txt = %q, %v, want the user edit kept", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(statErr) {
		t.Errorf("a.txt stat = %v, want the batch rolled back", statErr)
	}
}
