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

// nameTooLongLength exceeds the 255-byte filename limit common to Linux and macOS filesystems.
const nameTooLongLength = 300

// skipIfRoot skips permission-based tests: root ignores mode bits.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeployCancelledBeforeCommitAppliesNothing(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := DeployContextProgress(ctx, root, []string{"new/a.txt", "b.txt"}, []Artifact{
		{Path: "new/a.txt", Content: []byte("a")},
		{Path: "b.txt", Content: []byte("b")},
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !slices.Equal(result.NotApplied, []string{"b.txt", "new/a.txt"}) || len(result.Changed) != 0 {
		t.Fatalf("result = %+v, want everything not applied", result)
	}
	if _, statErr := os.Stat(filepath.Join(root, "new")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("created directory survived cancellation: %v", statErr)
	}
	assertNoDeploymentTemps(t, root)
}

func TestDeployCancelledMidCommitKeepsInstalledFiles(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancel as soon as the first artifact is reported applied.
	progress := func(event DeploymentProgress) {
		if event.Stage == "applied" {
			cancel()
		}
	}

	result, err := DeployContextProgress(ctx, root, []string{"a.txt", "b.txt"}, []Artifact{
		{Path: "a.txt", Content: []byte("a")},
		{Path: "b.txt", Content: []byte("b")},
	}, progress)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !slices.Equal(result.Changed, []string{"a.txt"}) || !slices.Equal(result.NotApplied, []string{"b.txt"}) {
		t.Fatalf("result = %+v, want a.txt changed and b.txt not applied", result)
	}
	if got, readErr := os.ReadFile(filepath.Join(root, "a.txt")); readErr != nil || string(got) != "a" {
		t.Errorf("installed a.txt = %q, %v", got, readErr)
	}
	assertNoDeploymentTemps(t, root)
}

func TestDeployCommitFailureRemovesEarlierNewFiles(t *testing.T) {
	root := t.TempDir()
	original := renameFile
	t.Cleanup(func() { renameFile = original })
	renameFile = func(from, to string) error {
		if strings.HasSuffix(to, "z.txt") {
			return errors.New("injected rename failure")
		}
		return os.Rename(from, to)
	}

	_, err := DeployContextProgress(context.Background(), root, []string{"a.txt", "z.txt"}, []Artifact{
		{Path: "a.txt", Content: []byte("a")},
		{Path: "z.txt", Content: []byte("z")},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "z.txt") {
		t.Fatalf("error = %v, want z.txt failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "a.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("earlier new file survived rollback: %v", statErr)
	}
	assertNoDeploymentTemps(t, root)
}

func TestDeployReportsTemporaryCleanupFailureAfterCommit(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	writeFile(t, target, "old")
	// After the install, swap the leftover backup temp for a non-empty
	// directory so removing it fails.
	progress := func(event DeploymentProgress) {
		if event.Stage != "applied" {
			return
		}
		matches, _ := filepath.Glob(filepath.Join(root, ".takt-setup-*"))
		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				t.Error(err)
			}
			writeFile(t, filepath.Join(match, "child"), "x")
		}
	}

	_, err := DeployContextProgress(context.Background(), root, []string{"a.txt"}, []Artifact{{Path: "a.txt", Content: []byte("new")}}, progress)
	if err == nil || !strings.Contains(err.Error(), "temporary cleanup failed") {
		t.Fatalf("error = %v, want temporary cleanup failure", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Errorf("a.txt = %q, want committed content", got)
	}
}

func TestDeployStagingFailureInReadOnlyRootAborts(t *testing.T) {
	skipIfRoot(t)
	for name, artifactPath := range map[string]string{
		"file in root":       "a.txt",
		"file in new subdir": "sub/a.txt",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

			result, err := DeployContextProgress(context.Background(), root, []string{artifactPath}, []Artifact{{Path: artifactPath, Content: []byte("x")}}, nil)
			if err == nil || !strings.Contains(err.Error(), artifactPath) {
				t.Fatalf("error = %v, want failure naming %s", err, artifactPath)
			}
			if len(result.Changed) != 0 {
				t.Errorf("result = %+v, want nothing changed", result)
			}
		})
	}
}

func TestInspectArtifactRejectsUnreadableManagedFile(t *testing.T) {
	skipIfRoot(t)
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	writeFile(t, target, "secret")
	if err := os.Chmod(target, 0); err != nil {
		t.Fatal(err)
	}
	_, _, err := inspectArtifact(root, Artifact{Path: "a.txt", Content: []byte("new")})
	if err == nil || !strings.Contains(err.Error(), "read managed artifact") {
		t.Fatalf("error = %v, want read failure", err)
	}
}

func TestInspectArtifactPropagatesPathErrors(t *testing.T) {
	root := t.TempDir()
	_, _, err := inspectArtifact(root, Artifact{Path: strings.Repeat("n", nameTooLongLength), Content: []byte("x")})
	if err == nil || !strings.Contains(err.Error(), "inspect deployment path") {
		t.Fatalf("error = %v, want path inspection failure", err)
	}
}

func TestInspectDeploymentPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "file"), "x")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(root, "leaf")); err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"file/child": "is not a directory",
		"link/child": "contains a symlink",
		"leaf":       "contains a symlink",
	}
	for rel, want := range tests {
		if _, err := inspectDeploymentPath(root, rel); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("inspectDeploymentPath(%q) error = %v, want %q", rel, err, want)
		}
	}
	missing, err := inspectDeploymentPath(root, "x/y/z.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "x"), filepath.Join(root, "x", "y")}
	if !slices.Equal(missing, want) {
		t.Errorf("missing dirs = %v, want %v", missing, want)
	}
}

func TestCreateMissingDirectories(t *testing.T) {
	root := t.TempDir()
	created := map[string]struct{}{}
	var order []string
	first, second := filepath.Join(root, "a"), filepath.Join(root, "a", "b")

	if err := createMissingDirectories([]string{first, second}, created, &order); err != nil {
		t.Fatal(err)
	}
	// Already-recorded directories are skipped; already-existing ones are tolerated.
	if err := createMissingDirectories([]string{first}, created, &order); err != nil {
		t.Fatal(err)
	}
	if err := createMissingDirectories([]string{first}, map[string]struct{}{}, &order); err != nil {
		t.Fatalf("existing directory should be tolerated: %v", err)
	}
	if len(order) != 3 || order[0] != first || order[1] != second {
		t.Errorf("creation order = %v", order)
	}
	err := createMissingDirectories([]string{filepath.Join(root, "absent", "child")}, map[string]struct{}{}, &order)
	if err == nil || !strings.Contains(err.Error(), "create parent directory") {
		t.Errorf("error = %v, want parent creation failure", err)
	}
}

func TestValidateDeploymentInputs(t *testing.T) {
	if _, err := validateManagedPaths([]string{"../x"}); err == nil || !strings.Contains(err.Error(), "invalid managed path") {
		t.Errorf("validateManagedPaths(escape) error = %v", err)
	}
	if _, err := validateManagedPaths([]string{"a", "./a"}); err == nil || !strings.Contains(err.Error(), "duplicate managed path") {
		t.Errorf("validateManagedPaths(duplicate) error = %v", err)
	}
	managed := map[string]struct{}{"a": {}}
	for _, input := range [][]Artifact{
		{{Path: "../a"}},
		{{Path: "a"}, {Path: "./a"}},
		{{Path: "b"}},
	} {
		if _, err := validateArtifacts(managed, input); err == nil {
			t.Errorf("validateArtifacts(%v) succeeded, want error", input)
		}
	}
	if err := validateArtifactPathConflicts([]Artifact{{Path: "a"}, {Path: "a/b"}}); err == nil {
		t.Error("file-then-child artifact conflict was accepted")
	}
	if _, _, err := prepareDeployment("  ", nil, nil); err == nil {
		t.Error("blank deployment root was accepted")
	}
}

func TestRollbackReportsDirectoryAndSyncFailures(t *testing.T) {
	root := t.TempDir()
	occupied := filepath.Join(root, "occupied")
	writeFile(t, filepath.Join(occupied, "child"), "x")
	orphan := filepath.Join(root, "gone", "dir")

	tx := &deploymentTransaction{created: map[string]struct{}{}, createdOrder: []string{orphan, occupied}}
	err := tx.abort(errors.New("original"))
	if err == nil || !strings.Contains(err.Error(), "original") || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("abort() error = %v, want original plus rollback failure", err)
	}
	for _, want := range []string{"remove created directory", "sync parent of removed directory"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestStagedDeploymentRestoreFailures(t *testing.T) {
	root := t.TempDir()

	t.Run("not installed is a no-op", func(t *testing.T) {
		if err := (&stagedDeployment{}).restore(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("missing backup fails retry", func(t *testing.T) {
		dest := filepath.Join(root, "a.txt")
		writeFile(t, dest, "installed")
		file := &stagedDeployment{path: "a.txt", destination: dest, backup: filepath.Join(root, "no-backup"), exists: true, installed: true}
		err := file.restore()
		if err == nil || !strings.Contains(err.Error(), "restore managed artifact") || !strings.Contains(err.Error(), "retry restore") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("destination that cannot be removed", func(t *testing.T) {
		dest := filepath.Join(root, "dir")
		writeFile(t, filepath.Join(dest, "child"), "x")
		backup := filepath.Join(root, "backup")
		writeFile(t, backup, "old")
		err := restoreBackup(&stagedDeployment{destination: dest, backup: backup})
		if err == nil || !strings.Contains(err.Error(), "remove installed file") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("new file already gone", func(t *testing.T) {
		file := &stagedDeployment{path: "b", destination: filepath.Join(root, "b"), installed: true}
		if err := file.restore(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("new file removal fails", func(t *testing.T) {
		dest := filepath.Join(root, "occupied")
		writeFile(t, filepath.Join(dest, "child"), "x")
		err := (&stagedDeployment{path: "occupied", destination: dest, installed: true}).restore()
		if err == nil || !strings.Contains(err.Error(), "remove new managed artifact") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("new file removed", func(t *testing.T) {
		dest := filepath.Join(root, "c")
		writeFile(t, dest, "x")
		if err := (&stagedDeployment{path: "c", destination: dest, installed: true}).restore(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("file survived removal: %v", err)
		}
	})
}
