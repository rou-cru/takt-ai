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

package catalog

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/rou-cru/takt-ai/takt/model"
	"gopkg.in/yaml.v2"
)

//go:embed subagents.yaml
var subAgentsYAML []byte

const (
	officialPersona = "takt"
	defaultSubAgent = "default"
)

type rawDefinition struct {
	ID          string                   `yaml:"id"`
	Persona     string                   `yaml:"persona"`
	Assignments map[string]rawAssignment `yaml:"assignments"`
}

type rawAssignment struct {
	Model  string `yaml:"model"`
	Effort string `yaml:"effort"`
}

// Load parses and validates the embedded semantic sub-agent catalog.
func Load() ([]model.CanonicalSubAgent, error) {
	return load(subAgentsYAML)
}

func load(data []byte) ([]model.CanonicalSubAgent, error) {
	var raw []rawDefinition
	if err := yaml.UnmarshalStrict(data, &raw); err != nil {
		return nil, fmt.Errorf("catalog YAML: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("catalog YAML: no sub-agents")
	}

	catalog := make([]model.CanonicalSubAgent, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for index, definition := range raw {
		if strings.TrimSpace(definition.ID) == "" {
			return nil, fmt.Errorf("catalog entry %d: missing id", index)
		}
		if _, exists := seen[definition.ID]; exists {
			return nil, fmt.Errorf("catalog entry %d: duplicate id %q", index, definition.ID)
		}
		seen[definition.ID] = struct{}{}
		if definition.Persona != officialPersona {
			return nil, fmt.Errorf("catalog entry %q: persona must be %q", definition.ID, officialPersona)
		}
		assignments := make(map[model.AgentID]model.ModelAssignment, len(definition.Assignments))
		for target, assignment := range definition.Assignments {
			agent := model.AgentID(target)
			if agent != model.AgentClaudeCode && agent != model.AgentCodex {
				return nil, fmt.Errorf("catalog entry %q: unsupported assignment target %q", definition.ID, target)
			}
			if strings.TrimSpace(assignment.Model) == "" {
				return nil, fmt.Errorf("catalog entry %q: %s assignment has no model", definition.ID, target)
			}
			assignments[agent] = model.ModelAssignment{
				Model:  assignment.Model,
				Effort: assignment.Effort,
			}
		}
		if len(definition.Assignments) != 2 {
			return nil, fmt.Errorf("catalog entry %q: must define Claude and Codex assignments", definition.ID)
		}
		if _, ok := assignments[model.AgentClaudeCode]; !ok {
			return nil, fmt.Errorf("catalog entry %q: missing Claude assignment", definition.ID)
		}
		if _, ok := assignments[model.AgentCodex]; !ok {
			return nil, fmt.Errorf("catalog entry %q: missing Codex assignment", definition.ID)
		}
		for _, agent := range []model.AgentID{model.AgentClaudeCode, model.AgentCodex} {
			var err error
			assignment := assignments[agent]
			switch agent {
			case model.AgentClaudeCode:
				assignment, err = model.ValidateClaudeModelAssignment(definition.ID, assignment)
			case model.AgentCodex:
				assignment, err = model.ValidateCodexModelAssignment(definition.ID, assignment)
			}
			if err != nil {
				return nil, fmt.Errorf("catalog entry %q: %w", definition.ID, err)
			}
			assignments[agent] = assignment
		}
		catalog = append(catalog, model.CanonicalSubAgent{
			Name:        definition.ID,
			Persona:     definition.Persona,
			Assignments: assignments,
		})
	}

	if _, ok := seen[defaultSubAgent]; !ok {
		return nil, fmt.Errorf("catalog: missing default sub-agent %q", defaultSubAgent)
	}
	return catalog, nil
}

// CanonicalSubAgentCatalog returns the validated catalog in YAML order.
func CanonicalSubAgentCatalog() []model.CanonicalSubAgent {
	catalog, err := Load()
	if err != nil {
		panic(err)
	}
	return catalog
}

// CanonicalSubAgents returns catalog IDs in their YAML order.
func CanonicalSubAgents() []string {
	catalog := CanonicalSubAgentCatalog()
	names := make([]string, len(catalog))
	for index, subAgent := range catalog {
		names[index] = subAgent.Name
	}
	return names
}

// ClaudeDefaultPreset returns the catalog's Claude assignment projection.
func ClaudeDefaultPreset() map[string]model.ModelAssignment {
	return defaultPreset(model.AgentClaudeCode)
}

// CodexDefaultPreset returns the catalog's Codex assignment projection.
func CodexDefaultPreset() map[string]model.ModelAssignment {
	return defaultPreset(model.AgentCodex)
}

func defaultPreset(agent model.AgentID) map[string]model.ModelAssignment {
	catalog := CanonicalSubAgentCatalog()
	preset := make(map[string]model.ModelAssignment, len(catalog))
	for _, subAgent := range catalog {
		preset[subAgent.Name] = subAgent.Assignments[agent]
	}
	return preset
}

// ResolveClaudeSubAgentAssignment applies catalog defaults and Claude validation.
func ResolveClaudeSubAgentAssignment(subAgent string, overrides map[string]model.ModelAssignment) (model.ModelAssignment, error) {
	assignment, err := model.ResolveSubAgentAssignment(model.AgentClaudeCode, subAgent, defaultSubAgent, overrides, ClaudeDefaultPreset())
	if err != nil {
		return model.ModelAssignment{}, err
	}
	return model.ValidateClaudeModelAssignment(subAgent, assignment)
}

// ResolveCodexSubAgentAssignment applies catalog defaults and Codex precedence.
func ResolveCodexSubAgentAssignment(subAgent string, overrides map[string]model.ModelAssignment) (string, string, error) {
	assignment, err := model.ResolveSubAgentAssignment(model.AgentCodex, subAgent, defaultSubAgent, overrides, CodexDefaultPreset())
	if err != nil {
		return "", "", err
	}
	return assignment.Model, assignment.Effort, nil
}
