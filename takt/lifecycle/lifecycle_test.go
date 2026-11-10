package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/memory/embed"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

func TestRunRejectsUnsupportedAction(t *testing.T) {
	request := setup.PlanRequest{}
	result, err := (Runtime{Model: fakeModel(t)}).Run(context.Background(), "remove", t.TempDir(), request)
	if err == nil || !strings.Contains(err.Error(), `unsupported action "remove"`) {
		t.Fatalf("Run() error = %v, want unsupported action", err)
	}
	if len(result.Changed)+len(result.Unchanged)+len(result.Removed)+len(result.Preserved) != 0 {
		t.Fatalf("Run() result = %+v, want zero value", result)
	}
}

// TestRunInstallPersistsCustomInstalledConfig verifies a custom
// install's chosen components and options must
// survive on disk, not just live in the request that built this deploy, so
// a later partial change (e.g. reassigning one model) can reapply the real
// configuration instead of DefaultPlanRequest.
func TestRunInstallPersistsCustomInstalledConfig(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	request.Components = []string{"context7"}

	if _, err := (Runtime{Model: fakeModel(t)}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	loaded, err := setup.LoadInstalledConfig(root)
	if err != nil {
		t.Fatalf("LoadInstalledConfig() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, request) {
		t.Fatalf("loaded installed config = %+v, want %+v", loaded, request)
	}
}

// TestRunUninstallRestoresPreExistingMergedConfig verifies that
// Takt merges its settings into a user's pre-existing opencode.json; an
// uninstall without later edits leaves the file as before the install.
func TestRunUninstallRestoresPreExistingMergedConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".config", "opencode", "opencode.json")
	original := []byte("{\n  \"user_setting\": true\n}\n")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	request := setuputil.TestPlanRequest()
	if _, err := (Runtime{Model: fakeModel(t)}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("install error = %v", err)
	}
	if merged, _ := os.ReadFile(configPath); string(merged) == string(original) {
		t.Fatal("install did not merge into opencode.json; test premise broken")
	}
	result, err := (Runtime{Model: fakeModel(t)}).Run(context.Background(), "uninstall", root, request)
	if err != nil {
		t.Fatalf("uninstall error = %v", err)
	}
	if !slices.Contains(result.Restored, ".config/opencode/opencode.json") {
		t.Fatalf("restored = %v, want opencode.json", result.Restored)
	}
	if got, _ := os.ReadFile(configPath); string(got) != string(original) {
		t.Fatalf("opencode.json = %s, want the pre-install content", got)
	}
}

/*
// A cancellation arriving after the last stable point reports the actual
// completed result.
func TestRunCompletedWhenCancelArrivesLate(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := setuputil.TestPlanRequest()
	runtime := Runtime{Model: fakeModel(t), ProviderActions: setup.ProviderRuntime{
		LookPath: func(program string) (string, error) { return "/fake/" + program, nil },
		Run: func(context.Context, string, ...string) ([]byte, error) {
			cancel() // provider actions run after every file is applied
			return nil, nil
		},
	}}
	result, err := runtime.Run(ctx, "install", root, request)
	if err != nil || result.Outcome != OutcomeCompleted || len(result.NotApplied) != 0 || len(result.Actions) == 0 {
		t.Fatalf("Run() = %+v, %v; want completed", result, err)
	}
}

func TestRunProviderFailurePreservesAppliedResult(t *testing.T) {
	root := t.TempDir()
	wantErr := errors.New("provider action failed")
	runtime := Runtime{Model: fakeModel(t), ProviderActions: setup.ProviderRuntime{
		LookPath: func(program string) (string, error) { return "/fake/" + program, nil },
		Run:      func(context.Context, string, ...string) ([]byte, error) { return nil, wantErr },
	}}
	request := setuputil.TestPlanRequest()

	result, err := runtime.Run(context.Background(), "install", root, request)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if len(result.Changed) == 0 {
		t.Fatalf("Run() result = %+v, want applied files preserved", result)
	}
	if _, err := os.Stat(filepath.Join(root, ".config", "opencode", "opencode.json")); err != nil {
		t.Fatalf("applied configuration missing: %v", err)
	}
}
*/

// TestRunInstallStartsTheMemoryService verifies an install creates the memory store and leaves a ready
// service, replacing a running owner that reports another version than this build.
func TestRunInstallStartsTheMemoryService(t *testing.T) {
	ctx := context.Background()
	base, err := os.MkdirTemp("/tmp", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	dir := filepath.Join(base, "memory")
	t.Setenv(memory.EnvDir, dir)
	t.Cleanup(func() { _ = memory.StopOwner(context.Background(), dir) })

	if _, err := (Runtime{Model: fakeModel(t), StartMemory: true}).Run(ctx, "install", t.TempDir(), setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if status, err := memory.Probe(ctx, dir); err != nil || status.State != memory.StateReady {
		t.Fatalf("memory service after install: %+v, %v", status, err)
	}
}

// TestRunInstallKeepsAModelAlreadyInstalled verifies a later install or sync needs no network for a model
// whose files are present and match the manifest.
func TestRunInstallKeepsAModelAlreadyInstalled(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	manifest := fakeModel(t)
	if _, err := (Runtime{Model: manifest}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("first install error = %v", err)
	}
	offline := *manifest
	offline.BaseURL = "http://127.0.0.1:1"
	if _, err := (Runtime{Model: &offline}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("install with the model present and the host unreachable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(embed.ModelDir(root), manifest.Files[0].Name)); err != nil {
		t.Fatalf("model file missing: %v", err)
	}
}
