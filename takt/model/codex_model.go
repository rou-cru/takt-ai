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

// ValidateCodexModelAssignment validates a canonical Codex model and effort pair.
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
