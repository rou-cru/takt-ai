// Package opencode renders native OpenCode configuration from catalog
// references.
package opencode

import (
	"path"
	"strings"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/model"
)

const (
	configDir           = model.OpenCodeConfigDir
	packageName         = "takt"
	contextPathCapacity = 5 // Context files plus the memory clause in a composed prompt.
)

// ConfigDir is OpenCode's config root, relative to the install root.
func ConfigDir() string { return configDir }

// ConfigPath returns the OpenCode config file path relative to the install root.
func ConfigPath() string { return model.OpenCodeConfigRelativePath }

// DeployPath returns the deployed location of a package file at rel.
func DeployPath(rel string) string { return path.Join(configDir, packageName, rel) }

// FileRef returns the {file:...} reference OpenCode resolves relative to the config root.
func FileRef(rel string) string { return "{file:./" + packageName + "/" + rel + "}" }

// AgentMode is primary for the orchestrator, all for interlocutors, subagent otherwise.
func AgentMode(role model.RoleClass) string {
	switch {
	case role == model.RoleOrchestrator:
		return "primary"
	case role.HoldsInterface():
		return "all"
	default:
		return "subagent"
	}
}

// ComposePrompt chains BASELINE, PERSONA, SOUL and OPERATIONS by file
// reference, then the memory paths.
func ComposePrompt(def catalog.AgentDefinition, instanceID string) string {
	parts := make([]string, 0, contextPathCapacity)
	for _, p := range def.ContextPaths() {
		parts = append(parts, FileRef(p))
	}
	parts = append(parts, MemoryClause(instanceID))
	return strings.Join(parts, "\n\n")
}

// MemoryClause names installed skill paths without injecting their content.
func MemoryClause(authorID string) string {
	return "## Memory\n\n" +
		"Before using memory, read `~/" + engram.ContractSkillPath +
		"` and `~/" + engram.MemorySkillPath(authorID) +
		"`; they are the only memory rules that apply to you.\n"
}
