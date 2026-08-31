package opencode

import (
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"gopkg.in/yaml.v2"
)

func TestRenderSubAgent(t *testing.T) {
	artifact, err := RenderSubAgent(SubAgentRequest{
		ID:           "takt-dev",
		Description:  "Development specialist",
		Instructions: "Implement the requested change.",
		Assignment:   model.ModelAssignment{Model: "openai/gpt-5.6-luna"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "---\n" +
		"mode: subagent\n" +
		"description: \"Development specialist\"\n" +
		"model: \"openai/gpt-5.6-luna\"\n" +
		"---\n\n" +
		"Implement the requested change.\n"
	if artifact.Path != ".config/opencode/agents/takt-dev.md" || string(artifact.Content) != want {
		t.Errorf("agent artifact = %#v, want path and frontmatter content", artifact)
	}
}

func TestRenderSubAgentValidation(t *testing.T) {
	base := SubAgentRequest{
		ID:           "agent",
		Description:  "Agent",
		Instructions: "work",
		Assignment:   model.ModelAssignment{Model: "openai/gpt-5.6-luna"},
	}
	tests := []struct {
		name    string
		mutate  func(*SubAgentRequest)
		wantErr string
	}{
		{name: "missing description", mutate: func(request *SubAgentRequest) { request.Description = "" }, wantErr: "description is required"},
		{name: "missing model", mutate: func(request *SubAgentRequest) { request.Assignment.Model = "" }, wantErr: "no model assignment"},
		{name: "unsafe id", mutate: func(request *SubAgentRequest) { request.ID = "../agent" }, wantErr: "invalid OpenCode sub-agent id"},
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
			Assignment: model.ModelAssignment{Model: "openai/gpt-5.6-luna"},
		}
	}
	rendered, err := RenderSubAgents([]SubAgentRequest{request("takt-z"), request("takt-a")})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{rendered[0].Path, rendered[1].Path}; !slices.Equal(got, []string{".config/opencode/agents/takt-a.md", ".config/opencode/agents/takt-z.md"}) {
		t.Errorf("artifact paths = %v, want sorted paths", got)
	}
	if _, err := RenderSubAgents([]SubAgentRequest{request("takt-a"), request("takt-a")}); err == nil {
		t.Fatal("RenderSubAgents() error = nil, want duplicate path error")
	}
}

func TestRenderConfig(t *testing.T) {
	artifact, err := RenderConfig(ConfigRequest{
		Assignment: model.ModelAssignment{Model: "openai/gpt-5.6-luna"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n" +
		"  \"$schema\": \"https://opencode.ai/config.json\",\n" +
		"  \"model\": \"openai/gpt-5.6-luna\"\n" +
		"}\n"
	if artifact.Path != ".config/opencode/opencode.json" || string(artifact.Content) != want {
		t.Errorf("config artifact = %#v, want path and JSON content", artifact)
	}
}

func TestRenderConfigValidation(t *testing.T) {
	_, err := RenderConfig(ConfigRequest{})
	if err == nil || !strings.Contains(err.Error(), "requires a model assignment") {
		t.Fatalf("RenderConfig() error = %v, want model assignment error", err)
	}
}

// TestRenderSubAgentModelScalarRoundTrip proves that a model string containing
// special YAML characters (":", double quotes, "#", leading/trailing spaces,
// newlines, and reserved tokens like null/~) is serialized as a safe
// double-quoted scalar and parses back to the identical value. This guards the
// fix for the raw model write that previously produced invalid/unparseable
// frontmatter.
func TestRenderSubAgentModelScalarRoundTrip(t *testing.T) {
	cases := []string{
		`anthropic/claude:opus "special" # note`,
		" leading:space #hash\nand a \"quoted\" newline",
		`null: ~ # true false`,
	}
	for _, modelStr := range cases {
		t.Run(modelStr, func(t *testing.T) {
			artifact, err := RenderSubAgent(SubAgentRequest{
				ID:           "takt-dev",
				Description:  "Dev: specialist #1",
				Instructions: "Do work.",
				Assignment:   model.ModelAssignment{Model: modelStr},
			})
			if err != nil {
				t.Fatal(err)
			}
			var parsed map[string]string
			if err := yaml.Unmarshal([]byte(strings.SplitN(string(artifact.Content), "---\n", 3)[1]), &parsed); err != nil {
				t.Fatal(err)
			}
			if got := parsed["model"]; got != modelStr {
				t.Errorf("model round-trip = %q, want %q", got, modelStr)
			}
			if got := parsed["description"]; got != "Dev: specialist #1" {
				t.Errorf("description round-trip = %q, want %q", got, "Dev: specialist #1")
			}
		})
	}
}
