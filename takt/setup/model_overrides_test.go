package setup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// installEditedConfig installs, then edits the managed config the way a user
// would: a provider block, a tweak inside another agent, and a private mode.
func installEditedConfig(t *testing.T) (root, configPath string, request setup.PlanRequest) {
	t.Helper()
	root = t.TempDir()
	request = setuputil.TestPlanRequest()
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
	configPath = filepath.Join(root, filepath.FromSlash(opencode.ConfigPath()))
	var config map[string]any
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	config["provider"] = map[string]any{"mine": map[string]any{"npm": "@ai-sdk/openai", "options": map[string]any{"baseURL": "https://x.example/v1?a=1&b=2"}}}
	config["agents"].(map[string]any)["architect"].(map[string]any)["description"] = "edited by the user"
	var edited bytes.Buffer
	encoder := json.NewEncoder(&edited)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		t.Fatalf("encode edited config: %v", err)
	}
	if err := os.WriteFile(configPath, edited.Bytes(), 0o600); err != nil {
		t.Fatalf("write edited config: %v", err)
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		t.Fatalf("chmod edited config: %v", err)
	}
	return root, configPath, request
}

func readConfigMap(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return config
}

func TestApplyModelOverrideChangeKeepsUserEditsInManagedConfig(t *testing.T) {
	root, configPath, request := installEditedConfig(t)
	manifestBefore, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("LoadOwnershipManifest() error = %v", err)
	}

	assignment := model.ModelAssignment{Model: "custom/override-model", Effort: "high"}
	result, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"analyst": assignment}, setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("ApplyModelOverrideChanges() error = %v", err)
	}

	config := readConfigMap(t, configPath)
	if _, ok := config["provider"]; !ok {
		t.Fatal("user-added provider dropped by a model reassignment")
	}
	agents := config["agents"].(map[string]any)
	if got := agents["analyst"].(map[string]any)["model"]; got != "custom/override-model#high" {
		t.Errorf("analyst model = %v, want custom/override-model#high", got)
	}
	if got := agents["architect"].(map[string]any)["description"]; got != "edited by the user" {
		t.Errorf("user edit inside another agent lost: description = %v", got)
	}
	raw, _ := os.ReadFile(configPath)
	if !bytes.Contains(raw, []byte("a=1&b=2")) {
		t.Error("user string rewritten: HTML escaping altered the provider baseURL")
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %v, want 0600 preserved", info.Mode().Perm())
	}
	if !slices.Contains(result.Changed, opencode.ConfigPath()) || slices.Contains(result.Unchanged, opencode.ConfigPath()) {
		t.Errorf("result misreports the patched config: changed=%v unchanged=%v", result.Changed, result.Unchanged)
	}

	manifestAfter, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("LoadOwnershipManifest() after error = %v", err)
	}
	path := opencode.ConfigPath()
	if manifestAfter.Entries[path].SHA256 != manifestBefore.Entries[path].SHA256 {
		t.Error("ownership hash changed: the file would stop being reported as user-edited")
	}
	plans, _, err := setup.BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	conflicts, err := setup.DetectConflicts(root, plans)
	if err != nil {
		t.Fatalf("DetectConflicts() error = %v", err)
	}
	flagged := false
	for _, conflict := range conflicts {
		if conflict.Path == path && conflict.Reason == "user-edited" {
			flagged = true
		}
	}
	if !flagged {
		t.Errorf("edited config not reported as user-edited after reassignment: %+v", conflicts)
	}
}

func TestApplyModelOverrideChangeClearsOverrideInEditedConfig(t *testing.T) {
	root, configPath, _ := installEditedConfig(t)
	set := map[string]model.ModelAssignment{"analyst": {Model: "custom/override-model"}}
	if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, set, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("set override error = %v", err)
	}
	if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"analyst": {}}, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("clear override error = %v", err)
	}
	config := readConfigMap(t, configPath)
	if _, ok := config["provider"]; !ok {
		t.Fatal("provider dropped when clearing an override")
	}
	if _, has := config["agents"].(map[string]any)["analyst"].(map[string]any)["model"]; has {
		t.Error("analyst still carries a model after clearing its override")
	}
}

func TestApplyModelOverrideChangeLeavesUnparseableConfigUntouched(t *testing.T) {
	root, configPath, _ := installEditedConfig(t)
	broken := []byte("{ not json // the user is mid-edit")
	if err := os.WriteFile(configPath, broken, 0o600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}
	if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"analyst": {Model: "custom/override-model"}}, setup.ProviderRuntime{}); err == nil {
		t.Fatal("ApplyModelOverrideChanges() on an unparseable edited config error = nil, want an error")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Equal(after, broken) {
		t.Errorf("unparseable config was modified: %q", after)
	}
	loaded, err := setup.LoadInstalledConfig(root)
	if err != nil {
		t.Fatalf("LoadInstalledConfig() error = %v", err)
	}
	if _, recorded := loaded.OpenCodeModelOverrides["analyst"]; recorded {
		t.Error("override recorded although the reassignment was not applied")
	}
}

func TestApplyModelOverrideChangeReadsAnEditedConfigWithComments(t *testing.T) {
	root, configPath, _ := installEditedConfig(t)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	commented := append([]byte("// my notes\n/* kept by hand */\n"), raw...)
	if err := os.WriteFile(configPath, commented, 0o600); err != nil {
		t.Fatalf("write commented config: %v", err)
	}
	if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"analyst": {Model: "custom/override-model"}}, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyModelOverrideChanges() on a commented config error = %v", err)
	}
	config := readConfigMap(t, configPath)
	if _, ok := config["provider"]; !ok {
		t.Fatal("user-added provider dropped from a commented config")
	}
	if got := config["agents"].(map[string]any)["analyst"].(map[string]any)["model"]; got != "custom/override-model" {
		t.Errorf("analyst model = %v, want custom/override-model", got)
	}
}

func TestApplyModelOverrideChangeRefusesAnEditedConfigItCannotPatch(t *testing.T) {
	for name, body := range map[string]string{
		"not an object":            `[]`,
		"no agents object":         `{"provider":{}}`,
		"agents of the wrong kind": `{"agents":[]}`,
		"agent missing":            `{"agents":{}}`,
		"agent null":               `{"agents":{"analyst":null}}`,
	} {
		t.Run(name, func(t *testing.T) {
			root, configPath, _ := installEditedConfig(t)
			if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"analyst": {Model: "custom/override-model"}}, setup.ProviderRuntime{}); err == nil {
				t.Fatal("ApplyModelOverrideChanges() error = nil, want a refusal")
			}
			if after, _ := os.ReadFile(configPath); string(after) != body {
				t.Errorf("config modified by a refused reassignment: %q", after)
			}
		})
	}
}

func TestApplyModelOverrideChangeRestoresAMissingConfig(t *testing.T) {
	root, configPath, _ := installEditedConfig(t)
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	if _, err := setup.ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"analyst": {Model: "custom/override-model"}}, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyModelOverrideChanges() with no config on disk error = %v", err)
	}
	if got := readConfigMap(t, configPath)["agents"].(map[string]any)["analyst"].(map[string]any)["model"]; got != "custom/override-model" {
		t.Errorf("analyst model = %v, want the redeployed override", got)
	}
}
