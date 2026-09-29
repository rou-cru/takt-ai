package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

func TestCorrectDriftContextAppliesSelectedPaths(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	plans, _, err := setup.BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}

	var target string
	for _, plan := range plans {
		if len(plan.ManagedPaths) > 0 {
			target = plan.ManagedPaths[0]
			break
		}
	}
	if target == "" {
		t.Fatal("no managed path found in the built plans")
	}
	if err := os.WriteFile(filepath.Join(root, target), []byte("locally drifted content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := setup.CorrectDriftContext(context.Background(), root, plans, []string{target}, setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("CorrectDriftContext() error = %v", err)
	}
	if len(result.Unresolved) != 0 {
		t.Errorf("CorrectDriftContext() Unresolved = %v, want none: the path is managed", result.Unresolved)
	}
	found := false
	for _, changed := range result.Changed {
		if changed == target {
			found = true
		}
	}
	if !found {
		t.Errorf("CorrectDriftContext() Changed = %v, want it to include %q", result.Changed, target)
	}
}

func TestCorrectDriftContextReportsUnresolvedPath(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	plans, _, err := setup.BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}

	result, err := setup.CorrectDriftContext(context.Background(), root, plans, []string{"not/a/real/managed/path.json"}, setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("CorrectDriftContext() error = %v", err)
	}
	if len(result.Unresolved) != 1 || result.Unresolved[0] != "not/a/real/managed/path.json" {
		t.Errorf("CorrectDriftContext() Unresolved = %v, want the unmanaged path listed", result.Unresolved)
	}
}
