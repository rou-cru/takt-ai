package lifecycle_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

func TestCancelledOutcome(t *testing.T) {
	if got := lifecycle.CancelledOutcome(0); got != lifecycle.OutcomeCancelledNothingApplied {
		t.Errorf("CancelledOutcome(0) = %v, want %v", got, lifecycle.OutcomeCancelledNothingApplied)
	}
	if got := lifecycle.CancelledOutcome(3); got != lifecycle.OutcomeCancelledPartial {
		t.Errorf("CancelledOutcome(3) = %v, want %v", got, lifecycle.OutcomeCancelledPartial)
	}
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !lifecycle.Cancelled(ctx, ctx.Err()) {
		t.Error("Cancelled(cancelled ctx, ctx.Err()) = false, want true")
	}
	if lifecycle.Cancelled(context.Background(), errors.New("some other error")) {
		t.Error("Cancelled(live ctx, unrelated error) = true, want false")
	}
	if lifecycle.Cancelled(context.Background(), nil) {
		t.Error("Cancelled(live ctx, nil) = true, want false")
	}
}

func TestPreviewLifecycleInstall(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	got, err := lifecycle.PreviewLifecycle("install", root, request)
	if err != nil {
		t.Fatalf("PreviewLifecycle(install) error = %v", err)
	}
	preview, ok := got.(lifecycle.InstallPreview)
	if !ok {
		t.Fatalf("PreviewLifecycle(install) = %T, want lifecycle.InstallPreview", got)
	}
	if len(preview.Plans) == 0 {
		t.Error("PreviewLifecycle(install) Plans is empty, want at least the OpenCode and skills plans")
	}
}

func TestPreviewLifecycleUninstallOnFreshRoot(t *testing.T) {
	got, err := lifecycle.PreviewLifecycle("uninstall", t.TempDir(), setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("PreviewLifecycle(uninstall) error = %v", err)
	}
	result, ok := got.(setup.UninstallResult)
	if !ok {
		t.Fatalf("PreviewLifecycle(uninstall) = %T, want setup.UninstallResult", got)
	}
	if len(result.Removed) != 0 {
		t.Errorf("PreviewLifecycle(uninstall) on a fresh root Removed = %v, want none", result.Removed)
	}
}

func TestPreviewLifecycleUnsupportedAction(t *testing.T) {
	if _, err := lifecycle.PreviewLifecycle("teleport", t.TempDir(), setuputil.TestPlanRequest()); err == nil {
		t.Fatal("PreviewLifecycle(teleport) error = nil, want an error")
	}
}

func TestRunInstallThenUninstallRoundTrip(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()

	installed, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, request)
	if err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	if installed.Outcome != lifecycle.OutcomeCompleted {
		t.Errorf("Run(install) Outcome = %v, want %v", installed.Outcome, lifecycle.OutcomeCompleted)
	}
	if len(installed.Changed) == 0 {
		t.Error("Run(install) Changed is empty, want at least the deployed artifacts")
	}
	if !setup.IsInstalled(root) {
		t.Error("IsInstalled() after Run(install) = false, want true")
	}

	uninstalled, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", root, request)
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	if uninstalled.Outcome != lifecycle.OutcomeCompleted {
		t.Errorf("Run(uninstall) Outcome = %v, want %v", uninstalled.Outcome, lifecycle.OutcomeCompleted)
	}
	if setup.IsInstalled(root) {
		t.Error("IsInstalled() after Run(uninstall) = true, want false")
	}
}

func TestRunSyncAfterInstall(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	result, err := (lifecycle.Runtime{}).Run(context.Background(), "sync", root, request)
	if err != nil {
		t.Fatalf("Run(sync) error = %v", err)
	}
	if result.Outcome != lifecycle.OutcomeCompleted {
		t.Errorf("Run(sync) Outcome = %v, want %v", result.Outcome, lifecycle.OutcomeCompleted)
	}
}

func TestRunUninstallEngramRemoveReportsIncomplete(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	runtime := lifecycle.Runtime{EngramChoice: lifecycle.EngramRemove}
	result, err := runtime.Run(context.Background(), "uninstall", root, request)
	if err != nil {
		t.Fatalf("Run(uninstall, EngramRemove) error = %v", err)
	}
	if len(result.Incomplete) != 1 {
		t.Errorf("Run(uninstall, EngramRemove) Incomplete = %v, want one entry explaining the database was left in place", result.Incomplete)
	}
}

func TestNewRuntimeWiresProductionSeams(t *testing.T) {
	runtime := lifecycle.NewRuntime()
	if runtime.OpenCodeHandshake == nil {
		t.Error("NewRuntime() OpenCodeHandshake = nil, want the production handshake wired")
	}
	if runtime.Reload == nil {
		t.Error("NewRuntime() Reload = nil, want the production reload wired")
	}
}

func TestInstallCodegraphInjectsIntoRoot(t *testing.T) {
	root := t.TempDir()
	// InstallCodegraph only writes the MCP entry; it needs the OpenCode
	// config file to already exist as an install would have deployed it.
	configPath := filepath.Join(root, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := lifecycle.InstallCodegraph(context.Background(), root); err != nil {
		t.Fatalf("InstallCodegraph() error = %v", err)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(content) == 0 {
		t.Error("InstallCodegraph() left opencode.json unchanged, want the codegraph MCP entry injected")
	}
}

func TestRunInstallHandshakeFailurePropagates(t *testing.T) {
	root := t.TempDir()
	runtime := lifecycle.Runtime{OpenCodeHandshake: func(context.Context) error { return errors.New("preflight refused") }}
	if _, err := runtime.Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err == nil {
		t.Fatal("Run(install) with a failing handshake error = nil, want an error")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".config")); !os.IsNotExist(statErr) {
		t.Error("Run(install) with a failing handshake wrote files, want the root untouched")
	}
}
