package opencode_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
)

const modelResponse = `{"location":{},"data":[
  {"providerID":"opencode-go","modelID":"glm-5.3","name":"GLM","enabled":true,"limit":{"context":1000,"output":100}},
  {"providerID":"openai","modelID":"gpt-5.6-luna","name":"GPT","enabled":true,"limit":{"context":2000,"output":200}}
]}`

// fakeCommand installs a deterministic OpenCode V2 API process in an isolated PATH.
func fakeCommand(t *testing.T, modelBody string) {
	t.Helper()
	dir := t.TempDir()
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "'\"'\"'") }
	script := "#!/bin/sh\ncase \"$*\" in\n  *'GET /api/info') printf '%s' '" + escape(`{"version":"2.0.16"}`) + "' ;;\n  *'GET /api/model') printf '%s' '" + escape(modelBody) + "' ;;\n  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())
}

func TestAvailableModelsReportsWhatOpenCodeListsInOrder(t *testing.T) {
	fakeCommand(t, modelResponse)

	models, err := opencode.AvailableModels(context.Background())
	if err != nil {
		t.Fatalf("AvailableModels() error = %v", err)
	}
	want := []string{"opencode-go/glm-5.3", "openai/gpt-5.6-luna"}
	if strings.Join(models, ",") != strings.Join(want, ",") {
		t.Fatalf("models = %v, want %v", models, want)
	}
}

func TestAvailableModelsFailsWhenOpenCodeIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	models, err := opencode.AvailableModels(context.Background())
	if err == nil {
		t.Fatalf("AvailableModels() = %v, want an error", models)
	}
	if !strings.Contains(err.Error(), "opencode binary not found") {
		t.Fatalf("error = %v, want it to name the missing binary", err)
	}
}

func TestAvailableModelsFailsOnNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	if models, err := opencode.AvailableModels(context.Background()); err == nil {
		t.Fatalf("AvailableModels() = %v, want an error", models)
	}
}

func TestAvailableModelsFailsOnEmptyOutput(t *testing.T) {
	fakeCommand(t, `{"location":{},"data":[]}`)

	models, err := opencode.AvailableModels(context.Background())
	if err == nil {
		t.Fatalf("AvailableModels() = %v, want an error", models)
	}
	if !strings.Contains(err.Error(), "no models") {
		t.Fatalf("error = %v, want it to say no models were reported", err)
	}
}

func TestHandshakeRequiresV2Routes(t *testing.T) {
	fakeCommand(t, modelResponse)

	result, err := opencode.Handshake(context.Background())
	if err != nil {
		t.Fatalf("Handshake() error = %v", err)
	}
	if result.Version != "2.0.16" || result.Major != 2 {
		t.Fatalf("Handshake() = %+v, want functional v2 result", result)
	}
}
