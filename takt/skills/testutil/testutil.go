// Package testutil provides shared test helpers for skills-related test suites.
package testutil

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/skills"
)

// FirstSkill returns the path and content of the first embedded skill. It is
// shared between cmd/takt-ai and takt/tui/runtime test suites to avoid
// duplicating the same loader in two packages.
func FirstSkill(t *testing.T) (string, []byte) {
	t.Helper()
	definitions, err := skills.LoadSkills()
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	artifacts := skills.BuildSkillArtifacts(definitions)
	if len(artifacts) == 0 {
		t.Fatal("no embedded skills found")
	}
	return artifacts[0].Path, artifacts[0].Content
}
