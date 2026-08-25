package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/claude"
	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestRunInstallReadsJSONFileAndWritesResult(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(t.TempDir(), "request.json")
	payload, err := json.Marshal(testPlanRequest(model.AgentClaudeCode))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err = run([]string{"setup", "install", "--root", root, "--input", inputPath}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"changed"`) {
		t.Fatalf("stdout = %q, want DeploymentResult JSON", stdout.String())
	}
	var result setup.DeploymentResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Changed, ".claude/CLAUDE.md") {
		t.Errorf("changed = %v, want Claude prompt", result.Changed)
	}
}

func TestRunUsesStdinAndHomeDirectoryDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	payload, err := json.Marshal(testPlanRequest(model.AgentCodex))
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err = run([]string{"setup", "sync"}, bytes.NewReader(payload), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var result setup.DeploymentResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Changed, ".codex/AGENTS.md") {
		t.Errorf("changed = %v, want Codex prompt", result.Changed)
	}
	if _, err := os.Stat(filepath.Join(root, ".codex", "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
}

func TestRunUninstallRemovesInstalledFilesForSelectedTarget(t *testing.T) {
	root := t.TempDir()
	payload, err := json.Marshal(testPlanRequest(model.AgentClaudeCode, model.AgentCodex))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"setup", "install", "--root", root}, bytes.NewReader(payload), &stdout, &stderr); err != nil {
		t.Fatalf("install run() error = %v", err)
	}

	stdout.Reset()
	uninstallPayload := strings.NewReader(`{"targets":["claude-code"]}`)
	if err := run([]string{"setup", "uninstall", "--root", root}, uninstallPayload, &stdout, &stderr); err != nil {
		t.Fatalf("uninstall run() error = %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var result setup.UninstallResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) == 0 || !slices.ContainsFunc(result.Removed, func(path string) bool { return strings.HasPrefix(path, ".claude/") }) {
		t.Errorf("removed = %v, want Claude files", result.Removed)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Errorf(".claude stat error = %v, want removed", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".codex", "AGENTS.md")); err != nil {
		t.Errorf("codex files must survive claude-scoped uninstall: %v", err)
	}
	if _, err := setup.LoadOwnershipManifest(root); err != nil {
		t.Errorf("manifest should keep codex entries: %v", err)
	}
}

func TestRunRejectsInvalidCommandAndJSON(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		input string
		want  string
	}{
		{name: "invalid command", args: []string{"setup", "remove"}, want: "usage:"},
		{name: "invalid JSON", args: []string{"setup", "install", "--root", t.TempDir()}, input: "{", want: "invalid input:"},
		{name: "unsupported uninstall target", args: []string{"setup", "uninstall", "--root", t.TempDir()}, input: `{"targets":["cursor"]}`, want: "unsupported target"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, strings.NewReader(tc.input), &stdout, &stderr)
			if err == nil {
				t.Fatal("run() error = nil, want error")
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want substring %q", stderr.String(), tc.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func testPlanRequest(targets ...model.AgentID) setup.PlanRequest {
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
