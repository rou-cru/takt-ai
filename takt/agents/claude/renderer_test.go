package claude

import (
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestRenderSubAgent(t *testing.T) {
	tests := []struct {
		name       string
		assignment model.ModelAssignment
		want       string
	}{
		{
			name:       "default effort is omitted",
			assignment: model.ModelAssignment{Model: "sonnet"},
			want: "---\n" +
				"name: takt-dev\n" +
				"description: \"Development specialist\"\n" +
				"model: sonnet\n" +
				"tools: Read, Write\n" +
				"---\n\n" +
				"Implement the requested change.\n",
		},
		{
			name:       "nondefault effort is rendered",
			assignment: model.ModelAssignment{Model: "sonnet", Effort: "high"},
			want: "---\n" +
				"name: takt-dev\n" +
				"description: \"Development specialist\"\n" +
				"model: sonnet\n" +
				"effort: high\n" +
				"tools: Read, Write\n" +
				"---\n\n" +
				"Implement the requested change.\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			artifact, err := RenderSubAgent(SubAgentRequest{
				ID:           "takt-dev",
				Description:  "Development specialist",
				Instructions: "Implement the requested change.",
				Tools:        []string{"Write", "Read"},
				Assignment:   tc.assignment,
			})
			if err != nil {
				t.Fatalf("RenderSubAgent() error = %v", err)
			}
			if artifact.Path != ".claude/agents/takt-dev.md" {
				t.Errorf("artifact path = %q, want .claude/agents/takt-dev.md", artifact.Path)
			}
			if got := string(artifact.Content); got != tc.want {
				t.Errorf("artifact content = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderSubAgentValidation(t *testing.T) {
	tests := []struct {
		name    string
		request SubAgentRequest
		wantErr string
	}{
		{name: "missing description", request: SubAgentRequest{ID: "agent", Instructions: "work", Assignment: model.ModelAssignment{Model: "sonnet"}}, wantErr: "description is required"},
		{name: "missing instructions", request: SubAgentRequest{ID: "agent", Description: "Agent", Assignment: model.ModelAssignment{Model: "sonnet"}}, wantErr: "instructions are required"},
		{name: "invalid model", request: SubAgentRequest{ID: "agent", Description: "Agent", Instructions: "work", Assignment: model.ModelAssignment{Model: "unknown"}}, wantErr: "invalid model assignment"},
		{name: "unsupported effort", request: SubAgentRequest{ID: "agent", Description: "Agent", Instructions: "work", Assignment: model.ModelAssignment{Model: "haiku", Effort: "high"}}, wantErr: "does not support effort"},
		{name: "unsafe id", request: SubAgentRequest{ID: "../agent", Description: "Agent", Instructions: "work", Assignment: model.ModelAssignment{Model: "sonnet"}}, wantErr: "invalid Claude sub-agent id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RenderSubAgent(tc.request)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("RenderSubAgent() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestRenderSubAgentsSortsArtifactsAndRejectsDuplicates(t *testing.T) {
	request := func(id string) SubAgentRequest {
		return SubAgentRequest{ID: id, Description: "Agent", Instructions: "work", Assignment: model.ModelAssignment{Model: "sonnet"}}
	}
	artifacts, err := RenderSubAgents([]SubAgentRequest{request("takt-z"), request("takt-a")})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{artifacts[0].Path, artifacts[1].Path}; !slices.Equal(got, []string{".claude/agents/takt-a.md", ".claude/agents/takt-z.md"}) {
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
	if artifact.Path != ".claude/CLAUDE.md" || string(artifact.Content) != "Use the supplied harness.\n" {
		t.Errorf("global artifact = %#v, want Claude prompt path and newline-terminated content", artifact)
	}
}

func TestRenderSettings(t *testing.T) {
	artifact, err := RenderSettings(SettingsRequest{
		Settings: map[string]any{"enabled": true},
		Hooks: map[string][]HookGroup{
			"UserPromptSubmit": {{Hooks: []Hook{{Type: "command", Command: "run-hook"}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n" +
		"  \"enabled\": true,\n" +
		"  \"hooks\": {\n" +
		"    \"UserPromptSubmit\": [\n" +
		"      {\n" +
		"        \"hooks\": [\n" +
		"          {\n" +
		"            \"type\": \"command\",\n" +
		"            \"command\": \"run-hook\"\n" +
		"          }\n" +
		"        ]\n" +
		"      }\n" +
		"    ]\n" +
		"  }\n" +
		"}\n"
	if artifact.Path != ".claude/settings.json" || string(artifact.Content) != want {
		t.Errorf("settings artifact = %q, want %q", artifact.Content, want)
	}
}

func TestRenderSettingsValidation(t *testing.T) {
	_, err := RenderSettings(SettingsRequest{Hooks: map[string][]HookGroup{
		"UserPromptSubmit": {{Hooks: []Hook{{Type: "command"}}}},
	}})
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("RenderSettings() error = %v, want incomplete hook error", err)
	}
}
