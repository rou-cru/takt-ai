package filemerge_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

func TestInjectMCPServerOnFreshFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	result, err := filemerge.InjectMCPServer(path, "engram", []string{"engram", "mcp"})
	if err != nil {
		t.Fatalf("InjectMCPServer() error = %v", err)
	}
	if !result.Changed || len(result.Files) != 1 {
		t.Errorf("InjectMCPServer() result = %+v, want Changed=true and one file", result)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(content)
	if !strings.Contains(got, `"engram"`) || !strings.Contains(got, `"servers"`) {
		t.Errorf("config = %s, want the engram server nested under mcp.servers", got)
	}
}

func TestInjectMCPServerDropsLegacyV1Entry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	seed := `{"mcp":{"engram":{"command":["old-engram"],"type":"local","disabled":false}}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := filemerge.InjectMCPServer(path, "engram", []string{"engram", "mcp"}); err != nil {
		t.Fatalf("InjectMCPServer() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(content)
	if strings.Count(got, `"engram": {`) != 1 {
		t.Errorf("config = %s, want the flat V1 entry dropped and only the V2 object left", got)
	}
	if !strings.Contains(got, `"servers"`) {
		t.Errorf("config = %s, want the remaining entry nested under mcp.servers", got)
	}
}

func TestRemoveMCPServerPrunesEmptyMCP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	seed := `{"theme":"x","mcp":{"servers":{"engram":{"type":"local","command":["engram","mcp"]}}}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	result, err := filemerge.RemoveMCPServer(path, "engram")
	if err != nil {
		t.Fatalf("RemoveMCPServer() error = %v", err)
	}
	if !result.Changed {
		t.Error("RemoveMCPServer() Changed = false, want true")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(content)
	if strings.Contains(got, "engram") || strings.Contains(got, `"mcp"`) {
		t.Errorf("config = %s, want the mcp section pruned once empty", got)
	}
	if !strings.Contains(got, `"theme"`) {
		t.Errorf("config = %s, want unrelated keys preserved", got)
	}
}

func TestRemoveMCPServerAbsentIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte(`{"theme":"x"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	result, err := filemerge.RemoveMCPServer(path, "engram")
	if err != nil {
		t.Fatalf("RemoveMCPServer() error = %v", err)
	}
	if result.Changed {
		t.Error("RemoveMCPServer() on an absent server Changed = true, want false")
	}
}

func TestRemoveMCPServerMissingFileIsNoop(t *testing.T) {
	result, err := filemerge.RemoveMCPServer(filepath.Join(t.TempDir(), "missing.json"), "engram")
	if err != nil {
		t.Fatalf("RemoveMCPServer() on a missing file error = %v, want nil", err)
	}
	if result.Changed {
		t.Error("RemoveMCPServer() on a missing file Changed = true, want false")
	}
}

func TestMergeJSONFileFreshFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	result, err := filemerge.MergeJSONFile(path, []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("MergeJSONFile() error = %v", err)
	}
	if !result.Changed {
		t.Error("MergeJSONFile() on a fresh file Changed = false, want true")
	}
	content, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(content), `"a"`) {
		t.Errorf("config = %s, %v, want the merged content written", content, err)
	}
}

func TestMergeJSONFileMergesOverExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"a":1,"b":2}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := filemerge.MergeJSONFile(path, []byte(`{"b":3}`)); err != nil {
		t.Fatalf("MergeJSONFile() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(content)
	if !strings.Contains(got, `"a"`) || !strings.Contains(got, "3") {
		t.Errorf("config = %s, want a preserved and b updated to 3", got)
	}
}

func TestReadFileOrEmptyMissingFile(t *testing.T) {
	got, err := filemerge.ReadFileOrEmpty(filepath.Join(t.TempDir(), "missing.txt"))
	if err != nil {
		t.Fatalf("ReadFileOrEmpty() on a missing file error = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("ReadFileOrEmpty() on a missing file = %q, want empty", got)
	}
}

func TestReadFileOrEmptyExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "present.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got, err := filemerge.ReadFileOrEmpty(path)
	if err != nil || got != "hello" {
		t.Errorf("ReadFileOrEmpty() = %q, %v, want %q, nil", got, err, "hello")
	}
}

func TestRemoveJSONKeyDeletesAndPrunes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"outer":{"inner":{"target":1}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	changed, err := filemerge.RemoveJSONKey(path, "target", "outer", "inner")
	if err != nil {
		t.Fatalf("RemoveJSONKey() error = %v", err)
	}
	if !changed {
		t.Error("RemoveJSONKey() Changed = false, want true")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(content), "outer") {
		t.Errorf("config = %s, want the now-empty outer/inner objects pruned", content)
	}
}

func TestRemoveJSONKeyMissingKeyIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"outer":{"inner":{"kept":1}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	changed, err := filemerge.RemoveJSONKey(path, "not-there", "outer", "inner")
	if err != nil {
		t.Fatalf("RemoveJSONKey() error = %v", err)
	}
	if changed {
		t.Error("RemoveJSONKey() for a missing key Changed = true, want false")
	}
}

func TestRemoveJSONKeyMissingFileIsNoop(t *testing.T) {
	changed, err := filemerge.RemoveJSONKey(filepath.Join(t.TempDir(), "missing.json"), "target", "outer")
	if err != nil {
		t.Fatalf("RemoveJSONKey() on a missing file error = %v, want nil", err)
	}
	if changed {
		t.Error("RemoveJSONKey() on a missing file Changed = true, want false")
	}
}

func TestRemoveJSONKeyUnparseableFileIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	changed, err := filemerge.RemoveJSONKey(path, "target", "outer")
	if err != nil {
		t.Fatalf("RemoveJSONKey() on an unparseable file error = %v, want nil", err)
	}
	if changed {
		t.Error("RemoveJSONKey() on an unparseable file Changed = true, want false")
	}
}
