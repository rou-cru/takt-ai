package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestContentMatches(t *testing.T) {
	config := opencode.ConfigPath()
	tests := []struct {
		name    string
		path    string
		current string
		want    string
		match   bool
	}{
		{"plain equal", "a.md", "x", "x", true},
		{"plain differ", "a.md", "x", "y", false},
		{"config injected servers ignored", config, `{"a":1,"mcp":{"servers":{"` + memory.ServerKey + `":{},"codegraph":{}}}}`, `{"a":1}`, true},
		{"config user server kept as drift", config, `{"a":1,"mcp":{"servers":{"` + memory.ServerKey + `":{},"mine":{}}}}`, `{"a":1}`, false},
		{"config user server under the memory key is not hidden by another", config, `{"a":1,"mcp":{"servers":{"memory":{}}}}`, `{"a":1}`, false},
		{"config mcp without servers", config, `{"mcp":{"x":1}}`, `{"mcp":{"x":1}}`, true},
		{"config mcp not an object", config, `{"mcp":1}`, `{"mcp":1}`, true},
		{"config servers not an object", config, `{"mcp":{"servers":1}}`, `{"mcp":{"servers":1}}`, true},
		{"config current invalid falls back to bytes", config, `nope`, `{}`, false},
		{"config current invalid equal bytes", config, `nope`, `nope`, true},
		{"config want invalid falls back to bytes", config, `{}`, `nope`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := setup.ContentMatches(tt.path, []byte(tt.current), []byte(tt.want)); got != tt.match {
				t.Errorf("ContentMatches() = %v, want %v", got, tt.match)
			}
		})
	}
}

// conflictsOf reports the conflicts DetectConflicts finds for one artifact
// planned at artifactPath whose file already sits unmanaged at the root.
func conflictsOf(t *testing.T, artifactPath string) setup.ConflictEntry {
	t.Helper()
	root := t.TempDir()
	destination := filepath.Join(root, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflicts, err := setup.DetectConflicts(root, []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{artifactPath},
		Artifacts:    []setup.Artifact{{Path: artifactPath, Content: []byte("takt\n")}},
	}})
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("DetectConflicts() = %v, %v; want one conflict", conflicts, err)
	}
	return conflicts[0]
}

func TestConflictNamesTheCapabilityTheFileDefines(t *testing.T) {
	for path, want := range map[string]string{
		model.OpenCodeSkillsDir + "/takt-memory/SKILL.md": "takt-memory skill",
		".config/opencode/agents/takt-dev.md":             "OpenCode takt-dev agent",
		".config/opencode/opencode.json":                  "OpenCode orchestrator agent and Takt configuration",
		".config/opencode/cli.json":                       "OpenCode cli.json",
	} {
		if got := conflictsOf(t, path).Affects; got != want {
			t.Errorf("Affects for %q = %q, want %q", path, got, want)
		}
	}
}

// deletedAfterInstall installs one artifact, deletes it, and returns the conflict an updated plan reports.
func deletedAfterInstall(t *testing.T, artifactPath string) setup.ConflictEntry {
	t.Helper()
	root := t.TempDir()
	plans := []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{artifactPath},
		Artifacts:    []setup.Artifact{{Path: artifactPath, Content: []byte("{}")}},
	}}
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(artifactPath))); err != nil {
		t.Fatal(err)
	}
	plans[0].Artifacts[0].Content = []byte("{\"updated\": true}")
	conflicts, err := setup.DetectConflicts(root, plans)
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("DetectConflicts() = %v, %v; want one conflict", conflicts, err)
	}
	return conflicts[0]
}

func TestDeletedInjectedFileIsIncompatible(t *testing.T) {
	conflict := deletedAfterInstall(t, opencode.ConfigPath())
	if conflict.Reason != "missing" || conflict.Impact != setup.ImpactIncompatible || conflict.Alternative == "" {
		t.Fatalf("conflict = %+v, want incompatible with alternative", conflict)
	}
}

func TestDeletedOrdinaryFileIsUncertainAndMentionsDeletion(t *testing.T) {
	conflict := deletedAfterInstall(t, ".config/opencode/agents/a.md")
	if conflict.Impact != setup.ImpactUncertain || conflict.Consequence != "Takt cannot guarantee the OpenCode a agent while this file stays deleted." {
		t.Fatalf("conflict = %+v", conflict)
	}
}
