// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package setup

import (
	"fmt"

	"github.com/rou-cru/takt-ai/takt/agents/claude"
	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// DefaultPlanRequest returns valid native content for the explicitly selected
// targets. Content follows the embedded catalog order and excludes default,
// which is an assignment fallback rather than a renderable sub-agent.
func DefaultPlanRequest(targets []model.AgentID) (PlanRequest, error) {
	orderedTargets, err := selectedTargets(targets)
	if err != nil {
		return PlanRequest{}, fmt.Errorf("default setup request: %w", err)
	}

	semantic, err := catalog.Load()
	if err != nil {
		return PlanRequest{}, fmt.Errorf("load default setup catalog: %w", err)
	}
	content := make([]catalog.NativeSubAgentContent, 0, len(semantic)-1)
	for _, definition := range semantic {
		if definition.Name == "default" {
			continue
		}
		entry, ok := defaultNativeContent(definition.Name)
		if !ok {
			return PlanRequest{}, fmt.Errorf("default setup content is missing catalog role %q", definition.Name)
		}
		content = append(content, entry)
	}

	return PlanRequest{
		Targets: orderedTargets,
		Content: content,
		Claude: ClaudePlanOptions{
			GlobalPrompt: defaultGlobalPrompt,
			Settings:     claudeSettings(),
		},
		Codex: CodexPlanOptions{
			GlobalPrompt: defaultGlobalPrompt,
			SandboxMode:  codex.SandboxWorkspaceWrite,
			WebSearch:    codex.WebSearchLive,
			MultiAgent:   true,
			MaxThreads:   4,
			MaxDepth:     2,
		},
	}, nil
}

const defaultGlobalPrompt = "Follow Takt's shared operating rules: verify claims against repository evidence, make minimal targeted changes, and return concise results."

func claudeSettings() claude.SettingsRequest {
	return claude.SettingsRequest{Settings: map[string]any{}}
}

func defaultNativeContent(id string) (catalog.NativeSubAgentContent, bool) {
	entry, ok := nativeContent[id]
	if !ok {
		return catalog.NativeSubAgentContent{}, false
	}
	entry.ID = id
	entry.ClaudeTools = append([]string(nil), entry.ClaudeTools...)
	return entry, true
}

var nativeContent = map[string]catalog.NativeSubAgentContent{
	"takt-init":             native("Bootstrap SDD context and project configuration.", "Execute SDD initialization directly: detect the stack, initialize the selected persistence backend, build the skill registry, and save project context. Do not delegate.", []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}, codex.SandboxWorkspaceWrite, codex.WebSearchDisabled),
	"takt-analyst":          native("Investigate codebases and clarify change ideas.", "Investigate before commitment: understand the topic, inspect relevant code and tests, identify constraints and coupling, compare approaches with tradeoffs, and recommend one. Do not modify project files or delegate.", []string{"Read", "Glob", "Grep", "WebFetch", "WebSearch"}, codex.SandboxReadOnly, codex.WebSearchLive),
	"takt-pm":               native("Create change proposals from exploration.", "Shape a proposal with intent, scope, approach, risks, and explicit assumptions. In interactive work, ask a focused product-question round about users, rules, outcomes, gaps, edge cases, boundaries, and tradeoffs; do not write code or specs.", []string{"Read", "Glob", "Grep", "Edit", "Write"}, codex.SandboxWorkspaceWrite, codex.WebSearchDisabled),
	"takt-spec":             native("Write requirements and acceptance scenarios.", "Read the proposal, extract requirements, write a delta specification and acceptance scenarios, and persist it. Specify what must be true, not implementation design; do not delegate.", []string{"Read", "Glob", "Grep", "Edit", "Write"}, codex.SandboxWorkspaceWrite, codex.WebSearchDisabled),
	"takt-architect":        native("Create technical designs from proposals.", "Read the proposal; choose architecture, boundaries, data flow, and integration points; record decisions, rationale, and rejected alternatives; then persist the design. Do not create tasks or delegate.", []string{"Read", "Glob", "Grep", "Edit", "Write"}, codex.SandboxWorkspaceWrite, codex.WebSearchDisabled),
	"takt-product-designer": native("Design useful, coherent, accessible, technically feasible UI/UX and developer experiences.", "Understand outcome, user, context, unmet need, constraints, evidence, uncertainties, and assumptions; do not present unverified premises as facts. Work iteratively through understand, define, explore, prototype, and validate. Compose with rhythm, scale, timing, timbre, contrast, and pattern; aesthetics come from proportion, hierarchy, layout harmony, and consistency. Deliver only the needed framing, assumptions, alternatives, text wireframes, PIRS, design invariants, experience or aesthetic specification, identity guide, acceptance criteria, and validation risks. Specify verifiable hierarchy, grid, spacing, responsive, state, content, and accessibility invariants; preserve meaningful order, focus, keyboard operation, contrast, reflow, and touch targets when applicable. Do not write code or prescribe frameworks, libraries, markup, CSS, components, file structure, or build details. State what implementation must preserve and why, and check repository feasibility before recommending anything that may conflict with it.", []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch"}, codex.SandboxReadOnly, codex.WebSearchLive),
	"takt-tpm":              native("Break specifications and designs into implementation tasks.", "Read the spec and design, decompose work into ordered, independently shippable tasks, link each to its requirement, identify parallelism, and persist the checklist. Do not implement or delegate.", []string{"Read", "Glob", "Grep", "Edit", "Write"}, codex.SandboxWorkspaceWrite, codex.WebSearchDisabled),
	"takt-dev":              native("Implement code changes from task definitions.", "Read tasks, spec, design, and any prior apply progress. Detect the testing mode, implement assigned work using TDD when required, follow repository conventions, mark completed tasks, and persist merged progress. Do not delegate.", []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}, codex.SandboxWorkspaceWrite, codex.WebSearchDisabled),
	"takt-verify":           native("Validate implementation against specifications, design, and tasks.", "Read the spec, tasks, and apply progress; run the appropriate tests; check every requirement against the implementation; report critical issues, warnings, and suggestions; confirm task completion; and persist the verification report. Do not modify code or delegate.", []string{"Read", "Glob", "Grep", "Bash"}, codex.SandboxReadOnly, codex.WebSearchDisabled),
	"takt-judge-a":          native("Adversarial reviewer for the judgment-day protocol.", "Execute the delegate review instructions exactly. Do not delegate or modify code. Assume bugs until disproven, review correctness, edge cases, security, performance, and project standards, then return structured findings.", []string{"Read", "Glob", "Grep", "Bash"}, codex.SandboxReadOnly, codex.WebSearchDisabled),
	"takt-judge-b":          native("Adversarial reviewer for the judgment-day protocol.", "Execute the delegate review instructions exactly. Do not delegate or modify code. Assume bugs until disproven, review correctness, edge cases, security, performance, and project standards, then return structured findings.", []string{"Read", "Glob", "Grep", "Bash"}, codex.SandboxReadOnly, codex.WebSearchDisabled),
	"takt-fix":              native("Apply confirmed judgment-day fixes.", "Execute only confirmed delegate findings. Do not delegate, refactor beyond the fix, or change unflagged code. Record each file and line changed; when fixing a pattern, find and fix the same pattern everywhere. Return the applied fixes.", []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}, codex.SandboxWorkspaceWrite, codex.WebSearchDisabled),
}

func native(description, instructions string, tools []string, sandbox codex.SandboxMode, webSearch codex.WebSearch) catalog.NativeSubAgentContent {
	return catalog.NativeSubAgentContent{
		Description:      description,
		Instructions:     instructions,
		ClaudeTools:      tools,
		CodexSandboxMode: sandbox,
		CodexWebSearch:   webSearch,
	}
}
