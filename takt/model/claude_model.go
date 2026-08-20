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

// ClaudeModelAlias represents one of the Claude model tiers.
type ClaudeModelAlias string

const (
	ClaudeModelFable  ClaudeModelAlias = "fable"
	ClaudeModelOpus   ClaudeModelAlias = "opus"
	ClaudeModelSonnet ClaudeModelAlias = "sonnet"
	ClaudeModelHaiku  ClaudeModelAlias = "haiku"
)

// String returns the string representation of the alias.
func (a ClaudeModelAlias) String() string { return string(a) }

// Valid reports whether the alias is one of the known Claude model tiers.
func (a ClaudeModelAlias) Valid() bool {
	switch a {
	case ClaudeModelFable, ClaudeModelOpus, ClaudeModelSonnet, ClaudeModelHaiku:
		return true
	default:
		return false
	}
}

// ClaudeEffort is a Claude effort level.
type ClaudeEffort string

const (
	ClaudeEffortDefault ClaudeEffort = ""
	ClaudeEffortLow     ClaudeEffort = "low"
	ClaudeEffortMedium  ClaudeEffort = "medium"
	ClaudeEffortHigh    ClaudeEffort = "high"
	ClaudeEffortXHigh   ClaudeEffort = "xhigh"
	ClaudeEffortMax     ClaudeEffort = "max"
)

// Valid reports whether the effort is known.
func (e ClaudeEffort) Valid() bool {
	switch e {
	case ClaudeEffortDefault, ClaudeEffortLow, ClaudeEffortMedium, ClaudeEffortHigh, ClaudeEffortXHigh, ClaudeEffortMax:
		return true
	default:
		return false
	}
}

// ClaudeEffortsForModel returns the effort choices for a model alias.
func ClaudeEffortsForModel(alias ClaudeModelAlias) []ClaudeEffort {
	switch alias {
	case ClaudeModelFable, ClaudeModelOpus:
		return []ClaudeEffort{ClaudeEffortDefault, ClaudeEffortLow, ClaudeEffortMedium, ClaudeEffortHigh, ClaudeEffortXHigh, ClaudeEffortMax}
	case ClaudeModelSonnet:
		return []ClaudeEffort{ClaudeEffortDefault, ClaudeEffortLow, ClaudeEffortMedium, ClaudeEffortHigh, ClaudeEffortMax}
	default:
		return []ClaudeEffort{ClaudeEffortDefault}
	}
}

// ClaudeEffortAllowedForModel reports whether effort is valid for alias.
func ClaudeEffortAllowedForModel(alias ClaudeModelAlias, effort ClaudeEffort) bool {
	if !effort.Valid() {
		return false
	}
	for _, allowed := range ClaudeEffortsForModel(alias) {
		if effort == allowed {
			return true
		}
	}
	return false
}

// ClaudeDefaultPreset returns the official Claude assignment projection.
func ClaudeDefaultPreset() map[string]ModelAssignment {
	catalog := CanonicalSubAgentCatalog()
	preset := make(map[string]ModelAssignment, len(catalog))
	for _, subAgent := range catalog {
		preset[subAgent.Name] = subAgent.Assignments[AgentClaudeCode]
	}
	return preset
}
