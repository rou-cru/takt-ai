package opencode_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// orchestratorDescription resolves the description the same way plan.go
// does, through AgentDefinition.Profile, instead of a shortcut var.
func orchestratorDescription(t *testing.T) string {
	t.Helper()
	pkgs, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range pkgs.Agents {
		if def.ID == shared.OrchestratorID {
			return def.Description
		}
	}
	t.Fatalf("no agent definition with ID %q", shared.OrchestratorID)
	return ""
}

// rule mirrors one entry of OpenCode V2's ordered permissions array.
type rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// v2Config decodes the parts of opencode.json these tests assert on.
type v2Config struct {
	DefaultAgent string `json:"default_agent"`
	Agents       map[string]struct {
		Description string `json:"description"`
		Mode        string `json:"mode"`
		System      string `json:"system"`
		Disabled    bool   `json:"disabled"`
		Permissions []rule `json:"permissions"`
	} `json:"agents"`
	Permissions []rule `json:"permissions"`
}

func decodeConfig(t *testing.T, content []byte) v2Config {
	t.Helper()
	var config v2Config
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

// effectOf returns the effect of the last rule matching action and resource,
// which is the rule OpenCode applies.
func effectOf(rules []rule, action, resource string) string {
	effect := ""
	for _, r := range rules {
		if r.Action == action && r.Resource == resource {
			effect = r.Effect
		}
	}
	return effect
}

// TestOrchestratorIsSelectableWithNativePermissions proves the orchestrator
// AgentSpec (built by the caller from the catalog, the same way plan.go does)
// renders as OpenCode's default, primary agent with edit/subagent permission,
// no inherited read override, only the asked dispatch/gc shell overrides plus
// the canonical Git classification, while the built-in general/explore agents
// stay disabled.
func TestOrchestratorIsSelectableWithNativePermissions(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Assignment:  model.ModelAssignment{Model: "openai/gpt-5.6-luna"},
		Permissions: true,
		Agents: []opencode.AgentSpec{
			{ID: shared.OrchestratorID, Description: orchestratorDescription(t), Mode: "primary", System: "Coordinate the crew.", Role: model.RoleOrchestrator, VFSCapabilities: vfsCaps(model.VFSCapabilityClaimList, model.VFSCapabilityClaimAssign, model.VFSCapabilityClaimRelease, model.VFSCapabilityConsolidate)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := decodeConfig(t, artifact.Content)
	if config.DefaultAgent != shared.OrchestratorID {
		t.Errorf("default_agent = %q, want %q", config.DefaultAgent, shared.OrchestratorID)
	}
	for _, id := range []string{"general", "explore"} {
		if !config.Agents[id].Disabled {
			t.Errorf("agent %q not disabled", id)
		}
	}
	agent, found := config.Agents[shared.OrchestratorID]
	if !found || agent.Mode != "primary" || agent.Description == "" || agent.System != "Coordinate the crew." {
		t.Fatalf("selectable orchestrator = %#v", agent)
	}
	for _, action := range []string{"edit", "subagent"} {
		if effectOf(agent.Permissions, action, "*") != "allow" {
			t.Errorf("%s permission = %q", action, effectOf(agent.Permissions, action, "*"))
		}
	}
	if slices.ContainsFunc(agent.Permissions, func(r rule) bool { return r.Action == "read" }) {
		t.Errorf("orchestrator overrides inherited read protections")
	}
	// Crew dispatch, GC coordination and Git are the orchestrator's only
	// shell overrides: dispatch and GC ask the user, and Git carries the
	// canonical classification. Every other shell protection stays.
	for _, r := range agent.Permissions {
		if r.Action == "shell" && r.Resource != "*takt-ai dispatch*" && r.Resource != "*takt-ai gc*" && !strings.HasPrefix(r.Resource, "git ") {
			t.Errorf("orchestrator overrides inherited shell protection %q", r.Resource)
		}
	}
	for _, resource := range []string{"*takt-ai dispatch*", "*takt-ai gc*"} {
		if got := effectOf(agent.Permissions, "shell", resource); got != "ask" {
			t.Errorf("orchestrator shell %s = %q, want ask", resource, got)
		}
	}
	for _, tc := range []struct{ action, resource, want string }{
		{"shell", "*", "allow"},
		{"shell", "git push", "ask"},
		{"shell", "git commit *", "ask"},
		{"shell", "git reset --hard *", "ask"},
		{"shell", "git clean *", "ask"},
		{"shell", "git restore *", "ask"},
		{"read", "**/.env", "deny"},
		{"read", "**/.ssh/**", "deny"},
	} {
		if got := effectOf(config.Permissions, tc.action, tc.resource); got != tc.want {
			t.Errorf("%s %s = %q, want %q", tc.action, tc.resource, got, tc.want)
		}
	}
}

// TestOrchestratorSubagentPermissionExcludesMaintenanceAgents proves maintenance
// agents are denied to the orchestrator's subagent tool while every other agent
// stays dispatchable, and that the blanket allow precedes the deny in the
// rendered array (OpenCode: last matching rule wins).
func TestOrchestratorSubagentPermissionExcludesMaintenanceAgents(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Agents: []opencode.AgentSpec{
			{ID: shared.OrchestratorID, Description: orchestratorDescription(t), Mode: "primary", System: "Coordinate the crew.", Role: model.RoleOrchestrator, VFSCapabilities: vfsCaps(model.VFSCapabilityClaimList, model.VFSCapabilityClaimAssign, model.VFSCapabilityClaimRelease, model.VFSCapabilityConsolidate)},
			{ID: "takt-gc", Description: "GC cycle", Mode: "subagent", System: "Collect.", Role: model.RoleMaintenance, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
			{ID: "dev", Description: "Dev", Mode: "subagent", System: "Work.", Role: model.RoleExecution, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rules := decodeConfig(t, artifact.Content).Agents[shared.OrchestratorID].Permissions
	var subagent []rule
	for _, r := range rules {
		if r.Action == "subagent" {
			subagent = append(subagent, r)
		}
	}
	if len(subagent) != 2 || subagent[0] != (rule{"subagent", "*", "allow"}) || subagent[1] != (rule{"subagent", "takt-gc", "deny"}) {
		t.Fatalf("subagent rules = %v, want * allow then takt-gc deny (last match wins)", subagent)
	}
}

// TestMaintenanceAgentKeepsVFSButDeniesMutatingGit proves the collector (PR-CRW-16)
// can drive the VFS CLI and tools while mutating Git stays denied and read-only
// Git stays allowed.
func TestMaintenanceAgentKeepsVFSButDeniesMutatingGit(t *testing.T) {
	artifact, err := opencode.RenderConfig(opencode.ConfigRequest{
		Agents: []opencode.AgentSpec{
			{ID: "takt-gc", Description: "GC cycle", Mode: "subagent", System: "Collect.", Role: model.RoleMaintenance, VFSCapabilities: vfsCaps(model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rules := decodeConfig(t, artifact.Content).Agents["takt-gc"].Permissions
	if effectOf(rules, "shell", "git *") != "deny" || effectOf(rules, "shell", "git diff") != "allow" || effectOf(rules, "shell", "git diff *--output*") != "deny" {
		t.Errorf("shell rules = %v, want mutating git denied, git diff allowed and its writing options denied", rules)
	}
	if effectOf(rules, "shell", "takt-ai vfs*") != "deny" {
		t.Errorf("shell rules = %v, maintenance drives the VFS only through its tools", rules)
	}
	// V2 has no per-agent tools map: a hidden VFS tool is a denied action.
	if effectOf(rules, "vfs_bind", "*") != "allow" {
		t.Errorf("rules = %v, maintenance must explicitly allow the VFS tools", rules)
	}
}
