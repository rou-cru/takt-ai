package setup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func simplePlan(content string) []setup.TargetPlan {
	return []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{"a.txt"},
		Artifacts:    []setup.Artifact{{Path: "a.txt", Content: []byte(content)}},
	}}
}

func TestApplyContextInstallsAndReapplyIsUnchanged(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("hello")

	result, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	if len(result.Changed) != 1 || result.Changed[0] != "a.txt" {
		t.Fatalf("first ApplyContext() Changed = %v, want [a.txt]", result.Changed)
	}
	content, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("a.txt content = %q, %v, want %q", content, err, "hello")
	}
	if _, err := setup.LoadOwnershipManifest(root); err != nil {
		t.Fatalf("LoadOwnershipManifest() error = %v, want the manifest to exist after install", err)
	}

	result, err = setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("second ApplyContext() error = %v", err)
	}
	if len(result.Changed) != 0 {
		t.Errorf("second ApplyContext() Changed = %v, want none (identical content)", result.Changed)
	}
	if len(result.Unchanged) != 1 || result.Unchanged[0] != "a.txt" {
		t.Errorf("second ApplyContext() Unchanged = %v, want [a.txt]", result.Unchanged)
	}
}

func TestApplyContextRejectsUnsupportedTarget(t *testing.T) {
	root := t.TempDir()
	plans := []setup.TargetPlan{{
		Target:       "not-a-real-target",
		ManagedPaths: []string{"a.txt"},
		Artifacts:    []setup.Artifact{{Path: "a.txt", Content: []byte("x")}},
	}}
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err == nil {
		t.Fatal("ApplyContext() error = nil, want an error for an unsupported target")
	}
}

func TestUninstallContextRemovesOwnedFiles(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("hello")
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}

	result, err := setup.UninstallContext(context.Background(), root, setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("UninstallContext() error = %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "a.txt" {
		t.Fatalf("UninstallContext() Removed = %v, want [a.txt]", result.Removed)
	}
	if _, statErr := os.Stat(filepath.Join(root, "a.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a.txt still exists after uninstall: %v", statErr)
	}
	if _, err := setup.LoadOwnershipManifest(root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("LoadOwnershipManifest() error = %v, want the manifest gone once every entry is removed", err)
	}
}

func TestUninstallContextNothingInstalled(t *testing.T) {
	root := t.TempDir()
	result, err := setup.UninstallContext(context.Background(), root, setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("UninstallContext() on an empty root error = %v", err)
	}
	if len(result.Removed) != 0 || len(result.Preserved) != 0 {
		t.Errorf("UninstallContext() on an empty root = %+v, want nothing removed or preserved", result)
	}
}

func TestUninstallContextRequiresAtLeastOneTarget(t *testing.T) {
	if _, err := setup.UninstallContext(context.Background(), t.TempDir()); err == nil {
		t.Fatal("UninstallContext() with no targets error = nil, want an error")
	}
}

func TestUninstallContextRequiresRootDir(t *testing.T) {
	if _, err := setup.UninstallContext(context.Background(), "", setup.TargetOpenCode); err == nil {
		t.Fatal("UninstallContext() with an empty root error = nil, want an error")
	}
}

func TestPreviewUninstallDoesNotTouchDisk(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("hello")
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}

	preview, err := setup.PreviewUninstall(root, setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("PreviewUninstall() error = %v", err)
	}
	if len(preview.Removed) != 1 || preview.Removed[0] != "a.txt" {
		t.Fatalf("PreviewUninstall() Removed = %v, want [a.txt]", preview.Removed)
	}
	if _, statErr := os.Stat(filepath.Join(root, "a.txt")); statErr != nil {
		t.Errorf("PreviewUninstall() must not remove a.txt from disk, stat error = %v", statErr)
	}
	if _, err := setup.LoadOwnershipManifest(root); err != nil {
		t.Errorf("PreviewUninstall() must not touch the manifest, load error = %v", err)
	}
}

func TestUninstallContextPreservesUserEditedFile(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("hello")
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("user edited this"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := setup.UninstallContext(context.Background(), root, setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("UninstallContext() error = %v", err)
	}
	if len(result.Preserved) != 1 || result.Preserved[0] != "a.txt" {
		t.Fatalf("UninstallContext() Preserved = %v, want [a.txt] since the file was edited", result.Preserved)
	}
	content, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(content) != "user edited this" {
		t.Errorf("edited a.txt content = %q, %v, want it preserved unchanged", content, err)
	}
}

func TestDetectConflictsPreExistingUnmanagedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("already here"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	plans := simplePlan("hello")

	conflicts, err := setup.DetectConflicts(root, plans)
	if err != nil {
		t.Fatalf("DetectConflicts() error = %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("DetectConflicts() = %+v, want exactly one conflict for the pre-existing file", conflicts)
	}
	if conflicts[0].Reason != "pre-existing" {
		t.Errorf("conflict.Reason = %q, want %q", conflicts[0].Reason, "pre-existing")
	}
	if conflicts[0].Impact != setup.ImpactUncertain {
		t.Errorf("conflict.Impact = %q, want %q", conflicts[0].Impact, setup.ImpactUncertain)
	}
	if conflicts[0].Accepted {
		t.Error("conflict.Accepted = true, want false: nothing was recorded as accepted yet")
	}
}

func TestDetectConflictsNoConflictWhenContentMatches(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("hello")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	conflicts, err := setup.DetectConflicts(root, plans)
	if err != nil {
		t.Fatalf("DetectConflicts() error = %v", err)
	}
	if len(conflicts) != 0 {
		t.Errorf("DetectConflicts() = %+v, want none when the pre-existing content already matches", conflicts)
	}
}

func TestUninstallContextKeepsSharedFileWhileOtherOwnerRemains(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "shared.txt"), []byte("shared content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	manifest := setup.NewOwnershipManifest()
	entry, err := setup.NewOwnershipEntry("shared.txt", []byte("shared content"), 0o644, false, "", "", setup.TargetOpenCode, setup.TargetSkills)
	if err != nil {
		t.Fatalf("NewOwnershipEntry() error = %v", err)
	}
	if err := manifest.Add(entry); err != nil {
		t.Fatalf("manifest.Add() error = %v", err)
	}
	if err := manifest.Save(root); err != nil {
		t.Fatalf("manifest.Save() error = %v", err)
	}

	result, err := setup.UninstallContext(context.Background(), root, setup.TargetSkills)
	if err != nil {
		t.Fatalf("UninstallContext(TargetSkills) error = %v", err)
	}
	if len(result.Removed) != 0 {
		t.Errorf("UninstallContext(TargetSkills) Removed = %v, want none: OpenCode still owns the file", result.Removed)
	}
	if _, statErr := os.Stat(filepath.Join(root, "shared.txt")); statErr != nil {
		t.Errorf("shared.txt missing after a partial uninstall: %v", statErr)
	}
	reloaded, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("LoadOwnershipManifest() error = %v", err)
	}
	if len(reloaded.Entries["shared.txt"].Targets) != 2 {
		t.Errorf("shared.txt targets = %v, want both owners still recorded", reloaded.Entries["shared.txt"].Targets)
	}
}

func TestUninstallContextRestoresPreExistingTakenOverFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("original user content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// Takt takes over the pre-existing file: it backs up the original
	// content before overwriting it with the deployed artifact.
	if _, err := setup.ApplyContext(context.Background(), root, simplePlan("hello"), setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("a.txt content after takeover = %q, %v, want %q", content, err, "hello")
	}

	// Uninstalling an unedited takeover restores the original content.
	result, err := setup.UninstallContext(context.Background(), root, setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("UninstallContext() error = %v", err)
	}
	if len(result.Restored) != 1 || result.Restored[0] != "a.txt" {
		t.Fatalf("UninstallContext() Restored = %v, want [a.txt]", result.Restored)
	}
	restored, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(restored) != "original user content" {
		t.Errorf("a.txt content after uninstall = %q, %v, want the original pre-existing content", restored, err)
	}
}

func TestPreviewUninstallReportsRestorableForPreExistingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("original user content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := setup.ApplyContext(context.Background(), root, simplePlan("hello"), setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}

	preview, err := setup.PreviewUninstall(root, setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("PreviewUninstall() error = %v", err)
	}
	if len(preview.Restored) != 1 || preview.Restored[0] != "a.txt" {
		t.Fatalf("PreviewUninstall() Restored = %v, want [a.txt]", preview.Restored)
	}
	if content, statErr := os.ReadFile(filepath.Join(root, "a.txt")); statErr != nil || string(content) != "hello" {
		t.Errorf("PreviewUninstall() must not touch disk; a.txt = %q, %v", content, statErr)
	}
}

func TestDetectConflictsUserEditedManagedFile(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("hello")
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("edited"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	conflicts, err := setup.DetectConflicts(root, plans)
	if err != nil {
		t.Fatalf("DetectConflicts() error = %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].Reason != "user-edited" {
		t.Fatalf("DetectConflicts() = %+v, want one user-edited conflict", conflicts)
	}
}
