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

// Package model defines the core domain types shared across all Takt
// target adapters: agent identifiers, model assignments, sub-agent
// catalog definitions, and installation selection state.
package model

// AgentID identifies a target coding-agent adapter (Claude, Codex, OpenCode).
type AgentID string

const (
	AgentClaudeCode AgentID = "claude-code" // Anthropic Claude Code agent
	AgentOpenCode   AgentID = "opencode"    // OpenCode agent
	AgentCodex      AgentID = "codex"       // OpenAI Codex agent
)

// ComponentID identifies an installable Takt component injected into a target agent.
type ComponentID string

const (
	ComponentEngram           ComponentID = "engram"             // memory/context graph
	ComponentSkills           ComponentID = "skills"             // skill system
	ComponentContext7         ComponentID = "context7"           // context7 integration
	ComponentPermission       ComponentID = "permissions"        // permission layer
	ComponentTheme            ComponentID = "theme"              // theme
	ComponentClaudeTheme      ComponentID = "claude-theme"       // Claude-specific theme
	ComponentOpenCodeTaktLogo ComponentID = "opencode-takt-logo" // OpenCode Takt branding
)

// UninstallMode controls how much of a previous installation is removed.
type UninstallMode string

const (
	UninstallModePartial      UninstallMode = "partial"       // only selected components
	UninstallModeFull         UninstallMode = "full"          // all Takt-managed files
	UninstallModeFullRemove   UninstallMode = "full-remove"   // full + delete agent files
	UninstallModeCleanInstall UninstallMode = "clean-install" // uninstall then fresh install
)

// EngramUninstallScope limits engram removal to a specific scope.
type EngramUninstallScope string

const (
	EngramUninstallScopeGlobal  EngramUninstallScope = "global"  // all projects
	EngramUninstallScopeProject EngramUninstallScope = "project" // current project only
)

// SkillID identifies a Takt skill delivered through the skill system.
type SkillID string

// CanonicalSubAgent is the platform-level definition of a specialist sub-agent,
// carrying its persona and per-target model assignments.
type CanonicalSubAgent struct {
	Name        string
	Persona     string
	Assignments map[AgentID]ModelAssignment
}

// OfficialPersona is the only Takt personalization applied to specialists.
const OfficialPersona = "takt"

const (
	// Canonical sub-agent identifiers in catalog order.
	SubAgentTaktInit            = "takt-init"             // project bootstrapping
	SubAgentTaktDev             = "takt-dev"              // feature implementation
	SubAgentTaktVerify          = "takt-verify"           // code verification
	SubAgentTaktAnalyst         = "takt-analyst"          // requirements analysis
	SubAgentTaktPM              = "takt-pm"               // product management
	SubAgentTaktSpec            = "takt-spec"             // specification authoring
	SubAgentTaktArchitect       = "takt-architect"        // architecture design
	SubAgentTaktTPM             = "takt-tpm"              // technical project management
	SubAgentTaktProductDesigner = "takt-product-designer" // product design
	SubAgentTaktFix             = "takt-fix"              // bug fixing
	SubAgentTaktJudgeA          = "takt-judge-a"          // adversarial judge A
	SubAgentTaktJudgeB          = "takt-judge-b"          // adversarial judge B
	SubAgentDefault             = "default"               // fallback sub-agent

	// SkillID aliases for each canonical sub-agent.
	SkillTaktInit      SkillID = SubAgentTaktInit
	SkillTaktDev       SkillID = SubAgentTaktDev
	SkillTaktVerify    SkillID = SubAgentTaktVerify
	SkillTaktAnalyst   SkillID = SubAgentTaktAnalyst
	SkillTaktPm        SkillID = SubAgentTaktPM
	SkillTaktSpec      SkillID = SubAgentTaktSpec
	SkillTaktArchitect SkillID = SubAgentTaktArchitect
	SkillTaktTpm       SkillID = SubAgentTaktTPM
	SkillCreator       SkillID = "skill-creator"
	SkillJudgment      SkillID = "judgment"
	SkillSkillRegistry SkillID = "skill-registry"
)

// CanonicalSubAgentCatalog returns the canonical specialist definitions in a fixed order,
// including each specialist's persona and agent-specific model assignments.
func CanonicalSubAgentCatalog() []CanonicalSubAgent {
	claude := func(model string) ModelAssignment { return ModelAssignment{Model: model} }
	codex := func(model, effort string) ModelAssignment {
		return ModelAssignment{Model: model, Effort: effort}
	}
	return []CanonicalSubAgent{
		{Name: SubAgentTaktInit, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("haiku"), AgentCodex: codex(CodexModelLuna, "low")}},
		{Name: SubAgentTaktAnalyst, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("sonnet"), AgentCodex: codex(CodexModelLuna, "medium")}},
		{Name: SubAgentTaktPM, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("opus"), AgentCodex: codex(CodexModelSol, "high")}},
		{Name: SubAgentTaktSpec, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("sonnet"), AgentCodex: codex(CodexModelTerra, "medium")}},
		{Name: SubAgentTaktArchitect, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("opus"), AgentCodex: codex(CodexModelSol, "high")}},
		{Name: SubAgentTaktProductDesigner, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("opus"), AgentCodex: codex(CodexModelSol, "high")}},
		{Name: SubAgentTaktTPM, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("sonnet"), AgentCodex: codex(CodexModelTerra, "medium")}},
		{Name: SubAgentTaktDev, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("sonnet"), AgentCodex: codex(CodexModelLuna, "high")}},
		{Name: SubAgentTaktVerify, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: ModelAssignment{Model: "sonnet", Effort: "high"}, AgentCodex: codex(CodexModelLuna, "high")}},
		{Name: SubAgentTaktJudgeA, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("sonnet"), AgentCodex: codex(CodexModelLuna, "high")}},
		{Name: SubAgentTaktJudgeB, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("sonnet"), AgentCodex: codex(CodexModelLuna, "high")}},
		{Name: SubAgentTaktFix, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("sonnet"), AgentCodex: codex(CodexModelTerra, "medium")}},
		{Name: SubAgentDefault, Persona: OfficialPersona, Assignments: map[AgentID]ModelAssignment{AgentClaudeCode: claude("sonnet"), AgentCodex: codex(CodexModelTerra, "medium")}},
	}
}

// CanonicalSubAgents returns the names of shared specialists in stable catalog order.
func CanonicalSubAgents() []string {
	catalog := CanonicalSubAgentCatalog()
	names := make([]string, len(catalog))
	for i, subAgent := range catalog {
		names[i] = subAgent.Name
	}
	return names
}

// SystemPromptStrategy defines how an agent system prompt is managed.
type SystemPromptStrategy int

const (
	// StrategyMarkdownSections inserts sections into an existing Markdown file.
	StrategyMarkdownSections SystemPromptStrategy = iota
	// StrategyFileReplace overwrites the system prompt file entirely.
	StrategyFileReplace
	// StrategyAppendToFile appends content to the existing system prompt file.
	StrategyAppendToFile
	// StrategySteeringFile writes a standalone steering file.
	StrategySteeringFile
)

// MCPStrategy defines how MCP server configs are written.
type MCPStrategy int

const (
	// StrategySeparateMCPFiles writes one JSON file per MCP server.
	StrategySeparateMCPFiles MCPStrategy = iota
	// StrategyMergeIntoSettings merges all servers into a settings file.
	StrategyMergeIntoSettings
	// StrategyMCPConfigFile writes all servers to a single MCP config file.
	StrategyMCPConfigFile
	// StrategyTOMLFile writes MCP config as TOML.
	StrategyTOMLFile
)

// OpenCodeCommunityPluginID identifies an optional OpenCode TUI plugin.
type OpenCodeCommunityPluginID string

const (
	OpenCodePluginSubAgentStatusline OpenCodeCommunityPluginID = "sub-agent-statusline" // sub-agent status bar
	OpenCodePluginTaktLogo           OpenCodeCommunityPluginID = "takt-logo"            // Takt branding logo
)

// CommunityToolID identifies an optional cross-agent community tool or plugin.
type CommunityToolID string

const (
	CommunityToolCodeGraph CommunityToolID = "codegraph" // code graph analysis tool
)

// DeliveryKind classifies whether an asset is baked in at build time or resolved at runtime.
type DeliveryKind string

const (
	DeliveryStatic  DeliveryKind = "static"
	DeliveryRuntime DeliveryKind = "runtime"
)

// InjectionResult is the common return value from component Inject functions.
type InjectionResult struct {
	Changed   bool
	Files     []string
	Preserved []string
}

// SetupChoice selects the installation path: zero-value defaults to standard setup.
type SetupChoice string

const (
	SetupDefault SetupChoice = ""       // standard installation flow
	SetupCustom  SetupChoice = "custom" // user-configured installation
)
