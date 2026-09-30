package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDeployPreflightsEveryDestinationBeforeWriting(t *testing.T) {
	root := t.TempDir()
	blockedPath := filepath.Join(root, "blocked")
	if err := os.WriteFile(blockedPath, []byte("user file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := DeployContextProgress(context.Background(), root, []string{"a.txt", "blocked/b.txt"}, []Artifact{
		{Path: "a.txt", Content: []byte("must not be deployed\n")},
		{Path: "blocked/b.txt", Content: []byte("must not be deployed\n")},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("DeployContextProgress() error = %v, want blocking parent error", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "a.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a.txt stat error = %v, want absent", statErr)
	}
	if got, readErr := os.ReadFile(blockedPath); readErr != nil || string(got) != "user file\n" {
		t.Fatalf("blocked content = %q, error = %v", got, readErr)
	}
	assertNoDeploymentTemps(t, root)
}

func TestDeployDeterministicResultOrder(t *testing.T) {
	root := t.TempDir()
	managed := []string{"b.txt", "a.txt"}
	result, err := DeployContextProgress(context.Background(), root, managed, []Artifact{
		{Path: "b.txt", Content: []byte("b")},
		{Path: "a.txt", Content: []byte("a")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Changed, []string{"a.txt", "b.txt"}) {
		t.Errorf("changed paths = %v, want sorted paths", result.Changed)
	}
}

func TestDeployContextProgressReportsPreparedAndAppliedArtifacts(t *testing.T) {
	root := t.TempDir()
	var events []DeploymentProgress
	result, err := DeployContextProgress(context.Background(), root, []string{"a.txt", "b.txt"}, []Artifact{
		{Path: "b.txt", Content: []byte("b")},
		{Path: "a.txt", Content: []byte("a")},
	}, func(event DeploymentProgress) {
		events = append(events, event)
		if event.Stage == "applied" {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(event.Path))); err != nil {
				t.Errorf("applied event for %q before destination exists: %v", event.Path, err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Changed, []string{"a.txt", "b.txt"}) {
		t.Fatalf("changed paths = %v", result.Changed)
	}
	if len(events) != 4 {
		t.Fatalf("progress events = %#v, want 2 preparing and 2 applied", events)
	}
	for index, stage := range []string{"preparing", "preparing", "applied", "applied"} {
		if events[index].Stage != stage || events[index].Total != 2 {
			t.Errorf("event[%d] = %#v, want stage %q of 2", index, events[index], stage)
		}
	}
	if events[2].Completed != 1 || events[3].Completed != 2 {
		t.Errorf("applied counts = %d, %d; want 1, 2", events[2].Completed, events[3].Completed)
	}
}

func TestDeployRejectsSeparatedArtifactPathConflict(t *testing.T) {
	root := t.TempDir()
	_, err := DeployContextProgress(context.Background(), root, []string{"a", "a.foo", "a/b"}, []Artifact{
		{Path: "a", Content: []byte("file")},
		{Path: "a.foo", Content: []byte("sibling")},
		{Path: "a/b", Content: []byte("child")},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), `artifact path "a" conflicts with child artifact "a/b"`) {
		t.Fatalf("DeployContextProgress() error = %v, want separated parent/child conflict", err)
	}
}

func TestDeployRejectsInvalidInputWithoutWriting(t *testing.T) {
	tests := []struct {
		name     string
		managed  []string
		artifact Artifact
		wantErr  string
	}{
		{name: "absolute artifact", managed: []string{"managed.txt"}, artifact: Artifact{Path: "/tmp/escape", Content: []byte("bad")}, wantErr: "path must be relative"},
		{name: "traversal artifact", managed: []string{"managed.txt"}, artifact: Artifact{Path: "../escape", Content: []byte("bad")}, wantErr: "path must not contain '..'"},
		{name: "backslash artifact", managed: []string{"managed.txt"}, artifact: Artifact{Path: "nested\\escape", Content: []byte("bad")}, wantErr: "slash separators"},
		{name: "duplicate artifacts", managed: []string{"managed.txt"}, artifact: Artifact{Path: "managed.txt", Content: []byte("bad")}, wantErr: "duplicate artifact path"},
		{name: "unmanaged artifact", managed: []string{"managed.txt"}, artifact: Artifact{Path: "user.txt", Content: []byte("bad")}, wantErr: "is not managed"},
		{name: "duplicate managed paths", managed: []string{"managed.txt", "./managed.txt"}, artifact: Artifact{Path: "managed.txt", Content: []byte("bad")}, wantErr: "duplicate managed path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			artifacts := []Artifact{tc.artifact}
			if tc.name == "duplicate artifacts" {
				artifacts = append(artifacts, tc.artifact)
			}
			if tc.name == "unmanaged artifact" {
				artifacts = append([]Artifact{{Path: "managed.txt", Content: []byte("must not write")}}, artifacts...)
			}
			_, err := DeployContextProgress(context.Background(), root, tc.managed, artifacts, nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("DeployContextProgress() error = %v, want substring %q", err, tc.wantErr)
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("rejected deployment created entries: %v", entries)
			}
		})
	}
}

func TestDeployPreservesExistingManagedModeWhenReplacing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "managed.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DeployContextProgress(context.Background(), root, []string{"managed.txt"}, []Artifact{{Path: "managed.txt", Content: []byte("new")}}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("replaced mode = %o, want 600", got)
	}
}

func TestDeployRollsBackWhenCommitFailsMidBatch(t *testing.T) {
	root := t.TempDir()
	existingPath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(existingPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	createdDir := filepath.Join(root, "new")

	original := renameFile
	t.Cleanup(func() { renameFile = original })
	renameFile = func(from, to string) error {
		if strings.HasSuffix(to, "z.txt") {
			return fmt.Errorf("injected rename failure")
		}
		return os.Rename(from, to)
	}

	_, err := DeployContextProgress(context.Background(), root, []string{"a.txt", "new/z.txt"}, []Artifact{
		{Path: "a.txt", Content: []byte("updated\n")},
		{Path: "new/z.txt", Content: []byte("fresh\n")},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "new/z.txt") {
		t.Fatalf("DeployContextProgress() error = %v, want error mentioning new/z.txt", err)
	}

	if got, readErr := os.ReadFile(existingPath); readErr != nil || string(got) != "old\n" {
		t.Fatalf("existing artifact after rollback = %q, error = %v, want restored original", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "new", "z.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partially installed artifact still exists: %v", statErr)
	}
	if _, statErr := os.Stat(createdDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("created directory not rolled back: %v", statErr)
	}
	assertNoDeploymentTemps(t, root)
}

func TestDeployFsyncCreatedDirectoriesOnSuccess(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c.txt")

	result, err := DeployContextProgress(context.Background(), root, []string{"a/b/c.txt"}, []Artifact{{Path: "a/b/c.txt", Content: []byte("durable\n")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Changed, []string{"a/b/c.txt"}) {
		t.Fatalf("changed paths = %v, want a/b/c.txt", result.Changed)
	}
	if got, err := os.ReadFile(nested); err != nil || string(got) != "durable\n" {
		t.Fatalf("nested content = %q, error = %v", got, err)
	}
	assertNoDeploymentTemps(t, root)
}

func TestSyncDir(t *testing.T) {
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatalf("syncDir(temp dir) error = %v, want nil", err)
	}
	if err := syncDir(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("syncDir(missing path) error = nil, want error")
	}
}

func assertNoDeploymentTemps(t *testing.T, root string) {
	t.Helper()
	var temporary string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && strings.HasPrefix(entry.Name(), ".takt-setup-") {
			temporary = path
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if temporary != "" {
		t.Fatalf("temporary deployment file remains: %s", temporary)
	}
}
