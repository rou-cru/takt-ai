package opencode_test

import (
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
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

// pluginTools lists every tool the Takt plugins register.
func pluginTools(t *testing.T) []string {
	t.Helper()
	sources := string(opencode.TaktVFSPluginArtifact("/test/takt-ai", true).Content) +
		string(opencode.TaktMemoryPluginArtifact("/test/takt-ai").Content)
	var tools []string
	for _, m := range toolRegistration.FindAllStringSubmatch(sources, -1) {
		if !slices.Contains(tools, m[1]) {
			tools = append(tools, m[1])
		}
	}
	for _, want := range []string{"gc_request", "dispatch_commit", "vfs_bind", "deliver_result", "memory_record", "gc_verdict"} {
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

func TestOnlyTheOrchestratorRequestsMaintenance(t *testing.T) {
	r := renderCatalog(t)
	for _, spec := range r.specs {
		got := effectOf(r.config.Agents[spec.ID].Permissions, "gc_request", "*")
		if spec.Role == model.RoleOrchestrator && got != "allow" {
			t.Errorf("orchestrator gc_request = %q, want allow", got)
		}
		if spec.Role != model.RoleOrchestrator && got != "deny" {
			t.Errorf("%s gc_request = %q, want deny", spec.ID, got)
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
			if effectOf(rules, "memory_record", "*") != "allow" {
				t.Fatal("record must remain available")
			}
			want := "deny"
			if spec.Role == model.RoleOrchestrator || spec.Role.HoldsInterface() {
				want = "allow"
			}
			for _, name := range []string{"memory_continue_session", "memory_close_session"} {
				if got := effectOf(rules, name, "*"); got != want {
					t.Errorf("%s = %s, want %s", name, got, want)
				}
			}
		})
	}
}
