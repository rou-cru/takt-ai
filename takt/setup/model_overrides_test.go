package setup_test

import (
	"context"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

func TestApplyModelOverrideChangeSetsAndClearsOverride(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	plans, _, err := setup.BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	if err := setup.SaveInstalledConfig(root, request); err != nil {
		t.Fatalf("SaveInstalledConfig() error = %v", err)
	}

	assignment := model.ModelAssignment{Model: "custom/override-model"}
	if _, err := setup.ApplyModelOverrideChange(context.Background(), root, "orchestrator", assignment, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyModelOverrideChange() set error = %v", err)
	}
	loaded, err := setup.LoadInstalledConfig(root)
	if err != nil {
		t.Fatalf("LoadInstalledConfig() error = %v", err)
	}
	if got := loaded.OpenCodeModelOverrides["orchestrator"]; got != assignment {
		t.Fatalf("override after set = %+v, want %+v", got, assignment)
	}

	if _, err := setup.ApplyModelOverrideChange(context.Background(), root, "orchestrator", model.ModelAssignment{}, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyModelOverrideChange() clear error = %v", err)
	}
	loaded, err = setup.LoadInstalledConfig(root)
	if err != nil {
		t.Fatalf("LoadInstalledConfig() error = %v", err)
	}
	if _, exists := loaded.OpenCodeModelOverrides["orchestrator"]; exists {
		t.Errorf("override still present after clearing with an empty model: %+v", loaded.OpenCodeModelOverrides)
	}
}

func TestApplyModelOverrideChangeRequiresPriorInstall(t *testing.T) {
	root := t.TempDir()
	assignment := model.ModelAssignment{Model: "custom/override-model"}
	if _, err := setup.ApplyModelOverrideChange(context.Background(), root, "orchestrator", assignment, setup.ProviderRuntime{}); err == nil {
		t.Fatal("ApplyModelOverrideChange() on an uninstalled root error = nil, want an error")
	}
}
