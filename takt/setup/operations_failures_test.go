package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

// failRenameTo makes renameFile fail for destinations with the given suffix and restores it on cleanup.
func failRenameTo(t *testing.T, suffix string) {
	t.Helper()
	original := renameFile
	t.Cleanup(func() { renameFile = original })
	renameFile = func(from, to string) error {
		if strings.HasSuffix(to, suffix) {
			return errors.New("injected rename failure")
		}
		return os.Rename(from, to)
	}
}

func TestApplyContextRejectsEmptyPlans(t *testing.T) {
	if _, err := ApplyContext(context.Background(), t.TempDir(), nil, ProviderRuntime{}); err == nil || !strings.Contains(err.Error(), "at least one target plan") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyContextFailsWhenExistingConfigIsUnreadable(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "cfg.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ApplyContext(context.Background(), root, opsPlan("opencode", map[string]string{"cfg.json": "{}"}), ProviderRuntime{})
	if err == nil || !strings.Contains(err.Error(), "cfg.json") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyContextFailsWhenManifestCannotBeSaved(t *testing.T) {
	root := t.TempDir()
	failRenameTo(t, OwnershipManifestFilename)
	_, err := ApplyContext(context.Background(), root, opsPlan("opencode", map[string]string{"a.txt": "a"}), ProviderRuntime{})
	if err == nil || !strings.Contains(err.Error(), "ownership manifest") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyContextFailsOnUninspectableArtifactPath(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("n", nameTooLongLength)
	_, err := ApplyContext(context.Background(), root, opsPlan("opencode", map[string]string{long: "a"}), ProviderRuntime{})
	if err == nil || !strings.Contains(err.Error(), "inspect managed file") {
		t.Fatalf("error = %v", err)
	}
}

func TestTakeoverBackupFailures(t *testing.T) {
	plans := opsPlan("opencode", map[string]string{"a.txt": "takt"})
	tests := []struct {
		name    string
		prepare func(t *testing.T, root, outside string)
		wantErr string
	}{
		{"backup dir is a symlink out of the root", func(t *testing.T, root, outside string) {
			if err := os.Symlink(outside, filepath.Join(root, BackupDir)); err != nil {
				t.Fatal(err)
			}
		}, "stage backup"},
		{"backup dir is a regular file", func(t *testing.T, root, _ string) {
			writeFile(t, filepath.Join(root, BackupDir), "in the way")
		}, "stage backup"},
		{"backup target is a directory", func(t *testing.T, root, _ string) {
			if err := os.MkdirAll(filepath.Join(root, BackupDir, "a.txt"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "write backup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "a.txt"), "original")
			tt.prepare(t, root, t.TempDir())
			_, err := ApplyContext(context.Background(), root, plans, ProviderRuntime{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
			if readString(t, filepath.Join(root, "a.txt")) != "original" {
				t.Error("file was overwritten although its backup failed")
			}
		})
	}
}

func TestSyncPreserveListAndNewArtifacts(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"old.txt": "v1"}))

	result, err := SyncContextProgress(context.Background(), root,
		opsPlan("opencode", map[string]string{"old.txt": "v2", "fresh.txt": "n", "held.txt": "h"}), ProviderRuntime{}, nil, "held.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Changed, []string{"fresh.txt", "old.txt"}) || !slices.Contains(result.Unchanged, "held.txt") {
		t.Errorf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "held.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("preserved path was written: %v", err)
	}
}

func TestDetectConflictsSkipsUneditedManagedFile(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "v1"}))
	conflicts, err := DetectConflicts(root, opsPlan("opencode", map[string]string{"a.txt": "v2"}))
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("conflicts = %+v, err = %v; want none for an unedited managed file", conflicts, err)
	}
}

func TestUninstallPreservesEditedPreExistingFileWithoutRestore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "original")
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "takt"}))
	writeFile(t, filepath.Join(root, "a.txt"), "user edit")

	result, err := UninstallContext(context.Background(), root, TargetOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Preserved, []string{"a.txt"}) || len(result.Restored) != 0 {
		t.Errorf("result = %+v", result)
	}
	if readString(t, filepath.Join(root, "a.txt")) != "user edit" {
		t.Error("edited file was replaced")
	}
}

func TestUninstallAndPreviewFailWhenManagedPathBecameADirectory(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "a"}))
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewUninstall(root, TargetOpenCode); err == nil || !strings.Contains(err.Error(), "inspect managed file") {
		t.Errorf("preview error = %v", err)
	}
	if _, err := UninstallContext(context.Background(), root, TargetOpenCode); err == nil || !strings.Contains(err.Error(), "inspect managed file") {
		t.Errorf("uninstall error = %v", err)
	}
	if _, err := LoadOwnershipManifest(root); err != nil {
		t.Errorf("manifest lost after failed uninstall: %v", err)
	}
}

func TestUninstallFailsWhenStagingDirectoryIsBlocked(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "a"}))
	writeFile(t, filepath.Join(root, ".takt-uninstall-staging"), "in the way")
	_, err := UninstallContext(context.Background(), root, TargetOpenCode)
	if err == nil || !strings.Contains(err.Error(), "stage managed file") {
		t.Fatalf("error = %v", err)
	}
	if readString(t, filepath.Join(root, "a.txt")) != "a" {
		t.Error("managed file was touched")
	}
}

func TestPartialUninstallFailsWhenManifestCannotBeSaved(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "a"}))
	manifest, err := LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := NewOwnershipEntry("skill.md", []byte("s"), ManagedFileMode, false, "", "", TargetSkills)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "skill.md"), "s")
	if err := manifest.Add(skill); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(root); err != nil {
		t.Fatal(err)
	}
	failRenameTo(t, OwnershipManifestFilename)

	// The skills entry stays owned, so the manifest must be rewritten, and that write fails.
	if _, err := UninstallContext(context.Background(), root, TargetOpenCode); err == nil || !strings.Contains(err.Error(), "ownership manifest") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyModelOverrideChangeRejectsUnknownInstalledComponent(t *testing.T) {
	root := t.TempDir()
	request := defaultRequestForTest(t)
	request.Components = []string{"no-such-component"}
	if err := RecordInstallation(root, request); err != nil {
		t.Fatal(err)
	}
	_, err := ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"takt-dev": {Model: "provider/model"}}, ProviderRuntime{})
	if err == nil || !strings.Contains(err.Error(), "unknown component") {
		t.Fatalf("error = %v", err)
	}
}
