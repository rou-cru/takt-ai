package runtime

import (
	"path"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

// InstallSummary is what an install sets up, in the user's terms rather than
// file paths, so review can state agents, skills and integrations directly.
type InstallSummary struct {
	// Agents is every deployed agent instance with its effective model.
	Agents []AgentModel
	// Skills counts the distinct Takt skills, not their files.
	Skills int
	// MCPServers names the MCP servers the install configures.
	MCPServers []string
	// Integrations names the OpenCode plugins the install deploys.
	Integrations []string
	// Configs lists the user's configuration files the install changes.
	Configs []ConfigChange
}

// AgentModel is one agent instance and the model it runs on; an empty Model
// means it inherits OpenCode's default.
type AgentModel struct {
	Name  string
	Role  model.RoleClass
	Model string
}

// ConfigChange is a configuration file the install changes. Merged means the
// file is the user's own and Takt merges into it, keeping their keys.
type ConfigChange struct {
	Path   string
	Merged bool
}

// injectedServers are configured after the files are written (lifecycle's
// InjectEngram/InjectCodegraph), so no planned artifact names them.
var injectedServers = []string{string(model.ComponentCodegraph), string(model.ComponentEngram)}

// integrationLabels names the deployed plugins by their artifact base name.
var integrationLabels = map[string]string{
	"tui.tsx":          "DAG panel",
	"takt-memory.ts":   "memory",
	"takt-vfs.ts":      "VFS",
	"takt-sandbox.mjs": "sandbox",
}

// Summarize states what an install plan sets up in the user's terms, for
// any surface that reviews a plan (the TUI review, the CLI's --plan-only).
func Summarize(rootDir string, request setup.PlanRequest, preview lifecycle.InstallPreview) (InstallSummary, error) {
	return summarize(rootDir, request, InstallPlan{InstallPreview: preview})
}

// summarize derives the review summary from the plan request and the planned
// files, and the ownership manifest to tell merged from replaced configs.
func summarize(rootDir string, request setup.PlanRequest, plan InstallPlan) (InstallSummary, error) {
	summary := InstallSummary{MCPServers: slices.Clone(injectedServers)}
	packages, err := catalog.LoadPackages()
	if err != nil {
		return InstallSummary{}, err
	}
	for _, def := range packages.Agents {
		for _, instance := range def.Instances {
			assignment, ok := request.OpenCodeModelOverrides[instance]
			if !ok {
				assignment = model.ModelAssignment{Model: request.OpenCode.Model}
			}
			summary.Agents = append(summary.Agents, AgentModel{Name: instance, Role: def.Role, Model: assignment.Model})
		}
	}
	summary.Skills = len(packages.Skills)
	if slices.Contains(request.Components, string(model.ComponentContext7)) {
		summary.MCPServers = append(summary.MCPServers, string(model.ComponentContext7))
	}
	slices.Sort(summary.MCPServers)
	for _, target := range plan.Plans {
		for _, artifact := range target.Artifacts {
			if label, ok := integrationLabels[path.Base(artifact.Path)]; ok && strings.Contains(artifact.Path, "/plugins/") {
				summary.Integrations = append(summary.Integrations, label)
			}
		}
	}
	manifest, _ := setup.LoadOwnershipManifest(rootDir)
	for _, changed := range plan.Modify {
		if !strings.HasSuffix(changed, ".json") {
			continue
		}
		merged := true
		if manifest != nil {
			_, managed := manifest.Entries[changed]
			merged = !managed
		}
		summary.Configs = append(summary.Configs, ConfigChange{Path: changed, Merged: merged})
	}
	return summary, nil
}
