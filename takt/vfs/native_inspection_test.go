package vfs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestNativeInspectionReservesOnlyItsScopeDurably(t *testing.T) {
	f, root, state := newStore(t)
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tests", "a.go"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	author, _ := delegate(t, f, state, testSession, "test", "tests", "tests/a.go")
	for _, tool := range []string{"read", "grep", "glob"} {
		t.Run(tool, func(t *testing.T) {
			resource := "tests"
			if tool == "read" {
				resource = "tests/a.go"
			}
			if err := f.StartNativeInspection(author, tool, tool, resource); err != nil {
				t.Fatal(err)
			}
			conflictPath := "tests/new.go"
			if tool == "read" {
				conflictPath = "tests/a.go"
			}
			if _, err := f.AssignScope(ident("dev", "blocked-"+tool, "dev"), []string{conflictPath}); err == nil {
				t.Fatal("intersecting claim admitted")
			}
			if _, err := f.AssignScope(ident("dev", "disjoint-"+tool, "dev"), []string{"src/" + tool + ".go"}); err != nil {
				t.Fatal(err)
			}
			if err := f.ReleaseShell(author, tool); err == nil {
				t.Fatal("shell released native reservation")
			}
			if err := f.EndNativeInspection(author, tool); err != nil {
				t.Fatal(err)
			}
			if err := f.EndNativeInspection(author, tool); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := f.StartNativeInspection(author, "persist", "read", "tests/a.go"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := reopened.CancelPreparedDelegation(testSession, "tests", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.AssignScope(ident("dev", "blocked", "dev"), []string{"tests/a.go"}); err == nil {
		t.Fatal("restart released reservation")
	}
	if err := reopened.EndNativeInspection(author, "persist"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.AssignScope(ident("dev", "unblocked", "dev"), []string{"another.go"}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeInspectionScopesAndAliases(t *testing.T) {
	f, root, state := newStore(t)
	if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "impl.go"), []byte("native"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _ = delegate(t, f, state, testSession, "dev", "impl", "src/impl.go")
	author, _ := delegate(t, f, state, testSession, "test", "tests", "tests.go")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ tool, path string }{
		{"read", "src/impl.go"}, {"read", "src"}, {"read", filepath.Join(alias, "src", "impl.go")},
		{"grep", "src"}, {"glob", ""}, {"codegraph_explore", "tests"},
	} {
		if err := f.StartNativeInspection(author, "denied", tt.tool, tt.path); err == nil {
			t.Fatalf("allowed %s %s", tt.tool, tt.path)
		}
	}
	for _, agent := range []vfs.AgentID{"verify", "qa", vfs.OrchestratorInstance} {
		if err := f.StartNativeInspection(vfs.Caller{Agent: agent}, "native", "read", "src"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.StartNativeInspection(author, "own", "read", "tests.go"); err != nil {
		t.Fatal(err)
	}
	if err := f.EndNativeInspection(author, "own"); err != nil {
		t.Fatal(err)
	}
}

func TestNativeInspectionRefusesSecretsAndNonCanonicalPaths(t *testing.T) {
	f, root, state := newStore(t)
	for _, dir := range []string{".ssh", "secrets"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".ssh", "id_rsa"), []byte("key"), 0600); err != nil {
		t.Fatal(err)
	}
	author, _ := delegate(t, f, state, testSession, "test", "tests", "tests.go")
	for _, tt := range []struct {
		tool, path string
		want       error
	}{
		{"grep", ".ssh", vfs.ErrScopeDenied},
		{"read", ".ssh/id_rsa", vfs.ErrScopeDenied},
		{"read", "secrets/missing.txt", vfs.ErrScopeDenied},
		{"read", "cafe\u0301.go", vfs.ErrInvalidPath},
	} {
		if err := f.StartNativeInspection(author, "secret", tt.tool, tt.path); !errors.Is(err, tt.want) {
			t.Fatalf("%s %q = %v, want %v", tt.tool, tt.path, err, tt.want)
		}
	}
}

func TestCodeGraphShellInspectionBlocksConcurrentClaims(t *testing.T) {
	f, _, state := newStore(t)
	author, _ := delegate(t, f, state, testSession, "test", "tests", "tests.go")
	plan, err := f.PrepareShell(author, "graph", "codegraph explore src", state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ContinueShell(author, "graph", "start"); err != nil {
		t.Fatal(err)
	}
	calls := f.NativeInspections()
	if len(calls) != 1 || calls[0].CallID != "graph" || calls[0].ToolCallID != "" {
		t.Fatalf("call registry = %+v", calls)
	}
	if _, err := f.AssignScope(ident("dev", "new", "dev"), []string{"src/new.go"}); err == nil {
		t.Fatal("CodeGraph workspace reservation did not defer claim")
	}
	if _, err := f.CompleteShell(author, "graph", "state"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.AssignScope(ident("dev", "new2", "dev"), []string{"src/new.go"}); err != nil {
		t.Fatal(err)
	}
	_ = plan
}

func TestCodeGraphShellInspectionEndsWithShellRelease(t *testing.T) {
	f, _, state := newStore(t)
	author, _ := delegate(t, f, state, testSession, "test", "tests", "tests.go")
	if _, err := f.PrepareShell(author, "graph-2", "codegraph explore src", state); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ContinueShell(author, "graph-2", "start"); err != nil {
		t.Fatal(err)
	}
	if err := f.ReleaseShell(author, "graph-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.AssignScope(ident("dev", "new", "dev"), []string{"src/other.go"}); err != nil {
		t.Fatal(err)
	}
}
