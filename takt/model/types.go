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

// Package model keeps one shared vocabulary for agents, components, and assignments so adapters never drift apart.
package model

// AgentOpenCode names the OpenCode adapter, the identifier install, inject and
// verify agree on.
const AgentOpenCode = "opencode"

// ComponentID names an installable piece so setup and lifecycle can select parts without string guessing.
type ComponentID string

// Component IDs are the closed set of independently installable pieces.
const (
	// ComponentEngram names the memory-graph piece so installs can toggle memory independently.
	ComponentEngram ComponentID = "engram"
	// ComponentSkills names the skills piece so installs can toggle skills independently.
	ComponentSkills ComponentID = "skills"
	// ComponentContext7 names the docs-lookup integration so installs can toggle it independently.
	ComponentContext7 ComponentID = "context7"
	// ComponentCodegraph names the codebase-exploration MCP integration, mandatory core like Engram.
	ComponentCodegraph ComponentID = "codegraph"
)

// CanonicalSubAgent names one specialist of the crew every adapter configures.
type CanonicalSubAgent struct {
	// Name identifies the specialist, stable so assignments can match by name.
	Name string
}

// InjectionResult reports what an inject or remove changed so callers can record ownership and show honest summaries.
type InjectionResult struct {
	// Changed flags whether any file changed, so callers can skip redundant bookkeeping.
	Changed bool
	// Files lists touched paths, so ownership and summaries stay accurate.
	Files []string
	// Preserved lists paths left for the user, so kept content is never reported as managed.
	Preserved []string
}
