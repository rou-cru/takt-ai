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

package setup

import (
	"fmt"
	"sort"

	"github.com/rou-cru/takt-ai/takt/agents/claude"
	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// ClaudePlanOptions contains explicit Claude global projection inputs.
type ClaudePlanOptions struct {
	GlobalPrompt string                 `json:"global_prompt"`
	Settings     claude.SettingsRequest `json:"settings"`
}

// CodexPlanOptions contains explicit Codex global projection inputs. The
// default catalog assignment supplies the global model and reasoning effort.
type CodexPlanOptions struct {
	GlobalPrompt string            `json:"global_prompt"`
	SandboxMode  codex.SandboxMode `json:"sandbox_mode"`
	WebSearch    codex.WebSearch   `json:"web_search"`
	MultiAgent   bool              `json:"multi_agent"`
	MaxThreads   int               `json:"max_threads"`
	MaxDepth     int               `json:"max_depth"`
}

// DefaultOpenCodeModel is the fallback OpenCode model assignment. The semantic
// catalog has no OpenCode assignments yet, so every OpenCode projection uses
// this default unless OpenCodePlanOptions overrides it.
const DefaultOpenCodeModel = "openai/gpt-5.6-luna"

// OpenCodePlanOptions contains explicit OpenCode global projection inputs.
type OpenCodePlanOptions struct {
	Model string `json:"model"`
}

// PlanRequest contains selected targets, explicit native content, and target
// model overrides. Content has no assignment fields; assignments are resolved
// from the semantic catalog for each selected target. Components optionally
// names artifact-only components for a custom setup; the default setup leaves
// it empty.
type PlanRequest struct {
	Targets              []model.AgentID                  `json:"targets"`
	Content              []catalog.NativeSubAgentContent  `json:"content"`
	Claude               ClaudePlanOptions                `json:"claude"`
	Codex                CodexPlanOptions                 `json:"codex"`
	OpenCode             OpenCodePlanOptions              `json:"opencode"`
	ClaudeModelOverrides map[string]model.ModelAssignment `json:"claude_model_overrides"`
	CodexModelOverrides  map[string]model.ModelAssignment `json:"codex_model_overrides"`
	Components           []string                         `json:"components"`
}

// BuildTargetPlans validates the requested targets and builds their native
// configuration plans in canonical order. Each plan's artifacts are ordered by
// relative path; errors are returned without any plans.
func BuildTargetPlans(request PlanRequest) ([]TargetPlan, error) {
	targets, err := selectedTargets(request.Targets)
	if err != nil {
		return nil, err
	}
	semantic, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	joined, err := catalog.JoinNativeContentForTargets(semantic, request.Content, targets)
	if err != nil {
		return nil, err
	}
	components, err := ValidateComponents(request.Components)
	if err != nil {
		return nil, err
	}

	plans := make([]TargetPlan, 0, len(targets))
	for _, target := range targets {
		plan, renderable, err := buildPlanForTarget(target, joined, request, components)
		if err != nil {
			return nil, err
		}
		if !renderable {
			continue
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// buildPlanForTarget renders one target's plan. Targets without a native
// projection report renderable=false.
func buildPlanForTarget(target model.AgentID, content []catalog.NativeSubAgent, request PlanRequest, components []model.ComponentID) (TargetPlan, bool, error) {
	switch target {
	case model.AgentClaudeCode:
		plan, err := buildClaudePlan(content, request.Claude, request.ClaudeModelOverrides, components)
		return plan, true, err
	case model.AgentCodex:
		plan, err := buildCodexPlan(content, request.Codex, request.CodexModelOverrides, components)
		return plan, true, err
	case model.AgentOpenCode:
		plan, err := buildOpenCodePlan(content, request.OpenCode, components)
		return plan, true, err
	}
	return TargetPlan{}, false, nil
}

// selectedTargets validates requested targets and returns them in canonical order.
func selectedTargets(targets []model.AgentID) ([]model.AgentID, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one target is required")
	}
	seen := make(map[model.AgentID]struct{}, len(targets))
	for _, target := range targets {
		if target != model.AgentClaudeCode && target != model.AgentCodex && target != model.AgentOpenCode {
			return nil, fmt.Errorf("unsupported target %q", target)
		}
		if _, exists := seen[target]; exists {
			return nil, fmt.Errorf("duplicate target %q", target)
		}
		seen[target] = struct{}{}
	}
	ordered := make([]model.AgentID, 0, len(seen))
	for _, target := range []model.AgentID{model.AgentClaudeCode, model.AgentCodex, model.AgentOpenCode} {
		if _, ok := seen[target]; ok {
			ordered = append(ordered, target)
		}
	}
	return ordered, nil
}

// buildClaudePlan creates the Claude configuration plan, including sub-agent files,
// the global prompt, settings, selected component artifacts, and their managed paths.
func buildClaudePlan(content []catalog.NativeSubAgent, options ClaudePlanOptions, overrides map[string]model.ModelAssignment, components []model.ComponentID) (TargetPlan, error) {
	requests := make([]claude.SubAgentRequest, 0, len(content))
	for _, entry := range content {
		preset := map[string]model.ModelAssignment{
			entry.ID:  entry.Assignments[model.AgentClaudeCode],
			"default": entry.DefaultAssignments[model.AgentClaudeCode],
		}
		assignment, err := model.ResolveSubAgentAssignment(model.AgentClaudeCode, entry.ID, "default", overrides, preset)
		if err != nil {
			return TargetPlan{}, err
		}
		assignment, err = model.ValidateClaudeModelAssignment(entry.ID, assignment)
		if err != nil {
			return TargetPlan{}, err
		}
		requests = append(requests, claude.SubAgentRequest{
			ID:           entry.ID,
			Description:  entry.Description,
			Instructions: entry.Instructions,
			Tools:        entry.ClaudeTools,
			Assignment:   assignment,
		})
	}
	agents, err := claude.RenderSubAgents(requests)
	if err != nil {
		return TargetPlan{}, err
	}
	globalPrompt, err := claude.RenderGlobalPrompt(options.GlobalPrompt)
	if err != nil {
		return TargetPlan{}, err
	}
	componentArtifacts := claudeComponentArtifacts(components, &options.Settings)
	settings, err := claude.RenderSettings(options.Settings)
	if err != nil {
		return TargetPlan{}, err
	}

	artifacts := make([]Artifact, 0, len(agents))
	generatedPaths := make([]string, 0, len(agents))
	for _, agent := range agents {
		artifacts = append(artifacts, Artifact{Path: agent.Path, Content: agent.Content})
		generatedPaths = append(generatedPaths, agent.Path)
	}
	for _, artifact := range componentArtifacts {
		artifacts = append(artifacts, artifact)
		generatedPaths = append(generatedPaths, artifact.Path)
	}
	artifacts = append(artifacts,
		Artifact{Path: globalPrompt.Path, Content: globalPrompt.Content},
		Artifact{Path: settings.Path, Content: settings.Content},
	)
	sortArtifacts(artifacts)
	managedPaths, err := claude.NewManagedPaths(generatedPaths)
	if err != nil {
		return TargetPlan{}, err
	}
	return TargetPlan{
		Target:       string(model.AgentClaudeCode),
		ManagedPaths: managedPaths,
		Artifacts:    artifacts,
	}, nil
}

// buildCodexPlan builds the Codex configuration plan and its generated artifacts.
// The plan includes sub-agent files, the global prompt, configuration, selected
// component merges, and managed paths. It returns an error if assignment
// resolution, rendering, or manifest creation fails.
func buildCodexPlan(content []catalog.NativeSubAgent, options CodexPlanOptions, overrides map[string]model.ModelAssignment, components []model.ComponentID) (TargetPlan, error) {
	requests := make([]codex.SubAgentRequest, 0, len(content))
	for _, entry := range content {
		preset := map[string]model.ModelAssignment{
			entry.ID:  entry.Assignments[model.AgentCodex],
			"default": entry.DefaultAssignments[model.AgentCodex],
		}
		assignment, err := model.ResolveSubAgentAssignment(model.AgentCodex, entry.ID, "default", overrides, preset)
		if err != nil {
			return TargetPlan{}, err
		}
		assignment, err = model.ValidateCodexModelAssignment(entry.ID, assignment)
		if err != nil {
			return TargetPlan{}, err
		}
		requests = append(requests, codex.SubAgentRequest{
			ID:           entry.ID,
			Description:  entry.Description,
			Instructions: entry.Instructions,
			Assignment:   assignment,
			SandboxMode:  entry.CodexSandboxMode,
			WebSearch:    entry.CodexWebSearch,
		})
	}
	agents, err := codex.RenderSubAgents(requests)
	if err != nil {
		return TargetPlan{}, err
	}
	globalPrompt, err := codex.RenderGlobalPrompt(options.GlobalPrompt)
	if err != nil {
		return TargetPlan{}, err
	}
	globalPreset := map[string]model.ModelAssignment{
		"default": content[0].DefaultAssignments[model.AgentCodex],
	}
	assignment, err := model.ResolveSubAgentAssignment(model.AgentCodex, "default", "default", overrides, globalPreset)
	if err != nil {
		return TargetPlan{}, err
	}
	configRequest := codex.ConfigRequest{
		Assignment:  assignment,
		SandboxMode: options.SandboxMode,
		WebSearch:   options.WebSearch,
		MultiAgent:  options.MultiAgent,
		MaxThreads:  options.MaxThreads,
		MaxDepth:    options.MaxDepth,
	}
	codexComponentArtifacts(components, &configRequest)
	config, err := codex.RenderConfig(configRequest)
	if err != nil {
		return TargetPlan{}, err
	}

	artifacts := make([]Artifact, 0, len(agents))
	generatedPaths := make([]string, 0, len(agents))
	for _, agent := range agents {
		artifacts = append(artifacts, Artifact{Path: agent.Path, Content: agent.Content})
		generatedPaths = append(generatedPaths, agent.Path)
	}
	artifacts = append(artifacts,
		Artifact{Path: globalPrompt.Path, Content: globalPrompt.Content},
		Artifact{Path: config.Path, Content: config.Content},
	)
	sortArtifacts(artifacts)
	managedPaths, err := codex.NewManagedPaths(generatedPaths)
	if err != nil {
		return TargetPlan{}, err
	}
	return TargetPlan{
		Target:       string(model.AgentCodex),
		ManagedPaths: managedPaths,
		Artifacts:    artifacts,
	}, nil
}

// buildOpenCodePlan builds an OpenCode configuration plan from native sub-agent content and the selected model, using the default model when none is specified.
func buildOpenCodePlan(content []catalog.NativeSubAgent, options OpenCodePlanOptions, components []model.ComponentID) (TargetPlan, error) {
	modelID := options.Model
	if modelID == "" {
		modelID = DefaultOpenCodeModel
	}
	requests := make([]opencode.SubAgentRequest, 0, len(content))
	for _, entry := range content {
		requests = append(requests, opencode.SubAgentRequest{
			ID:           entry.ID,
			Description:  entry.Description,
			Instructions: entry.Instructions,
			Assignment:   model.ModelAssignment{Model: modelID},
		})
	}
	agents, err := opencode.RenderSubAgents(requests)
	if err != nil {
		return TargetPlan{}, err
	}
	configRequest := opencode.ConfigRequest{
		Assignment: model.ModelAssignment{Model: modelID},
	}
	componentArtifacts := openCodeComponentArtifacts(components, &configRequest)
	config, err := opencode.RenderConfig(configRequest)
	if err != nil {
		return TargetPlan{}, err
	}

	artifacts := make([]Artifact, 0, len(agents))
	generatedPaths := make([]string, 0, len(agents))
	for _, agent := range agents {
		artifacts = append(artifacts, Artifact{Path: agent.Path, Content: agent.Content})
		generatedPaths = append(generatedPaths, agent.Path)
	}
	for _, artifact := range componentArtifacts {
		artifacts = append(artifacts, artifact)
		generatedPaths = append(generatedPaths, artifact.Path)
	}
	artifacts = append(artifacts, Artifact{Path: config.Path, Content: config.Content})
	sortArtifacts(artifacts)
	managedPaths, err := opencode.NewManagedPaths(generatedPaths)
	if err != nil {
		return TargetPlan{}, err
	}
	return TargetPlan{
		Target:       string(model.AgentOpenCode),
		ManagedPaths: managedPaths,
		Artifacts:    artifacts,
	}, nil
}

// sortArtifacts orders artifacts lexicographically by their relative path.
func sortArtifacts(artifacts []Artifact) {
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
}
