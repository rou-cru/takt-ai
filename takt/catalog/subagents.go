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

// load parses and validates a YAML sub-agent catalog.
// It returns canonical sub-agent definitions in catalog order, or an error describing the first invalid entry.
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
		entry, err := parseDefinition(index, definition, seen)
		if err != nil {
			return nil, err
		}
		catalog = append(catalog, entry)
	}

	if _, ok := seen[defaultSubAgent]; !ok {
		return nil, fmt.Errorf("catalog: missing default sub-agent %q", defaultSubAgent)
	}
	return catalog, nil
}

// parseDefinition validates one catalog entry's identity and persona and
// resolves its per-target model assignments.
func parseDefinition(index int, definition rawDefinition, seen map[string]struct{}) (model.CanonicalSubAgent, error) {
	if strings.TrimSpace(definition.ID) == "" {
		return model.CanonicalSubAgent{}, fmt.Errorf("catalog entry %d: missing id", index)
	}
	if _, exists := seen[definition.ID]; exists {
		return model.CanonicalSubAgent{}, fmt.Errorf("catalog entry %d: duplicate id %q", index, definition.ID)
	}
	seen[definition.ID] = struct{}{}
	if definition.Persona != officialPersona {
		return model.CanonicalSubAgent{}, fmt.Errorf("catalog entry %q: persona must be %q", definition.ID, officialPersona)
	}
	assignments, err := parseAssignments(definition.ID, definition.Assignments)
	if err != nil {
		return model.CanonicalSubAgent{}, err
	}
	return model.CanonicalSubAgent{
		Name:        definition.ID,
		Persona:     definition.Persona,
		Assignments: assignments,
	}, nil
}

func parseAssignments(id string, raw map[string]rawAssignment) (map[model.AgentID]model.ModelAssignment, error) {
	assignments := make(map[model.AgentID]model.ModelAssignment, len(raw))
	for target, assignment := range raw {
		agent := model.AgentID(target)
		if agent != model.AgentClaudeCode && agent != model.AgentCodex {
			return nil, fmt.Errorf("catalog entry %q: unsupported assignment target %q", id, target)
		}
		if strings.TrimSpace(assignment.Model) == "" {
			return nil, fmt.Errorf("catalog entry %q: %s assignment has no model", id, target)
		}
		assignments[agent] = model.ModelAssignment{
			Model:  assignment.Model,
			Effort: assignment.Effort,
		}
	}
	if len(assignments) != 2 {
		return nil, fmt.Errorf("catalog entry %q: must define Claude and Codex assignments", id)
	}
	return assignments, validateAssignments(id, assignments)
}

func validateAssignments(id string, assignments map[model.AgentID]model.ModelAssignment) error {
	for _, agent := range []model.AgentID{model.AgentClaudeCode, model.AgentCodex} {
		var err error
		assignment := assignments[agent]
		switch agent {
		case model.AgentClaudeCode:
			assignment, err = model.ValidateClaudeModelAssignment(id, assignment)
		case model.AgentCodex:
			assignment, err = model.ValidateCodexModelAssignment(id, assignment)
		}
		if err != nil {
			return fmt.Errorf("catalog entry %q: %w", id, err)
		}
		assignments[agent] = assignment
	}
	return nil
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

// defaultPreset builds model assignments for each cataloged sub-agent for the specified agent.
func defaultPreset(agent model.AgentID) map[string]model.ModelAssignment {
	catalog := CanonicalSubAgentCatalog()
	preset := make(map[string]model.ModelAssignment, len(catalog))
	for _, subAgent := range catalog {
		preset[subAgent.Name] = subAgent.Assignments[agent]
	}
	return preset
}

// ResolveClaudeSubAgentAssignment resolves a Claude model assignment using catalog defaults and optional overrides, then validates the result. It returns an error if resolution or validation fails.
func ResolveClaudeSubAgentAssignment(subAgent string, overrides map[string]model.ModelAssignment) (model.ModelAssignment, error) {
	assignment, err := model.ResolveSubAgentAssignment(model.AgentClaudeCode, subAgent, defaultSubAgent, overrides, ClaudeDefaultPreset())
	if err != nil {
		return model.ModelAssignment{}, err
	}
	return model.ValidateClaudeModelAssignment(subAgent, assignment)
}

// ResolveCodexSubAgentAssignment resolves a Codex model assignment using catalog defaults and overrides.
// It returns the resolved model and effort, or an error if the assignment cannot be resolved.
func ResolveCodexSubAgentAssignment(subAgent string, overrides map[string]model.ModelAssignment) (string, string, error) {
	assignment, err := model.ResolveSubAgentAssignment(model.AgentCodex, subAgent, defaultSubAgent, overrides, CodexDefaultPreset())
	if err != nil {
		return "", "", err
	}
	return assignment.Model, assignment.Effort, nil
}
