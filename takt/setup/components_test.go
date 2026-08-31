// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package setup

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestValidateComponents(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []model.ComponentID
		wantErr string
	}{
		{name: "nil stays empty", input: nil, want: []model.ComponentID{}},
		{name: "empty stays empty", input: []string{}, want: []model.ComponentID{}},
		{
			name:  "all components",
			input: []string{"context7", "permissions", "theme", "claude-theme", "opencode-takt-logo"},
			want: []model.ComponentID{
				model.ComponentContext7,
				model.ComponentPermission,
				model.ComponentTheme,
				model.ComponentClaudeTheme,
				model.ComponentOpenCodeTaktLogo,
			},
		},
		{name: "input order is normalized", input: []string{"theme", "context7"}, want: []model.ComponentID{model.ComponentContext7, model.ComponentTheme}},
		{name: "engram is reserved", input: []string{"engram"}, wantErr: `unknown component "engram"`},
		{name: "skills are reserved", input: []string{"skills"}, wantErr: `unknown component "skills"`},
		{name: "unknown name", input: []string{"logo"}, wantErr: `unknown component "logo"`},
		{name: "empty name", input: []string{"context7", " "}, wantErr: "component name is empty"},
		{name: "duplicate name", input: []string{"context7", "context7"}, wantErr: `duplicate component "context7"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateComponents(tc.input)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ValidateComponents() error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateComponents() error = %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ValidateComponents() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildTargetPlansComponentRouting pins the per-component routing matrix,
// the exact deployed bytes, and the managed-path coverage for every new
// artifact path. Expected content mirrors the validation-branch reference
// behavior for each target.
func TestBuildTargetPlansComponentRouting(t *testing.T) {
	tests := []struct {
		name         string
		components   []string
		targets      []model.AgentID
		expectedPath string
		expected     string
	}{
		{
			name:         "context7 claude standalone MCP file",
			components:   []string{"context7"},
			targets:      []model.AgentID{model.AgentClaudeCode},
			expectedPath: ".claude/mcp/context7.json",
			expected:     "{\n  \"command\": \"npx\",\n  \"args\": [\n    \"-y\",\n    \"--package=@upstash/context7-mcp@2.2.5\",\n    \"--\",\n    \"context7-mcp\"\n  ]\n}\n",
		},
		{
			name:         "context7 codex remote MCP block",
			components:   []string{"context7"},
			targets:      []model.AgentID{model.AgentCodex},
			expectedPath: ".codex/config.toml",
			expected:     "\n[mcp_servers.context7]\nurl = \"https://mcp.context7.com/mcp\"\n",
		},
		{
			name:         "context7 opencode merged MCP entry",
			components:   []string{"context7"},
			targets:      []model.AgentID{model.AgentOpenCode},
			expectedPath: ".config/opencode/opencode.json",
			expected:     "\"mcp\": {\n    \"context7\": {\n      \"enabled\": true,\n      \"type\": \"remote\",\n      \"url\": \"https://mcp.context7.com/mcp\"\n    }\n  },\n",
		},
		{
			name:         "permissions claude merged settings",
			components:   []string{"permissions"},
			targets:      []model.AgentID{model.AgentClaudeCode},
			expectedPath: ".claude/settings.json",
			expected:     "\"permissions\": {\n    \"defaultMode\": \"bypassPermissions\",\n    \"deny\": [\n      \"Bash(rm -rf /)\",\n      \"Bash(sudo rm -rf /)\",\n      \"Bash(rm -rf ~)\",\n      \"Bash(sudo rm -rf ~)\",\n      \"Read(**/.env)\",\n      \"Edit(**/.env)\",\n      \"Read(**/.env.*)\",\n      \"Edit(**/.env.*)\",\n      \"Read(**/.ssh/**)\",\n      \"Edit(**/.ssh/**)\",\n      \"Read(**/.credentials/**)\",\n      \"Edit(**/.credentials/**)\",\n      \"Read(**/Library/Keychains/**)\",\n      \"Edit(**/Library/Keychains/**)\",\n      \"Read(**/.aws/credentials)\",\n      \"Edit(**/.aws/credentials)\",\n      \"Read(**/.config/gh/hosts.yml)\",\n      \"Edit(**/.config/gh/hosts.yml)\",\n      \"Read(**/*.pem)\",\n      \"Edit(**/*.pem)\",\n      \"Read(**/*.key)\",\n      \"Edit(**/*.key)\",\n      \"Read(**/secrets/**)\",\n      \"Edit(**/secrets/**)\",\n      \"Read(**/credentials.json)\",\n      \"Edit(**/credentials.json)\"\n    ]\n  }\n}",
		},
		{
			name:         "permissions codex merged profile",
			components:   []string{"permissions"},
			targets:      []model.AgentID{model.AgentCodex},
			expectedPath: ".codex/config.toml",
			expected:     "approval_policy = \"on-request\"\ndefault_permissions = \"takt-dev\"\n",
		},
		{
			name:         "permissions opencode merged rules",
			components:   []string{"permissions"},
			targets:      []model.AgentID{model.AgentOpenCode},
			expectedPath: ".config/opencode/opencode.json",
			expected:     "\"permission\": {\n    \"bash\": {\n      \"*\": \"allow\",\n      \"git commit *\": \"ask\",\n      \"git push\": \"ask\",\n      \"git push *\": \"ask\",\n      \"git push --force *\": \"ask\",\n      \"git rebase *\": \"ask\",\n      \"git reset --hard *\": \"ask\"\n    },\n",
		},
		{
			name:         "theme opencode only",
			components:   []string{"theme"},
			targets:      []model.AgentID{model.AgentOpenCode},
			expectedPath: ".config/opencode/opencode.json",
			expected:     "\"theme\": \"takt-kanagawa\"\n",
		},
		{
			name:         "claude-theme standalone theme file",
			components:   []string{"claude-theme"},
			targets:      []model.AgentID{model.AgentClaudeCode},
			expectedPath: ".claude/themes/takt.json",
			expected:     "{\n  \"name\": \"Takt\",\n  \"base\": \"dark\",\n  \"overrides\": {\n    \"briefLabelYou\": \"#DCA561\",\n    \"chromeYellow\": \"#DCA561\",\n    \"diffAdded\": \"#3F4A2D\",\n    \"diffAddedWord\": \"#76946A\",\n    \"diffRemoved\": \"#5C3838\",\n    \"diffRemovedWord\": \"#C34043\",\n    \"rainbow_yellow\": \"#DCA561\",\n    \"yellow_FOR_SUBAGENTS_ONLY\": \"#DCA561\"\n  }\n}\n",
		},
		{
			name:         "opencode-takt-logo plugin asset",
			components:   []string{"opencode-takt-logo"},
			targets:      []model.AgentID{model.AgentOpenCode},
			expectedPath: ".config/opencode/tui-plugins/takt-logo.tsx",
			expected:     "const plugin = { id: \"takt-logo\", tui }\nexport default plugin\n",
		},
		{
			name:         "opencode-takt-logo registration",
			components:   []string{"opencode-takt-logo"},
			targets:      []model.AgentID{model.AgentOpenCode},
			expectedPath: ".config/opencode/tui.json",
			expected:     "{\n  \"$schema\": \"https://opencode.ai/tui.json\",\n  \"plugin\": [\n    \"./tui-plugins/takt-logo.tsx\"\n  ]\n}\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := validPlanRequest()
			request.Targets = tc.targets
			request.Components = tc.components
			plans, err := BuildTargetPlans(request)
			if err != nil {
				t.Fatalf("BuildTargetPlans() error = %v", err)
			}
			if len(plans) != 1 {
				t.Fatalf("plans = %d, want 1", len(plans))
			}
			plan := plans[0]
			artifact := findArtifact(plan, tc.expectedPath)
			if !bytes.Contains(artifact.Content, []byte(tc.expected)) {
				t.Fatalf("artifact %q missing expected content:\nwant snippet:\n%s\ngot:\n%s", tc.expectedPath, tc.expected, artifact.Content)
			}
			if !slices.Contains(plan.ManagedPaths, tc.expectedPath) {
				t.Errorf("managed paths %v missing %q", plan.ManagedPaths, tc.expectedPath)
			}
			if got := artifactPaths(plan.Artifacts); !slices.Equal(got, plan.ManagedPaths) {
				t.Errorf("managed paths %v != artifact paths %v", plan.ManagedPaths, got)
			}
		})
	}
}

func TestBuildTargetPlansClaudeThemeMergesSettings(t *testing.T) {
	t.Run("fresh settings", func(t *testing.T) {
		request := validPlanRequest()
		request.Targets = []model.AgentID{model.AgentClaudeCode}
		request.Components = []string{"theme"}
		request.Claude.Settings.Settings = nil

		plans, err := BuildTargetPlans(request)
		if err != nil {
			t.Fatalf("BuildTargetPlans() error = %v", err)
		}
		settingsCount := 0
		for _, artifact := range plans[0].Artifacts {
			if artifact.Path == ".claude/settings.json" {
				settingsCount++
			}
		}
		if settingsCount != 1 {
			t.Fatalf("Claude settings artifacts = %d, want 1", settingsCount)
		}
		settings := findArtifact(plans[0], ".claude/settings.json")
		want := "{\n  \"theme\": \"takt-kanagawa\"\n}\n"
		if string(settings.Content) != want {
			t.Fatalf("Claude settings bytes:\nwant:\n%s\ngot:\n%s", want, settings.Content)
		}
	})

	t.Run("existing settings survive", func(t *testing.T) {
		request := validPlanRequest()
		request.Targets = []model.AgentID{model.AgentClaudeCode}
		request.Components = []string{"theme"}

		plans, err := BuildTargetPlans(request)
		if err != nil {
			t.Fatalf("BuildTargetPlans() error = %v", err)
		}
		settings := findArtifact(plans[0], ".claude/settings.json")
		for _, want := range []string{"\"enabled\": true", "\"theme\": \"takt-kanagawa\""} {
			if !bytes.Contains(settings.Content, []byte(want)) {
				t.Errorf("Claude settings missing %s:\n%s", want, settings.Content)
			}
		}
	})
}

func TestBuildTargetPlansComponentRoutingExcludesOtherTargets(t *testing.T) {
	request := validPlanRequest()
	request.Targets = []model.AgentID{model.AgentClaudeCode, model.AgentCodex, model.AgentOpenCode}
	request.Components = []string{"theme", "claude-theme", "opencode-takt-logo"}
	plans, err := BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	byTarget := map[string]TargetPlan{}
	for _, plan := range plans {
		byTarget[plan.Target] = plan
	}

	claudePlan := byTarget[string(model.AgentClaudeCode)]
	if artifact := findArtifact(claudePlan, ".claude/themes/takt.json"); len(artifact.Content) == 0 {
		t.Error("claude plan missing the claude-theme artifact")
	}
	for _, artifact := range claudePlan.Artifacts {
		if strings.HasPrefix(artifact.Path, ".config/opencode/tui") {
			t.Errorf("claude plan gained an OpenCode-only artifact %q", artifact.Path)
		}
	}

	codexPlan := byTarget[string(model.AgentCodex)]
	for _, artifact := range codexPlan.Artifacts {
		if strings.Contains(artifact.Path, "themes/") || strings.HasPrefix(artifact.Path, ".config/opencode/tui") {
			t.Errorf("codex plan gained a theme or logo artifact %q", artifact.Path)
		}
	}
	if config := findArtifact(codexPlan, ".codex/config.toml"); bytes.Contains(config.Content, []byte("takt-kanagawa")) {
		t.Errorf("codex config gained a theme:\n%s", config.Content)
	}

	openCodePlan := byTarget[string(model.AgentOpenCode)]
	for _, path := range []string{".config/opencode/tui.json", ".config/opencode/tui-plugins/takt-logo.tsx"} {
		if artifact := findArtifact(openCodePlan, path); len(artifact.Content) == 0 {
			t.Errorf("opencode plan missing %q", path)
		}
	}
	if config := findArtifact(openCodePlan, ".config/opencode/opencode.json"); !bytes.Contains(config.Content, []byte("\"theme\": \"takt-kanagawa\"")) {
		t.Errorf("opencode config missing the theme key:\n%s", config.Content)
	}
}

func TestBuildTargetPlansRejectsUnknownComponent(t *testing.T) {
	request := validPlanRequest()
	request.Components = []string{"engram"}
	if _, err := BuildTargetPlans(request); err == nil || !strings.Contains(err.Error(), "unknown component") {
		t.Fatalf("BuildTargetPlans() error = %v, want unknown component", err)
	}
}

func TestBuildTargetPlansDefaultRequestHasNoComponentArtifacts(t *testing.T) {
	base := validPlanRequest()
	basePlans, err := BuildTargetPlans(base)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	request := validPlanRequest()
	request.Components = []string{}
	componentPlans, err := BuildTargetPlans(request)
	if err != nil {
		t.Fatalf("BuildTargetPlans() error = %v", err)
	}
	if !slices.EqualFunc(basePlans, componentPlans, equalTargetPlan) {
		t.Fatal("empty component list must produce the default plans")
	}
}
