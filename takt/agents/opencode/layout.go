// Package opencode renders native OpenCode configuration from catalog
// references.
package opencode

import (
	"path"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/prompts"
)

const (
	configDir           = model.OpenCodeConfigDir
	packageName         = "takt"
	archifyBridgeSkill  = "takt-archify"
	contextPathCapacity = 6 // Context files plus the memory clause and optional Archify clause.
)

// ConfigDir is OpenCode's config root, relative to the install root.
func ConfigDir() string { return configDir }

// ConfigPath returns the OpenCode config file path relative to the install root.
func ConfigPath() string { return model.OpenCodeConfigRelativePath }

// CLIConfigPath returns the terminal client configuration's home-relative path.
func CLIConfigPath() string { return model.OpenCodeCLIConfigRelativePath }

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
// reference, then the memory clause, and Archify guidance when Archify is selected
// and the agent declares the Takt Archify skill.
func ComposePrompt(def catalog.AgentDefinition, instanceID string, components ...model.ComponentID) string {
	parts := make([]string, 0, contextPathCapacity)
	for _, p := range def.ContextPaths() {
		parts = append(parts, FileRef(p))
	}
	parts = append(parts, MemoryClause(instanceID))
	if slices.Contains(components, model.ComponentArchify) && slices.Contains(def.Skills, archifyBridgeSkill) {
		parts = append(parts, ArchifyClause(def.Role))
	}
	return strings.Join(parts, "\n\n")
}

// MemoryClause states how consumed entries are read (CTX-11) and names the
// installed memory skills without injecting their content.
func MemoryClause(authorID string) string {
	return prompts.Default().MustRender("context.memory_clause", map[string]string{
		"contract_skill": shared.ContractSkillPath,
		"memory_skill":   shared.MemorySkillPath(authorID),
	})
}

// ArchifyClause introduces the installed Archify CLI and bridge skill with
// role-appropriate scope, and names the one output location the role's shell
// can write without approval: a sandboxed role's private $TMPDIR scratch, or a
// native shell's private directory created with archifyScratchCreate.
func ArchifyClause(role model.RoleClass) string {
	scope := "code-analysis check/task/drift and, when visuals or an on-demand explainer help, the upstream archify skill ([archify explainer: on-demand])"
	scratch := "a directory you create first with `" + archifyScratchCreate + "` (it prints the path and is readable only by you), then pass that printed path literally to every `--out` of the analysis; never invent another path"
	if role == model.RoleVerification || role == model.RoleQualityAssurance {
		scope = "read-only code-analysis check/task/drift only; never write workspace files"
		scratch = "`$TMPDIR/takt-archify`, inside your sandbox's private scratch. It lasts a single shell call, so run `check` and the `task` lookups that read its output in one command (`check ... && task ...`)"
	}
	return prompts.Default().MustRender("context.archify_clause", map[string]string{
		"archify_bin":   model.ArchifyBinaryRelPath,
		"archify_skill": path.Join(model.OpenCodeSkillsDir, archifyBridgeSkill, catalog.SkillFileName),
		"scope":         scope,
		"scratch":       scratch,
	})
}
