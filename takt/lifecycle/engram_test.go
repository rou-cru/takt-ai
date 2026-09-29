package lifecycle

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
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

// TestMain keeps every lifecycle test off the host PATH and the network.
func TestMain(m *testing.M) {
	acquireEngram = func(context.Context, string) (string, error) { return "/usr/local/bin/engram", nil }
	acquireCodegraph = func(context.Context, string) (string, error) { return "/usr/local/bin/codegraph", nil }
	installSandboxDependency = func(context.Context, string) error { return nil }
	os.Exit(m.Run())
}

func withAcquire(t *testing.T, fn func(context.Context, string) (string, error)) {
	t.Helper()
	original := acquireEngram
	acquireEngram = fn
	t.Cleanup(func() { acquireEngram = original })
}

func withAcquireCodegraph(t *testing.T, fn func(context.Context, string) (string, error)) {
	t.Helper()
	original := acquireCodegraph
	acquireCodegraph = fn
	t.Cleanup(func() { acquireCodegraph = original })
}

// TestRunInstallFailsBeforeWritingWhenCodegraphUnavailable verifies CodeGraph
// is a required harness capability and acquisition fails before deployment.
func TestRunInstallFailsBeforeWritingWhenCodegraphUnavailable(t *testing.T) {
	root := t.TempDir()
	withAcquireCodegraph(t, func(context.Context, string) (string, error) { return "", errors.New("network unreachable") })
	result, err := (Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err == nil || !strings.Contains(err.Error(), "codegraph capability unavailable") {
		t.Fatalf("Run() error = %v, want CodeGraph prerequisite failure", err)
	}
	if len(result.Changed) != 0 {
		t.Fatalf("Changed = %v, CodeGraph prerequisite should fail before writes", result.Changed)
	}
	if _, statErr := os.Stat(model.OpenCodeConfigPath(root)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("OpenCode config exists after prerequisite failure: %v", statErr)
	}
}

// TestRunInstallRecordsCodegraphInstallAction verifies a successful
// codegraph install is recorded in the result.
func TestRunInstallRecordsCodegraphInstallAction(t *testing.T) {
	root := t.TempDir()
	withAcquireCodegraph(t, func(context.Context, string) (string, error) { return "/custom/bin/codegraph", nil })
	result, err := (Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Actions, "codegraph-install") {
		t.Fatalf("Actions = %v, want codegraph-install", result.Actions)
	}
}

func TestRunInstallReportsSandboxDependencyFailure(t *testing.T) {
	root := t.TempDir()
	original := installSandboxDependency
	installSandboxDependency = func(context.Context, string) error { return errors.New("npm unavailable") }
	t.Cleanup(func() { installSandboxDependency = original })

	result, err := (Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Incomplete) != 1 || !strings.Contains(result.Incomplete[0], "npm unavailable") {
		t.Fatalf("Incomplete = %v, want sandbox dependency failure", result.Incomplete)
	}
	if !setup.IsInstalled(root) {
		t.Fatal("the completed configuration install should still be recorded")
	}
}

// TestRunInstallInjectsResolvedCodegraphCommand verifies the MCP entry carries
// the absolute path Takt resolved, never the bare name a host must find on PATH.
func TestRunInstallInjectsResolvedCodegraphCommand(t *testing.T) {
	root := t.TempDir()
	const command = "/custom/bin/codegraph"
	withAcquireCodegraph(t, func(context.Context, string) (string, error) { return command, nil })
	if _, err := (Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(model.OpenCodeConfigPath(root))
	if err != nil || !strings.Contains(string(config), `"`+command+`"`) {
		t.Fatalf("opencode.json lacks %q (err %v):\n%s", command, err, config)
	}
}

func TestUninstallNeverTouchesReusedCodegraph(t *testing.T) {
	root := t.TempDir()
	reused := filepath.Join(t.TempDir(), "codegraph")
	if err := os.WriteFile(reused, []byte("user binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	withAcquireCodegraph(t, func(context.Context, string) (string, error) { return reused, nil })
	request := setuputil.TestPlanRequest()
	if _, err := (Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatal(err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, owned := manifest.Entries[codegraph.ManagedMarkerKey()]; owned {
		t.Fatal("a reused user binary was recorded as Takt-owned")
	}
	if _, err := (Runtime{}).Run(context.Background(), "uninstall", root, request); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(reused); err != nil || string(data) != "user binary" {
		t.Fatalf("reused binary was touched: %q, %v", data, err)
	}
}

func TestRunInstallInjectsResolvedCommand(t *testing.T) {
	root := t.TempDir()
	const command = "/custom/bin/engram"
	withAcquire(t, func(context.Context, string) (string, error) { return command, nil })
	if _, err := (Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(model.OpenCodeConfigPath(root))
	if err != nil || !strings.Contains(string(config), `"`+command+`"`) {
		t.Fatalf("opencode.json lacks %q (err %v):\n%s", command, err, config)
	}
}

func TestUninstallNeverTouchesReusedBinary(t *testing.T) {
	root := t.TempDir()
	reused := filepath.Join(t.TempDir(), "engram")
	if err := os.WriteFile(reused, []byte("user binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	withAcquire(t, func(context.Context, string) (string, error) { return reused, nil })
	request := setuputil.TestPlanRequest()
	if _, err := (Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatal(err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, owned := manifest.Entries[".takt-ai/bin/engram"]; owned {
		t.Fatal("a reused user binary was recorded as Takt-owned")
	}
	if _, err := (Runtime{}).Run(context.Background(), "uninstall", root, request); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(reused); err != nil || string(data) != "user binary" {
		t.Fatalf("reused binary was touched: %q, %v", data, err)
	}
}
