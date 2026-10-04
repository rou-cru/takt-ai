package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// installedRoot installs a real root with the fake engram/opencode/codegraph
// binaries the repo's tui/testutil convention already provides, so Adapter
// methods that shell out to them run against a deterministic fake tool
// instead of a real one, and returns the root plus the components installed.
func installedRoot(t *testing.T) (string, []string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "takt-runtime-fake-bin-")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.WriteFile(filepath.Join(dir, "engram"), testutil.FakeEngramScript("dev"), 0o755); err != nil {
		t.Fatalf("WriteFile(engram) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode"), testutil.FakeOpenCodeScript(), 0o755); err != nil {
		t.Fatalf("WriteFile(opencode) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codegraph"), testutil.FakeCodegraphScript(codegraph.CodegraphVersion), 0o755); err != nil {
		t.Fatalf("WriteFile(codegraph) error = %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	components, err := setup.AllComponents()
	if err != nil {
		t.Fatalf("AllComponents() error = %v", err)
	}
	names := make([]string, len(components))
	for i, c := range components {
		names[i] = string(c)
	}
	request := setuputil.TestPlanRequest()
	request.Components = names
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	return root, names
}

// nonJSONPlannedPath selects a non-mergeable artifact from the path-sorted
// install plan of components. The ownership manifest also lists runtime
// binaries, which drift and preview never compare, so picking from it by map
// order made the edit-detection tests fail whenever a binary came first.
func nonJSONPlannedPath(t *testing.T, components []string) string {
	t.Helper()
	request := setuputil.TestPlanRequest()
	request.Components = components
	plans, _, err := setup.BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	for _, plan := range plans {
		for _, artifact := range plan.Artifacts {
			if !strings.HasSuffix(artifact.Path, ".json") {
				return artifact.Path
			}
		}
	}
	t.Fatal("no non-JSON artifact found in the install plan")
	return ""
}

func TestAdapterPreviewPlanOnFreshInstallHasNothingToChange(t *testing.T) {
	root, components := installedRoot(t)
	adapter := runtime.Adapter{}

	plan, err := adapter.PreviewPlan(runtime.PreviewRequest{Action: runtime.ActionInstall, RootDir: root, Components: components})
	if err != nil {
		t.Fatalf("PreviewPlan() error = %v", err)
	}
	if len(plan.Add) != 0 || len(plan.Modify) != 0 {
		t.Errorf("PreviewPlan() on an unchanged install = %+v, want no Add/Modify", plan)
	}
	if len(plan.Plans) == 0 {
		t.Error("PreviewPlan() returned no plans for a fully-selected install")
	}
}

func TestAdapterPreviewPlanReportsModifiedFile(t *testing.T) {
	root, components := installedRoot(t)
	edited := nonJSONPlannedPath(t, components)
	full := filepath.Join(root, filepath.FromSlash(edited))
	content, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := os.WriteFile(full, append(content, []byte("\n# tampered\n")...), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	plan, err := (runtime.Adapter{}).PreviewPlan(runtime.PreviewRequest{Action: runtime.ActionInstall, RootDir: root, Components: components})
	if err != nil {
		t.Fatalf("PreviewPlan() error = %v", err)
	}
	found := false
	for _, path := range plan.Modify {
		if path == edited {
			found = true
		}
	}
	if !found {
		t.Errorf("PreviewPlan().Modify = %v, want it to include the tampered path %q", plan.Modify, edited)
	}
}

func TestAdapterPreviewUninstall(t *testing.T) {
	root, _ := installedRoot(t)
	result, err := (runtime.Adapter{}).PreviewUninstall(root)
	if err != nil {
		t.Fatalf("PreviewUninstall() error = %v", err)
	}
	if len(result.Removed) == 0 {
		t.Error("PreviewUninstall() reported nothing to remove for an installed root")
	}
}

func TestAdapterScanDriftCleanInstall(t *testing.T) {
	root, _ := installedRoot(t)
	conflicts, err := (runtime.Adapter{}).ScanDrift(root)
	if err != nil {
		t.Fatalf("ScanDrift() error = %v", err)
	}
	if len(conflicts) != 0 {
		t.Errorf("ScanDrift() on an unchanged install = %v, want no conflicts", conflicts)
	}
}

func TestAdapterScanDriftReportsEditedFile(t *testing.T) {
	root, components := installedRoot(t)
	edited := nonJSONPlannedPath(t, components)
	full := filepath.Join(root, filepath.FromSlash(edited))
	content, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := os.WriteFile(full, append(content, []byte("\n# tampered\n")...), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	conflicts, err := (runtime.Adapter{}).ScanDrift(root)
	if err != nil {
		t.Fatalf("ScanDrift() error = %v", err)
	}
	found := false
	for _, c := range conflicts {
		if c.Path == edited {
			found = true
		}
	}
	if !found {
		t.Errorf("ScanDrift() = %+v, want it to report the tampered path %q", conflicts, edited)
	}
}

func TestAdapterOpenCodeModels(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode"), testutil.FakeOpenCodeScript(), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	models, err := (runtime.Adapter{}).OpenCodeModels()
	if err != nil {
		t.Fatalf("OpenCodeModels() error = %v", err)
	}
	if models == nil {
		t.Error("OpenCodeModels() = nil, want a non-nil (possibly empty) slice")
	}
}
