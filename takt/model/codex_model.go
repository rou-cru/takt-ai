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

package model

import (
	"fmt"
	"slices"
	"strings"
)

var codexAvailableModels = []string{CodexModelSol, CodexModelTerra, CodexModelLuna}

const (
	CodexModelSol   = "openai/gpt-5.6-sol"
	CodexModelTerra = "openai/gpt-5.6-terra"
	CodexModelLuna  = "openai/gpt-5.6-luna"
)

// CodexAvailableModels returns a copy of the available model IDs.
func CodexAvailableModels() []string {
	out := make([]string, len(codexAvailableModels))
	copy(out, codexAvailableModels)
	return out
}

// FilterCodexModels returns model IDs containing the query, ignoring letter case.
// A blank query returns all available model IDs.
func FilterCodexModels(query string) []string {
	all := CodexAvailableModels()
	if strings.TrimSpace(query) == "" {
		return all
	}
	q := strings.ToLower(query)
	out := make([]string, 0, len(all))
	for _, modelID := range all {
		if strings.Contains(strings.ToLower(modelID), q) {
			out = append(out, modelID)
		}
	}
	return out
}

// CodexDefaultPreset returns the official Codex assignment projection.
func CodexDefaultPreset() map[string]ModelAssignment {
	catalog := CanonicalSubAgentCatalog()
	preset := make(map[string]ModelAssignment, len(catalog))
	for _, subAgent := range catalog {
		preset[subAgent.Name] = subAgent.Assignments[AgentCodex]
	}
	return preset
}

// ResolveCodexSubAgentAssignment resolves a sub-agent to its assigned model and effort, using the default sub-agent assignment when needed.
// It returns an error when neither assignment provides a model.
func ResolveCodexSubAgentAssignment(subAgent string, assignments map[string]ModelAssignment) (string, string, error) {
	if len(assignments) == 0 {
		assignments = CodexDefaultPreset()
	}
	a, ok := assignments[subAgent]
	if !ok {
		a, ok = assignments[SubAgentDefault]
	}
	if !ok || a.Model == "" {
		return "", "", fmt.Errorf("codex sub-agent %q has no model assignment", subAgent)
	}
	return a.Model, a.Effort, nil
}

// RenderCodexSubAgentAssignments renders sub-agent model assignments as a Markdown table.
// An empty assignment map uses the default preset, and missing sub-agent assignments use
// the default sub-agent assignment. It returns an error if any sub-agent has no model.
func RenderCodexSubAgentAssignments(assignments map[string]ModelAssignment) (string, error) {
	if len(assignments) == 0 {
		assignments = CodexDefaultPreset()
	}
	var sb strings.Builder
	sb.WriteString("| Sub-Agent | Model | `reasoning_effort` |\n")
	sb.WriteString("|-----------|-------|--------------------|\n")
	for _, subAgent := range CanonicalSubAgents() {
		assignment, ok := assignments[subAgent]
		if !ok {
			assignment, ok = assignments[SubAgentDefault]
		}
		if !ok || assignment.Model == "" {
			return "", fmt.Errorf("codex sub-agent %q has no model assignment", subAgent)
		}
		fmt.Fprintf(&sb, "| `%s` | `%s` | `%s` |\n", subAgent, assignment.Model, assignment.Effort)
	}
	return sb.String(), nil
}

// ValidateCodexModelAssignment validates a Codex model and effort pair.
func ValidateCodexModelAssignment(subAgent string, assignment ModelAssignment) (ModelAssignment, error) {
	if !slices.Contains(codexAvailableModels, assignment.Model) {
		return ModelAssignment{}, fmt.Errorf("codex sub-agent %q has invalid model assignment %q", subAgent, assignment.Model)
	}
	switch assignment.Effort {
	case "", "low", "medium", "high":
		return assignment, nil
	default:
		return ModelAssignment{}, fmt.Errorf("codex sub-agent %q has invalid effort %q", subAgent, assignment.Effort)
	}
}
