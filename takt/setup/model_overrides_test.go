package setup_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
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
	if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"orchestrator": assignment}, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyModelOverrideChanges() set error = %v", err)
	}
	loaded, err := setup.LoadInstalledConfig(root)
	if err != nil {
		t.Fatalf("LoadInstalledConfig() error = %v", err)
	}
	if got := loaded.OpenCodeModelOverrides["orchestrator"]; got != assignment {
		t.Fatalf("override after set = %+v, want %+v", got, assignment)
	}

	if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"orchestrator": {}}, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyModelOverrideChanges() clear error = %v", err)
	}
	loaded, err = setup.LoadInstalledConfig(root)
	if err != nil {
		t.Fatalf("LoadInstalledConfig() error = %v", err)
	}
	if _, exists := loaded.OpenCodeModelOverrides["orchestrator"]; exists {
		t.Errorf("override still present after clearing with an empty model: %+v", loaded.OpenCodeModelOverrides)
	}
}

func TestApplyModelOverrideChangeKeepsInjectedMCPServers(t *testing.T) {
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
	configPath := filepath.Join(root, filepath.FromSlash(opencode.ConfigPath()))
	injected := []byte(`{"mcp":{"servers":{"engram":{"type":"local","command":["engram","mcp"]},"codegraph":{"type":"local","command":["codegraph","serve"]}}}}`)
	current, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	withServers, err := filemerge.MergeJSONObjects(current, injected)
	if err != nil {
		t.Fatalf("merge injected servers: %v", err)
	}
	if err := os.WriteFile(configPath, withServers, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"analyst": {Model: "custom/override-model"}}, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyModelOverrideChanges() error = %v", err)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after reassignment: %v", err)
	}
	var config struct {
		MCP struct {
			Servers map[string]json.RawMessage `json:"servers"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("parse config after reassignment: %v", err)
	}
	for _, name := range []string{"engram", "codegraph"} {
		if _, ok := config.MCP.Servers[name]; !ok {
			t.Errorf("MCP server %q dropped by model reassignment; servers = %v", name, config.MCP.Servers)
		}
	}
}

func TestApplyModelOverrideChangeRequiresPriorInstall(t *testing.T) {
	root := t.TempDir()
	assignment := model.ModelAssignment{Model: "custom/override-model"}
	if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"orchestrator": assignment}, setup.ProviderRuntime{}); err == nil {
		t.Fatal("ApplyModelOverrideChanges() on an uninstalled root error = nil, want an error")
	}
}
