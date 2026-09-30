package setup

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/engram"
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
		{"config injected servers ignored", config, `{"a":1,"mcp":{"servers":{"engram":{},"codegraph":{}}}}`, `{"a":1}`, true},
		{"config user server kept as drift", config, `{"a":1,"mcp":{"servers":{"engram":{},"mine":{}}}}`, `{"a":1}`, false},
		{"config mcp without servers", config, `{"mcp":{"x":1}}`, `{"mcp":{"x":1}}`, true},
		{"config mcp not an object", config, `{"mcp":1}`, `{"mcp":1}`, true},
		{"config servers not an object", config, `{"mcp":{"servers":1}}`, `{"mcp":{"servers":1}}`, true},
		{"config current invalid falls back to bytes", config, `nope`, `{}`, false},
		{"config current invalid equal bytes", config, `nope`, `nope`, true},
		{"config want invalid falls back to bytes", config, `{}`, `nope`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContentMatches(tt.path, []byte(tt.current), []byte(tt.want)); got != tt.match {
				t.Errorf("ContentMatches() = %v, want %v", got, tt.match)
			}
		})
	}
}

func TestIsJSONObjectInputs(t *testing.T) {
	for raw, want := range map[string]bool{
		"":         true,
		"  \n":     true,
		`{"a":1}`:  true,
		`{}`:       true,
		"not json": false,
		`{"a":`:    false,
	} {
		if got := isJSONObject([]byte(raw)); got != want {
			t.Errorf("isJSONObject(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestCapabilityFor(t *testing.T) {
	for path, want := range map[string]string{
		engram.MemorySkillsDir + "/takt-memory/SKILL.md": "takt-memory skill",
		".config/opencode/agents/takt-dev.md":            "OpenCode takt-dev agent",
		".config/opencode/opencode.json":                 "OpenCode orchestrator agent and Takt configuration",
		".config/opencode/cli.json":                      "OpenCode cli.json",
	} {
		if got := capabilityFor(path); got != want {
			t.Errorf("capabilityFor(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestClassifyConflictMissingInjectedFileIsIncompatible(t *testing.T) {
	conflict := ConflictEntry{Reason: "missing"}
	artifact := Artifact{Path: opencode.ConfigPath(), Content: []byte("{}")}
	classifyConflict(&conflict, artifact, OwnershipEntry{}, true, nil)
	if conflict.Impact != ImpactIncompatible || conflict.Alternative == "" {
		t.Fatalf("conflict = %+v, want incompatible with alternative", conflict)
	}
}

func TestClassifyConflictMissingOrdinaryFileMentionsDeletion(t *testing.T) {
	conflict := ConflictEntry{Reason: "missing"}
	classifyConflict(&conflict, Artifact{Path: "x/agents/a.md", Content: []byte("new")}, OwnershipEntry{SHA256: hashOf([]byte("old"))}, true, nil)
	if conflict.Impact != ImpactUncertain || conflict.Consequence != "Takt cannot guarantee the OpenCode a agent while this file stays deleted." {
		t.Fatalf("conflict = %+v", conflict)
	}
}
