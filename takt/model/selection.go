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

import "slices"

// Selection contains the user's installation and model choices.
type Selection struct {
	Agents             []AgentID
	Components         []ComponentID
	Skills             []SkillID
	Setup              SetupChoice                 // Default or Custom
	ModelOverrides     map[string]ModelAssignment  // key = canonical sub-agent name
	OpenCodePlugins    []OpenCodeCommunityPluginID // optional community OpenCode TUI plugins
	CommunityTools     []CommunityToolID           // optional cross-agent community tools/plugins
	NonDetectedAgents  []AgentID                   // selected targets that were not detected at runtime
	PreservedSubagents []string                    // sub-agent names preserved as-is during install/sync
}

// HasCommunityTool reports whether the selection includes tool.
func (s Selection) HasCommunityTool(tool CommunityToolID) bool {
	return slices.Contains(s.CommunityTools, tool)
}

// HasAgent reports whether the selection includes agent.
func (s Selection) HasAgent(agent AgentID) bool {
	return slices.Contains(s.Agents, agent)
}

// HasComponent reports whether the selection includes component.
func (s Selection) HasComponent(component ComponentID) bool {
	return slices.Contains(s.Components, component)
}

// IsNonDetected reports whether agent was selected but not detected.
func (s Selection) IsNonDetected(agent AgentID) bool {
	return slices.Contains(s.NonDetectedAgents, agent)
}

// SyncOverrides holds optional selection overrides.
type SyncOverrides struct {
	// TargetAgents forces TUI sync to run the adapter(s) affected by the
	// override, even when persisted install state omits them. This is used by
	// model/profile configurators, where the user picked a concrete target agent.
	TargetAgents   []AgentID
	ModelOverrides map[string]ModelAssignment // nil = no override; empty map = reset to defaults
}
