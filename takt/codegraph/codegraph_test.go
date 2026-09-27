package codegraph

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestEnsureIndexedInitializesBeforeReadyAndIsIdempotent(t *testing.T) {
	workspace := t.TempDir()
	var calls [][]string
	original := runCommand
	runCommand = func(_ context.Context, binary, dir string, args ...string) ([]byte, error) {
		if binary != "/bin/codegraph" || dir != workspace {
			t.Fatalf("runCommand(binary=%q, dir=%q)", binary, dir)
		}
		calls = append(calls, append([]string(nil), args...))
		if args[0] == "status" {
			if _, err := os.Stat(filepath.Join(workspace, ".codegraph")); err == nil {
				return []byte("ready"), nil
			}
			return []byte("Not initialized"), nil
		}
		if !reflect.DeepEqual(args, []string{"init", "--yes", workspace}) {
			t.Fatalf("init args = %q", args)
		}
		if err := os.Mkdir(filepath.Join(workspace, ".codegraph"), 0o755); err != nil {
			t.Fatal(err)
		}
		return []byte("initialized"), nil
	}
	t.Cleanup(func() { runCommand = original })

	if err := EnsureIndexed(context.Background(), "/bin/codegraph", workspace); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexed(context.Background(), "/bin/codegraph", workspace); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"status", workspace}, {"init", "--yes", workspace}, {"status", workspace}, {"status", workspace}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("command order = %#v, want %#v", calls, want)
	}
}

func TestEnsureIndexedDoesNotResetBrokenExistingIndex(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	original := runCommand
	var calls int
	runCommand = func(context.Context, string, string, ...string) ([]byte, error) {
		calls++
		return []byte("database corrupt"), errors.New("status failed")
	}
	t.Cleanup(func() { runCommand = original })
	if err := EnsureIndexed(context.Background(), "/bin/codegraph", workspace); err == nil || !strings.Contains(err.Error(), "database corrupt") {
		t.Fatalf("EnsureIndexed error = %v, want existing-index diagnosis", err)
	}
	if calls != 1 {
		t.Fatalf("commands = %d, want no destructive re-init", calls)
	}
}

func script(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	return dir
}

func TestCompatibleVersion(t *testing.T) {
	for version, want := range map[string]bool{"1.6.0": true, "1.6.1\n": true, "2.0.0": true, "1.3.1": false, "1.5.9": false, "0.9.9": false, "dev": false, "1.6.0-beta": false, "": false} {
		if got := compatibleVersion(version); got != want {
			t.Errorf("compatibleVersion(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestResolveReusesCompatibleBinaryOnPath(t *testing.T) {
	bin := isolate(t)
	script(t, filepath.Join(bin, "codegraph"), "echo 1.6.0")
	got, found := Resolve(t.TempDir())
	if !found || got != filepath.Join(bin, "codegraph") {
		t.Fatalf("Resolve = %q, %v", got, found)
	}
}

func TestOldBinaryOnPathIsNotResolved(t *testing.T) {
	bin := isolate(t)
	script(t, filepath.Join(bin, "codegraph"), "echo 1.0.0")
	if got, found := Resolve(t.TempDir()); found {
		t.Fatalf("Resolve = %q, want not found", got)
	}
}

func TestAcquireInstallsPinnedPackageWithNpm(t *testing.T) {
	root := t.TempDir()
	isolate(t)
	var args []string
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		if name == "npm" {
			args = arg
			script(t, ManagedBinaryPath(root), "echo "+CodegraphVersion)
			noop := filepath.Join(t.TempDir(), "noop")
			script(t, noop, "exit 0")
			return exec.CommandContext(ctx, noop)
		}
		return exec.CommandContext(ctx, name, arg...)
	}
	got, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got != ManagedBinaryPath(root) {
		t.Fatalf("Acquire = %q, want %q", got, ManagedBinaryPath(root))
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--prefix "+ManagedPrefix(root)) || !strings.HasSuffix(joined, npmPackage+"@"+CodegraphVersion) {
		t.Fatalf("npm args = %q", joined)
	}
}

func TestAcquireFailsWithoutNpm(t *testing.T) {
	isolate(t)
	if _, err := Acquire(context.Background(), t.TempDir()); err == nil {
		t.Fatal("Acquire without npm succeeded")
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestInjectOpenCodeKeepsUserKeysAndReplacesPlainEntry also covers the V1
// carry-over: the flat mcp.codegraph entry is dropped so the server is not
// registered twice once V2 normalizes the old shape.
func TestInjectOpenCodeKeepsUserKeysAndReplacesPlainEntry(t *testing.T) {
	home := t.TempDir()
	path := model.OpenCodeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"theme":"x","mcp":{"codegraph":{"command":["codegraph","serve","--mcp"],"enabled":true,"type":"local"},"other":{"type":"remote"}}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Inject(home, "/abs/codegraph"); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, `"theme"`) || !strings.Contains(got, `"other"`) || !strings.Contains(got, "/abs/codegraph") || strings.Count(got, `"codegraph"`) != 1 {
		t.Fatalf("config = %s", got)
	}
	if !strings.Contains(got, `"servers"`) {
		t.Fatalf("codegraph must land under mcp.servers: %s", got)
	}
}
