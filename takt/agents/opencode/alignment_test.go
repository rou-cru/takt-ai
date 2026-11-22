package opencode_test

import (
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/prompts"
)

// These tests keep the three layers in step: every tool the plugins register
// has an explicit rule for every agent, every tool the prose tells an agent to
// use is one that agent may call, and no edit allow reopens a secret.

type renderedCatalog struct {
	pack    catalog.Catalog
	specs   []opencode.AgentSpec
	config  v2Config
	loaders map[string][]string // asset path -> instance ids whose context carries it
}

func renderCatalog(t *testing.T) renderedCatalog {
	t.Helper()
	pack, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	r := renderedCatalog{pack: pack, loaders: map[string][]string{}}
	for _, def := range pack.Agents {
		for _, id := range def.Instances {
			grants, _ := def.VFSCapabilities(id)
			r.specs = append(r.specs, opencode.AgentSpec{ID: id, Description: def.Description, Mode: opencode.AgentMode(def.Role), System: opencode.ComposePrompt(def, id), Role: def.Role, VFSCapabilities: grants, Skills: def.Skills})
			for _, p := range def.ContextPaths() {
				r.loaders[p] = append(r.loaders[p], id)
			}
			for _, skill := range def.Skills {
				p := path.Join("skills", skill, catalog.SkillFileName)
				r.loaders[p] = append(r.loaders[p], id)
			}
		}
	}
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{Permissions: true, Agents: r.specs})
	if err != nil {
		t.Fatal(err)
	}
	r.config = decodeConfig(t, artifact.Content)
	return r
}

var toolRegistration = regexp.MustCompile(`(?:name: |\bgc\(|\bdispatch\()"([a-z]+_[a-z_]+)"`)

func memoryToolName(tool string) string { return memory.PrefixedToolName(tool) }

// memoryToolNames lists the tools OpenCode exposes from the memory MCP server.
func memoryToolNames() []string {
	var names []string
	for _, tool := range memory.Tools {
		names = append(names, memoryToolName(tool.Name))
	}
	return names
}

// pluginTools lists every tool the Takt plugins register.
func pluginTools(t *testing.T) []string {
	t.Helper()
	source := string(opencode.TaktVFSPluginArtifact("/test/takt-ai", true).Content)
	// The memory tools are served by the memory MCP server, not registered by a plugin.
	tools := memoryToolNames()
	for _, m := range toolRegistration.FindAllStringSubmatch(source, -1) {
		if !slices.Contains(tools, m[1]) {
			tools = append(tools, m[1])
		}
	}
	for _, want := range []string{"dispatch_commit", "vfs_bind", "deliver_result", memoryToolName(memory.ToolRecord)} {
		if !slices.Contains(tools, want) {
			t.Fatalf("tool registration pattern no longer matches the plugins: %s missing from %v", want, tools)
		}
	}
	return tools
}

func TestEveryPluginToolHasExplicitRuleForEveryAgent(t *testing.T) {
	r := renderCatalog(t)
	for _, tool := range pluginTools(t) {
		for _, spec := range r.specs {
			if effectOf(r.config.Agents[spec.ID].Permissions, tool, "*") == "" {
				t.Errorf("%s has no explicit rule for plugin tool %s", spec.ID, tool)
			}
		}
	}
}

func TestNoEditAllowReopensASecret(t *testing.T) {
	r := renderCatalog(t)
	for _, spec := range r.specs {
		rules := r.config.Agents[spec.ID].Permissions
		if effectOf(rules, "edit", "*") != "allow" {
			continue
		}
		for _, glob := range model.SensitivePathGlobs {
			if got := effectOf(rules, "edit", "**/"+glob); got != "deny" {
				t.Errorf("%s may edit %s: effect %q", spec.ID, glob, got)
			}
		}
	}
}

var toolMention = regexp.MustCompile(`\b([a-z]+_[a-z_]+)\b`)

// retiredTools are the GC tools the plugin no longer carries; prose naming one
// sends its readers to a tool that does not exist.
var retiredTools = []string{"gc_acceptance", "gc_authorize", "gc_baseline", "gc_collected", "gc_delta", "gc_findings",
	"gc_investigate", "gc_no_change", "gc_prepare", "gc_request", "gc_verdict"}

// TestProseNamesOnlyToolsItsReadersMayCall fails when a text tells its readers
// to use a plugin tool that any agent carrying that text is denied.
func TestProseNamesOnlyToolsItsReadersMayCall(t *testing.T) {
	r := renderCatalog(t)
	tools := pluginTools(t)
	checked := 0
	for p, readers := range r.loaders {
		text, err := fs.ReadFile(catalog.AssetFS(), p)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range toolMention.FindAllStringSubmatch(string(text), -1) {
			tool := m[1]
			if slices.Contains(retiredTools, tool) {
				t.Errorf("%s names retired tool %s", p, tool)
			}
			if !slices.Contains(tools, tool) {
				continue
			}
			checked++
			for _, id := range readers {
				if effectOf(r.config.Agents[id].Permissions, tool, "*") != "allow" {
					t.Errorf("%s tells %s to use denied tool %s", p, id, tool)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no prose tool reference was checked; the pattern no longer matches the catalog")
	}
	if _, ok := r.loaders[catalog.BaselinePath]; !ok {
		t.Fatalf("BASELINE is not in any agent's context: %v", slices.Sorted(maps.Keys(r.loaders)))
	}
}

func TestMemoryPermissionsMatchEveryInstanceRole(t *testing.T) {
	r := renderCatalog(t)
	for _, spec := range r.specs {
		t.Run(spec.ID, func(t *testing.T) {
			rules := r.config.Agents[spec.ID].Permissions
			for _, name := range []string{memory.ToolRecord, memory.ToolSearch, memory.ToolRead, memory.ToolSessions} {
				if got := effectOf(rules, memoryToolName(name), "*"); got != "allow" {
					t.Errorf("%s = %s, want allow", name, got)
				}
			}
			want := "deny"
			if spec.Role == model.RoleOrchestrator || spec.Role.HoldsInterface() {
				want = "allow"
			}
			for _, name := range []string{memory.ToolContinueSession, memory.ToolCloseSession} {
				if got := effectOf(rules, memoryToolName(name), "*"); got != want {
					t.Errorf("%s = %s, want %s", name, got, want)
				}
			}
		})
	}
}

func resolveAudience(t *testing.T, r renderedCatalog, selectors []string) []string {
	t.Helper()
	var ids []string
	add := func(id string) {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, sel := range selectors {
		switch sel {
		case "all":
			for _, spec := range r.specs {
				add(spec.ID)
			}
		case "orchestrator":
			for _, spec := range r.specs {
				if spec.Role == model.RoleOrchestrator {
					add(spec.ID)
				}
			}
		case "interlocutors":
			for _, spec := range r.specs {
				if spec.Role.HoldsInterface() {
					add(spec.ID)
				}
			}
		case "delegated":
			for _, spec := range r.specs {
				if spec.Role != model.RoleOrchestrator {
					add(spec.ID)
				}
			}
		case "vfs_authors":
			for _, id := range r.pack.VFSAuthors() {
				add(id)
			}
		case "verifiers":
			for _, spec := range r.specs {
				if slices.Contains(spec.VFSCapabilities, model.VFSCapabilityVerify) {
					add(spec.ID)
				}
			}
		case "result_agents":
			for _, spec := range r.specs {
				if slices.Contains(spec.Skills, "takt-result-handoff") {
					add(spec.ID)
				}
			}
		default:
			if _, ok := r.config.Agents[sel]; !ok {
				t.Fatalf("unknown prompt audience selector %q", sel)
			}
			add(sel)
		}
	}
	return ids
}

func TestHarnessPromptsCorrelateWithExistingCatalogProse(t *testing.T) {
	reg := prompts.Default()
	catFS := catalog.AssetFS()
	for _, entry := range reg.Entries {
		for _, p := range entry.CorrelatesWith {
			if _, err := fs.Stat(catFS, p); err != nil {
				t.Errorf("prompt %s correlates_with missing catalog file %q: %v", entry.ID, p, err)
			}
		}
	}
	for _, tool := range reg.Tools {
		for _, p := range tool.CorrelatesWith {
			if _, err := fs.Stat(catFS, p); err != nil {
				t.Errorf("tool %s correlates_with missing catalog file %q: %v", tool.Name, p, err)
			}
		}
	}
}

func TestHarnessPromptsNameOnlyPermittedToolsForAudience(t *testing.T) {
	r := renderCatalog(t)
	tools := pluginTools(t)
	reg := prompts.Default()
	checkText := func(label string, audience []string, text string, ignoreTool string) {
		readers := resolveAudience(t, r, audience)
		for _, m := range toolMention.FindAllStringSubmatch(text, -1) {
			tool := m[1]
			if slices.Contains(retiredTools, tool) {
				t.Errorf("%s names retired tool %s", label, tool)
			}
			if tool == ignoreTool || !slices.Contains(tools, tool) {
				continue
			}
			for _, id := range readers {
				if effectOf(r.config.Agents[id].Permissions, tool, "*") != "allow" {
					t.Errorf("%s tells %s to use denied tool %s", label, id, tool)
				}
			}
		}
	}
	for _, entry := range reg.Entries {
		ignore := ""
		if entry.ID == "refusal.vfs.vfs_bind_verifier" {
			// Names vfs_bind specifically to explain that verifiers must not call it.
			ignore = "vfs_bind"
		}
		checkText("prompt "+entry.ID, entry.Audience, entry.Text, ignore)
	}
	for _, tool := range reg.Tools {
		if tool.Description != "" {
			checkText("tool "+tool.Name+".description", tool.Audience, tool.Description, "")
		}
		for param, desc := range tool.Params {
			checkText("tool "+tool.Name+".param."+param, tool.Audience, desc, "")
		}
	}
}

func TestEveryHarnessPromptHasAConsumer(t *testing.T) {
	reg := prompts.Default()
	ownerFiles := map[string]string{
		"opencode/layout":   "layout.go",
		"cli/dispatch":      filepath.Join("..", "..", "cli", "dispatch.go"),
		"memory/mcp":        filepath.Join("..", "..", "memory", "tools.go"),
		"plugin/takt-vfs":   filepath.Join("assets", "takt-vfs.ts"),
		"plugin/takt-memory": filepath.Join("assets", "takt-memory.ts"),
		"plugin/takt-dag":   filepath.Join("assets", "takt-dag.tsx"),
	}
	sources := make(map[string]string, len(ownerFiles))
	for owner, rel := range ownerFiles {
		raw, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("read owner %s (%s): %v", owner, rel, err)
		}
		sources[owner] = string(raw)
	}
	for _, entry := range reg.Entries {
		src, ok := sources[entry.Owner]
		if !ok {
			t.Errorf("prompt %s has unmapped owner %q", entry.ID, entry.Owner)
			continue
		}
		if !strings.Contains(src, `"`+entry.ID+`"`) {
			t.Errorf("orphan prompt %s is not referenced in %s", entry.ID, ownerFiles[entry.Owner])
		}
	}
	for _, tool := range reg.Tools {
		src, ok := sources[tool.Owner]
		if !ok {
			t.Errorf("tool %s has unmapped owner %q", tool.Name, tool.Owner)
			continue
		}
		if tool.Owner == "memory/mcp" {
			// memory/tools.go looks up each tool by its Tool* constant; verify constant matches name.
			if !strings.Contains(src, `"`+tool.Name+`"`) {
				t.Errorf("orphan memory tool spec %s is not referenced in %s", tool.Name, ownerFiles[tool.Owner])
			}
			continue
		}
		if tool.Description != "" && !strings.Contains(src, `toolDesc("`+tool.Name+`"`) {
			t.Errorf("orphan tool description %s is not referenced in %s", tool.Name, ownerFiles[tool.Owner])
		}
		for param := range tool.Params {
			if !strings.Contains(src, `paramDesc("`+tool.Name+`", "`+param+`"`) {
				t.Errorf("orphan tool param %s.%s is not referenced in %s", tool.Name, param, ownerFiles[tool.Owner])
			}
		}
	}
}
