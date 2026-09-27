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
	"path"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/engram"
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
	for i, curr := range skills[1:] {
		prevPath := skillDeploymentPath(skills[i])
		currPath := skillDeploymentPath(curr)
		if prevPath > currPath {
			t.Errorf("skills not sorted: %s > %s", prevPath, currPath)
		}
	}
}

func TestBuildSkillArtifacts(t *testing.T) {
	definitions := []SkillDefinition{
		{Name: "takt-test-skill", FileName: "SKILL.md", Content: []byte("# Test Skill\n")},
	}

	artifacts := BuildSkillArtifacts(definitions)
	if len(artifacts) != 1 {
		t.Fatalf("BuildSkillArtifacts() returned %d artifacts, want 1", len(artifacts))
	}

	artifact := artifacts[0]
	expectedPath := ".opencode/skills/takt-test-skill/SKILL.md"
	if artifact.Path != expectedPath {
		t.Errorf("artifact path = %q, want %q", artifact.Path, expectedPath)
	}
	if string(artifact.Content) != "# Test Skill\n" {
		t.Errorf("artifact content = %q, want %q", string(artifact.Content), "# Test Skill\n")
	}
}

func TestBuildSkillManagedPaths(t *testing.T) {
	artifacts := []setup.Artifact{
		{Path: ".opencode/skills/b-skill/SKILL.md"},
		{Path: ".opencode/skills/a-skill/SKILL.md"},
	}

	paths := BuildSkillManagedPaths(artifacts)
	if len(paths) != 2 {
		t.Fatalf("BuildSkillManagedPaths() returned %d paths, want 2", len(paths))
	}

	// Verify paths are sorted.
	if paths[0] != ".opencode/skills/a-skill/SKILL.md" {
		t.Errorf("paths[0] = %q, want %q", paths[0], ".opencode/skills/a-skill/SKILL.md")
	}
	if paths[1] != ".opencode/skills/b-skill/SKILL.md" {
		t.Errorf("paths[1] = %q, want %q", paths[1], ".opencode/skills/b-skill/SKILL.md")
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

func TestSkillDeploymentPath(t *testing.T) {
	skill := SkillDefinition{
		Name:     "takt-sdd-workflow",
		FileName: "SKILL.md",
	}

	got := skillDeploymentPath(skill)
	want := ".opencode/skills/takt-sdd-workflow/SKILL.md"
	if got != want {
		t.Errorf("skillDeploymentPath() = %q, want %q", got, want)
	}
}

// memorySkills indexes deployed memory skills by path so tests check what agents actually read.
func memorySkills(t *testing.T) map[string]string {
	t.Helper()
	definitions, err := LoadSkills()
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, definition := range definitions {
		if !strings.HasPrefix(definition.Name, "takt-memory-") {
			continue
		}
		byPath[skillDeploymentPath(definition)] = string(definition.Content)
	}
	return byPath
}

// TestEveryAuthorHasExactlyOneMemoryRoleSkill verifies no specialist ships without memory rules and no role skill is orphaned.
func TestEveryAuthorHasExactlyOneMemoryRoleSkill(t *testing.T) {
	skills := memorySkills(t)
	if _, ok := skills[engram.ContractSkillPath]; !ok {
		t.Fatalf("contract skill missing at %s", engram.ContractSkillPath)
	}
	content, err := catalog.LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	authors := []string{shared.OrchestratorID}
	for id := range content {
		authors = append(authors, id)
	}
	used := map[string]bool{engram.ContractSkillPath: true}
	for _, author := range authors {
		rolePath := engram.MemorySkillPath(author)
		if _, ok := skills[rolePath]; !ok {
			t.Errorf("author %s has no memory skill at %s", author, rolePath)
		}
		used[rolePath] = true
	}
	for deployed := range skills {
		if !used[deployed] {
			t.Errorf("memory skill %s belongs to no author", deployed)
		}
	}
}

// TestMemoryRoleSkillsLinkContractWithoutRestatingIt verifies shared rules stay in the contract alone.
func TestMemoryRoleSkillsLinkContractWithoutRestatingIt(t *testing.T) {
	for deployed, content := range memorySkills(t) {
		for _, metadata := range []string{"mem_save", "mem_session_", "`session_id`", "**Author**:", "Memory: "} {
			if strings.Contains(content, metadata) {
				t.Errorf("%s asks the agent for harness metadata %q", deployed, metadata)
			}
		}
		if strings.Contains(strings.ToLower(content), "## next steps") {
			t.Errorf("%s carries a Next Steps section", deployed)
		}
		if deployed == engram.ContractSkillPath {
			continue
		}
		if !strings.Contains(content, "takt-memory-contract/SKILL.md") {
			t.Errorf("%s does not link the contract", path.Dir(deployed))
		}
		for _, shared := range []string{"| `proposal` |", "`proposal`, `decision`", "memory_record(", "relates_to", "mem_search", "mem_save", "Memory: ~"} {
			if strings.Contains(content, shared) {
				t.Errorf("%s restates contract rule %q", path.Dir(deployed), shared)
			}
		}
	}
}
