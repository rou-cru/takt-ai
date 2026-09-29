package opencode_test

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

func vfsCaps(capabilities ...model.VFSCapability) []model.VFSCapability {
	return append([]model.VFSCapability{}, capabilities...)
}

func TestAnalystDocumentEditsKeepSensitiveDenies(t *testing.T) {
	r := renderCatalog(t)
	rules := r.config.Agents["analyst"].Permissions
	allowIndex := -1
	for i, rule := range rules {
		if rule.Action == "edit" && rule.Resource == "*" {
			if rule.Effect != "allow" {
				t.Fatalf("analyst document edits overridden by %+v", rule)
			}
			allowIndex = i
		}
	}
	if allowIndex < 0 {
		t.Fatal("analyst lacks native document edit permission")
	}
	for _, glob := range shared.SensitivePathGlobs {
		resource := "**/" + glob
		if got := effectOf(rules[allowIndex+1:], "edit", resource); got != "deny" {
			t.Errorf("sensitive path %s must be denied after the edit allow, got %q", resource, got)
		}
	}
	for _, tool := range []string{"vfs_bind", "vfs_write", "vfs_delete", "subagent"} {
		if got := effectOf(rules, tool, "*"); got != "deny" {
			t.Errorf("document author gained %s permission: %q", tool, got)
		}
	}
	for _, id := range []string{"analyst", "pm", "architect", "product-designer", "spec", "tpm"} {
		// Designed skills inherit native access; only excluded skills get a rule.
		if got := effectOf(r.config.Agents[id].Permissions, "skill", "takt-invariant-authoring"); got == "deny" {
			t.Errorf("%s cannot access its invariant authoring skill", id)
		}
	}
}

// TestCatalogProjectionEnforcesResponsibilityBoundaries checks that catalog agents
// receive delegation, handoff, and skill access appropriate to their responsibilities.
func TestCatalogProjectionEnforcesResponsibilityBoundaries(t *testing.T) {
	pack, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	var specs []opencode.AgentSpec
	for _, def := range pack.Agents {
		for _, id := range def.Instances {
			profile := def.Profile(id)
			grants, _ := def.VFSCapabilities(id)
			specs = append(specs, opencode.AgentSpec{ID: id, Description: profile.Description, Mode: opencode.AgentMode(profile.Role), System: opencode.ComposePrompt(def, id), Role: profile.Role, VFSCapabilities: grants, Skills: def.Skills})
		}
	}
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{Permissions: true, Agents: specs})
	if err != nil {
		t.Fatal(err)
	}
	config := decodeConfig(t, artifact.Content)
	for _, spec := range specs {
		t.Run(spec.ID, func(t *testing.T) {
			rules := config.Agents[spec.ID].Permissions
			if spec.Role != model.RoleOrchestrator && effectOf(rules, "subagent", "*") != "deny" {
				t.Error("specialist may delegate")
			}
			for _, tool := range []string{"dispatch_switch", "dispatch_abort_switch"} {
				want := "deny"
				if spec.Role == model.RoleOrchestrator {
					want = "allow"
				}
				if got := effectOf(rules, tool, "*"); got != want {
					t.Errorf("%s = %q, want %q", tool, got, want)
				}
			}
			want := "deny"
			if spec.Role == model.RoleDirectInterlocutor {
				want = "allow"
			}
			if got := effectOf(rules, "dispatch_handoff", "*"); got != want {
				t.Errorf("handoff = %q, want %q", got, want)
			}
			for _, skill := range []string{"takt-handoff", "takt-memory-contract"} {
				if !slices.Contains(spec.Skills, skill) || effectOf(rules, "skill", skill) == "deny" {
					t.Errorf("missing accessible %s", skill)
				}
			}
			producer := slices.Contains([]string{"analyst", "pm", "architect", "product-designer", "spec", "tpm"}, spec.ID)
			if slices.Contains(spec.Skills, "takt-result-handoff") != producer {
				t.Error("incorrect result handoff assignment")
			}
			interlocutor := spec.Role == model.RoleOrchestrator || spec.Role == model.RoleDirectInterlocutor
			if slices.Contains(spec.Skills, "takt-interlocutor-handoff") != interlocutor {
				t.Error("incorrect interlocutor handoff assignment")
			}
		})
	}
}

func TestRenderConfig(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Assignment: model.ModelAssignment{Model: "openai/gpt-5.6-luna"},
		Agents: []opencode.AgentSpec{
			{ID: "dev", Description: "Development specialist", Mode: "subagent", System: "Implement the requested change.", Model: "openai/gpt-5.6-luna", Role: model.RoleExecution, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
			{ID: "pm", Description: "Product specialist", Mode: "all", System: "Shape a proposal.", Model: "openai/gpt-5.6-luna", Role: model.RoleDirectInterlocutor, VFSCapabilities: vfsCaps()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(artifact.Content, &config); err != nil {
		t.Fatal(err)
	}
	if artifact.Path != ".config/opencode/opencode.json" || config["model"] != "openai/gpt-5.6-luna" || config["$schema"] != "https://opencode.ai/config.json" {
		t.Fatalf("config = %#v", config)
	}
	agents, ok := config["agents"].(map[string]any)
	if !ok {
		t.Fatalf("agents config = %#v, want map", config["agents"])
	}
	dev, ok := agents["dev"].(map[string]any)
	if !ok {
		t.Fatalf("dev agent = %#v, want map", agents["dev"])
	}
	if dev["description"] != "Development specialist" || dev["mode"] != "subagent" || dev["system"] != "Implement the requested change." || dev["model"] != "openai/gpt-5.6-luna" {
		t.Errorf("dev agent = %#v", dev)
	}
	if _, hasPrompt := dev["prompt"]; hasPrompt {
		t.Errorf("dev agent uses legacy prompt field: %#v", dev)
	}
	if _, ok := agents["pm"]; !ok {
		t.Errorf("agent config missing pm, got %#v", agents)
	}
}

func TestRenderConfigValidation(t *testing.T) {
	_, err := opencode.RenderConfig(opencode.ConfigRequest{})
	if err != nil {
		t.Fatalf("opencode.RenderConfig() error = %v, want model inheritance", err)
	}
}

func TestRenderConfigAppliesGitShellPermissionForNonOrchestratorRole(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Agents: []opencode.AgentSpec{
			{ID: "dev", Description: "Development specialist", Mode: "subagent", System: "Implement the requested change.", Model: "openai/gpt-5.6-luna", Role: model.RoleExecution, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rules := decodeConfig(t, artifact.Content).Agents["dev"].Permissions
	if effectOf(rules, "shell", "git *") != "deny" || effectOf(rules, "shell", "git diff") != "allow" || effectOf(rules, "shell", "git diff *") != "allow" {
		t.Errorf("dev shell rules = %v", rules)
	}
	// The broad deny must come first so the read-only allows win as last match.
	if slices.IndexFunc(rules, func(r rule) bool { return r.Resource == "git *" }) > slices.IndexFunc(rules, func(r rule) bool { return r.Resource == "git diff" }) {
		t.Errorf("git * deny must precede the read-only allows: %v", rules)
	}
}

func TestRenderConfigOrchestratorRoleHasEditAndSubagentPermission(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Agents: []opencode.AgentSpec{
			{ID: "takt", Description: "Orchestrator", Mode: "primary", System: "Coordinate.", Model: "openai/gpt-5.6-luna", Role: model.RoleOrchestrator, VFSCapabilities: vfsCaps(model.VFSCapabilityClaimList, model.VFSCapabilityClaimAssign, model.VFSCapabilityClaimRelease, model.VFSCapabilityConsolidate)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rules := decodeConfig(t, artifact.Content).Agents["takt"].Permissions
	if effectOf(rules, "edit", "*") != "allow" || effectOf(rules, "subagent", "*") != "allow" {
		t.Errorf("orchestrator role must get edit/subagent permission, got %v", rules)
	}
}

func TestRenderConfigJoinsModelAndVariant(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Agents: []opencode.AgentSpec{
			{ID: "dev", Description: "Dev", Mode: "subagent", System: "p", Model: "openai/gpt-5.6-luna", Variant: "high", Role: model.RoleExecution, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
			{ID: "verify", Description: "Verify", Mode: "subagent", System: "p", Model: "openai/gpt-5.6-luna", Role: model.RoleVerification, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityRead, model.VFSCapabilityVerify)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(artifact.Content, &config); err != nil {
		t.Fatal(err)
	}
	agents := config["agents"].(map[string]any)
	if got := agents["dev"].(map[string]any)["model"]; got != "openai/gpt-5.6-luna#high" {
		t.Errorf("model = %v, want the variant joined with #", got)
	}
	if got := agents["verify"].(map[string]any)["model"]; got != "openai/gpt-5.6-luna" {
		t.Errorf("model = %v, want no variant suffix", got)
	}
}

func TestRenderConfigValidatesIncompleteAgent(t *testing.T) {
	tests := []struct {
		name string
		spec opencode.AgentSpec
	}{
		{name: "missing description", spec: opencode.AgentSpec{ID: "dev", Mode: "subagent", System: "work"}},
		{name: "missing mode", spec: opencode.AgentSpec{ID: "dev", Description: "Agent", System: "work"}},
		{name: "missing system", spec: opencode.AgentSpec{ID: "dev", Description: "Agent", Mode: "subagent"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := opencode.RenderConfig(opencode.ConfigRequest{Agents: []opencode.AgentSpec{tc.spec}})
			if err == nil || !strings.Contains(err.Error(), "incomplete") {
				t.Fatalf("opencode.RenderConfig() error = %v, want incomplete agent error", err)
			}
		})
	}
}

func TestRenderConfigRequiresExplicitVFSGrant(t *testing.T) {
	spec := opencode.AgentSpec{ID: "agent", Description: "Agent", Mode: "subagent", System: "work", Role: model.RoleExecution}
	if _, err := opencode.RenderConfig(opencode.ConfigRequest{Agents: []opencode.AgentSpec{spec}}); err == nil || !strings.Contains(err.Error(), "VFS capabilities") {
		t.Fatalf("RenderConfig() error = %v, want missing explicit VFS capability", err)
	}
}

func TestRenderConfigVFSGrantDoesNotFollowRole(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{Agents: []opencode.AgentSpec{
		{ID: "execution-none", Description: "No access", Mode: "subagent", System: "work", Role: model.RoleExecution, VFSCapabilities: vfsCaps()},
		{ID: "planning-write", Description: "Explicit access", Mode: "subagent", System: "plan", Role: model.RolePlanningAuthor, VFSCapabilities: vfsCaps(model.VFSCapabilityWrite)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	config := decodeConfig(t, artifact.Content)
	if got := effectOf(config.Agents["execution-none"].Permissions, "vfs_bind", "*"); got != "deny" {
		t.Errorf("execution role with none grant: vfs_bind = %q, want deny", got)
	}
	if got := effectOf(config.Agents["planning-write"].Permissions, "vfs_write", "*"); got != "allow" {
		t.Errorf("planning role with write grant: vfs_write = %q, want allow", got)
	}
}

func TestRenderConfigRejectsDuplicateAgentID(t *testing.T) {
	spec := opencode.AgentSpec{ID: "dev", Description: "Agent", Mode: "subagent", System: "work", VFSCapabilities: vfsCaps()}
	_, err := opencode.RenderConfig(opencode.ConfigRequest{Agents: []opencode.AgentSpec{spec, spec}})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("opencode.RenderConfig() error = %v, want duplicate agent error", err)
	}
}

func TestRenderConfigProjectsExplicitVFSGrants(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Agents: []opencode.AgentSpec{
			{ID: "takt", Description: "Orchestrator", Mode: "primary", System: "p", Role: model.RoleOrchestrator, VFSCapabilities: vfsCaps(model.VFSCapabilityClaimList, model.VFSCapabilityClaimAssign, model.VFSCapabilityClaimRelease, model.VFSCapabilityDiscard, model.VFSCapabilityConsolidate)},
			{ID: "pm", Description: "PM", Mode: "all", System: "p", Role: model.RoleDirectInterlocutor, VFSCapabilities: vfsCaps()},
			{ID: "analyst", Description: "Analyst", Mode: "subagent", System: "p", Role: model.RolePlanningAuthor, VFSCapabilities: vfsCaps()},
			{ID: "dev", Description: "Dev", Mode: "subagent", System: "p", Role: model.RoleExecution, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
			{ID: "verify", Description: "Verify", Mode: "subagent", System: "p", Role: model.RoleVerification, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityRead, model.VFSCapabilityVerify)},
			{ID: "gc", Description: "Collector", Mode: "subagent", System: "p", Role: model.RoleMaintenance, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := decodeConfig(t, artifact.Content)
	vfsTools := []string{"vfs_bind", "vfs_write", "vfs_read", "vfs_delete", "vfs_discard", "vfs_verify", "vfs_consolidate"}
	for _, id := range []string{"pm", "analyst"} {
		rules := config.Agents[id].Permissions
		// V2 drops the per-agent tools map: a blanket deny on the tool's own
		// action is what removes it from the agent's tool snapshot.
		for _, tool := range vfsTools {
			if effectOf(rules, tool, "*") != "deny" {
				t.Errorf("%s %s = %q, want deny", id, tool, effectOf(rules, tool, "*"))
			}
		}
	}
	// No specialist drives the VFS CLI or the orchestrator's own declarations.
	for _, id := range []string{"pm", "analyst", "dev", "verify", "gc"} {
		rules := config.Agents[id].Permissions
		if effectOf(rules, "shell", "takt-ai vfs*") != "deny" {
			t.Errorf("%s shell rules = %v, want takt-ai vfs* denied", id, rules)
		}
		for _, tool := range []string{"dispatch_commit", "dispatch_restore", "dispatch_declare_recovery", "gc_prepare"} {
			if got := effectOf(rules, tool, "*"); got != "deny" {
				t.Errorf("%s %s = %q, want deny", id, tool, got)
			}
		}
	}
	// Writes, reads, and verdicts have exactly their declared tool sets;
	// discard and consolidation are the orchestrator's alone.
	for _, id := range []string{"dev", "verify", "gc"} {
		for _, tool := range vfsTools {
			want := "allow"
			if tool == "vfs_consolidate" || tool == "vfs_discard" ||
				(id == "verify" && (tool == "vfs_write" || tool == "vfs_delete")) ||
				(id != "verify" && tool == "vfs_verify") {
				want = "deny"
			}
			if got := effectOf(config.Agents[id].Permissions, tool, "*"); got != want {
				t.Errorf("%s %s = %q, want %q", id, tool, got, want)
			}
		}
	}
	for _, tool := range vfsTools {
		want := "deny"
		if tool == "vfs_consolidate" || tool == "vfs_discard" {
			want = "allow"
		}
		if got := effectOf(config.Agents["takt"].Permissions, tool, "*"); got != want {
			t.Errorf("takt %s = %q, want %q", tool, got, want)
		}
	}
	for _, action := range []string{"claim_list", "claim_assign", "claim_assign_verifier", "claim_release"} {
		if got := effectOf(config.Agents["takt"].Permissions, action, "*"); got != "allow" {
			t.Errorf("takt %s permission = %q, want allow", action, got)
		}
		for _, id := range []string{"pm", "analyst", "dev", "verify", "gc"} {
			if got := effectOf(config.Agents[id].Permissions, action, "*"); got != "deny" {
				t.Errorf("%s %s permission = %q, want deny", id, action, got)
			}
		}
	}
}

func TestVFSPluginToolsAllHaveRolePermissions(t *testing.T) {
	plugin := string(opencode.TaktVFSPluginArtifact("/test/takt-ai", false).Content)
	matches := regexp.MustCompile(`name: "(vfs_[a-z_]+)"`).FindAllStringSubmatch(plugin, -1)
	if len(matches) == 0 {
		t.Fatal("plugin exposes no VFS tools")
	}
	agents := []opencode.AgentSpec{
		{ID: "takt", Description: "Orchestrator", Mode: "primary", System: "p", Role: model.RoleOrchestrator, VFSCapabilities: vfsCaps(model.VFSCapabilityClaimList, model.VFSCapabilityClaimAssign, model.VFSCapabilityClaimRelease, model.VFSCapabilityDiscard, model.VFSCapabilityConsolidate)},
		{ID: "pm", Description: "Interlocutor", Mode: "all", System: "p", Role: model.RoleDirectInterlocutor, VFSCapabilities: vfsCaps()},
	}
	configArtifact, err := opencode.RenderConfig(opencode.ConfigRequest{Agents: agents})
	if err != nil {
		t.Fatal(err)
	}
	config := decodeConfig(t, configArtifact.Content)
	for _, match := range matches {
		name := match[1]
		if name == "vfs_consolidate" || name == "vfs_discard" {
			if effectOf(config.Agents["takt"].Permissions, name, "*") == "deny" {
				t.Errorf("orchestrator must retain %s", name)
			}
			continue
		}
		for _, id := range []string{"takt", "pm"} {
			if got := effectOf(config.Agents[id].Permissions, name, "*"); got != "deny" {
				t.Errorf("%s sees registered tool %s: effect %q, want deny", id, name, got)
			}
		}
	}
}

// TestMutationSkillAndVFSAccessFollowExplicitInstanceGrant checks that VFS tools and
// mutation skills follow explicit grants while document authors retain native edits.
func TestMutationSkillAndVFSAccessFollowExplicitInstanceGrant(t *testing.T) {
	content, err := catalog.LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	pack, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	fsys := catalog.AssetFS()
	capabilities := map[string][]model.VFSCapability{}
	defByInstance := map[string]catalog.AgentDefinition{}
	for _, def := range pack.Agents {
		for _, id := range def.Instances {
			if grant, ok := def.VFSCapabilities(id); ok {
				capabilities[id] = grant
			}
			defByInstance[id] = def
		}
	}
	agents := []opencode.AgentSpec{{ID: "takt", Description: "Orchestrator", Mode: "primary", System: "p", Role: model.RoleOrchestrator, VFSCapabilities: capabilities["takt"]}}
	for id, instance := range content {
		grant, ok := capabilities[id]
		if !ok {
			t.Fatalf("instance %q has no VFS capability", id)
		}
		def := defByInstance[id]
		text, err := def.ComposeText(fsys)
		if err != nil {
			t.Fatal(err)
		}
		agents = append(agents, opencode.AgentSpec{ID: id, Description: def.Profile(id).Description, Mode: "subagent", System: text, Role: instance.Role, VFSCapabilities: grant})
	}
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{Agents: agents})
	if err != nil {
		t.Fatal(err)
	}
	config := decodeConfig(t, artifact.Content)
	for _, agent := range agents {
		t.Run(agent.ID, func(t *testing.T) {
			rules := config.Agents[agent.ID].Permissions
			want := "deny"
			if agent.Role == model.RoleExecution && slices.Contains(agent.VFSCapabilities, model.VFSCapabilityWrite) {
				want = "allow"
			}
			if got := effectOf(rules, "skill", "takt-vfs-mutation"); got != want {
				t.Errorf("mutation skill permission = %q, want %q for role %s", got, want, agent.Role)
			}
			for _, tool := range []string{"vfs_bind", "vfs_write", "vfs_read", "vfs_delete", "vfs_discard", "vfs_verify", "vfs_consolidate"} {
				toolWant := "deny"
				capabilityByTool := map[string]model.VFSCapability{
					"vfs_bind":  model.VFSCapabilityBind,
					"vfs_write": model.VFSCapabilityWrite, "vfs_read": model.VFSCapabilityRead,
					"vfs_delete": model.VFSCapabilityDelete, "vfs_discard": model.VFSCapabilityDiscard,
					"vfs_verify": model.VFSCapabilityVerify, "vfs_consolidate": model.VFSCapabilityConsolidate,
				}
				if slices.Contains(agent.VFSCapabilities, capabilityByTool[tool]) {
					toolWant = "allow"
				}
				if got := effectOf(rules, tool, "*"); got != toolWant {
					t.Errorf("%s permission = %q, want %q for role %s", tool, got, toolWant, agent.Role)
				}
			}
			if agent.Role == model.RoleDirectInterlocutor || agent.Role == model.RolePlanningAuthor {
				if got := effectOf(rules, "edit", "*"); got != "allow" {
					t.Errorf("document author edit = %q, want %q", got, "allow")
				}
			}
		})
	}
}

func TestRenderConfigDeniesInterlocutorStackForVerificationAndMaintenance(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Agents: []opencode.AgentSpec{
			{ID: "takt", Description: "Orchestrator", Mode: "primary", System: "p", Role: model.RoleOrchestrator, VFSCapabilities: vfsCaps(model.VFSCapabilityClaimList, model.VFSCapabilityClaimAssign, model.VFSCapabilityClaimRelease, model.VFSCapabilityConsolidate)},
			{ID: "pm", Description: "PM", Mode: "all", System: "p", Role: model.RoleDirectInterlocutor, VFSCapabilities: vfsCaps()},
			{ID: "verify", Description: "Verify", Mode: "subagent", System: "p", Role: model.RoleVerification, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityRead, model.VFSCapabilityVerify)},
			{ID: "takt-gc", Description: "GC", Mode: "subagent", System: "p", Role: model.RoleMaintenance, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := decodeConfig(t, artifact.Content)
	switchTools := []string{"dispatch_switch", "dispatch_abort_switch"}
	for _, id := range []string{"verify", "takt-gc", "pm"} {
		rules := config.Agents[id].Permissions
		for _, tool := range switchTools {
			if effectOf(rules, tool, "*") != "deny" {
				t.Errorf("%s %s = %q, want deny", id, tool, effectOf(rules, tool, "*"))
			}
		}
	}
	for _, tool := range switchTools {
		rules := config.Agents["takt"].Permissions
		if effectOf(rules, tool, "*") == "deny" {
			t.Errorf("takt %s must not be denied by the static interlocutor rule", tool)
		}
	}
	for _, id := range []string{"verify", "takt-gc"} {
		rules := config.Agents[id].Permissions
		if effectOf(rules, "dispatch_handoff", "*") != "deny" {
			t.Errorf("%s dispatch_handoff = %q, want deny", id, effectOf(rules, "dispatch_handoff", "*"))
		}
	}
	if got := effectOf(config.Agents["pm"].Permissions, "dispatch_handoff", "*"); got == "deny" {
		t.Errorf("pm dispatch_handoff must not be denied, got %q", got)
	}
}

func TestRenderConfigLetPlanningLanesWriteFiles(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Agents: []opencode.AgentSpec{
			{ID: "pm", Description: "PM", Mode: "all", System: "p", Role: model.RoleDirectInterlocutor, VFSCapabilities: vfsCaps()},
			{ID: "spec", Description: "Spec", Mode: "subagent", System: "p", Role: model.RolePlanningAuthor, VFSCapabilities: vfsCaps()},
			{ID: "dev", Description: "Dev", Mode: "subagent", System: "p", Role: model.RoleExecution, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
			{ID: "verify", Description: "Verify", Mode: "subagent", System: "p", Role: model.RoleVerification, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityRead, model.VFSCapabilityVerify)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := decodeConfig(t, artifact.Content)
	for _, id := range []string{"pm", "spec"} {
		rules := config.Agents[id].Permissions
		// V2 folds write and patch into edit, so one allow covers both.
		if effectOf(rules, "edit", "*") != "allow" {
			t.Errorf("%s rules = %v, want edit allowed", id, rules)
		}
		if !slices.ContainsFunc(rules, func(r rule) bool { return r.Action == "shell" }) {
			t.Errorf("%s lost its shell rules: %v", id, rules)
		}
	}
	for _, id := range []string{"dev", "verify"} {
		if effectOf(config.Agents[id].Permissions, "edit", "*") != "" {
			t.Errorf("%s must keep no native edit override", id)
		}
	}
}

func TestRenderConfigCarriesOnlyDesignedSkillsAndTools(t *testing.T) {
	designed := []string{"takt-invariant-planning", "takt-memory-contract", "takt-memory-orchestrator", "takt-sdd-workflow"}
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Agents: []opencode.AgentSpec{
			{ID: "takt", Description: "Orchestrator", Mode: "primary", System: "p", Role: model.RoleOrchestrator, VFSCapabilities: vfsCaps(model.VFSCapabilityConsolidate), Skills: designed},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rules := decodeConfig(t, artifact.Content).Agents["takt"].Permissions
	for _, skill := range designed {
		if effectOf(rules, "skill", skill) == "deny" {
			t.Errorf("designed skill %s hidden from the orchestrator", skill)
		}
	}
	for _, skill := range []string{"takt-interlocutor-handoff", "takt-memory-dev", "takt-memory-pm", "takt-vfs-mutation"} {
		if got := effectOf(rules, "skill", skill); got != "deny" {
			t.Errorf("skill %s = %q for the orchestrator, want deny", skill, got)
		}
	}
	for _, tool := range []string{"gc_findings", "gc_verdict", "gc_no_change"} {
		if got := effectOf(rules, tool, "*"); got != "deny" {
			t.Errorf("GC cycle tool %s = %q for the orchestrator, want deny", tool, got)
		}
	}
	if effectOf(rules, "skill", "someone-elses-skill") == "deny" {
		t.Errorf("a skill Takt does not install was denied")
	}
}
