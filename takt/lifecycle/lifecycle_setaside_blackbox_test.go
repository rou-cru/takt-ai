package lifecycle_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

// staleLauncher reports a version older than any Takt pins, so resolution
// rejects it and acquisition would replace it.
const staleLauncher = "#!/bin/sh\necho 0.0.1\n"

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

// isolateNpm leaves PATH holding only an npm that runs script, so no compatible host binary resolves and
// acquisition is decided by what that npm does.
func isolateNpm(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// npmInstallsCodegraph links a compatible codegraph, its ownership marker and the sandbox runtime where npm
// would put them.
var npmInstallsCodegraph = `bin="$3/node_modules/.bin"
marker="$3/` + strings.TrimPrefix(codegraph.ManagedMarkerKey(), codegraph.ManagedPrefixKey()+"/") + `"
/bin/mkdir -p "$bin" "$(/usr/bin/dirname "$marker")" "$3/node_modules/@anthropic-ai/sandbox-runtime"
echo '{}' > "$marker"
printf '#!/bin/sh\necho ` + codegraph.CodegraphVersion + `\n' > "$bin/codegraph"
/bin/chmod +x "$bin/codegraph"
echo '{}' > "$3/node_modules/@anthropic-ai/sandbox-runtime/package.json"
`

// npmFails is an npm that cannot install.
const npmFails = "exit 1\n"

func TestInstallBacksUpAStaleManagedPrerequisiteBeforeReplacingIt(t *testing.T) {
	isolateNpm(t, npmInstallsCodegraph)
	root := t.TempDir()
	launcher := filepath.ToSlash(filepath.Join(codegraph.ManagedPrefixKey(), "node_modules", ".bin", "codegraph"))
	writeStale(t, root, launcher)

	result, err := installRuntime(t).Run(context.Background(), "install", root, setuputil.TestPlanRequest())
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

func TestFailedInstallRestoresAStaleManagedPrerequisite(t *testing.T) {
	isolateNpm(t, npmFails)
	root := t.TempDir()
	launcher := filepath.ToSlash(filepath.Join(codegraph.ManagedPrefixKey(), "node_modules", ".bin", "codegraph"))
	binary := writeStale(t, root, launcher)

	if _, err := installRuntime(t).Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err == nil || !strings.Contains(err.Error(), "codegraph capability unavailable") {
		t.Fatalf("Run(install) error = %v, want the CodeGraph prerequisite failure", err)
	}
	content, err := os.ReadFile(binary)
	if err != nil || string(content) != staleLauncher {
		t.Fatalf("managed codegraph after the failed install = %q, %v; want it restored", content, err)
	}
	if _, err := os.Lstat(filepath.Join(root, setup.BackupDir, filepath.FromSlash(launcher))); !os.IsNotExist(err) {
		t.Fatalf("set-aside copy still present after restore: %v", err)
	}
}
