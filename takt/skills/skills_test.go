// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package skills

import (
	"testing"
	"testing/fstest"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestLoadSkills(t *testing.T) {
	skills, err := LoadSkills()
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("LoadSkills() returned no skills")
	}

	// Verify each skill has required fields.
	for _, skill := range skills {
		if skill.Name == "" {
			t.Error("skill has empty name")
		}
		if skill.FileName == "" {
			t.Error("skill has empty filename")
		}
		if len(skill.Content) == 0 {
			t.Errorf("skill %q has empty content", skill.Name)
		}
	}
}

func TestLoadSkillsSorted(t *testing.T) {
	skills, err := LoadSkills()
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(skills) < 2 {
		t.Skip("need at least 2 skills to test sorting")
	}

	// Verify skills are sorted by deployment path.
	for i := 1; i < len(skills); i++ {
		prev := skillDeploymentPath(skills[i-1])
		curr := skillDeploymentPath(skills[i])
		if prev > curr {
			t.Errorf("skills not sorted: %s > %s", prev, curr)
		}
	}
}

func TestLoadSkillsRecursivelyPreservesWorkflowPaths(t *testing.T) {
	definitions, err := loadSkills(fstest.MapFS{
		"workflows/SDD/SKILL.md":          {Data: []byte("# SDD")},
		"workflows/examples/SDD/SKILL.md": {Data: []byte("# Example SDD")},
		"workflows/flat/SKILL.md":         {Data: []byte("# Flat")},
	})
	if err != nil {
		t.Fatalf("loadSkills() error = %v", err)
	}

	got := make([]string, len(definitions))
	for i, definition := range definitions {
		got[i] = skillDeploymentPath(definition)
	}
	want := []string{
		".agents/skills/workflows/SDD/SKILL.md",
		".agents/skills/workflows/examples/SDD/SKILL.md",
		".agents/skills/workflows/flat/SKILL.md",
	}
	if !equalStrings(got, want) {
		t.Errorf("deployment paths = %q, want %q", got, want)
	}
}

func TestLoadSkillsRejectsUnexpectedFiles(t *testing.T) {
	_, err := loadSkills(fstest.MapFS{
		"workflows/SDD/README.md": {Data: []byte("unexpected")},
	})
	if err == nil {
		t.Fatal("loadSkills() error = nil, want unexpected file error")
	}
}

func TestBuildSkillArtifacts(t *testing.T) {
	definitions := []SkillDefinition{
		{Name: "workflows/test-skill", FileName: "SKILL.md", Content: []byte("# Test Skill\n")},
	}

	artifacts := BuildSkillArtifacts(definitions)
	if len(artifacts) != 1 {
		t.Fatalf("BuildSkillArtifacts() returned %d artifacts, want 1", len(artifacts))
	}

	artifact := artifacts[0]
	expectedPath := ".agents/skills/workflows/test-skill/SKILL.md"
	if artifact.Path != expectedPath {
		t.Errorf("artifact path = %q, want %q", artifact.Path, expectedPath)
	}
	if string(artifact.Content) != "# Test Skill\n" {
		t.Errorf("artifact content = %q, want %q", string(artifact.Content), "# Test Skill\n")
	}
}

func TestBuildSkillManagedPaths(t *testing.T) {
	artifacts := []setup.Artifact{
		{Path: ".agents/skills/b-skill/SKILL.md"},
		{Path: ".agents/skills/a-skill/SKILL.md"},
	}

	paths := BuildSkillManagedPaths(artifacts)
	if len(paths) != 2 {
		t.Fatalf("BuildSkillManagedPaths() returned %d paths, want 2", len(paths))
	}

	// Verify paths are sorted.
	if paths[0] != ".agents/skills/a-skill/SKILL.md" {
		t.Errorf("paths[0] = %q, want %q", paths[0], ".agents/skills/a-skill/SKILL.md")
	}
	if paths[1] != ".agents/skills/b-skill/SKILL.md" {
		t.Errorf("paths[1] = %q, want %q", paths[1], ".agents/skills/b-skill/SKILL.md")
	}
}

func TestBuildSkillPlan(t *testing.T) {
	plan, err := BuildSkillPlan()
	if err != nil {
		t.Fatalf("BuildSkillPlan() error = %v", err)
	}

	if plan.Target != TargetSkills {
		t.Errorf("plan.Target = %q, want %q", plan.Target, TargetSkills)
	}
	if len(plan.ManagedPaths) == 0 {
		t.Error("plan.ManagedPaths is empty")
	}
	if len(plan.Artifacts) == 0 {
		t.Error("plan.Artifacts is empty")
	}
	if len(plan.ManagedPaths) != len(plan.Artifacts) {
		t.Errorf("ManagedPaths count %d != Artifacts count %d", len(plan.ManagedPaths), len(plan.Artifacts))
	}
}

func TestListSkills(t *testing.T) {
	names, err := ListSkills()
	if err != nil {
		t.Fatalf("ListSkills() error = %v", err)
	}
	if len(names) == 0 {
		t.Fatal("ListSkills() returned no names")
	}

	// Verify names are sorted.
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("names not sorted: %s > %s", names[i-1], names[i])
		}
	}
}

func TestGetSkillContent(t *testing.T) {
	// First, get the list of skills to find a valid name.
	names, err := ListSkills()
	if err != nil {
		t.Fatalf("ListSkills() error = %v", err)
	}
	if len(names) == 0 {
		t.Skip("no skills to test")
	}

	// Test getting content for an existing skill.
	content, err := GetSkillContent(names[0])
	if err != nil {
		t.Fatalf("GetSkillContent(%q) error = %v", names[0], err)
	}
	if len(content) == 0 {
		t.Errorf("GetSkillContent(%q) returned empty content", names[0])
	}

	// Test getting content for a non-existent skill.
	_, err = GetSkillContent("non-existent-skill")
	if err == nil {
		t.Error("GetSkillContent(non-existent) should return error")
	}
}

func TestNormalizeSkillName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"linear-workflow", "linear-workflow"},
		{"Linear Workflow", "linear-workflow"},
		{"linear_workflow", "linear-workflow"},
		{"linear--workflow", "linear-workflow"},
		{"  linear-workflow  ", "linear-workflow"},
		{"LINEAR-WORKFLOW", "linear-workflow"},
	}

	for _, tc := range tests {
		got := NormalizeSkillName(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeSkillName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSkillDeploymentPath(t *testing.T) {
	skill := SkillDefinition{
		Name:     "workflows/linear-workflow",
		FileName: "SKILL.md",
	}

	got := skillDeploymentPath(skill)
	want := ".agents/skills/workflows/linear-workflow/SKILL.md"
	if got != want {
		t.Errorf("skillDeploymentPath() = %q, want %q", got, want)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
