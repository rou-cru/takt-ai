package gc

import (
	"path/filepath"
	"testing"
)

func TestFindingPathRejectsRelativeWorkspaceEscape(t *testing.T) {
	workspace := filepath.Join(string(filepath.Separator), "workspace")
	for _, raw := range []string{"../outside.go", "nested/../../outside.go", "." + string(filepath.Separator) + "../outside.go"} {
		if _, err := findingPath(workspace, raw, "fixture"); err == nil {
			t.Errorf("findingPath(%q) accepted a path outside the workspace", raw)
		}
	}
}

func TestFindingPathNormalizesInWorkspacePaths(t *testing.T) {
	workspace := filepath.Join(string(filepath.Separator), "workspace")
	got, err := findingPath(workspace, filepath.Join(workspace, "nested", "..", "main.go"), "fixture")
	if err != nil || got != "main.go" {
		t.Fatalf("findingPath returned %q, %v; want main.go", got, err)
	}
}
