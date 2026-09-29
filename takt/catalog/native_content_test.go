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

package catalog

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestLoadNativeContent(t *testing.T) {
	content, err := LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}

	wantIDs := []string{
		"analyst", "pm", "spec", "architect",
		"product-designer", "tpm", "dev", "verify",
		"judge-a", "judge-b", "fix", "simplify",
	}
	if len(content) != len(wantIDs) {
		t.Fatalf("loaded %d entries, want %d: %v", len(content), len(wantIDs), content)
	}
	for _, id := range wantIDs {
		entry, ok := content[id]
		if !ok {
			t.Errorf("missing native content %q", id)
			continue
		}
		if entry.ID != id {
			t.Errorf("entry %q has ID %q", id, entry.ID)
		}
		if entry.Description == "" || entry.Instructions == "" {
			t.Errorf("entry %q missing description or instructions", id)
		}
	}
	if _, exposed := content["takt-judge"]; exposed {
		t.Error("takt-judge must not be exposed on its own, only projected as -a/-b")
	}
	if content["judge-a"].Description != content["judge-b"].Description {
		t.Error("judge-a and judge-b must share the same content, only the ID differs")
	}
}

func TestLoadNativeContentInterlocutors(t *testing.T) {
	// Holding the interface is a property of the role class, not a separately
	// declared flag: exactly the direct interlocutors derive it.
	content, err := LoadNativeContent()
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
	content, err := LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	for id, entry := range content {
		if err := model.ValidateRoleClass(id, entry.Role); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestSimplifyInstancesHoldOneAdmittedRoleEach(t *testing.T) {
	// The simplify specialty is a single execution instance, shared by
	// ordinary cleanup and harness-attached GC collection. GC authority comes
	// from the cycle attachment, never from a second maintenance instance, and
	// the independent verifier stays a distinct instance.
	content, err := LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	if got := content["simplify"].Role; got != model.RoleExecution {
		t.Errorf("simplify role = %q, want %q", got, model.RoleExecution)
	}
	wantVFS := map[string][]model.VFSCapability{
		"simplify": {model.VFSCapabilityBind, model.VFSCapabilityWrite, model.VFSCapabilityRead, model.VFSCapabilityDelete},
		"verify":   {model.VFSCapabilityBind, model.VFSCapabilityRead, model.VFSCapabilityVerify},
	}
	packages, err := LoadPackages()
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

// assertInstanceHasExactVFSCapabilities checks that some agent definition
// grants instance id exactly the expected VFS capabilities.
func assertInstanceHasExactVFSCapabilities(t *testing.T, packages Catalog, id string, expected []model.VFSCapability) {
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
func assertRetiredSimplifyInstancesGone(t *testing.T, content map[string]NativeSubAgentContent) {
	t.Helper()
	for _, retired := range []string{"simplify-align", "simplify-plan", "simplify-verify", "simplify-gc"} {
		if _, ok := content[retired]; ok {
			t.Errorf("retired instance %q still exposed", retired)
		}
	}
}

func TestInstanceProfileValidation(t *testing.T) {
	base := "id: x\ninstances: [x, y]\ndescription: d\nrole: execution\nvfs_capabilities: {x: [bind, write], y: []}\ncontext: {operations: OPERATIONS.md}\n"
	for name, tc := range map[string]struct{ extra, want string }{
		"undeclared instance": {"instance_profiles: {z: {role: verification}}\n", "undeclared instance"},
		"unknown role":        {"instance_profiles: {y: {role: janitor}}\n", "unknown role class"},
		"orchestrator":        {"instance_profiles: {y: {role: orchestrator}}\n", "not a specialty role"},
		"unknown field":       {"instance_profiles: {y: {mode: all}}\n", "field mode not found"},
	} {
		fsys := fstest.MapFS{
			"README.md":              {Data: []byte("r")},
			BaselinePath:             {Data: []byte("b")},
			"skills/s/SKILL.md":      {Data: []byte("---\nname: s\ndescription: d\n---\n")},
			"agents/x/agent.yaml":    {Data: []byte(base + tc.extra)},
			"agents/x/OPERATIONS.md": {Data: []byte("ops")},
		}
		if _, err := LoadFS(fsys); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestVFSGrantValidationRequiresExactInstanceCoverage(t *testing.T) {
	base := "id: x\ninstances: [x, y]\ndescription: d\nrole: execution\ncontext: {operations: OPERATIONS.md}\n"
	for name, tc := range map[string]struct{ grant, want string }{
		"missing instance":           {"vfs_capabilities: {x: [bind]}\n", "missing explicit VFS capability"},
		"undeclared instance":        {"vfs_capabilities: {x: [bind], y: [], z: [read]}\n", "undeclared instance"},
		"unknown capability":         {"vfs_capabilities: {x: [bind], y: [inherit]}\n", "unknown VFS capability"},
		"null instead of empty list": {"vfs_capabilities: {x: [bind], y: null}\n", "explicit list"},
	} {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"README.md":              {Data: []byte("r")},
				BaselinePath:             {Data: []byte("b")},
				"skills/s/SKILL.md":      {Data: []byte("---\nname: s\ndescription: d\n---\n")},
				"agents/x/agent.yaml":    {Data: []byte(base + tc.grant)},
				"agents/x/OPERATIONS.md": {Data: []byte("ops")},
			}
			if _, err := LoadFS(fsys); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadFS() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestVFSCapabilitiesUsesExactIDAndExplicitEmpty(t *testing.T) {
	definition := AgentDefinition{
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
	content, err := LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	if _, exposed := content["takt"]; exposed {
		t.Error("the orchestrator agent \"takt\" must not be exposed as native sub-agent content")
	}
}

// TestVFSOnlyMutationSkill checks that every agent whose native `edit` is
// denied by permissionsConfig (renderer.go) declares the takt-vfs-mutation
// skill, and that the skill itself still carries the VFS-only and
// destructive-effects instructions. Native edit is a harness-level deny
// regardless of prompt content, so this guidance lives in an on-demand skill
// rather than duplicated per-agent Instructions text.
func TestVFSOnlyMutationSkill(t *testing.T) {
	cat, err := LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	skill := findSkillPackage(t, cat, "takt-vfs-mutation")
	assertSkillDescriptorContains(t, skill, "only through your VFS assignment", "Never use native edit or write tools", "as destructive")
	assertAgentsDeclareSkill(t, cat, "takt-vfs-mutation", "takt-dev", "takt-fix", "takt-simplify")
}

// TestNoOrphanSkills operationalizes DOCTRINE's no-orphan-skill norm
// (STR-13): every skill in the catalog must have at least one real
// consumer, declared on some agent's skills list.
func TestNoOrphanSkills(t *testing.T) {
	cat, err := LoadPackages()
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

// findSkillPackage returns the catalog's skill with the given id, failing
// the test if it is not found.
func findSkillPackage(t *testing.T, cat Catalog, id string) *SkillPackage {
	t.Helper()
	for i := range cat.Skills {
		if cat.Skills[i].ID == id {
			return &cat.Skills[i]
		}
	}
	t.Fatalf("%s skill not found in catalog", id)
	return nil
}

// assertSkillDescriptorContains checks that skill's descriptor mentions
// every one of want.
func assertSkillDescriptorContains(t *testing.T, skill *SkillPackage, want ...string) {
	t.Helper()
	descriptor := string(skill.Descriptor)
	for _, w := range want {
		if !strings.Contains(descriptor, w) {
			t.Errorf("%s skill missing %q", skill.ID, w)
		}
	}
}

// assertAgentsDeclareSkill checks that every named agent in the catalog
// declares skillID among its skills.
func assertAgentsDeclareSkill(t *testing.T, cat Catalog, skillID string, agentIDs ...string) {
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

func readAssets(t *testing.T, globs ...string) map[string]string {
	t.Helper()
	fsys := AssetFS()
	files := map[string]string{}
	for _, g := range globs {
		paths, err := fs.Glob(fsys, g)
		if err != nil || len(paths) == 0 {
			t.Fatalf("glob %q: %v (%d matches)", g, err, len(paths))
		}
		for _, p := range paths {
			b, err := fs.ReadFile(fsys, p)
			if err != nil {
				t.Fatal(err)
			}
			files[p] = string(b)
		}
	}
	return files
}

func TestAgentProseNeverExplainsMechanism(t *testing.T) {
	files := readAssets(t, "shared/BASELINE.md", "agents/takt/*.md", "agents/*/OPERATIONS.md",
		"skills/takt-sdd-workflow/SKILL.md", "skills/takt-sdd-recovery/SKILL.md", "skills/takt-memory-*/SKILL.md", "skills/takt-vfs-mutation/SKILL.md",
		"skills/takt-bounded-workflow/SKILL.md", "skills/takt-bounded-planning/SKILL.md",
		"skills/takt-workflow-selection/SKILL.md", "skills/takt-workflow-selection-exceptions/SKILL.md")
	forbidden := []string{
		"harness", "admission", "enforce", "nobody reviews", "You write content only",
		"server instructions", "tool descriptions", "verification controls", "accepted as written",
	}
	for path, text := range files {
		lower := strings.ToLower(text)
		for _, term := range forbidden {
			if strings.Contains(lower, strings.ToLower(term)) {
				t.Errorf("%s explains mechanism: contains %q", path, term)
			}
		}
	}
}

func TestOrchestratorOperationsKeepDelegationAsDefault(t *testing.T) {
	text := readAssets(t, "agents/takt/OPERATIONS.md")["agents/takt/OPERATIONS.md"]
	for _, want := range []string{
		"Your normal mode for implementation is to delegate",
		"You can also use native OpenCode file tools or shell",
		"a critical or urgent intervention",
		"work the user explicitly asks you to do",
		"never on a path an active claim holds",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("orchestrator operations missing %q", want)
		}
	}
	for _, pushy := range []string{
		"use native OpenCode file tools and shell for quick adjustments",
		"not limited to inspection",
		"not a reason to skip delegation",
	} {
		if strings.Contains(text, pushy) {
			t.Errorf("orchestrator operations overemphasize direct execution with %q", pushy)
		}
	}
}

func TestOrchestratorOperationsNameItsHarnessTools(t *testing.T) {
	text := readAssets(t, "agents/takt/OPERATIONS.md")["agents/takt/OPERATIONS.md"]
	for _, tool := range []string{"claim_assign`", "claim_list`", "claim_assign_verifier`", "dispatch_activity_start`", "dispatch_activity_finish`", "gc_request`"} {
		if !strings.Contains(text, "`"+tool) {
			t.Errorf("orchestrator operations do not say when to use %s", strings.TrimSuffix(tool, "`"))
		}
	}
}

// TestToolInvocationGuidanceDistinguishesDirectAndCodeMode checks that the shared
// baseline owns tool discovery guidance and its examples avoid undiscovered calls.
func TestToolInvocationGuidanceDistinguishesDirectAndCodeMode(t *testing.T) {
	baseline := readAssets(t, "shared/BASELINE.md")["shared/BASELINE.md"]
	for _, want := range []string{"direct assistant tool call", "not in its catalog", "return search({ query: \"dispatch_activity_start\" })", "subsequent", "returned entry's `path` and `signature`"} {
		if !strings.Contains(baseline, want) {
			t.Errorf("shared guidance missing tool boundary %q", want)
		}
	}
	for _, fence := range strings.Split(baseline, "```js")[1:] {
		code, _, _ := strings.Cut(fence, "```")
		if strings.Contains(code, "tools.") || strings.Contains(code, "shell(") {
			t.Errorf("shared guidance has an undiscovered or native tool call in executable example: %s", code)
		}
	}
	for path, body := range readAssets(t, "agents/*/OPERATIONS.md", "skills/*/SKILL.md") {
		if strings.Contains(body, "return search({ query:") || strings.Contains(body, "tools.shell") || strings.Contains(body, "tools.search") {
			t.Errorf("%s duplicates shared tool invocation guidance", path)
		}
	}
}

func TestMemoryContractAnyRoleRecordsDecision(t *testing.T) {
	text := readAssets(t, "skills/takt-memory-contract/SKILL.md")["skills/takt-memory-contract/SKILL.md"]
	for _, gone := range []string{"Only interface roles record it", "non-interface role"} {
		if strings.Contains(text, gone) {
			t.Errorf("contract still says %q", gone)
		}
	}
	if !strings.Contains(text, "Any role may record it") {
		t.Error("contract must say any role may record a decision")
	}
	// MEM-TYP-8: role skills reference the contract, never restate its decision rule.
	for path, body := range readAssets(t, "skills/takt-memory-*/SKILL.md") {
		if strings.Contains(body, "decisions belong to interface roles") {
			t.Errorf("%s restates the decision rule", path)
		}
	}
}
