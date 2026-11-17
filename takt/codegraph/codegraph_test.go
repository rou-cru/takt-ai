package codegraph_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/model"
)

// recorder is a codegraph double on disk: it appends its working directory and
// arguments to the returned log, then runs body.
func recorder(t *testing.T, body string) (binary, log string) {
	t.Helper()
	dir := t.TempDir()
	binary, log = filepath.Join(dir, "codegraph"), filepath.Join(dir, "calls")
	script(t, binary, `echo "$PWD|$*" >> "`+log+`"`+"\n"+body)
	return binary, log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestEnsureIndexedInitializesBeforeReadyAndIsIdempotent(t *testing.T) {
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary, log := recorder(t, `if [ "$1" = status ]; then
  if [ -d "$2/.codegraph" ]; then echo ready; else echo "Not initialized"; fi
  exit 0
fi
/bin/mkdir "$3/.codegraph" && echo initialized`)

	if err := codegraph.EnsureIndexed(context.Background(), binary, workspace); err != nil {
		t.Fatal(err)
	}
	if err := codegraph.EnsureIndexed(context.Background(), binary, workspace); err != nil {
		t.Fatal(err)
	}
	status, init := workspace+"|status "+workspace, workspace+"|init --yes "+workspace
	if got, want := calls(t, log), []string{status, init, status, status}; !slices.Equal(got, want) {
		t.Fatalf("command order = %q, want %q", got, want)
	}
}

func TestEnsureIndexedDoesNotResetBrokenExistingIndex(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	binary, log := recorder(t, "echo database corrupt\nexit 1")
	if err := codegraph.EnsureIndexed(context.Background(), binary, workspace); err == nil || !strings.Contains(err.Error(), "database corrupt") {
		t.Fatalf("EnsureIndexed error = %v, want existing-index diagnosis", err)
	}
	if got := calls(t, log); len(got) != 1 {
		t.Fatalf("commands = %q, want no destructive re-init", got)
	}
}

func TestEnsureIndexedRefusesHomeBeforeRunningCodegraph(t *testing.T) {
	home := t.TempDir()
	workspaceAlias := filepath.Join(t.TempDir(), "home-link")
	if err := os.Symlink(home, workspaceAlias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	binary, log := recorder(t, "exit 0")

	for _, workspace := range []string{home, workspaceAlias} {
		err := codegraph.EnsureIndexed(context.Background(), binary, workspace)
		if err == nil || !strings.Contains(err.Error(), "refusing to initialize CodeGraph in home directory") {
			t.Errorf("EnsureIndexed(%q) error = %v, want home-directory refusal", workspace, err)
		}
	}
	if got := calls(t, log); len(got) != 0 {
		t.Fatalf("CodeGraph commands = %q, want none for HOME or its symlink", got)
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

func TestResolveAcceptsOnlyCompatibleVersions(t *testing.T) {
	for version, want := range map[string]bool{"1.6.1": false, "1.6.2": true, "2.0.0": true, "1.3.1": false, "1.5.9": false, "1.0.0": false, "0.9.9": false, "dev": false, "1.6.0-beta": false, "": false} {
		t.Run(version, func(t *testing.T) {
			bin := isolate(t)
			script(t, filepath.Join(bin, "codegraph"), `echo "`+version+`"`)
			got, found := codegraph.Resolve(t.TempDir())
			if found != want || (found && got != filepath.Join(bin, "codegraph")) {
				t.Fatalf("Resolve with codegraph %q on PATH = %q, %v; want found %v", version, got, found, want)
			}
		})
	}
}

func TestAcquireInstallsPinnedPackageWithNpm(t *testing.T) {
	root := t.TempDir()
	bin := isolate(t)
	managed, log := codegraph.ManagedBinaryPath(root), filepath.Join(bin, "npm-args")
	script(t, filepath.Join(bin, "npm"), `echo "$*" > "`+log+`"
/bin/mkdir -p "`+filepath.Dir(managed)+`"
echo "#!/bin/sh" > "`+managed+`"
echo "echo `+codegraph.CodegraphVersion+`" >> "`+managed+`"
/bin/chmod +x "`+managed+`"`)
	got, err := codegraph.Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got != managed {
		t.Fatalf("Acquire = %q, want %q", got, managed)
	}
	args := strings.TrimSpace(read(t, log))
	if !strings.Contains(args, "--prefix "+codegraph.ManagedPrefix(root)) || !strings.HasSuffix(args, "@colbymchenry/codegraph@"+codegraph.CodegraphVersion) {
		t.Fatalf("npm args = %q", args)
	}
}

func TestAcquireFailsWithoutNpm(t *testing.T) {
	isolate(t)
	if _, err := codegraph.Acquire(context.Background(), t.TempDir()); err == nil {
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
	if _, err := codegraph.Inject(home, "/abs/codegraph"); err != nil {
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
