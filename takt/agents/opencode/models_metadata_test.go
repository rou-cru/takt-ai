package opencode_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
)

const metadataModels = `{"data":[
  {"providerID":"opencode-go","id":"haiku","modelID":"haiku","name":"Haiku","enabled":true,
   "limit":{"context":1000000,"input":900000,"output":128000},
   "capabilities":{"input":["text","image"]},
   "variants":[{"id":"low"},{"id":"high"}],
   "time":{"released":1791331200000},
   "cost":[{"input":0.1,"output":0.5,"cache":{"read":0.01,"write":0.125}},
           {"tier":{"type":"context","size":100000},"input":0.5,"output":2.5,"cache":{"read":0.05,"write":0.625}},
           {"tier":{"type":"mystery","size":1},"input":9,"output":9,"cache":{"read":0,"write":0}}]},
  {"providerID":"openai","id":"luna","modelID":"luna","name":"Luna","enabled":true,"limit":{"context":400000,"output":128000},"cost":[]}
]}`

const metadataProviders = `{"data":[{"id":"opencode-go","name":"OpenCode Go"},{"id":"openai","name":"OpenAI"}]}`

func fakeMetadataCommand(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "'\"'\"'") }
	script := "#!/bin/sh\ncase \"$*\" in\n  *'GET /api/model') printf '%s' '" + escape(metadataModels) + "' ;;\n  *'GET /api/provider') printf '%s' '" + escape(metadataProviders) + "' ;;\n  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())
}

// Only what the API states is carried: no pricing stays no pricing, and a
// price tier of an unknown type is dropped.
func TestAvailableModelsCarriesStatedMetadata(t *testing.T) {
	fakeMetadataCommand(t)
	models, err := opencode.AvailableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	haiku, luna := models[0], models[1]
	wantCost := []opencodeapi.CostTier{
		{Input: 0.1, Output: 0.5, CacheRead: 0.01, CacheWrite: 0.125},
		{AboveContext: 100000, Input: 0.5, Output: 2.5, CacheRead: 0.05, CacheWrite: 0.625},
	}
	if len(haiku.Cost) != len(wantCost) || haiku.Cost[0] != wantCost[0] || haiku.Cost[1] != wantCost[1] {
		t.Fatalf("cost = %+v, want %+v", haiku.Cost, wantCost)
	}
	if haiku.ContextLimit != 1000000 || haiku.InputLimit != 900000 || haiku.OutputLimit != 128000 {
		t.Fatalf("limits = %d/%d/%d", haiku.ContextLimit, haiku.InputLimit, haiku.OutputLimit)
	}
	if len(haiku.Input) != 2 || len(haiku.Variants) != 2 || haiku.Variants[1] != "high" {
		t.Fatalf("input = %v, variants = %v", haiku.Input, haiku.Variants)
	}
	if want := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC); !haiku.Released.Equal(want) {
		t.Fatalf("released = %v, want %v", haiku.Released, want)
	}
	if len(luna.Cost) != 0 || luna.InputLimit != 0 || !luna.Released.IsZero() {
		t.Fatalf("unstated metadata was invented: %+v", luna)
	}
}

func TestProviderNamesMapsIDsToDisplayNames(t *testing.T) {
	fakeMetadataCommand(t)
	names, err := opencode.ProviderNames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if names["openai"] != "OpenAI" || names["opencode-go"] != "OpenCode Go" || len(names) != 2 {
		t.Fatalf("names = %v", names)
	}
}
