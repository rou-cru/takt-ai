package codegraph_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/model"
)

func TestManagedMarkerKeyIsStable(t *testing.T) {
	first := codegraph.ManagedMarkerKey()
	if first == "" {
		t.Fatal("ManagedMarkerKey() = \"\", want a stable non-empty ownership-manifest key")
	}
	if second := codegraph.ManagedMarkerKey(); first != second {
		t.Fatal("ManagedMarkerKey() is not stable across calls")
	}
}

func TestRemoveStripsCodegraphMCPEntry(t *testing.T) {
	home := t.TempDir()
	path := model.OpenCodeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	seed := `{"theme":"x","mcp":{"servers":{"codegraph":{"command":["codegraph","serve","--mcp"],"type":"local"},"other":{"type":"remote"}}}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := codegraph.Remove(home); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(content)
	if strings.Contains(got, `"codegraph"`) {
		t.Errorf("config after Remove() = %s, want the codegraph entry gone", got)
	}
	if !strings.Contains(got, `"other"`) || !strings.Contains(got, `"theme"`) {
		t.Errorf("config after Remove() = %s, want unrelated keys preserved", got)
	}
}

func TestRemoveOnConfigWithoutCodegraphIsNoop(t *testing.T) {
	home := t.TempDir()
	path := model.OpenCodeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	seed := `{"theme":"x"}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := codegraph.Remove(home); err != nil {
		t.Fatalf("Remove() on a config without codegraph error = %v, want nil", err)
	}
}
