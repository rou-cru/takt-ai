package lifecycle_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

// staleLauncher reports a version older than any Takt pins, so resolution
// rejects it and acquisition would replace it.
const staleLauncher = "#!/bin/sh\necho 'engram 0.0.1'\necho 0.0.1\n"

// writeStale places an executable stale launcher at rel beneath root.
func writeStale(t *testing.T, root, rel string) string {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(staleLauncher), 0o755); err != nil {
		t.Fatal(err)
	}
	return file
}

// isolatePath keeps PATH from resolving a compatible host binary.
func isolatePath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestInstallBacksUpAStaleManagedPrerequisiteBeforeReplacingIt(t *testing.T) {
	isolatePath(t)
	root := t.TempDir()
	launcher := filepath.ToSlash(filepath.Join(codegraph.ManagedPrefixKey(), "node_modules", ".bin", "codegraph"))
	writeStale(t, root, launcher)

	result, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	want := setup.Backup{Path: codegraph.ManagedPrefixKey(), Copy: setup.BackupDir + "/" + codegraph.ManagedPrefixKey()}
	if !slices.Contains(result.BackedUp, want) {
		t.Fatalf("BackedUp = %+v, want it to contain %+v", result.BackedUp, want)
	}
	copied, err := os.ReadFile(filepath.Join(root, setup.BackupDir, filepath.FromSlash(launcher)))
	if err != nil || string(copied) != staleLauncher {
		t.Fatalf("backup of the stale launcher = %q, %v; want the earlier content", copied, err)
	}
}

func TestCancelledInstallRestoresAStaleManagedPrerequisite(t *testing.T) {
	isolatePath(t)
	root := t.TempDir()
	binary := writeStale(t, root, engram.ManagedBinaryKey())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := (lifecycle.Runtime{}).Run(ctx, "install", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	if result.Outcome != lifecycle.OutcomeCancelledNothingApplied {
		t.Fatalf("Outcome = %v, want %v", result.Outcome, lifecycle.OutcomeCancelledNothingApplied)
	}
	content, err := os.ReadFile(binary)
	if err != nil || string(content) != staleLauncher {
		t.Fatalf("managed engram after cancelled install = %q, %v; want it restored", content, err)
	}
	if _, err := os.Lstat(filepath.Join(root, setup.BackupDir, filepath.FromSlash(engram.ManagedBinaryKey()))); !os.IsNotExist(err) {
		t.Fatalf("set-aside copy still present after restore: %v", err)
	}
}
