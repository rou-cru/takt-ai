package setup

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

// TestBuildOpenCodePlanShipsToolInvocationGuidance verifies that the deployment
// includes shared discovery instructions and every enabled agent loads them.
func TestBuildOpenCodePlanShipsToolInvocationGuidance(t *testing.T) {
	request, err := DefaultPlanRequest()
	if err != nil {
		t.Fatal(err)
	}
	plans, _, err := BuildTargetPlans(request)
	if err != nil {
		t.Fatal(err)
	}
	plan := plans[0]
	baseline := findArtifact(plan, ".config/opencode/takt/shared/BASELINE.md")
	if !strings.Contains(string(baseline.Content), "return search({ query:") {
		t.Error("deployed shared baseline lacks Code Mode discovery instructions")
	}
	for id, agent := range openCodeAgents(t, plan) {
		if agent["disabled"] == true {
			continue
		}
		prompt, ok := agent["system"].(string)
		if !ok || !strings.Contains(prompt, "{file:./takt/shared/BASELINE.md}") {
			t.Errorf("%s does not load the single shared tool invocation guidance", id)
		}
	}
}

func TestBuildOpenCodePlanAppliesOrchestratorModelOverride(t *testing.T) {
	request, err := DefaultPlanRequest()
	if err != nil {
		t.Fatal(err)
	}
	request.OpenCodeModelOverrides = map[string]model.ModelAssignment{
		"takt": {Model: "provider/orchestrator-model", Effort: "high"},
	}
	plans, _, err := BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	agent := openCodeAgents(t, plans[0])["takt"]
	if got, want := agent["model"], "provider/orchestrator-model#high"; got != want {
		t.Fatalf("orchestrator model = %v, want %q", got, want)
	}
}

// TestDefaultPlanRequestOpenCodeInterlocutorMode proves the real declarative
// catalog marks only pm, architect and product-designer as Direct
// Interlocutors (mode: all in opencode.json); every other specialist stays
// delegate-only (mode: subagent).
func TestDefaultPlanRequestOpenCodeInterlocutorMode(t *testing.T) {
	request, err := DefaultPlanRequest()
	if err != nil {
		t.Fatalf("DefaultPlanRequest() error = %v", err)
	}
	plans, _, err := BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	agents := openCodeAgents(t, plans[0])
	interlocutors := map[string]bool{"pm": true, "architect": true, "product-designer": true}
	for id, agent := range agents {
		if id == "takt" || id == "build" || id == "plan" || id == "general" || id == "explore" {
			continue
		}
		wantMode := "subagent"
		if interlocutors[id] {
			wantMode = "all"
		}
		if agent["mode"] != wantMode {
			t.Errorf("%s mode = %v, want %q", id, agent["mode"], wantMode)
		}
	}
}

// TestBuildOpenCodePlanProjectsVFSGrantsPerInstance checks that the default OpenCode
// plan allows or denies the VFS tools per instance according to its grants.
func TestBuildOpenCodePlanProjectsVFSGrantsPerInstance(t *testing.T) {
	request, err := DefaultPlanRequest()
	if err != nil {
		t.Fatal(err)
	}
	plans, _, err := BuildTargetPlans(request)
	if err != nil {
		t.Fatal(err)
	}
	agents := openCodeAgents(t, plans[0])
	for id, want := range map[string]string{
		"pm": "deny", "analyst": "deny", "dev": "allow", "verify": "deny",
		"simplify": "allow", "takt": "allow",
	} {
		permissions, ok := agents[id]["permissions"].([]any)
		if !ok {
			t.Fatalf("%s permissions = %#v, want explicit permission rules", id, agents[id]["permissions"])
		}
		action := "vfs_bind"
		if id == "takt" {
			action = "vfs_consolidate"
		}
		got := ""
		for _, item := range permissions {
			rule, ok := item.(map[string]any)
			if ok && rule["action"] == action && rule["resource"] == "*" {
				got, _ = rule["effect"].(string)
			}
		}
		if got != want {
			t.Errorf("%s %s permission = %q, want %q", id, action, got, want)
		}
	}
	for _, action := range []string{"claim_list", "claim_assign", "claim_release"} {
		for id, want := range map[string]string{"takt": "allow", "pm": "deny", "dev": "deny"} {
			permissions := agents[id]["permissions"].([]any)
			got := ""
			for _, item := range permissions {
				rule, ok := item.(map[string]any)
				if ok && rule["action"] == action && rule["resource"] == "*" {
					got, _ = rule["effect"].(string)
				}
			}
			if got != want {
				t.Errorf("%s %s permission = %q, want %q", id, action, got, want)
			}
		}
	}
}

// openCodeAgents decodes the plan's opencode.json and returns its "agents" map.
func openCodeAgents(t *testing.T, plan TargetPlan) map[string]map[string]any {
	t.Helper()
	config := findArtifact(plan, ".config/opencode/opencode.json")
	var decoded struct {
		Agents map[string]map[string]any `json:"agents"`
	}
	if err := json.Unmarshal(config.Content, &decoded); err != nil {
		t.Fatalf("decode opencode.json: %v (%s)", err, config.Content)
	}
	return decoded.Agents
}

func findArtifact(plan TargetPlan, path string) Artifact {
	for _, artifact := range plan.Artifacts {
		if artifact.Path == path {
			return artifact
		}
	}
	return Artifact{}
}

func TestBuildTargetPlansExcludesArchifyWhenDeselectedAndIncludesWhenSelected(t *testing.T) {
	withoutArchify, _, err := BuildTargetPlans(PlanRequest{Components: []string{string(model.ComponentContext7)}})
	if err != nil {
		t.Fatalf("BuildTargetPlans(context7) error = %v", err)
	}
	for _, a := range withoutArchify[0].Artifacts {
		if strings.Contains(a.Path, "takt-archify") {
			t.Errorf("inactive optional skill deployed: %s", a.Path)
		}
	}
	cfgWithout := findArtifact(withoutArchify[0], ".config/opencode/opencode.json")
	if strings.Contains(strings.ToLower(string(cfgWithout.Content)), "archify") {
		t.Error("opencode.json mentions archify when deselected")
	}

	withArchify, _, err := BuildTargetPlans(PlanRequest{Components: []string{string(model.ComponentContext7), string(model.ComponentArchify)}})
	if err != nil {
		t.Fatalf("BuildTargetPlans(context7, archify) error = %v", err)
	}
	if a := findArtifact(withArchify[0], ".config/opencode/takt/skills/takt-archify/SKILL.md"); len(a.Content) == 0 {
		t.Error("missing .config/opencode/takt/skills/takt-archify/SKILL.md when archify selected")
	}
	agents := openCodeAgents(t, withArchify[0])
	if !strings.Contains(agents["architect"]["system"].(string), "Archify is available") {
		t.Error("architect system prompt missing Archify clause when selected")
	}
	if strings.Contains(agents["dev"]["system"].(string), "Archify") {
		t.Error("dev system prompt unexpectedly contains Archify clause")
	}
}
