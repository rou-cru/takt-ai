package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	skillsutil "github.com/rou-cru/takt-ai/takt/skills/testutil"
)

func TestRunInstallReadsJSONFileAndWritesResult(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(t.TempDir(), "request.json")
	payload, err := json.Marshal(setuputil.TestPlanRequest(model.AgentClaudeCode))
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

func TestRunWithoutArgumentsLaunchesTUI(t *testing.T) {
	original := runTUI
	t.Cleanup(func() { runTUI = original })
	called := false
	runTUI = func(input io.Reader, output io.Writer) error {
		called = input != nil && output != nil
		return nil
	}

	var stdout, stderr bytes.Buffer
	if err := run(nil, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !called {
		t.Fatal("TUI launcher was not called")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q, want empty", stdout.String(), stderr.String())
	}
}

func TestRunUsesStdinAndHomeDirectoryDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	payload, err := json.Marshal(setuputil.TestPlanRequest(model.AgentCodex))
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
	payload, err := json.Marshal(setuputil.TestPlanRequest(model.AgentClaudeCode, model.AgentCodex))
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

func TestRunVersionPrintsResolvedVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run([]string{arg}, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if !strings.HasPrefix(stdout.String(), "takt-ai ") {
				t.Errorf("stdout = %q, want version prefix", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	original := buildInfoReader
	t.Cleanup(func() { buildInfoReader = original })

	tests := []struct {
		name        string
		ldflags     string
		buildInfo   *debug.BuildInfo
		buildInfoOK bool
		want        string
	}{
		{name: "ldflags override wins", ldflags: "1.2.3", want: "1.2.3"},
		{name: "build info semver trims v prefix", ldflags: "dev", buildInfo: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, buildInfoOK: true, want: "1.2.3"},
		{name: "devel build info falls back to dev", ldflags: "dev", buildInfo: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, buildInfoOK: true, want: "dev"},
		{name: "missing build info falls back to dev", ldflags: "dev", want: "dev"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buildInfoReader = func() (*debug.BuildInfo, bool) { return tc.buildInfo, tc.buildInfoOK }
			if got := resolveVersion(tc.ldflags); got != tc.want {
				t.Errorf("resolveVersion(%q) = %q, want %q", tc.ldflags, got, tc.want)
			}
		})
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

// TestRunLifecycleDeploysAndRemovesSkills covers the JSON setup interface:
// install deploys embedded skills and records them in the ownership manifest,
// sync preserves local skill modifications, and uninstalling any agent target
// also removes the skill files.
func TestRunLifecycleDeploysAndRemovesSkills(t *testing.T) {
	root := t.TempDir()
	skillPath, embedded := skillsutil.FirstSkill(t)
	payload, err := json.Marshal(setuputil.TestPlanRequest(model.AgentOpenCode))
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := run([]string{"setup", "install", "--root", root}, bytes.NewReader(payload), &stdout, &stderr); err != nil {
		t.Fatalf("install run() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(skillPath))); err != nil {
		t.Fatalf("deployed skill file: %v", err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	entry, ok := manifest.Entries[skillPath]
	if !ok || !slices.Contains(entry.Targets, setup.OwnershipTarget("skills")) {
		t.Fatalf("manifest entry for %q = %+v, want skills ownership", skillPath, entry)
	}

	local := append(append([]byte(nil), embedded...), []byte("\nlocal edit")...)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(skillPath)), local, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := run([]string{"setup", "sync", "--root", root}, bytes.NewReader(payload), &stdout, &stderr); err != nil {
		t.Fatalf("sync run() error = %v", err)
	}
	var syncResult setup.DeploymentResult
	if err := json.Unmarshal(stdout.Bytes(), &syncResult); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(syncResult.Changed, skillPath) {
		t.Fatalf("sync changed = %v, want locally modified skill preserved", syncResult.Changed)
	}
	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(skillPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, local) {
		t.Fatal("sync did not preserve the locally modified skill content")
	}

	// Restore the embedded content before uninstalling: uninstall preserves
	// locally modified managed files by design (#12689).
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(skillPath)), embedded, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := run([]string{"setup", "uninstall", "--root", root}, strings.NewReader(`{"targets":["opencode"]}`), &stdout, &stderr); err != nil {
		t.Fatalf("uninstall run() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(skillPath))); !os.IsNotExist(err) {
		t.Fatalf("skill file after uninstall stat error = %v, want removed", err)
	}
}

func TestDecodeRequestAcceptsComponents(t *testing.T) {
	request, err := decodeRequest(strings.NewReader(`{"targets":["opencode"],"components":["theme","context7"]}`))
	if err != nil {
		t.Fatalf("decodeRequest() error = %v", err)
	}
	want := []string{"theme", "context7"}
	if !slices.Equal(request.Components, want) {
		t.Fatalf("components = %v, want %v", request.Components, want)
	}
}

func TestDecodeRequestStillRejectsUnknownFields(t *testing.T) {
	if _, err := decodeRequest(strings.NewReader(`{"components":["theme"],"bogus":true}`)); err == nil {
		t.Fatal("decodeRequest() error = nil, want unknown field rejection")
	}
}

func TestRunInstallDeploysSelectedComponents(t *testing.T) {
	root := t.TempDir()
	payload, err := json.Marshal(setuputil.TestPlanRequest(model.AgentOpenCode))
	if err != nil {
		t.Fatal(err)
	}
	var request setup.PlanRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatal(err)
	}
	request.Components = []string{"theme", "opencode-takt-logo"}
	payload, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := run([]string{"setup", "install", "--root", root}, bytes.NewReader(payload), &stdout, &stderr); err != nil {
		t.Fatalf("install run() error = %v", err)
	}

	config, err := os.ReadFile(filepath.Join(root, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	if !bytes.Contains(config, []byte(`"theme": "takt-kanagawa"`)) {
		t.Fatalf("opencode.json missing theme merge:\n%s", config)
	}
	for _, deployed := range []string{
		filepath.Join(root, ".config", "opencode", "tui.json"),
		filepath.Join(root, ".config", "opencode", "tui-plugins", "takt-logo.tsx"),
	} {
		if _, err := os.Stat(deployed); err != nil {
			t.Errorf("deployed component artifact: %v", err)
		}
	}
}
