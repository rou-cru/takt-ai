package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDeployWritesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	managed := []string{".claude/CLAUDE.md"}
	artifact := Artifact{Path: ".claude/CLAUDE.md", Content: []byte("v1\n")}

	first, err := Deploy(root, managed, []Artifact{artifact})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(first.Changed, []string{".claude/CLAUDE.md"}) || len(first.Unchanged) != 0 {
		t.Fatalf("first result = %#v, want one changed path", first)
	}
	path := filepath.Join(root, ".claude", "CLAUDE.md")
	if got, err := os.ReadFile(path); err != nil || string(got) != "v1\n" {
		t.Fatalf("deployed content = %q, error = %v", got, err)
	}
	assertNoDeploymentTemps(t, root)

	second, err := Deploy(root, managed, []Artifact{artifact})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Changed) != 0 || !slices.Equal(second.Unchanged, []string{".claude/CLAUDE.md"}) {
		t.Fatalf("second result = %#v, want one unchanged path", second)
	}
	assertNoDeploymentTemps(t, root)
}

func TestDeployPreflightsEveryDestinationBeforeWriting(t *testing.T) {
	root := t.TempDir()
	blockedPath := filepath.Join(root, "blocked")
	if err := os.WriteFile(blockedPath, []byte("user file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Deploy(root, []string{"a.txt", "blocked/b.txt"}, []Artifact{
		{Path: "a.txt", Content: []byte("must not be deployed\n")},
		{Path: "blocked/b.txt", Content: []byte("must not be deployed\n")},
	})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Deploy() error = %v, want blocking parent error", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("a.txt stat error = %v, want absent", statErr)
	}
	if got, readErr := os.ReadFile(blockedPath); readErr != nil || string(got) != "user file\n" {
		t.Fatalf("blocked content = %q, error = %v", got, readErr)
	}
	assertNoDeploymentTemps(t, root)
}

func TestDeployReplacesManagedArtifactAndPreservesUnrelatedFile(t *testing.T) {
	root := t.TempDir()
	userPath := filepath.Join(root, "user-owned.txt")
	if err := os.WriteFile(userPath, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	managed := []string{".codex/AGENTS.md"}
	if _, err := Deploy(root, managed, []Artifact{{Path: ".codex/AGENTS.md", Content: []byte("old\n")}}); err != nil {
		t.Fatal(err)
	}
	result, err := Deploy(root, managed, []Artifact{{Path: ".codex/AGENTS.md", Content: []byte("new\n")}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Changed, []string{".codex/AGENTS.md"}) || len(result.Unchanged) != 0 {
		t.Fatalf("replacement result = %#v, want one changed path", result)
	}
	if got, err := os.ReadFile(filepath.Join(root, ".codex", "AGENTS.md")); err != nil || string(got) != "new\n" {
		t.Fatalf("replacement content = %q, error = %v", got, err)
	}
	if got, err := os.ReadFile(userPath); err != nil || string(got) != "keep\n" {
		t.Fatalf("unrelated content = %q, error = %v", got, err)
	}
}

func TestDeployDeterministicResultOrder(t *testing.T) {
	root := t.TempDir()
	managed := []string{"b.txt", "a.txt"}
	result, err := Deploy(root, managed, []Artifact{
		{Path: "b.txt", Content: []byte("b")},
		{Path: "a.txt", Content: []byte("a")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Changed, []string{"a.txt", "b.txt"}) {
		t.Errorf("changed paths = %v, want sorted paths", result.Changed)
	}
}

func TestDeployRejectsSeparatedArtifactPathConflict(t *testing.T) {
	root := t.TempDir()
	_, err := Deploy(root, []string{"a", "a.foo", "a/b"}, []Artifact{
		{Path: "a", Content: []byte("file")},
		{Path: "a.foo", Content: []byte("sibling")},
		{Path: "a/b", Content: []byte("child")},
	})
	if err == nil || !strings.Contains(err.Error(), `artifact path "a" conflicts with child artifact "a/b"`) {
		t.Fatalf("Deploy() error = %v, want separated parent/child conflict", err)
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
			_, err := Deploy(root, tc.managed, artifacts)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Deploy() error = %v, want substring %q", err, tc.wantErr)
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
	if _, err := Deploy(root, []string{"managed.txt"}, []Artifact{{Path: "managed.txt", Content: []byte("new")}}); err != nil {
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

	_, err := Deploy(root, []string{"a.txt", "new/z.txt"}, []Artifact{
		{Path: "a.txt", Content: []byte("updated\n")},
		{Path: "new/z.txt", Content: []byte("fresh\n")},
	})
	if err == nil || !strings.Contains(err.Error(), "new/z.txt") {
		t.Fatalf("Deploy() error = %v, want error mentioning new/z.txt", err)
	}

	if got, readErr := os.ReadFile(existingPath); readErr != nil || string(got) != "old\n" {
		t.Fatalf("existing artifact after rollback = %q, error = %v, want restored original", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "new", "z.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("partially installed artifact still exists: %v", statErr)
	}
	if _, statErr := os.Stat(createdDir); !os.IsNotExist(statErr) {
		t.Fatalf("created directory not rolled back: %v", statErr)
	}
	assertNoDeploymentTemps(t, root)
}

func TestDeployFsyncCreatedDirectoriesOnSuccess(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c.txt")

	result, err := Deploy(root, []string{"a/b/c.txt"}, []Artifact{{Path: "a/b/c.txt", Content: []byte("durable\n")}})
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
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path != root && strings.HasPrefix(info.Name(), ".takt-setup-") {
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
