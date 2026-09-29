package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectConflictsClassifiesImpact(t *testing.T) {
	root := t.TempDir()
	installed := []TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{".config/opencode/agents/takt-dev.md", ".config/opencode/agents/takt-pm.md", ".config/opencode/opencode.json", ".config/opencode/cli.json"},
		Artifacts: []Artifact{
			{Path: ".config/opencode/agents/takt-dev.md", Content: []byte("dev v1\n")},
			{Path: ".config/opencode/agents/takt-pm.md", Content: []byte("pm v1\n")},
			{Path: ".config/opencode/opencode.json", Content: []byte("{\"a\": 1}\n")},
			{Path: ".config/opencode/cli.json", Content: []byte("{}\n")},
		},
	}}
	if _, err := ApplyContext(context.Background(), root, installed, ProviderRuntime{}); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".config/opencode/agents/takt-dev.md", "my dev\n") // Takt version unchanged below: unrelated
	write(".config/opencode/agents/takt-pm.md", "my pm\n")   // Takt version changes: uncertain
	write(".config/opencode/opencode.json", "not json")      // injected JSON merge: incompatible
	if err := os.Remove(filepath.Join(root, ".config/opencode/cli.json")); err != nil {
		t.Fatal(err)
	}

	requested := []TargetPlan{{
		Target:       "opencode",
		ManagedPaths: installed[0].ManagedPaths,
		Artifacts: []Artifact{
			{Path: ".config/opencode/agents/takt-dev.md", Content: []byte("dev v1\n")},
			{Path: ".config/opencode/agents/takt-pm.md", Content: []byte("pm v2\n")},
			{Path: ".config/opencode/opencode.json", Content: []byte("{\"a\": 2}\n")},
			{Path: ".config/opencode/cli.json", Content: []byte("{\"b\": 1}\n")},
		},
	}}
	conflicts, err := DetectConflicts(root, requested)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ConflictEntry{}
	for _, conflict := range conflicts {
		got[conflict.Path] = conflict
	}
	for path, want := range map[string]string{
		".config/opencode/agents/takt-dev.md": ImpactUnrelated,
		".config/opencode/agents/takt-pm.md":  ImpactUncertain,
		".config/opencode/opencode.json":      ImpactIncompatible,
		".config/opencode/cli.json":           ImpactUncertain,
	} {
		if got[path].Impact != want {
			t.Errorf("%s impact = %q, want %q (%+v)", path, got[path].Impact, want, got[path])
		}
	}
	if pm := got[".config/opencode/agents/takt-pm.md"]; pm.Affects != "OpenCode takt-pm agent" || pm.SHA256 == "" || pm.Consequence != "Takt cannot guarantee the OpenCode takt-pm agent while your version is kept." {
		t.Errorf("uncertain entry = %+v", pm)
	}
	if cfg := got[".config/opencode/opencode.json"]; cfg.Alternative == "" {
		t.Errorf("incompatible entry has no alternative: %+v", cfg)
	}
}

func TestIsJSONObject(t *testing.T) {
	for raw, want := range map[string]bool{"": true, "{}": true, `{"a":1}`: true, "{\n// c\n\"a\": 1,\n}": true, "not json": false, "[1]": false} {
		if got := isJSONObject([]byte(raw)); got != want {
			t.Errorf("isJSONObject(%q) = %v, want %v", raw, got, want)
		}
	}
}
