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

import "fmt"

// ModelAssignment is the canonical model and effort assigned to a sub-agent.
type ModelAssignment struct {
	Model  string `json:"model"`  // target-specific model identifier
	Effort string `json:"effort"` // "" = target default; "low" | "medium" | "high"
}

// ResolveSubAgentAssignment applies provider-neutral assignment precedence.
// A sub-agent override wins, followed by its preset assignment, then the
// explicit and preset fallback assignments.
func ResolveSubAgentAssignment(agent AgentID, subAgent, defaultSubAgent string, overrides, preset map[string]ModelAssignment) (ModelAssignment, error) {
	assignment, ok := overrides[subAgent]
	if !ok {
		assignment, ok = preset[subAgent]
	}
	if !ok {
		assignment, ok = overrides[defaultSubAgent]
	}
	if !ok {
		assignment, ok = preset[defaultSubAgent]
	}
	if !ok {
		return ModelAssignment{}, fmt.Errorf("%s sub-agent %q has no model assignment", agent, subAgent)
	}
	return validateModelAssignment(agent, subAgent, assignment)
}

func validateModelAssignment(agent AgentID, subAgent string, assignment ModelAssignment) (ModelAssignment, error) {
	if assignment.Model == "" {
		return ModelAssignment{}, fmt.Errorf("%s sub-agent %q has no model assignment", agent, subAgent)
	}
	return assignment, nil
}
