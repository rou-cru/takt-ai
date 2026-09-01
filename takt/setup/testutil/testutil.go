// Package testutil provides shared test helpers for setup and CLI test suites.
package testutil

import (
	"slices"

	"github.com/rou-cru/takt-ai/takt/agents/claude"
	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

// TestPlanRequest builds a setup.PlanRequest populated with all catalog entries
// for the given targets. It is shared between takt/setup and cmd/takt-ai test
// suites to avoid duplicating the same builder in two packages.
func TestPlanRequest(targets ...model.AgentID) setup.PlanRequest {
	semantic, err := catalog.Load()
	if err != nil {
		panic(err)
	}
	content := make([]catalog.NativeSubAgentContent, 0, len(semantic)-1)
	for _, entry := range semantic {
		if entry.Name == "default" {
			continue
		}
		native := catalog.NativeSubAgentContent{
			ID:           entry.Name,
			Description:  entry.Name + " test specialist",
			Instructions: "Follow the explicit test instructions.",
		}
		if slices.Contains(targets, model.AgentClaudeCode) {
			native.ClaudeTools = []string{"Read"}
		}
		if slices.Contains(targets, model.AgentCodex) {
			native.CodexSandboxMode = codex.SandboxWorkspaceWrite
			native.CodexWebSearch = codex.WebSearchDisabled
		}
		content = append(content, native)
	}

	request := setup.PlanRequest{Targets: targets, Content: content}
	if slices.Contains(targets, model.AgentClaudeCode) {
		request.Claude = setup.ClaudePlanOptions{
			GlobalPrompt: "Use the explicit Claude test prompt.",
			Settings:     claude.SettingsRequest{Settings: map[string]any{"enabled": true}},
		}
	}
	if slices.Contains(targets, model.AgentCodex) {
		request.Codex = setup.CodexPlanOptions{
			GlobalPrompt: "Use the explicit Codex test prompt.",
			SandboxMode:  codex.SandboxWorkspaceWrite,
			WebSearch:    codex.WebSearchDisabled,
			MaxThreads:   2,
			MaxDepth:     1,
		}
	}
	return request
}
