// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package opencode

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// Context7RemoteURL is the canonical context7 remote MCP endpoint deployed
// into opencode.json by the context7 component.
const Context7RemoteURL = shared.Context7RemoteURL

// TaktTheme is the OpenCode theme deployed by the theme component.
const TaktTheme = "takt"

// context7Config returns the mcp entry for the context7 component. V2 nests
// every server under mcp.servers and spells the switch as "disabled". The map
// shape matches a deep merge of the canonical context7 overlay into
// opencode.json.
func context7Config() map[string]any {
	return map[string]any{
		"servers": map[string]any{
			string(model.ComponentContext7): map[string]any{
				"type":     "remote",
				"url":      Context7RemoteURL,
				"disabled": false,
			},
		},
	}
}

// permissionsConfig returns the canonical OpenCode permission rules: shell and
// read are permissive by default with ask rules for state-changing Git
// commands and deny rules for secret files and credentials. V2 evaluates the
// array in order and the last match wins, so every blanket allow is emitted
// before the narrower rules that override it.
func permissionsConfig() []permissionRule {
	rules := []permissionRule{
		{"shell", allResources, "allow"},
		// Redirection, tee, sed -i, cp and mv used to carry their own "ask"
		// rules here as a conservative fallback for mutations the VFS could
		// not yet capture. They were dropped: the VFS plugin's
		// permission.hook("evaluate") resolves every non-denied shell action
		// through takt/vfs/shell.go's classify() and overwrites event.effect
		// with that verdict regardless of what this static table said, so
		// these rules were already inert. classify() captures the mutation
		// (allow), denies it when its paths are not bound in scope, or asks
		// only when the target is genuinely outside the workspace (git
		// mutations, privilege escalation, absolute paths elsewhere) —
		// PR-HAR-15's "deny rather than execute unstaged" now runs per
		// command instead of by blanket static glob.
		{"shell", "git commit *", "ask"},
		{"shell", "git push", "ask"},
		{"shell", "git push *", "ask"},
		{"shell", "git push --force *", "ask"},
		{"shell", "git rebase *", "ask"},
		{"shell", "git reset --hard *", "ask"},
		// Memory is written only through the takt-memory plugin tools, which inject the harness identity.
		{"shell", "takt-ai memory*", "deny"},
		{"shell", "*takt-ai memory*", "deny"},
		// Crew dispatch and GC coordination are driven only by the takt-vfs
		// plugin's own hooks, which inject the harness-resolved identity and
		// session; no agent has a legitimate reason to shell either directly.
		{"shell", "takt-ai gc*", "deny"},
		{"shell", "*takt-ai gc*", "deny"},
		{"shell", "takt-ai dispatch*", "deny"},
		{"shell", "*takt-ai dispatch*", "deny"},
		{"read", allResources, "allow"},
		{"read", "*.env", "deny"},
		{"read", "*.env.*", "deny"},
	}
	for _, glob := range shared.SensitivePathGlobs {
		rules = append(rules, permissionRule{"read", "**/" + glob, "deny"})
	}
	// Native write tools are denied: every workspace mutation must go
	// through the takt-vfs plugin tools so staging and the verdict gate
	// cannot be bypassed. V2 folds write and patch into edit.
	return append(rules, permissionRule{"edit", allResources, "deny"})
}

//go:embed assets/takt-memory.ts
var taktMemoryPluginSource string

// TaktMemoryPluginArtifact returns the OpenCode plugin exposing the memory
// tools; taktAIBinary is the absolute takt-ai path the plugin spawns.
func TaktMemoryPluginArtifact(taktAIBinary string) Artifact {
	return taktPluginArtifact(taktMemoryPluginSource, "takt-memory.ts", taktAIBinary)
}

//go:embed assets/takt-vfs.ts
var taktVFSPluginSource string

// VFSShellEnforced controls whether the VFS plugin intercepts specialist shell
// commands (PR-HAR-15). File edits stay VFS-governed either way.
const VFSShellEnforced = true

// TaktVFSPluginArtifact returns the OpenCode plugin exposing the governed
// VFS tools; taktAIBinary is the absolute takt-ai path the plugin spawns and
// shellEnforced decides whether it intercepts specialist shell commands.
func TaktVFSPluginArtifact(taktAIBinary string, shellEnforced bool) Artifact {
	artifact := taktPluginArtifact(taktVFSPluginSource, model.VFSPluginFile, taktAIBinary)
	enforced, _ := json.Marshal(shellEnforced)              // a bool always marshals
	vfsAgents, _ := json.Marshal(vfsAgentIDs())             // a string slice always marshals
	resultAgents, _ := json.Marshal(resultAgentIDs())       // a string slice always marshals
	sensitive, _ := json.Marshal(shared.SensitivePathGlobs) // a string slice always marshals
	content := strings.Replace(string(artifact.Content), `"__TAKT_VFS_SHELL_ENFORCED__"`, string(enforced), 1)
	content = strings.Replace(content, `"__TAKT_SENSITIVE_READ_GLOBS__"`, string(sensitive), 1)
	content = strings.Replace(content, `"__TAKT_VFS_AGENTS__"`, string(vfsAgents), 1)
	artifact.Content = []byte(strings.Replace(content, `"__TAKT_RESULT_AGENTS__"`, string(resultAgents), 1))
	return artifact
}

// taktSkillIDs lists every skill the Takt catalog installs, sorted.
func taktSkillIDs() []string {
	ids := []string{}
	for _, skill := range loadCatalog().Skills {
		ids = append(ids, skill.ID)
	}
	slices.Sort(ids)
	return ids
}

// loadCatalog reads the embedded catalog, which its own tests validate.
func loadCatalog() catalog.Catalog {
	pack, err := catalog.LoadPackages()
	if err != nil {
		panic("load embedded Takt catalog: " + err.Error())
	}
	return pack
}

// vfsAgentIDs lists the catalog instances that stage their own work: those
// declaring the write capability, whose launch requires a claim. A verifier
// launched without a gate claim works in acceptance mode.
func vfsAgentIDs() []string {
	ids := []string{}
	for _, def := range loadCatalog().Agents {
		for _, id := range def.Instances {
			if grants, _ := def.VFSCapabilities(id); slices.Contains(grants, model.VFSCapabilityWrite) {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	return ids
}

// resultAgentIDs lists the catalog instances designed to use takt-result-handoff:
// the producers that deliver a complete Engram artifact as their final result.
func resultAgentIDs() []string {
	ids := []string{}
	for _, def := range loadCatalog().Agents {
		if slices.Contains(def.Skills, "takt-result-handoff") {
			ids = append(ids, def.Instances...)
		}
	}
	slices.Sort(ids)
	return ids
}

// OpenCodePluginPackageArtifact returns the package manifest for the shared
// OpenCode plugin directory. OpenCode resolves bare imports from this package
// root, so SDK dependencies must be declared here rather than left as
// transitive or manually-installed node_modules.
func OpenCodePluginPackageArtifact() Artifact {
	manifest := struct {
		Private      bool              `json:"private"`
		Type         string            `json:"type"`
		Dependencies map[string]string `json:"dependencies"`
	}{
		Private: true,
		Type:    "module",
		Dependencies: map[string]string{
			"@anthropic-ai/sandbox-runtime": SandboxRuntimeVersion,
			"@opencode/client":              pluginDevDependency("@opencode/client"),
			"@opencode/plugin":              pluginDevDependency("@opencode/plugin"),
		},
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		panic("marshal OpenCode plugin package manifest: " + err.Error())
	}
	return Artifact{
		Path:    ConfigDir() + "/plugins/package.json",
		Content: append(content, '\n'),
	}
}

//go:embed assets/package.json
var pluginPackageJSON []byte

// pluginDevDependency reads a pin from assets/package.json, the single source
// the plugin assets are also typechecked against.
func pluginDevDependency(name string) string {
	var pkg struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(pluginPackageJSON, &pkg); err != nil {
		panic("parse assets/package.json: " + err.Error())
	}
	version, ok := pkg.DevDependencies[name]
	if !ok {
		panic("assets/package.json has no devDependency " + name)
	}
	return version
}

// OpenCodePluginSDKVersion pins plugin SDK packages to the deployed OpenCode
// runtime. Keep client and plugin on the same release.
var OpenCodePluginSDKVersion = pluginDevDependency("@opencode/plugin")

func taktPluginArtifact(source, file, taktAIBinary string) Artifact {
	quoted, _ := json.Marshal(taktAIBinary) // a string always marshals
	orchestrator, _ := json.Marshal(shared.OrchestratorID)
	content := strings.Replace(source, `"__TAKT_AI_BINARY__"`, string(quoted), 1)
	content = strings.Replace(content, `"__TAKT_ORCHESTRATOR_ID__"`, string(orchestrator), 1)
	return Artifact{
		Path:    ConfigDir() + "/plugins/" + file,
		Content: []byte(content),
	}
}

//go:embed assets/takt-sandbox.mjs
var taktSandboxAdapterSource string

// TaktSandboxAdapterArtifact returns the sandbox adapter module the VFS
// plugin loads (as "./takt-sandbox.mjs", a sibling of the VFS plugin file) to
// wrap shell commands with @anthropic-ai/sandbox-runtime before they run
// (PR-HAR-15). Without this deployed, every shell command is denied.
func TaktSandboxAdapterArtifact() Artifact {
	return Artifact{
		Path:    ConfigDir() + "/plugins/takt-sandbox.mjs",
		Content: []byte(taktSandboxAdapterSource),
	}
}

// SandboxRuntimeVersion is the @anthropic-ai/sandbox-runtime version pinned
// in takt/runtime/sandbox/package.json — the same dependency the adapter's
// probe test runs against.
const SandboxRuntimeVersion = "0.0.76"

// sandboxPluginsDir is the absolute directory the sandbox adapter and its
// node_modules/ sibling deploy into, so a bare `from
// '@anthropic-ai/sandbox-runtime'` import resolves via Node's normal
// module lookup.
func sandboxPluginsDir(rootDir string) string {
	return filepath.Join(rootDir, filepath.FromSlash(ConfigDir()), "plugins")
}

// sandboxDependencyInstalled reports whether the pinned dependency is already
// present, so a repeat install/sync never re-runs npm.
func sandboxDependencyInstalled(rootDir string) bool {
	_, err := os.Stat(filepath.Join(sandboxPluginsDir(rootDir), "node_modules", "@anthropic-ai", "sandbox-runtime", "package.json"))
	return err == nil
}

// installSandboxDependencyCommand runs npm; tests replace it to avoid PATH and network.
var installSandboxDependencyCommand = exec.CommandContext

// InstallSandboxDependency npm-installs the sandbox adapter's one dependency
// next to its deployed location. The adapter remains an optional component:
// absence of npm degrades shell capture but does not block other setup work.
func InstallSandboxDependency(ctx context.Context, rootDir string) error {
	if sandboxDependencyInstalled(rootDir) {
		return nil
	}
	dir := sandboxPluginsDir(rootDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("prepare sandbox plugin dir: %w", err)
	}
	output, err := installSandboxDependencyCommand(ctx, "npm", "install", "--prefix", dir,
		"--no-fund", "--no-audit", "--no-package-lock", "--no-save", "--loglevel=error",
		"@anthropic-ai/sandbox-runtime@"+SandboxRuntimeVersion).CombinedOutput()
	if err != nil {
		return fmt.Errorf("install @anthropic-ai/sandbox-runtime %s with npm: %w: %s", SandboxRuntimeVersion, err, strings.TrimSpace(string(output)))
	}
	if !sandboxDependencyInstalled(rootDir) {
		return fmt.Errorf("@anthropic-ai/sandbox-runtime not usable after npm install at %s", dir)
	}
	return nil
}

//go:embed assets/takt-dag.tsx
var taktDagPluginSource string

// TaktDagPluginArtifact returns the OpenCode TUI plugin rendering Takt's
// read-only execution DAG projection (PRD_DAG_TUI.md). It is deployed at the
// documented discovery path <config>/plugins/<name>/tui.tsx, so no config
// file registers it; taktAIBinary is the takt-ai path it polls.
func TaktDagPluginArtifact(taktAIBinary string) Artifact {
	return taktPluginArtifact(taktDagPluginSource, "takt-dag/tui.tsx", taktAIBinary)
}

// cliConfig is the OpenCode terminal-client configuration projection. V2 owns
// the terminal in cli.json: the theme lives here rather than in opencode.json.
type cliConfig struct {
	Schema string    `json:"$schema"`
	Theme  *cliTheme `json:"theme,omitempty"`
}

// cliTheme is cli.json's theme selection; the name is a file in themes/.
type cliTheme struct {
	Name string `json:"name"`
}

// TaktCLIArtifact returns cli.json selecting the Takt theme when theme is
// non-empty.
func TaktCLIArtifact(theme string) Artifact {
	config := cliConfig{Schema: "https://opencode.ai/v2/cli.json"}
	if theme != "" {
		config.Theme = &cliTheme{Name: theme}
	}
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		// A fixed string slice cannot fail to marshal; guard so a future field
		// type change cannot deploy a truncated registration file.
		panic("marshal OpenCode cli.json: " + err.Error())
	}
	return Artifact{
		Path:    ConfigDir() + "/cli.json",
		Content: append(content, '\n'),
	}
}
