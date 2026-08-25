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

type AgentID string

const (
	AgentClaudeCode AgentID = "claude-code"
	AgentOpenCode   AgentID = "opencode"
	AgentCodex      AgentID = "codex"
)

type ComponentID string

const (
	ComponentEngram           ComponentID = "engram"
	ComponentSkills           ComponentID = "skills"
	ComponentContext7         ComponentID = "context7"
	ComponentPermission       ComponentID = "permissions"
	ComponentTheme            ComponentID = "theme"
	ComponentClaudeTheme      ComponentID = "claude-theme"
	ComponentOpenCodeTaktLogo ComponentID = "opencode-takt-logo"
)

type UninstallMode string

const (
	UninstallModePartial      UninstallMode = "partial"
	UninstallModeFull         UninstallMode = "full"
	UninstallModeFullRemove   UninstallMode = "full-remove"
	UninstallModeCleanInstall UninstallMode = "clean-install"
)

type EngramUninstallScope string

const (
	EngramUninstallScopeGlobal  EngramUninstallScope = "global"
	EngramUninstallScopeProject EngramUninstallScope = "project"
)

type SkillID string

// CanonicalSubAgent describes one specialist shared by every target adapter.
type CanonicalSubAgent struct {
	Name        string
	Persona     string
	Assignments map[AgentID]ModelAssignment
}

// SystemPromptStrategy defines how an agent system prompt is managed.
type SystemPromptStrategy int

const (
	// StrategyMarkdownSections injects sections into an existing Markdown file.
	StrategyMarkdownSections SystemPromptStrategy = iota
	// StrategyFileReplace replaces the system prompt file.
	StrategyFileReplace
	// StrategyAppendToFile appends content to the system prompt file.
	StrategyAppendToFile
	// StrategySteeringFile writes a steering file.
	StrategySteeringFile
)

// MCPStrategy defines how MCP server configs are written.
type MCPStrategy int

const (
	// StrategySeparateMCPFiles writes one JSON file per server.
	StrategySeparateMCPFiles MCPStrategy = iota
	// StrategyMergeIntoSettings merges servers into a settings file.
	StrategyMergeIntoSettings
	// StrategyMCPConfigFile writes to a dedicated MCP config file.
	StrategyMCPConfigFile
	// StrategyTOMLFile writes MCP config to a TOML file.
	StrategyTOMLFile
)

type OpenCodeCommunityPluginID string

const (
	OpenCodePluginSubAgentStatusline OpenCodeCommunityPluginID = "sub-agent-statusline"
	OpenCodePluginTaktLogo           OpenCodeCommunityPluginID = "takt-logo"
)

type CommunityToolID string

const (
	CommunityToolCodeGraph CommunityToolID = "codegraph"
)

// DeliveryKind classifies how an asset reaches a target agent.
type DeliveryKind string

const (
	DeliveryStatic  DeliveryKind = "static"
	DeliveryRuntime DeliveryKind = "runtime"
)

// InjectionResult is the common return value from component Inject functions.
// It reports whether any files changed and lists their paths.
type InjectionResult struct {
	Changed   bool
	Files     []string
	Preserved []string
}

// SetupChoice is the setup path for the new installation flow.
// Exactly Default and Custom.
type SetupChoice string

const (
	// SetupDefault is the zero value — a Selection with no explicit setup
	// choice behaves as Default.
	SetupDefault SetupChoice = ""
	SetupCustom  SetupChoice = "custom"
)
