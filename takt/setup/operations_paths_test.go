package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// providerActionID names the stub integration action used by cancellation tests.
const providerActionID = "integration"

// opsPlan builds a single-target plan that manages the given path/content pairs.
func opsPlan(target string, files map[string]string) []TargetPlan {
	plan := TargetPlan{Target: target}
	for path, content := range files {
		plan.ManagedPaths = append(plan.ManagedPaths, path)
		plan.Artifacts = append(plan.Artifacts, Artifact{Path: path, Content: []byte(content)})
	}
	slices.Sort(plan.ManagedPaths)
	slices.SortFunc(plan.Artifacts, func(a, b Artifact) int { return strings.Compare(a.Path, b.Path) })
	return []TargetPlan{plan}
}

func mustApply(t *testing.T, root string, plans []TargetPlan, preserve ...string) DeploymentResult {
	t.Helper()
	result, err := ApplyContext(context.Background(), root, plans, ProviderRuntime{}, preserve...)
	if err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	return result
}

func readString(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestApplyContextProgressReportsStages(t *testing.T) {
	root := t.TempDir()
	var stages []string
	progress := func(event DeploymentProgress) { stages = append(stages, event.Stage) }

	result, err := ApplyContextProgress(context.Background(), root, opsPlan("opencode", map[string]string{"a.txt": "a"}), ProviderRuntime{}, progress)
	if err != nil || !slices.Equal(result.Changed, []string{"a.txt"}) {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if !slices.Contains(stages, "preparing") || !slices.Contains(stages, "applied") {
		t.Errorf("stages = %v, want preparing and applied", stages)
	}
}

func TestCancelledApplyListsSkippedProviderActionsAsNotApplied(t *testing.T) {
	root := t.TempDir()
	plans := opsPlan("opencode", map[string]string{"a.txt": "a"})
	plans[0].Actions = []ProviderAction{{ID: providerActionID, Program: "stub"}}
	runtime := ProviderRuntime{LookPath: func(string) (string, error) { return "/stub", nil }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := ApplyContext(ctx, root, plans, runtime)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !slices.Equal(result.NotApplied, []string{"a.txt", providerActionID}) {
		t.Errorf("NotApplied = %v, want file then provider action", result.NotApplied)
	}
	if _, statErr := os.Stat(filepath.Join(root, OwnershipManifestFilename)); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("manifest written for a deployment that applied nothing: %v", statErr)
	}
}

func TestSyncContextProgressInputErrors(t *testing.T) {
	plans := opsPlan("opencode", map[string]string{"a.txt": "a"})
	if _, err := SyncContextProgress(context.Background(), " ", plans, ProviderRuntime{}, nil); err == nil || !strings.Contains(err.Error(), "deployment root is required") {
		t.Errorf("blank root error = %v", err)
	}
	if _, err := SyncContextProgress(context.Background(), t.TempDir(), nil, ProviderRuntime{}, nil); err == nil || !strings.Contains(err.Error(), "at least one target plan") {
		t.Errorf("no plans error = %v", err)
	}
	corrupt := t.TempDir()
	writeFile(t, filepath.Join(corrupt, OwnershipManifestFilename), "{")
	if _, err := SyncContextProgress(context.Background(), corrupt, plans, ProviderRuntime{}, nil); err == nil || !strings.Contains(err.Error(), "parse ownership manifest") {
		t.Errorf("corrupt manifest error = %v", err)
	}
}

func TestSyncPreservesLocalEditsAndRedeploysDeletedFiles(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"edited.txt": "v1", "deleted.txt": "v1", "clean.txt": "v1"}))
	writeFile(t, filepath.Join(root, "edited.txt"), "mine")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}

	result, err := SyncContextProgress(context.Background(), root,
		opsPlan("opencode", map[string]string{"edited.txt": "v2", "deleted.txt": "v2", "clean.txt": "v2"}), ProviderRuntime{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := readString(t, filepath.Join(root, "edited.txt")); got != "mine" {
		t.Errorf("edited.txt = %q, want the local edit kept", got)
	}
	if got := readString(t, filepath.Join(root, "deleted.txt")); got != "v2" {
		t.Errorf("deleted.txt = %q, want redeployed v2", got)
	}
	if got := readString(t, filepath.Join(root, "clean.txt")); got != "v2" {
		t.Errorf("clean.txt = %q, want updated v2", got)
	}
	if !slices.Contains(result.Unchanged, "edited.txt") || !slices.Equal(result.Changed, []string{"clean.txt", "deleted.txt"}) {
		t.Errorf("result = %+v", result)
	}
}

func TestSyncFailsWhenManagedPathIsUnreadable(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "v1"}))
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := SyncContextProgress(context.Background(), root, opsPlan("opencode", map[string]string{"a.txt": "v2"}), ProviderRuntime{}, nil)
	if err == nil || !strings.Contains(err.Error(), `inspect managed file "a.txt"`) {
		t.Fatalf("error = %v, want inspect failure", err)
	}
}

func TestDetectConflictsErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := DetectConflicts(root, nil); err == nil {
		t.Error("DetectConflicts without plans succeeded")
	}
	writeFile(t, filepath.Join(root, OwnershipManifestFilename), "{")
	if _, err := DetectConflicts(root, opsPlan("opencode", map[string]string{"a.txt": "a"})); err == nil || !strings.Contains(err.Error(), "parse ownership manifest") {
		t.Errorf("corrupt manifest error = %v", err)
	}
	clean := t.TempDir()
	if err := os.Mkdir(filepath.Join(clean, "a.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := DetectConflicts(clean, opsPlan("opencode", map[string]string{"a.txt": "a"})); err == nil || !strings.Contains(err.Error(), "inspect managed file") {
		t.Errorf("unreadable path error = %v", err)
	}
}

func TestDetectConflictsIgnoresMissingUnmanagedAndMergeableConfig(t *testing.T) {
	root := t.TempDir()
	conflicts, err := DetectConflicts(root, opsPlan("opencode", map[string]string{"never-installed.txt": "a"}))
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("conflicts = %+v, err = %v; want none for a missing unmanaged path", conflicts, err)
	}
}

func TestConflictAcceptedRequiresMatchingUncertainRisk(t *testing.T) {
	conflict := ConflictEntry{Path: "a", Impact: ImpactUncertain, SHA256: "h"}
	accepted := map[string]RiskAcceptance{"a": {Path: "a", SHA256: "h", Impact: ImpactUncertain}}
	if !conflictAccepted(conflict, accepted) {
		t.Error("matching acceptance was not honoured")
	}
	for name, mutate := range map[string]func(*ConflictEntry){
		"changed content": func(c *ConflictEntry) { c.SHA256 = "other" },
		"incompatible":    func(c *ConflictEntry) { c.Impact = ImpactIncompatible },
		"different path":  func(c *ConflictEntry) { c.Path = "b" },
	} {
		changed := conflict
		mutate(&changed)
		if conflictAccepted(changed, accepted) {
			t.Errorf("%s: acceptance honoured, want asked again", name)
		}
	}
}

func TestUninstallAndPreviewInputErrors(t *testing.T) {
	for name, call := range map[string]func(root string, targets ...OwnershipTarget) (UninstallResult, error){
		"uninstall": func(root string, targets ...OwnershipTarget) (UninstallResult, error) {
			return UninstallContext(context.Background(), root, targets...)
		},
		"preview": PreviewUninstall,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := call(t.TempDir(), "cursor"); err == nil || !strings.Contains(err.Error(), "unknown ownership target") {
				t.Errorf("unknown target error = %v", err)
			}
			corrupt := t.TempDir()
			writeFile(t, filepath.Join(corrupt, OwnershipManifestFilename), "{")
			if _, err := call(corrupt, TargetOpenCode); err == nil || !strings.Contains(err.Error(), "parse ownership manifest") {
				t.Errorf("corrupt manifest error = %v", err)
			}
		})
	}
	if _, err := PreviewUninstall("", TargetOpenCode); err == nil {
		t.Error("PreviewUninstall with blank root succeeded")
	}
	empty, err := PreviewUninstall(t.TempDir(), TargetOpenCode)
	if err != nil || len(empty.Removed) != 0 || empty.Removed == nil || empty.Preserved == nil {
		t.Errorf("preview of nothing installed = %+v, %v; want empty non-nil lists", empty, err)
	}
}

func TestPreviewUninstallClassifiesEveryOutcome(t *testing.T) {
	root := t.TempDir()
	// preexisting.txt is taken over, then its backup is lost: preserved as pre-existing.
	writeFile(t, filepath.Join(root, "preexisting.txt"), "original")
	writeFile(t, filepath.Join(root, "additions.txt"), "original")
	mustApply(t, root, opsPlan("opencode", map[string]string{"clean.txt": "c", "edited.txt": "e", "preexisting.txt": "p", "additions.txt": "x"}))
	writeFile(t, filepath.Join(root, "edited.txt"), "mine")
	if err := os.RemoveAll(filepath.Join(root, BackupDir, "preexisting.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "preexisting.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "additions.txt"), "user changed after takeover")

	preview, err := PreviewUninstall(root, TargetOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(preview.Removed, []string{"clean.txt"}) {
		t.Errorf("Removed = %v", preview.Removed)
	}
	want := map[string]string{"edited.txt": "user-edited", "preexisting.txt": "pre-existing", "additions.txt": "takt-additions"}
	for path, reason := range want {
		if preview.PreservedReasons[path] != reason {
			t.Errorf("PreservedReasons[%s] = %q, want %q", path, preview.PreservedReasons[path], reason)
		}
	}
	if len(preview.Preserved) != len(want) {
		t.Errorf("Preserved = %v", preview.Preserved)
	}
}

func TestUninstallCancelledLeavesEntriesNotApplied(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "a", "b.txt": "b"}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := UninstallContext(ctx, root, TargetOpenCode)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !slices.Equal(result.NotApplied, []string{"a.txt", "b.txt"}) {
		t.Errorf("NotApplied = %v", result.NotApplied)
	}
	if readString(t, filepath.Join(root, "a.txt")) != "a" {
		t.Error("cancelled uninstall touched a managed file")
	}
}

func TestUninstallReportsFilesAlreadyDeletedAndPrunesEmptyDirs(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"d/e/gone.txt": "g", "d/e/f/kept.txt": "k", "top/x.txt": "x"}))
	if err := os.Remove(filepath.Join(root, "d", "e", "gone.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "top", "user.txt"), "user file")

	result, err := UninstallContext(context.Background(), root, TargetOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Removed, []string{"d/e/f/kept.txt", "d/e/gone.txt", "top/x.txt"}) {
		t.Errorf("Removed = %v", result.Removed)
	}
	if _, err := os.Stat(filepath.Join(root, "d")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("emptied directory tree survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "top", "user.txt")); err != nil {
		t.Errorf("directory holding a user file was pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".takt-uninstall-staging")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staging directory left behind: %v", err)
	}
}

func TestPartialUninstallKeepsSharedSkillsAndRewritesManifest(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"agent.md": "a"}))
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

	// Selecting only skills while opencode still owns files must leave the skill alone.
	result, err := UninstallContext(context.Background(), root, TargetSkills)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 0 || readString(t, filepath.Join(root, "skill.md")) != "s" {
		t.Errorf("shared skill was removed: %+v", result)
	}
	reloaded, err := LoadOwnershipManifest(root)
	if err != nil || len(reloaded.Entries) != 2 {
		t.Errorf("manifest = %+v, %v; want both entries kept", reloaded, err)
	}
}

func TestUninstallRollsBackWhenStagingFails(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "a", "b.txt": "b"}))
	original := renameFile
	t.Cleanup(func() { renameFile = original })
	renameFile = func(from, to string) error {
		if strings.HasSuffix(from, "b.txt") {
			return errors.New("injected staging failure")
		}
		return os.Rename(from, to)
	}

	_, err := UninstallContext(context.Background(), root, TargetOpenCode)
	if err == nil || !strings.Contains(err.Error(), "stage managed file") {
		t.Fatalf("error = %v, want staging failure", err)
	}
	renameFile = original
	if readString(t, filepath.Join(root, "a.txt")) != "a" || readString(t, filepath.Join(root, "b.txt")) != "b" {
		t.Error("files were not restored after the failed uninstall")
	}
	if _, err := LoadOwnershipManifest(root); err != nil {
		t.Errorf("manifest lost after failed uninstall: %v", err)
	}
}

func TestUninstallFailsWhenBackupIsGoneForRestorableEntry(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "original")
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "takt"}))
	// Make the recorded backup a directory: it still "exists" for the
	// restorable check, but reading it as content fails.
	backup := filepath.Join(root, BackupDir, "a.txt")
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := UninstallContext(context.Background(), root, TargetOpenCode)
	if err == nil || !strings.Contains(err.Error(), "read backup") {
		t.Fatalf("error = %v, want backup read failure", err)
	}
	if readString(t, filepath.Join(root, "a.txt")) != "takt" {
		t.Error("managed file changed although restore failed")
	}
}

func TestReapplyBacksUpUserEditsWithoutOverwritingEarlierBackup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "original")
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "v1"}))
	writeFile(t, filepath.Join(root, "a.txt"), "user edit")

	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "v2"}))

	if got := readString(t, filepath.Join(root, BackupDir, "a.txt")); got != "original" {
		t.Errorf("first backup = %q, want the pre-Takt original untouched", got)
	}
	manifest, err := LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entries["a.txt"]
	if !entry.PreExisting || entry.BackupPath != BackupDir+"/a.txt" {
		t.Errorf("entry = %+v, want takeover backup retained", entry)
	}
	edits, _ := filepath.Glob(filepath.Join(root, BackupDir, "a.txt.*"))
	if len(edits) != 1 || readString(t, edits[0]) != "user edit" {
		t.Errorf("edit backups = %v, want one hashed copy of the user edit", edits)
	}
}

func TestReapplyRecordsBackupForEditedNonPreExistingFile(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "v1"}))
	writeFile(t, filepath.Join(root, "a.txt"), "user edit")
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "v2"}))

	manifest, err := LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entries["a.txt"]
	if entry.PreExisting || entry.BackupPath != BackupDir+"/a.txt" || entry.PriorSHA256 != hashOf([]byte("user edit")) {
		t.Errorf("entry = %+v, want the edit backed up as prior content", entry)
	}
	if readString(t, filepath.Join(root, "a.txt")) != "v2" {
		t.Error("new version not deployed")
	}
}

func TestApplyKeepsManagedEntryWhenFileWasDeleted(t *testing.T) {
	root := t.TempDir()
	mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "v1"}))
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	result := mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "v1"}))
	if !slices.Equal(result.Changed, []string{"a.txt"}) {
		t.Errorf("Changed = %v, want redeploy", result.Changed)
	}
}

func TestApplyRejectsUnreadableOrInvalidArtifacts(t *testing.T) {
	t.Run("directory in place of a file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "a.txt"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := ApplyContext(context.Background(), root, opsPlan("opencode", map[string]string{"a.txt": "a"}), ProviderRuntime{})
		if err == nil || !strings.Contains(err.Error(), "inspect managed file") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("directory in place of a managed file", func(t *testing.T) {
		root := t.TempDir()
		mustApply(t, root, opsPlan("opencode", map[string]string{"a.txt": "a"}))
		if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "a.txt"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := ApplyContext(context.Background(), root, opsPlan("opencode", map[string]string{"a.txt": "a"}), ProviderRuntime{})
		if err == nil || !strings.Contains(err.Error(), "inspect managed file") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("empty artifact content", func(t *testing.T) {
		root := t.TempDir()
		_, err := ApplyContext(context.Background(), root, opsPlan("opencode", map[string]string{"a.txt": ""}), ProviderRuntime{})
		if err == nil || !strings.Contains(err.Error(), "requires managed content") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("corrupt manifest", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, OwnershipManifestFilename), "{")
		if _, err := ApplyContext(context.Background(), root, opsPlan("opencode", map[string]string{"a.txt": "a"}), ProviderRuntime{}); err == nil {
			t.Error("corrupt manifest was accepted")
		}
	})
}

func TestFlattenPlansValidation(t *testing.T) {
	plan := func(target string, managed []string, artifacts ...string) TargetPlan {
		p := TargetPlan{Target: target, ManagedPaths: managed}
		for _, path := range artifacts {
			p.Artifacts = append(p.Artifacts, Artifact{Path: path, Content: []byte("x")})
		}
		return p
	}
	tests := []struct {
		name    string
		plans   []TargetPlan
		wantErr string
	}{
		{"no plans", nil, "at least one target plan"},
		{"blank target", []TargetPlan{plan(" ", []string{"a"})}, "identity is required"},
		{"duplicate target", []TargetPlan{plan("opencode", []string{"a"}), plan("opencode", []string{"b"})}, "duplicate target plan"},
		{"no managed paths", []TargetPlan{plan("opencode", nil)}, "no managed paths"},
		{"bad managed path", []TargetPlan{plan("opencode", []string{"../a"})}, "invalid managed path"},
		{"bad artifact path", []TargetPlan{plan("opencode", []string{"a"}, "../a")}, "invalid artifact path"},
		{"cross-target managed path", []TargetPlan{plan("opencode", []string{"a"}), plan("skills", []string{"./a"})}, "belongs to targets"},
		{"cross-target artifact path", []TargetPlan{plan("opencode", []string{"a"}, "shared"), plan("skills", []string{"b"}, "shared")}, "belongs to targets"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := flattenPlans(tt.plans)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestApplyPreserveKeepsFileUntouchedAndReportsUnchanged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.txt"), "mine")
	result := mustApply(t, root, opsPlan("opencode", map[string]string{"keep.txt": "takt", "new.txt": "n"}), "keep.txt")
	if readString(t, filepath.Join(root, "keep.txt")) != "mine" {
		t.Error("preserved file was overwritten")
	}
	if !slices.Equal(result.Unchanged, []string{"keep.txt"}) || !slices.Equal(result.Changed, []string{"new.txt"}) {
		t.Errorf("result = %+v", result)
	}
}
