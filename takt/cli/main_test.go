package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	skillsutil "github.com/rou-cru/takt-ai/takt/skills/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// TestMain puts fake opencode, codegraph and npm first on PATH, so setup never touches the host
// tools or the network, and lets this test binary act as the memory owner that memory.Ensure launches.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithFakeTools(m))
}

// TestRunWithoutTerminalRefusesTUI verifies the TUI refuses to start without a terminal.
func TestRunWithoutTerminalRefusesTUI(t *testing.T) {
	res := newSession(t, buildTaktAI(t)).run()
	if res.code == 0 {
		t.Fatal("takt-ai without a terminal succeeded")
	}
	if !strings.Contains(res.stderr, "Usage:") || !strings.Contains(res.stderr, "requires a terminal") {
		t.Fatalf("stderr = %q", res.stderr)
	}
}

// TestRunVersionPrintsResolvedVersion verifies every version flag prints the resolved version.
func TestRunVersionPrintsResolvedVersion(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			res := s.run(arg)
			if res.code != 0 || !strings.HasPrefix(res.stdout, "takt-ai ") || res.stderr != "" {
				t.Errorf("%s = %+v, want the version line", arg, res)
			}
		})
	}
}

// installPayload is the plan request the install tests send, with the selected components set when given.
func installPayload(t *testing.T, components ...string) string {
	t.Helper()
	payload, err := json.Marshal(setuputil.TestPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if components == nil {
		return string(payload)
	}
	var request setup.PlanRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatal(err)
	}
	request.Components = components
	payload, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

// TestRunLifecycleDeploysAndRemovesSkills verifies skills deploy, survive sync edits, and uninstall cleanly.
func TestRunLifecycleDeploysAndRemovesSkills(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	root := t.TempDir()
	skillPath, embedded := skillsutil.FirstSkill(t)
	skillFile := filepath.Join(root, filepath.FromSlash(skillPath))
	payload := installPayload(t)

	if res := s.runIn(payload, "setup", "install", "--root", root, "--yes"); res.code != 0 {
		t.Fatalf("install exited %d: %s", res.code, res.stderr)
	}
	if _, err := os.Stat(skillFile); err != nil {
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
	if err := os.WriteFile(skillFile, local, 0o644); err != nil {
		t.Fatal(err)
	}
	res := s.runIn(payload, "setup", "sync", "--root", root, "--yes", "--json")
	if res.code != 0 {
		t.Fatalf("sync exited %d: %s", res.code, res.stderr)
	}
	var syncResult setup.DeploymentResult
	if err := json.Unmarshal([]byte(res.stdout), &syncResult); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(syncResult.Changed, skillPath) {
		t.Fatalf("sync changed = %v, want locally modified skill preserved", syncResult.Changed)
	}
	current, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, local) {
		t.Fatal("sync did not preserve the locally modified skill content")
	}

	// Uninstall preserves locally modified managed files, so restore the
	// embedded content before uninstalling.
	if err := os.WriteFile(skillFile, embedded, 0o644); err != nil {
		t.Fatal(err)
	}
	// The CLI leaves a memory store's retain-or-discard decision to the TUI (PR-INS-42), so this
	// uninstall runs where no store exists.
	s.env = append(s.env, memory.EnvDir+"="+filepath.Join(t.TempDir(), "absent"))
	if res := s.runIn("{}", "setup", "uninstall", "--root", root, "--yes"); res.code != 0 {
		t.Fatalf("uninstall exited %d: %s", res.code, res.stderr)
	}
	if _, err := os.Stat(skillFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skill file after uninstall stat error = %v, want removed", err)
	}
}

// TestRunInstallBlocksOnAFileOnlyTheTUICanDecide verifies a user's own file at a managed path is reported, not overwritten.
func TestRunInstallBlocksOnAFileOnlyTheTUICanDecide(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	root := t.TempDir()
	owned := filepath.Join(".config", "opencode", "takt", "agents", "takt", "PERSONA.md")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, owned)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, owned), []byte("my own persona"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireRefusal(t, s.runIn(installPayload(t), "setup", "install", "--root", root, "--yes"), owned)
	if got, err := os.ReadFile(filepath.Join(root, owned)); err != nil || string(got) != "my own persona" {
		t.Fatalf("the refused install touched the file: %q, %v", got, err)
	}
}

// TestRunInstallRejectsUnknownRequestFields verifies typos in the request fail instead of being ignored.
func TestRunInstallRejectsUnknownRequestFields(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	if res := s.runIn(`{"components":["context7"],"bogus":true}`, "setup", "install", "--root", t.TempDir(), "--yes"); res.code == 0 {
		t.Fatal("install accepted an unknown request field")
	}
}

// TestRunInstallDeploysSelectedComponents verifies chosen components land in the right files.
func TestRunInstallDeploysSelectedComponents(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	root := t.TempDir()
	if res := s.runIn(installPayload(t, "context7"), "setup", "install", "--root", root, "--yes"); res.code != 0 {
		t.Fatalf("install exited %d: %s", res.code, res.stderr)
	}

	config, err := os.ReadFile(filepath.Join(root, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	if !bytes.Contains(config, []byte(`"context7"`)) {
		t.Fatalf("opencode.json missing the selected component:\n%s", config)
	}
	if _, err := os.Stat(filepath.Join(root, ".config", "opencode", "plugins", "takt-dag", "tui.tsx")); err != nil {
		t.Errorf("deployed component artifact: %v", err)
	}
	cli, err := os.ReadFile(filepath.Join(root, ".config", "opencode", "cli.json"))
	if err != nil || !bytes.Contains(cli, []byte(`"attention"`)) {
		t.Errorf("cli.json must carry the attention settings OpenCode V2 reads there: %v\n%s", err, cli)
	}
}
