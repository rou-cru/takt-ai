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

// Package skills implements the skill deployment lifecycle: it loads skill
// definitions from the catalog, renders them as deployment artifacts, and
// tracks ownership through the shared setup manifest.
package skills

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/setup"
)

// SkillDefinition holds the raw content of a skill loaded from the catalog.
type SkillDefinition struct {
	Name     string // catalog skill ID (e.g., "takt-sdd-workflow")
	FileName string // file name (e.g., "SKILL.md")
	Content  []byte
}

// LoadSkills reads all skill definitions from the catalog.
// It returns skills sorted by their deployment path for deterministic output.
func LoadSkills() ([]SkillDefinition, error) {
	c, err := catalog.LoadPackages()
	if err != nil {
		return nil, fmt.Errorf("load catalog packages: %w", err)
	}

	definitions := make([]SkillDefinition, 0, len(c.Skills))
	for _, pkg := range c.Skills {
		for _, f := range pkg.Files {
			definitions = append(definitions, SkillDefinition{Name: pkg.ID, FileName: f.Path, Content: f.Content})
		}
	}

	slices.SortFunc(definitions, func(a, b SkillDefinition) int {
		return strings.Compare(skillDeploymentPath(a), skillDeploymentPath(b))
	})

	return definitions, nil
}

// skillDeploymentPath returns the deployment path for a skill definition.
// The path format is the preferred OpenCode V2 location:
// .opencode/skills/<ID>/<filename>
func skillDeploymentPath(skill SkillDefinition) string {
	return path.Join(".opencode", "skills", skill.Name, skill.FileName)
}

// BuildSkillArtifacts converts skill definitions into setup.Artifact values
// ready for deployment.
func BuildSkillArtifacts(definitions []SkillDefinition) []setup.Artifact {
	artifacts := make([]setup.Artifact, 0, len(definitions))
	for _, def := range definitions {
		artifacts = append(artifacts, setup.Artifact{
			Path:    skillDeploymentPath(def),
			Content: def.Content,
		})
	}
	return artifacts
}

// BuildSkillManagedPaths returns the sorted list of managed paths for skill artifacts.
func BuildSkillManagedPaths(artifacts []setup.Artifact) []string {
	paths := make([]string, 0, len(artifacts))
	for _, a := range artifacts {
		paths = append(paths, a.Path)
	}
	slices.Sort(paths)
	return paths
}

// BuildSkillPlan creates a TargetPlan for deploying skills to the .opencode/skills/ directory.
// The plan targets the "skills" ownership target and includes all embedded skill files.
func BuildSkillPlan() (setup.TargetPlan, error) {
	definitions, err := LoadSkills()
	if err != nil {
		return setup.TargetPlan{}, fmt.Errorf("load skills: %w", err)
	}
	if len(definitions) == 0 {
		return setup.TargetPlan{}, fmt.Errorf("no skills found")
	}

	artifacts := BuildSkillArtifacts(definitions)
	managedPaths := BuildSkillManagedPaths(artifacts)

	return setup.TargetPlan{
		Target:       TargetSkills,
		ManagedPaths: managedPaths,
		Artifacts:    artifacts,
	}, nil
}

// TargetSkills is the ownership target for skill files.
const TargetSkills = "skills"
