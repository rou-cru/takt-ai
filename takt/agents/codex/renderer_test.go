package codex

import (
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestRenderSubAgent(t *testing.T) {
	artifact, err := RenderSubAgent(SubAgentRequest{
		ID:           "takt-dev",
		Description:  "Development specialist",
		Instructions: "Implement the requested change.",
		Assignment:   model.ModelAssignment{Model: model.CodexModelLuna, Effort: "high"},
		SandboxMode:  SandboxWorkspaceWrite,
		WebSearch:    WebSearchDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "name = \"takt-dev\"\n" +
		"description = \"Development specialist\"\n" +
		"model = \"openai/gpt-5.6-luna\"\n" +
		"model_reasoning_effort = \"high\"\n" +
		"sandbox_mode = \"workspace-write\"\n" +
		"web_search = \"disabled\"\n" +
		"developer_instructions = \"Implement the requested change.\"\n"
	if artifact.Path != ".codex/agents/takt-dev.toml" || string(artifact.Content) != want {
		t.Errorf("agent artifact = %#v, want path and TOML content", artifact)
	}
}

func TestRenderSubAgentValidation(t *testing.T) {
	base := SubAgentRequest{
		ID:           "agent",
		Description:  "Agent",
		Instructions: "work",
		Assignment:   model.ModelAssignment{Model: model.CodexModelLuna, Effort: "high"},
		SandboxMode:  SandboxReadOnly,
		WebSearch:    WebSearchDisabled,
	}
	tests := []struct {
		name    string
		mutate  func(*SubAgentRequest)
		wantErr string
	}{
		{name: "missing description", mutate: func(request *SubAgentRequest) { request.Description = "" }, wantErr: "description is required"},
		{name: "invalid model", mutate: func(request *SubAgentRequest) { request.Assignment.Model = "openai/unknown" }, wantErr: "invalid model assignment"},
		{name: "invalid effort", mutate: func(request *SubAgentRequest) { request.Assignment.Effort = "xhigh" }, wantErr: "invalid effort"},
		{name: "invalid sandbox", mutate: func(request *SubAgentRequest) { request.SandboxMode = "unsafe" }, wantErr: "invalid Codex sandbox mode"},
		{name: "invalid web search", mutate: func(request *SubAgentRequest) { request.WebSearch = "unsafe" }, wantErr: "invalid Codex web search"},
		{name: "unsafe id", mutate: func(request *SubAgentRequest) { request.ID = "../agent" }, wantErr: "invalid Codex sub-agent id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			tc.mutate(&request)
			_, err := RenderSubAgent(request)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("RenderSubAgent() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestRenderSubAgentsSortsArtifactsAndRejectsDuplicates(t *testing.T) {
	request := func(id string) SubAgentRequest {
		return SubAgentRequest{
			ID: id, Description: "Agent", Instructions: "work",
			Assignment:  model.ModelAssignment{Model: model.CodexModelLuna, Effort: "high"},
			SandboxMode: SandboxReadOnly, WebSearch: WebSearchDisabled,
		}
	}
	artifacts, err := RenderSubAgents([]SubAgentRequest{request("takt-z"), request("takt-a")})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{artifacts[0].Path, artifacts[1].Path}; !slices.Equal(got, []string{".codex/agents/takt-a.toml", ".codex/agents/takt-z.toml"}) {
		t.Errorf("artifact paths = %v, want sorted paths", got)
	}
	if _, err := RenderSubAgents([]SubAgentRequest{request("takt-a"), request("takt-a")}); err == nil {
		t.Fatal("RenderSubAgents() error = nil, want duplicate path error")
	}
}

func TestRenderGlobalPrompt(t *testing.T) {
	artifact, err := RenderGlobalPrompt("Use the supplied harness.")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Path != ".codex/AGENTS.md" || string(artifact.Content) != "Use the supplied harness.\n" {
		t.Errorf("global artifact = %#v, want Codex prompt path and newline-terminated content", artifact)
	}
}

func TestRenderConfig(t *testing.T) {
	artifact, err := RenderConfig(ConfigRequest{
		Assignment:  model.ModelAssignment{Model: model.CodexModelLuna, Effort: "high"},
		SandboxMode: SandboxWorkspaceWrite,
		WebSearch:   WebSearchLive,
		MultiAgent:  true,
		MaxThreads:  4,
		MaxDepth:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "model = \"openai/gpt-5.6-luna\"\n" +
		"model_reasoning_effort = \"high\"\n" +
		"sandbox_mode = \"workspace-write\"\n" +
		"web_search = \"live\"\n\n" +
		"[features]\n" +
		"multi_agent = true\n\n" +
		"[agents]\n" +
		"max_threads = 4\n" +
		"max_depth = 2\n"
	if artifact.Path != ".codex/config.toml" || string(artifact.Content) != want {
		t.Errorf("config artifact = %q, want %q", artifact.Content, want)
	}
}

func TestRenderConfigValidation(t *testing.T) {
	_, err := RenderConfig(ConfigRequest{
		Assignment:  model.ModelAssignment{Model: model.CodexModelLuna, Effort: "high"},
		SandboxMode: SandboxReadOnly,
		WebSearch:   WebSearchDisabled,
	})
	if err == nil || !strings.Contains(err.Error(), "limits must be positive") {
		t.Fatalf("RenderConfig() error = %v, want positive limits error", err)
	}
}
