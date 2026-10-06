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

// Package opencode renders native OpenCode configuration from catalog references.
package opencode

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// permissionRule is one entry of an OpenCode V2 permissions array. V2
// evaluates rules top to bottom and the last match wins, so slice order is
// the contract: broad allows first, specific denies after them.
type permissionRule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// allResources is the wildcard every rule that is not pattern-scoped uses.
const allResources = "*"

// Artifact is a filesystem-free OpenCode projection. Path is relative to the
// user's home directory and Content is ready for a later deployer to write.
type Artifact = shared.Artifact

// AgentSpec is one native OpenCode agent entry keyed by instance ID.
type AgentSpec struct {
	ID          string
	Description string
	Mode        string
	System      string
	Model       string
	// Variant is the model variant (OpenCode V2's name for the reasoning tier
	// that ModelAssignment.Effort carries); empty leaves the provider default.
	Variant string
	Role    model.RoleClass
	// VFSCapabilities is the explicit operation grant for this exact instance;
	// an empty non-nil slice means no VFS access, while nil is undeclared.
	VFSCapabilities []model.VFSCapability
	// Skills are the Takt skills this agent is designed to use; every other
	// Takt skill is hidden from it. Skills Takt does not install are untouched.
	Skills []string
}

// ConfigRequest contains the native OpenCode global configuration projection.
type ConfigRequest struct {
	Assignment model.ModelAssignment
	// Context7 adds the canonical context7 remote MCP server entry.
	Context7 bool
	// Permissions adds the canonical bash/read permission rules.
	Permissions bool
	// Agents are Takt instances registered natively in opencode.json.
	Agents []AgentSpec
}

// RenderConfig returns the native OpenCode opencode.json artifact.
func RenderConfig(request ConfigRequest) (Artifact, error) {
	agents, err := renderAgents(request.Agents)
	if err != nil {
		return Artifact{}, err
	}
	config := map[string]any{
		"$schema":       "https://opencode.ai/config.json",
		"default_agent": shared.OrchestratorID,
		"agents":        agents,
		// Enable OpenCode's built-in formatters after file edits and retain
		// snapshots so changes can be reverted through the native UI.
		"formatter": true,
		"snapshot":  true,
		"attention": map[string]any{
			"enabled":       true,
			"notifications": true,
		},
	}
	addConfigOptions(config, request)
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Artifact{}, fmt.Errorf("marshal OpenCode config: %w", err)
	}
	return Artifact{
		Path:    ConfigPath(),
		Content: append(content, '\n'),
	}, nil
}

func renderAgents(specs []AgentSpec) (map[string]any, error) {
	agents := map[string]any{
		// OpenCode's built-in agents are disabled: build and plan would compete
		// with the orchestrator in the agent picker, and general and explore
		// would let it reach for a generic sub-agent instead of a Takt
		// specialist. Agents the user adds themselves are left untouched.
		"build":   map[string]any{"disabled": true},
		"plan":    map[string]any{"disabled": true},
		"general": map[string]any{"disabled": true},
		"explore": map[string]any{"disabled": true},
	}
	var maintenance []string
	for _, spec := range specs {
		if spec.Role == model.RoleMaintenance {
			maintenance = append(maintenance, spec.ID)
		}
	}
	for _, spec := range specs {
		if spec.ID == "" || spec.Description == "" || spec.Mode == "" || spec.System == "" {
			return nil, fmt.Errorf("OpenCode agent %q is incomplete", spec.ID)
		}
		if spec.VFSCapabilities == nil {
			return nil, fmt.Errorf("OpenCode agent %q has no explicit VFS capabilities", spec.ID)
		}
		if _, exists := agents[spec.ID]; exists {
			return nil, fmt.Errorf("duplicate OpenCode agent %q", spec.ID)
		}
		agents[spec.ID] = agentEntry(spec, maintenance)
	}
	return agents, nil
}

func addConfigOptions(config map[string]any, request ConfigRequest) {
	if request.Assignment.Model != "" {
		config["model"] = request.Assignment.Model
	}
	if request.Context7 {
		config["mcp"] = context7Config()
	}
	if request.Permissions {
		config["permissions"] = permissionsConfig()
	}
}

// vfsToolNames lists every tool the takt-vfs plugin registers so each explicit
// instance grant can emit a corresponding OpenCode permission rule.
var vfsToolNames = []string{"vfs_bind", "vfs_write", "vfs_read", "vfs_delete", "vfs_discard", "vfs_verify", vfsConsolidateTool}

// claimToolNames are the orchestration-only operations in the VFS capability
// contract; their native permission rules are projected even when a plugin
// lane registers the tools separately.
var claimToolNames = []string{"claim_list", "claim_assign", "claim_release"}

// gcCycleToolNames are the maintenance-cycle tools. Ordinary agent entries
// never expose them; harness-created GC child sessions override these denies
// with session permissions, and coordinator admission stays authoritative.
var gcCycleToolNames = []string{"gc_baseline", "gc_findings", "gc_investigate", "gc_authorize", "gc_collected", "gc_delta", "gc_verdict", "gc_acceptance", "gc_no_change"}

// vfsConsolidateTool moves verified staged work into the workspace; only the
// orchestrator holds it.
const vfsConsolidateTool = "vfs_consolidate"

// orchestratorToolNames are the orchestrator's own declarations to the
// harness (plan, consumed inputs, direct activity, recovery, exceptions, contests, GC
// preparation and requests); no other agent is offered them.
var orchestratorToolNames = []string{"dispatch_commit", "dispatch_inputs", "dispatch_activity_start", "dispatch_activity_finish", "dispatch_declare_recovery", "dispatch_close_recovery", "dispatch_restore", "dispatch_exception", "dispatch_contest", "gc_prepare", "gc_request"}

// vfsMutationSkill carries the conduct of agents that stage their own work.
const vfsMutationSkill = "takt-vfs-mutation"

// resultHandoffSkill gates the deliver_result tool: only the producers
// designed to use it may present a complete Engram artifact as their result.
const resultHandoffSkill = "takt-result-handoff"

func agentEntry(spec AgentSpec, maintenance []string) map[string]any {
	entry := map[string]any{
		"description": spec.Description,
		"mode":        spec.Mode,
		"system":      spec.System,
	}
	if spec.Model != "" {
		entry["model"] = modelRef(spec)
	}
	rules := agentPermissionRules(spec, maintenance)
	if len(rules) > 0 {
		entry["permissions"] = rules
	}
	return entry
}

// agentPermissionRules builds ordered OpenCode permissions from the agent's role,
// skills, and explicit VFS grants, using maintenance to restrict delegation targets.
func agentPermissionRules(spec AgentSpec, maintenance []string) []permissionRule {
	var rules []permissionRule
	if spec.Role == model.RoleOrchestrator {
		// The orchestrator is the trusted coordinator (vfs.Identity's own
		// doc comment): it keeps full native permissions, no shell overrides.
		// The VFS grant is projected independently below.
		rules = append(rules, permissionRule{"edit", allResources, "allow"})
		rules = append(rules, sensitiveEditDenies()...)
		rules = append(rules, subagentRules(maintenance)...)
		for _, name := range orchestratorToolNames {
			rules = append(rules, permissionRule{name, allResources, "allow"})
		}
	} else {
		if spec.Role == model.RoleVerification {
			rules = append(rules, permissionRule{"edit", allResources, "deny"})
		}
		rules = append(rules, shellRules(spec.Role)...)
		// The workspace event store records every session's activity; only the
		// orchestrator reads it. The inspection sandbox keeps it private too; a
		// shell that runs natively (a result agent, or enforcement off) does not.
		rules = append(rules, permissionRule{"read", eventStoreResource, "deny"})
		rules = append(rules, permissionRule{"subagent", allResources, "deny"})
		for _, name := range orchestratorToolNames {
			rules = append(rules, permissionRule{name, allResources, "deny"})
		}
	}
	if spec.Role == model.RoleDirectInterlocutor || spec.Role == model.RolePlanningAuthor {
		// Native file writes let these roles deliver authored documents when the
		// user asks for it; this permission is independent of their VFS grant.
		// V2 folds write and patch into edit, so one rule covers both.
		rules = append(rules, permissionRule{"edit", allResources, "allow"})
		rules = append(rules, sensitiveEditDenies()...)
		rules = append(rules, permissionRule{"edit", eventStoreResource, "deny"})
	}
	rules = append(rules, skillRules(spec.Skills)...)
	// Ordinary sessions never expose GC tools. Harness-created GC sessions
	// override these denies with session permissions; coordinator admission is
	// still the authority for every phase.
	for _, name := range gcCycleToolNames {
		rules = append(rules, permissionRule{name, allResources, "deny"})
	}
	// VFS access is an explicit per-instance grant, not a role-derived default.
	rules = append(rules, vfsPermissionRules(spec.VFSCapabilities)...)
	// The mutation skill rule comes after skillRules so it decides alone: only
	// an execution instance that stages its own work reads it; a maintenance
	// collector receives its conduct from the cycle itself.
	skillEffect := "deny"
	if spec.Role == model.RoleExecution && slices.Contains(spec.VFSCapabilities, model.VFSCapabilityWrite) {
		skillEffect = "allow"
	}
	rules = append(rules, permissionRule{"skill", vfsMutationSkill, skillEffect})
	deliverResultEffect := "deny"
	if slices.Contains(spec.Skills, resultHandoffSkill) {
		deliverResultEffect = "allow"
	}
	rules = append(rules, permissionRule{"deliver_result", allResources, deliverResultEffect})
	rules = append(rules, interlocutorRules(spec.Role)...)
	return rules
}

// modelRef renders the agent's model in V2's compact "provider/model#variant"
// form. Without a variant it returns the plain provider/model reference.
func modelRef(spec AgentSpec) any {
	if spec.Variant == "" {
		return spec.Model
	}
	return spec.Model + "#" + spec.Variant
}

// eventStoreResource is the workspace event store as a permission resource.
var eventStoreResource = "**/" + obs.StateDirName + "/" + obs.StoreFileName

// sensitiveEditDenies follows every edit allow: the last match wins, so a
// blanket allow never reopens a secret-bearing path to writes.
func sensitiveEditDenies() []permissionRule {
	rules := make([]permissionRule, 0, len(model.SensitivePathGlobs))
	for _, glob := range model.SensitivePathGlobs {
		rules = append(rules, permissionRule{"edit", "**/" + glob, "deny"})
	}
	return rules
}

// subagentRules keeps a single blanket allow when there is nothing to exclude.
// Otherwise the blanket allow comes first and the per-id denies follow, because
// V2 evaluates the array in order and the last match wins. Maintenance cycles
// are harness-run, never dispatched by the orchestrator as ordinary work.
func subagentRules(maintenance []string) []permissionRule {
	rules := []permissionRule{{"subagent", allResources, "allow"}}
	ids := append([]string(nil), maintenance...)
	slices.Sort(ids)
	for _, id := range ids {
		rules = append(rules, permissionRule{"subagent", id, "deny"})
	}
	return rules
}

// switchTools lend and return the chat interface itself; only the
// orchestrator, which owns the interface, may hand it out or reclaim it.
var switchTools = []string{"dispatch_switch", "dispatch_abort_switch"}

// interlocutorRules projects the interlocutor-stack tools by role: switching
// the interface is the orchestrator's alone, and handing a completed
// interlocution back is the direct_interlocutor's alone. No other role class
// ever holds chat (PIRS_ORCHESTRATION's IR-3/IR-17).
func interlocutorRules(role model.RoleClass) []permissionRule {
	switchEffect := "deny"
	if role == model.RoleOrchestrator {
		switchEffect = "allow"
	}
	handoffEffect := "deny"
	if role == model.RoleDirectInterlocutor {
		handoffEffect = "allow"
	}
	rules := make([]permissionRule, 0, len(switchTools)+1)
	for _, name := range switchTools {
		rules = append(rules, permissionRule{name, allResources, switchEffect})
	}
	return append(rules, permissionRule{"dispatch_handoff", allResources, handoffEffect})
}

// skillRules hides every Takt skill the agent is not designed to use, so its
// skill listing carries only its own.
func skillRules(designed []string) []permissionRule {
	var rules []permissionRule
	for _, id := range taktSkillIDs() {
		if !slices.Contains(designed, id) {
			rules = append(rules, permissionRule{"skill", id, "deny"})
		}
	}
	return rules
}

// shellRules merges the git-mutation guard and the VFS-CLI guard into one
// ordered rule list. The broad "git *" deny comes first so the read-only
// subcommand allows that follow can win as the last match. No specialist
// drives the VFS CLI directly: its tools inject the harness identity.
func shellRules(role model.RoleClass) []permissionRule {
	return append(gitRules(role), vfsShellRules()...)
}

// vfsPermissionRules emits an explicit allow or deny for every plugin tool.
// OpenCode V2 has no per-agent tool map, so these action rules both hide denied
// tools and reject attempted calls. The grant is deliberately role-independent.
func vfsPermissionRules(capabilities []model.VFSCapability) []permissionRule {
	allowed := map[string]bool{}
	capabilityActions := map[model.VFSCapability]string{
		model.VFSCapabilityClaimList:    "claim_list",
		model.VFSCapabilityClaimAssign:  "claim_assign",
		model.VFSCapabilityClaimRelease: "claim_release",
		model.VFSCapabilityBind:         "vfs_bind",
		model.VFSCapabilityWrite:        "vfs_write",
		model.VFSCapabilityRead:         "vfs_read",
		model.VFSCapabilityDelete:       "vfs_delete",
		model.VFSCapabilityDiscard:      "vfs_discard",
		model.VFSCapabilityVerify:       "vfs_verify",
		model.VFSCapabilityConsolidate:  vfsConsolidateTool,
	}
	for capability, action := range capabilityActions {
		if slices.Contains(capabilities, capability) {
			allowed[action] = true
		}
	}
	// Verifier adoption belongs to the harness, not to a model-facing tool.
	if allowed["vfs_verify"] {
		delete(allowed, "vfs_bind")
	}
	rules := make([]permissionRule, 0, len(vfsToolNames)+len(claimToolNames)+1)
	for _, name := range slices.Concat(vfsToolNames, claimToolNames) {
		effect := "deny"
		if allowed[name] {
			effect = "allow"
		}
		rules = append(rules, permissionRule{name, allResources, effect})
	}
	return rules
}

func gitRules(role model.RoleClass) []permissionRule {
	if role == "" || vfs.GuardGitMutation(role) == nil {
		return nil
	}
	rules := []permissionRule{{"shell", "git *", "deny"}}
	for _, cmd := range vfs.ReadOnlyGitSubcommands() {
		rules = append(rules, permissionRule{"shell", "git " + cmd, "allow"}, permissionRule{"shell", "git " + cmd + " *", "allow"})
	}
	return rules
}

func vfsShellRules() []permissionRule {
	return []permissionRule{{"shell", "takt-ai vfs*", "deny"}, {"shell", "*takt-ai vfs*", "deny"}}
}
