package setup_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestReleaseLegacyCLIConfigDropsDanglingThemeAndForgetsTheFile(t *testing.T) {
	root := t.TempDir()
	const path = ".config/opencode/cli.json"
	plans := []setup.TargetPlan{{Target: "opencode", ManagedPaths: []string{path}, Artifacts: []setup.Artifact{{Path: path, Content: []byte("{}\n")}}}}
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	file := filepath.Join(root, path)
	if err := os.WriteFile(file, []byte(`{"$schema":"s","theme":{"mode":"dark","name":"takt"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := setup.ReleaseLegacyCLIConfig(root); err != nil {
		t.Fatalf("ReleaseLegacyCLIConfig() error = %v", err)
	}
	var got map[string]any
	raw, _ := os.ReadFile(file)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse cli.json: %v", err)
	}
	if want := map[string]any{"$schema": "s", "theme": map[string]any{"mode": "dark"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cli.json = %v, want the user's keys kept and the takt theme dropped", got)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, managed := manifest.Entries[path]; managed {
		t.Fatal("cli.json is still managed, so uninstall would delete it")
	}
	if err := setup.ReleaseLegacyCLIConfig(root); err != nil {
		t.Fatalf("second ReleaseLegacyCLIConfig() error = %v, want a no-op", err)
	}
}
