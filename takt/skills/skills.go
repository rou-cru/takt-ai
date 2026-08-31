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
// definitions from the embedded source directory, renders them as deployment
// artifacts, and tracks ownership through the shared setup manifest.
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// nonSkillChars matches any character outside [a-z0-9-] once the name has
// been lowercased, so punctuation like "/" or "!" collapses to a separator
// instead of passing through untouched.
var nonSkillChars = regexp.MustCompile(`[^a-z0-9-]+`)

//go:embed workflows
var skillsFS embed.FS

// SkillDir is the namespace root for embedded skill definitions.
const SkillDir = "workflows"

// SkillArtifact is a deployment artifact for a skill file. Path is relative to
// the deployment root (e.g., ".agents/skills/linear-workflow/SKILL.md").
type SkillArtifact struct {
	Path    string
	Content []byte
}

// SkillDefinition holds the raw content of a skill loaded from the embedded filesystem.
type SkillDefinition struct {
	Name     string // skill path relative to the package (e.g., "workflows/linear-workflow")
	FileName string // file name (e.g., "SKILL.md")
	Content  []byte
}

// LoadSkills reads all skill definitions from the embedded filesystem.
// It returns skills sorted by their deployment path for deterministic output.
func LoadSkills() ([]SkillDefinition, error) {
	return loadSkills(skillsFS)
}

// loadSkills reads skill definitions from the given filesystem.
func loadSkills(fsys fs.FS) ([]SkillDefinition, error) {
	var skills []SkillDefinition
	seen := make(map[string]struct{})
	err := fs.WalkDir(fsys, SkillDir, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if path.Base(filePath) != "SKILL.md" || path.Dir(filePath) == SkillDir {
			return fmt.Errorf("unexpected skill file %q", filePath)
		}

		name := strings.TrimSuffix(filePath, "/SKILL.md")
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate skill identity %q", name)
		}
		seen[name] = struct{}{}

		content, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return fmt.Errorf("read skill %q: %w", filePath, err)
		}
		skills = append(skills, SkillDefinition{Name: name, FileName: "SKILL.md", Content: content})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk skills directory: %w", err)
	}

	// Sort by deployment path for deterministic output.
	sort.Slice(skills, func(i, j int) bool {
		return skillDeploymentPath(skills[i]) < skillDeploymentPath(skills[j])
	})

	return skills, nil
}

// skillDeploymentPath returns the deployment path for a skill definition.
// The path format is: .agents/skills/<namespace>/<relative-path>/<filename>
func skillDeploymentPath(skill SkillDefinition) string {
	return path.Join(".agents", "skills", skill.Name, skill.FileName)
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
	sort.Strings(paths)
	return paths
}

// BuildSkillPlan creates a TargetPlan for deploying skills to the .agents/skills/ directory.
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

// ListSkills returns the names of all available skills in sorted order.
func ListSkills() ([]string, error) {
	definitions, err := LoadSkills()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var names []string
	for _, def := range definitions {
		if _, exists := seen[def.Name]; exists {
			continue
		}
		seen[def.Name] = struct{}{}
		names = append(names, def.Name)
	}
	sort.Strings(names)
	return names, nil
}

// GetSkillContent returns the content of a specific skill file.
// The name parameter is the namespace-qualified skill path (e.g., "workflows/linear-workflow").
func GetSkillContent(name string) ([]byte, error) {
	definitions, err := LoadSkills()
	if err != nil {
		return nil, err
	}

	for _, def := range definitions {
		if def.Name == name {
			return def.Content, nil
		}
	}
	return nil, fmt.Errorf("skill %q not found", name)
}

// NormalizeSkillName normalizes a skill name for use in paths and identifiers.
// It converts to lowercase and replaces spaces/special characters with hyphens.
func NormalizeSkillName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	name = nonSkillChars.ReplaceAllString(name, "-")
	// Remove consecutive hyphens.
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	return name
}
