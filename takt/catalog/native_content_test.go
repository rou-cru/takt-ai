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

package catalog_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

func TestLoadNativeContent(t *testing.T) {
	content, err := catalog.LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}

	wantIDs := []string{
		"analyst", "pm", "spec", "architect",
		"product-designer", "tpm", "dev", "verify",
		"judge-a", "judge-b", "fix", "simplify",
		"qa", "test", "mutation", "explorer", "quality",
	}
	if len(content) != len(wantIDs) {
		t.Fatalf("loaded %d entries, want %d: %v", len(content), len(wantIDs), content)
	}
	packages, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	defByInstance := make(map[string]catalog.AgentDefinition)
	for _, def := range packages.Agents {
		for _, id := range def.Instances {
			defByInstance[id] = def
		}
	}
	for _, id := range wantIDs {
		if _, ok := content[id]; !ok {
			t.Errorf("missing native content %q", id)
			continue
		}
		def, ok := defByInstance[id]
		if !ok {
			t.Fatalf("no agent definition declares instance %q", id)
		}
		if def.Description == "" {
			t.Errorf("entry %q missing description", id)
		}
		if text, err := def.ComposeText(catalog.AssetFS()); err != nil || text == "" {
			t.Errorf("entry %q missing instructions: %v", id, err)
		}
	}
	if _, exposed := content["takt-judge"]; exposed {
		t.Error("takt-judge must not be exposed on its own, only projected as -a/-b")
	}
}

func TestLoadNativeContentInterlocutors(t *testing.T) {
	// Holding the interface is a property of the role class, not a separately
	// declared flag: exactly the direct interlocutors derive it.
	content, err := catalog.LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"pm": true, "architect": true, "product-designer": true,
	}
	for id, entry := range content {
		if entry.Role.HoldsInterface() != want[id] {
			t.Errorf("%s HoldsInterface = %v, want %v", id, entry.Role.HoldsInterface(), want[id])
		}
	}
}

func TestLoadNativeContentDeclaresRoleClass(t *testing.T) {
	// Every specialist in the canonical definition belongs to exactly one
	// role class; without it there is no dispatch.
	content, err := catalog.LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	for id, entry := range content {
		if err := model.ValidateRoleClass(id, entry.Role); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

// TestSimplifyInstancesHoldOneAdmittedRoleEach checks that the simplify specialty is
// one execution instance and that verification instances hold only verifier grants.
func TestSimplifyInstancesHoldOneAdmittedRoleEach(t *testing.T) {
	// The simplify specialty is a single execution instance, shared by
	// ordinary cleanup and harness-attached GC collection. GC authority comes
	// from the cycle attachment, never from a second maintenance instance, and
	// the independent verifier stays a distinct instance.
	content, err := catalog.LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	if got := content["simplify"].Role; got != model.RoleExecution {
		t.Errorf("simplify role = %q, want %q", got, model.RoleExecution)
	}
	wantVFS := map[string][]model.VFSCapability{
		"simplify": {model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete},
		"verify":   {model.VFSCapabilityBind, model.VFSCapabilityRead, model.VFSCapabilityVerify},
		// Judges are verification instances: only their objective differs.
		"judge-a": {model.VFSCapabilityBind, model.VFSCapabilityRead, model.VFSCapabilityVerify},
		"judge-b": {model.VFSCapabilityBind, model.VFSCapabilityRead, model.VFSCapabilityVerify},
	}
	packages, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	for id, expected := range wantVFS {
		assertInstanceHasExactVFSCapabilities(t, packages, id, expected)
	}
	assertRetiredSimplifyInstancesGone(t, content)
	if content["dev"].Role != model.RoleExecution || content["judge-b"].Role != model.RoleVerification {
		t.Error("single-role agents must keep their definition-level role")
	}
}

// TestQAInstancesHoldExistingClassProfiles checks that the QA specialties
// introduce no role class or permission profile of their own: the QA lead is a
// planning author, the test author an execution instance, and the mutation,
// exploration and quality analysts verification instances like verify.
func TestQAInstancesHoldExistingClassProfiles(t *testing.T) {
	content, err := catalog.LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	packages, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]struct {
		role model.RoleClass
		vfs  []model.VFSCapability
	}{
		"qa":       {model.RolePlanningAuthor, []model.VFSCapability{}},
		"test":     {model.RoleExecution, []model.VFSCapability{model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete}},
		"mutation": {model.RoleQualityAssurance, []model.VFSCapability{}},
		"explorer": {model.RoleQualityAssurance, []model.VFSCapability{}},
		"quality":  {model.RoleQualityAssurance, []model.VFSCapability{}},
	} {
		if got := content[id].Role; got != want.role {
			t.Errorf("%s role = %q, want %q", id, got, want.role)
		}
		assertInstanceHasExactVFSCapabilities(t, packages, id, want.vfs)
	}
}

// assertInstanceHasExactVFSCapabilities checks that some agent definition
// grants instance id exactly the expected VFS capabilities.
func assertInstanceHasExactVFSCapabilities(t *testing.T, packages catalog.Catalog, id string, expected []model.VFSCapability) {
	t.Helper()
	found := false
	for _, def := range packages.Agents {
		if capabilities, ok := def.VFSCapabilities(id); ok {
			found = true
			if !slices.Equal(capabilities, expected) {
				t.Errorf("%s VFS capabilities = %q, want %q", id, capabilities, expected)
			}
		}
	}
	if !found {
		t.Errorf("%s has no exact-instance VFS capability", id)
	}
}

// assertRetiredSimplifyInstancesGone checks none of the retired simplify
// instances are still exposed in content.
func assertRetiredSimplifyInstancesGone(t *testing.T, content map[string]catalog.NativeSubAgentContent) {
	t.Helper()
	for _, retired := range []string{"simplify-align", "simplify-plan", "simplify-verify", "simplify-gc"} {
		if _, ok := content[retired]; ok {
			t.Errorf("retired instance %q still exposed", retired)
		}
	}
}

func TestVFSGrantValidationRequiresExactInstanceCoverage(t *testing.T) {
	base := "id: x\nlabel: x\ninstances: [x, y]\ndescription: d\nrole: execution\ncontext: {operations: OPERATIONS.md}\n"
	for name, tc := range map[string]struct{ grant, want string }{
		"missing instance":           {"vfs_capabilities: {x: [bind]}\n", "missing explicit VFS capability"},
		"undeclared instance":        {"vfs_capabilities: {x: [bind], y: [], z: [read]}\n", "undeclared instance"},
		"unknown capability":         {"vfs_capabilities: {x: [bind], y: [inherit]}\n", "unknown VFS capability"},
		"null instead of empty list": {"vfs_capabilities: {x: [bind], y: null}\n", "explicit list"},
	} {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"README.md":              {Data: []byte("r")},
				catalog.BaselinePath:     {Data: []byte("b")},
				"skills/s/SKILL.md":      {Data: []byte("---\nname: s\ndescription: d\n---\n")},
				"agents/x/agent.yaml":    {Data: []byte(base + tc.grant)},
				"agents/x/OPERATIONS.md": {Data: []byte("ops")},
			}
			if _, err := catalog.LoadFS(fsys); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadFS() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestVFSCapabilitiesUsesExactIDAndExplicitEmpty(t *testing.T) {
	definition := catalog.AgentDefinition{
		Role: model.RoleExecution,
		VFSGrants: map[string][]model.VFSCapability{
			"writer": {model.VFSCapabilityBind, model.VFSCapabilityWrite},
			"none":   {},
		},
	}
	if got, ok := definition.VFSCapabilities("writer"); !ok || !slices.Equal(got, []model.VFSCapability{model.VFSCapabilityBind, model.VFSCapabilityWrite}) {
		t.Fatalf("VFSCapabilities(writer) = %q, %v, want explicit bind/write", got, ok)
	}
	if got, ok := definition.VFSCapabilities("none"); !ok || got == nil || len(got) != 0 {
		t.Fatalf("VFSCapabilities(none) = %#v, %v, want explicit empty list", got, ok)
	}
	if got, ok := definition.VFSCapabilities("another-instance"); ok || got != nil {
		t.Fatalf("VFSCapabilities(another-instance) = %q, %v, want missing (no role fallback)", got, ok)
	}
}

func TestBuildNativeContentSkipsOrchestrator(t *testing.T) {
	content, err := catalog.LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	if _, exposed := content["takt"]; exposed {
		t.Error("the orchestrator agent \"takt\" must not be exposed as native sub-agent content")
	}
}

// TestVFSMutationSkillDeclaredByMutatingAgents checks that every agent whose
// native `edit` is denied by permissionsConfig (renderer.go) declares the
// takt-vfs-mutation skill.
func TestVFSMutationSkillDeclaredByMutatingAgents(t *testing.T) {
	cat, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	assertAgentsDeclareSkill(t, cat, "takt-vfs-mutation", "takt-dev", "takt-fix", "takt-simplify", "takt-test")
}

// TestNoOrphanSkills operationalizes DOCTRINE's no-orphan-skill norm
// (STR-13): every skill in the catalog must have at least one real
// consumer, declared on some agent's skills list.
func TestNoOrphanSkills(t *testing.T) {
	cat, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, agent := range cat.Agents {
		for _, s := range agent.Skills {
			declared[s] = true
		}
	}
	for _, skill := range cat.Skills {
		if !declared[skill.ID] {
			t.Errorf("%s skill has no declared consumer in any agent.yaml", skill.ID)
		}
	}
}

// assertAgentsDeclareSkill checks that every named agent in the catalog
// declares skillID among its skills.
func assertAgentsDeclareSkill(t *testing.T, cat catalog.Catalog, skillID string, agentIDs ...string) {
	t.Helper()
	wantAgents := map[string]bool{}
	for _, id := range agentIDs {
		wantAgents[id] = false
	}
	for _, agent := range cat.Agents {
		if _, ok := wantAgents[agent.ID]; !ok {
			continue
		}
		for _, s := range agent.Skills {
			if s == skillID {
				wantAgents[agent.ID] = true
			}
		}
	}
	for id, found := range wantAgents {
		if !found {
			t.Errorf("%s does not declare the %s skill", id, skillID)
		}
	}
}

// TestQualityAssuranceSquadCarriesItsOwnConduct checks that the QA analysts
// follow their class's conduct, never the gate verifier's, and that the
// orchestrator carries the QA workflow that routes them.
func TestQualityAssuranceSquadCarriesItsOwnConduct(t *testing.T) {
	pack, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	analysts := []string{"takt-mutation", "takt-explorer", "takt-quality"}
	assertAgentsDeclareSkill(t, pack, "takt-qa-conduct", analysts...)
	assertAgentsDeclareSkill(t, pack, "takt-qa-workflow", "takt")
	assertAgentsDeclareSkill(t, pack, "takt-test-strategy", "takt-qa")
	for _, agent := range pack.Agents {
		if !slices.Contains(analysts, agent.ID) {
			continue
		}
		for _, verifierSkill := range []string{"takt-verification-conduct", "takt-vfs-verification"} {
			if slices.Contains(agent.Skills, verifierSkill) {
				t.Errorf("%s declares the verifier skill %s", agent.ID, verifierSkill)
			}
		}
	}
}
