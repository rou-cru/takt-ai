package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	skillsutil "github.com/rou-cru/takt-ai/takt/skills/testutil"
)

// TestRunWithoutArgumentsLaunchesTUI verifies empty args start the interactive UI.
func TestRunWithoutArgumentsLaunchesTUI(t *testing.T) {
	original, originalInteractive := runTUI, isInteractive
	t.Cleanup(func() { runTUI, isInteractive = original, originalInteractive })
	isInteractive = func(io.Reader, io.Writer) bool { return true }
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

// TestRunWithoutTerminalRefusesTUI verifies the TUI refuses to start without a terminal.
func TestRunWithoutTerminalRefusesTUI(t *testing.T) {
	original := runTUI
	t.Cleanup(func() { runTUI = original })
	runTUI = func(io.Reader, io.Writer) error {
		t.Fatal("TUI launched without a terminal")
		return nil
	}

	var stdout, stderr bytes.Buffer
	if err := run(nil, strings.NewReader(""), &stdout, &stderr); err == nil {
		t.Fatal("run() error = nil, want non-terminal refusal")
	}
	if !strings.Contains(stderr.String(), "usage:") || !strings.Contains(stderr.String(), "requires a terminal") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// TestRunVersionPrintsResolvedVersion verifies every version flag prints the resolved version.
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

// TestResolveVersion verifies release, build, and dev versions resolve correctly.
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

// TestRunLifecycleDeploysAndRemovesSkills verifies skills deploy, survive sync edits, and uninstall cleanly.
func TestRunLifecycleDeploysAndRemovesSkills(t *testing.T) {
	fakeOpenCodeLifecycle(t)
	root := t.TempDir()
	skillPath, embedded := skillsutil.FirstSkill(t)
	payload, err := json.Marshal(setuputil.TestPlanRequest())
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := run([]string{"setup", "install", "--root", root, "--yes"}, bytes.NewReader(payload), &stdout, &stderr); err != nil {
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
	if err := run([]string{"setup", "sync", "--root", root, "--yes", "--json"}, bytes.NewReader(payload), &stdout, &stderr); err != nil {
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

	// Uninstall preserves locally modified managed files, so restore the
	// embedded content before uninstalling.
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(skillPath)), embedded, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := run([]string{"setup", "uninstall", "--root", root, "--yes"}, strings.NewReader(`{}`), &stdout, &stderr); err != nil {
		t.Fatalf("uninstall run() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(skillPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skill file after uninstall stat error = %v, want removed", err)
	}
}

// TestDecodeRequestAcceptsComponents verifies component selection passes through decoding.
func TestDecodeRequestAcceptsComponents(t *testing.T) {
	request, err := decodeStrict[setup.PlanRequest](strings.NewReader(`{"components":["theme","context7"]}`))
	if err != nil {
		t.Fatalf("decodeStrict() error = %v", err)
	}
	want := []string{"theme", "context7"}
	if !slices.Equal(request.Components, want) {
		t.Fatalf("components = %v, want %v", request.Components, want)
	}
}

// TestDecodeRequestStillRejectsUnknownFields verifies typos in input fail instead of being ignored.
func TestDecodeRequestStillRejectsUnknownFields(t *testing.T) {
	if _, err := decodeStrict[setup.PlanRequest](strings.NewReader(`{"components":["theme"],"bogus":true}`)); err == nil {
		t.Fatal("decodeStrict() error = nil, want unknown field rejection")
	}
}

// TestRunInstallDeploysSelectedComponents verifies chosen components land in the right files.
func TestRunInstallDeploysSelectedComponents(t *testing.T) {
	fakeOpenCodeLifecycle(t)
	root := t.TempDir()
	payload, err := json.Marshal(setuputil.TestPlanRequest())
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
	if err := run([]string{"setup", "install", "--root", root, "--yes"}, bytes.NewReader(payload), &stdout, &stderr); err != nil {
		t.Fatalf("install run() error = %v", err)
	}

	// V2 keeps theme selection in the terminal client's own cli.json.
	config, err := os.ReadFile(filepath.Join(root, ".config", "opencode", "cli.json"))
	if err != nil {
		t.Fatalf("read cli.json: %v", err)
	}
	if !bytes.Contains(config, []byte(`"name": "takt"`)) {
		t.Fatalf("cli.json missing theme selection:\n%s", config)
	}
	for _, deployed := range []string{
		filepath.Join(root, ".config", "opencode", "cli.json"),
		filepath.Join(root, ".config", "opencode", "plugins", "takt-dag", "tui.tsx"),
	} {
		if _, err := os.Stat(deployed); err != nil {
			t.Errorf("deployed component artifact: %v", err)
		}
	}
}

// TestBlockOnConflictsHonorsPriorAcceptanceOnly verifies accepted edits pass while new conflicts block.
func TestBlockOnConflictsHonorsPriorAcceptanceOnly(t *testing.T) {
	preserve, err := blockOnConflicts("install", []setup.ConflictEntry{
		{Path: "a", Reason: "user-edited", Impact: setup.ImpactUncertain, Accepted: true},
		{Path: "b", Reason: "user-edited", Impact: setup.ImpactUnrelated},
	})
	if err != nil || len(preserve) != 2 {
		t.Fatalf("accepted+unrelated must pass: preserve=%v err=%v", preserve, err)
	}
	if _, err := blockOnConflicts("install", []setup.ConflictEntry{{Path: "c", Reason: "user-edited", Impact: setup.ImpactUncertain}}); err == nil {
		t.Fatal("unaccepted uncertain conflict must block")
	}
}

// fakeOpenCodeLifecycle exercises the production preflight and reload through
// the process boundary without using the developer's running OpenCode service.
func fakeOpenCodeLifecycle(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := `#!/bin/sh
case "$*" in
  'api GET /api/info') printf '%s' '{"version":"2.0.16"}' ;;
  'api GET /api/model') printf '%s' '{"location":{},"data":[]}' ;;
  'service restart'|'api POST /api/location/reload') exit 0 ;;
  *) echo "unexpected OpenCode invocation: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestExecuteSetupPreservesPartialResultOnFailure(t *testing.T) {
	original := runLifecycle
	t.Cleanup(func() { runLifecycle = original })
	wantErr := errors.New("provider action failed")
	runLifecycle = func(context.Context, string, string, setup.PlanRequest, ...string) (lifecycle.LifecycleResult, error) {
		return lifecycle.LifecycleResult{Changed: []string{"config.json"}}, wantErr
	}

	result, outcome, err := executeSetup(context.Background(), "install", t.TempDir(), setup.PlanRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("executeSetup() error = %v, want %v", err, wantErr)
	}
	deployed, ok := result.(setup.DeploymentResult)
	if !ok || !slices.Equal(deployed.Changed, []string{"config.json"}) || !slices.Equal(outcome.Changed, []string{"config.json"}) {
		t.Fatalf("partial result = %#v, lifecycle = %#v", result, outcome)
	}
}

func TestRenderPartialResultTextIncludesAppliedWork(t *testing.T) {
	var output bytes.Buffer
	err := renderPartialResultText(&output, "install", setup.DeploymentResult{Changed: []string{"config.json"}}, errors.New("provider action failed"))
	if err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "Install failed after applying partial work") || !strings.Contains(text, "Install partial: 1 changed") || !strings.Contains(text, "[changed] config.json") {
		t.Fatalf("partial output = %q", text)
	}
}
