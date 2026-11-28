package lifecycle_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rou-cru/takt-ai/takt/archify"
	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/memory/embed"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// TestMain keeps every lifecycle test off the host PATH, the network, and the user's memory, and lets the
// test binary act as the memory owner that install launches.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithFakeTools(m))
}

// installRuntime acquires a fake model and starts no memory service, as image assembly does.
func installRuntime(t *testing.T) lifecycle.Runtime {
	t.Helper()
	return lifecycle.Runtime{Model: testutil.FakeModel(t), Archify: testutil.FakeArchify(t)}
}

// startingRuntime also runs the memory service, in a memory directory private to the test.
func startingRuntime(t *testing.T) lifecycle.Runtime {
	t.Helper()
	testutil.MemoryDir(t)
	runtime := installRuntime(t)
	runtime.StartMemory = true
	return runtime
}

// registeredCommand reads the command of the MCP server key from the installed OpenCode config.
func registeredCommand(t *testing.T, root, key string) []string {
	t.Helper()
	data, err := os.ReadFile(model.OpenCodeConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		MCP struct {
			Servers map[string]struct {
				Command []string `json:"command"`
			} `json:"servers"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse opencode.json: %v", err)
	}
	return config.MCP.Servers[key].Command
}

func TestInstallRegistersTheMemoryServerAndConfirmsItReady(t *testing.T) {
	root := t.TempDir()
	runtime := startingRuntime(t)
	if _, err := runtime.Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	command := registeredCommand(t, root, memory.ServerKey)
	if len(command) != 4 || !reflect.DeepEqual(command[1:], []string{"memory", "mcp", "--crew"}) {
		t.Fatalf("takt_memory command = %v, want <takt-ai> memory mcp --crew", command)
	}
	dir, err := memory.ResolveDir()
	if err != nil {
		t.Fatal(err)
	}
	status, err := memory.Probe(context.Background(), dir)
	if err != nil || status.State != memory.StateReady {
		t.Fatalf("status after install = %+v, %v; want ready", status, err)
	}
	for _, file := range runtime.Model.Files {
		if _, err := os.Stat(filepath.Join(embed.ModelDir(root), file.Name)); err != nil {
			t.Errorf("model file %s was not acquired: %v", file.Name, err)
		}
	}
}

func TestImageInstallAcquiresTheModelAndCreatesNoStore(t *testing.T) {
	root := t.TempDir()
	dir := testutil.MemoryDir(t)
	runtime := installRuntime(t)
	if _, err := runtime.Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, memory.StoreFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("image install created a store: %v", err)
	}
	if _, err := memory.Probe(context.Background(), dir); !errors.Is(err, memory.ErrUnreachable) {
		t.Fatalf("image install started a service: %v", err)
	}
	if _, err := os.Stat(filepath.Join(embed.ModelDir(root), runtime.Model.Files[0].Name)); err != nil {
		t.Fatalf("image install did not acquire the model: %v", err)
	}
	if len(registeredCommand(t, root, memory.ServerKey)) == 0 {
		t.Fatal("image install did not register takt_memory")
	}
}

func TestSyncEnsuresTheOwnerAndNeverCreatesAStore(t *testing.T) {
	root := t.TempDir()
	runtime := startingRuntime(t)
	request := setuputil.TestPlanRequest()
	if _, err := runtime.Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	if _, err := runtime.Run(context.Background(), "sync", root, request); err != nil {
		t.Fatalf("Run(sync) over a ready service error = %v", err)
	}

	bare := t.TempDir()
	dir := testutil.MemoryDir(t)
	if _, err := installRuntime(t).Run(context.Background(), "install", bare, request); err != nil {
		t.Fatalf("Run(install) without service error = %v", err)
	}
	runtime.StartMemory = true
	_, err := runtime.Run(context.Background(), "sync", bare, request)
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("Run(sync) without a store error = %v, want it reported not ready", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, memory.StoreFile)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sync created a store: %v", statErr)
	}
}

func TestInstallReportsAMemoryServiceThatCannotStart(t *testing.T) {
	root := t.TempDir()
	runtime := startingRuntime(t)
	t.Setenv(memory.EnvDir, filepath.Join(t.TempDir(), strings.Repeat("d", 120)))
	result, err := runtime.Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err == nil || !strings.Contains(err.Error(), "memory service did not become ready") {
		t.Fatalf("Run(install) error = %v, want the unready memory service reported", err)
	}
	if len(result.Changed) == 0 {
		t.Error("the files written before the failure were not reported")
	}
	if setup.IsInstalled(root) {
		t.Error("an install whose memory service is not ready was recorded as installed")
	}
}

func TestInstallFailsBeforeWritingWhenTheModelIsUnavailable(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	runtime := installRuntime(t)
	runtime.Model = &embed.Manifest{BaseURL: server.URL, Files: runtime.Model.Files}
	result, err := runtime.Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err == nil || !strings.Contains(err.Error(), "memory model unavailable") {
		t.Fatalf("Run(install) error = %v, want the model failure", err)
	}
	if len(result.Changed) != 0 {
		t.Errorf("Changed = %v, want none: the prerequisite fails before writes", result.Changed)
	}
	if _, statErr := os.Stat(model.OpenCodeConfigPath(root)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("OpenCode config exists after the prerequisite failure: %v", statErr)
	}
}

// TestInstallKeepsAModelAlreadyInstalled verifies a later install needs no network for a model whose
// files are present and match the manifest.
func TestInstallKeepsAModelAlreadyInstalled(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	runtime := installRuntime(t)
	if _, err := runtime.Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("first install error = %v", err)
	}
	offline := *runtime.Model
	offline.BaseURL = "http://127.0.0.1:1"
	runtime.Model = &offline
	if _, err := runtime.Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("install with the model present and the host unreachable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(embed.ModelDir(root), offline.Files[0].Name)); err != nil {
		t.Fatalf("model file missing: %v", err)
	}
}

func TestInstallCancelledDuringModelAcquisitionAppliesNothing(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := installRuntime(t).Run(ctx, "install", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run() error = %v, want nil for a cancellation", err)
	}
	if result.Outcome != lifecycle.OutcomeCancelledNothingApplied {
		t.Errorf("Outcome = %v, want %v", result.Outcome, lifecycle.OutcomeCancelledNothingApplied)
	}
	if len(result.NotApplied) == 0 {
		t.Error("NotApplied is empty, want the planned artifacts")
	}
	if setup.IsInstalled(root) {
		t.Error("a cancelled install was recorded as installed")
	}
}

func TestInstallFailsBeforeWritingWhenCodegraphIsUnavailable(t *testing.T) {
	isolateNpm(t, npmFails)
	root := t.TempDir()
	result, err := installRuntime(t).Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err == nil || !strings.Contains(err.Error(), "codegraph capability unavailable") {
		t.Fatalf("Run() error = %v, want the CodeGraph prerequisite failure", err)
	}
	if len(result.Changed) != 0 {
		t.Fatalf("Changed = %v, the prerequisite should fail before writes", result.Changed)
	}
}

func TestInstallRecordsCodegraphAndInjectsItsResolvedCommand(t *testing.T) {
	root := t.TempDir()
	result, err := installRuntime(t).Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Actions, "codegraph-install") {
		t.Fatalf("Actions = %v, want codegraph-install", result.Actions)
	}
	resolved, err := exec.LookPath("codegraph")
	if err != nil {
		t.Fatal(err)
	}
	if command := registeredCommand(t, root, "codegraph"); len(command) == 0 || command[0] != resolved {
		t.Fatalf("codegraph command = %v, want the resolved path %s", command, resolved)
	}
}

func TestUninstallNeverTouchesReusedCodegraph(t *testing.T) {
	root := t.TempDir()
	reused, err := exec.LookPath("codegraph")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(reused)
	if err != nil {
		t.Fatal(err)
	}
	request := setuputil.TestPlanRequest()
	if _, err := installRuntime(t).Run(context.Background(), "install", root, request); err != nil {
		t.Fatal(err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, owned := manifest.Entries[codegraph.ManagedMarkerKey()]; owned {
		t.Fatal("a reused user binary was recorded as Takt-owned")
	}
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", root, request); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(reused); err != nil || string(after) != string(before) {
		t.Fatalf("reused binary was touched: %q, %v", after, err)
	}
}

func TestInstallReportsProgressAndReload(t *testing.T) {
	root := t.TempDir()
	var messages []string
	reloaded := 0
	runtime := installRuntime(t)
	runtime.Progress = func(progress setup.DeploymentProgress) { messages = append(messages, progress.Message) }
	runtime.Reload = func(context.Context) error { reloaded++; return nil }
	result, err := runtime.Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded != 1 || !result.ReloadAttempted || result.ReloadError != nil {
		t.Errorf("reloaded = %d, ReloadAttempted = %v, ReloadError = %v; want one clean reload", reloaded, result.ReloadAttempted, result.ReloadError)
	}
	for _, want := range []string{"Preparing installation plan", "Acquiring the memory model", "Installing memory integration", "Installing CodeGraph integration", "Recording installation", "Reloading OpenCode"} {
		if !slices.Contains(messages, want) {
			t.Errorf("progress messages = %v, missing %q", messages, want)
		}
	}
}

// TestInstallReloadFailureDoesNotRollBack verifies a reload failure is reported separately: the files
// are valid on disk and stay recorded.
func TestInstallReloadFailureDoesNotRollBack(t *testing.T) {
	root := t.TempDir()
	sentinel := errors.New("reload refused")
	runtime := installRuntime(t)
	runtime.Reload = func(context.Context) error { return sentinel }
	result, err := runtime.Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: a reload failure is not an install failure", err)
	}
	if !result.ReloadAttempted || !errors.Is(result.ReloadError, sentinel) {
		t.Errorf("ReloadAttempted = %v, ReloadError = %v; want the reload failure surfaced", result.ReloadAttempted, result.ReloadError)
	}
	if !setup.IsInstalled(root) {
		t.Error("a reload failure un-recorded the installation")
	}
}

func TestInstallWrapsHandshakeFailure(t *testing.T) {
	sentinel := errors.New("socket closed")
	runtime := installRuntime(t)
	runtime.OpenCodeHandshake = func(context.Context) error { return sentinel }
	_, err := runtime.Run(context.Background(), "install", t.TempDir(), setuputil.TestPlanRequest())
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "OpenCode V2 preflight failed") {
		t.Fatalf("Run() error = %v, want the handshake error wrapped as a preflight failure", err)
	}
}

func TestInstallRunsTheHandshakeOnceBeforeDeployment(t *testing.T) {
	handshakes := 0
	runtime := installRuntime(t)
	runtime.OpenCodeHandshake = func(context.Context) error { handshakes++; return nil }
	if _, err := runtime.Run(context.Background(), "install", t.TempDir(), setuputil.TestPlanRequest()); err != nil {
		t.Fatal(err)
	}
	if handshakes != 1 {
		t.Errorf("handshake ran %d times, want exactly once", handshakes)
	}
}

func TestInstallRejectsAnUnknownComponent(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	request.Components = []string{"no-such-component"}
	if _, err := installRuntime(t).Run(context.Background(), "install", root, request); err == nil {
		t.Fatal("Run(install, unknown component) error = nil, want an error")
	}
	if setup.IsInstalled(root) {
		t.Error("a rejected plan was recorded as installed")
	}
}

func TestInstallInjectionFailureIsNotRecorded(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(model.OpenCodeConfigPath(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installRuntime(t).Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err == nil {
		t.Fatal("Run() error = nil, want the injection failure")
	}
	if setup.IsInstalled(root) {
		t.Error("a failed install was recorded as installed")
	}
}

func TestUninstallCancelledReturnsTypedOutcome(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	if _, err := installRuntime(t).Run(context.Background(), "install", root, request); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (lifecycle.Runtime{}).Run(ctx, "uninstall", root, request)
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v, want nil for a cancellation", err)
	}
	if result.Outcome == lifecycle.OutcomeCompleted {
		t.Errorf("Outcome = %v, want a cancelled outcome", result.Outcome)
	}
	if !setup.IsInstalled(root) {
		t.Error("a cancelled uninstall forgot the installation, want it kept: part of it remains")
	}
}

// recordViaService records one global entry through the /external endpoint of dir's owner, the way a
// caller outside the crew reaches the store. It returns an error when the service is unavailable.
func recordViaService(ctx context.Context, dir, title string) error {
	sock := filepath.Join(dir, memory.SocketFile)
	client := mcp.NewClient(&mcp.Implementation{Name: "uninstall-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: "http://memory" + memory.PathExternal,
		HTTPClient: &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Meta: mcp.Meta{memory.MetaExternalSession: "uninstall-test"}, Name: memory.ToolRecord, Arguments: map[string]any{
		"nature": memory.NatureObservation, "scope": memory.ScopeGlobal, "title": title, "content": "body of " + title, "evidence": "seen",
	}})
	if err != nil {
		return err
	}
	if res.IsError {
		text, _ := res.Content[0].(*mcp.TextContent)
		return fmt.Errorf("record %q refused: %s", title, text.Text)
	}
	return nil
}

// installedWithMemory installs into a fresh root and starts the memory service in a private directory
// holding count entries.
func installedWithMemory(t *testing.T, count int) (root, dir string) {
	t.Helper()
	dir = testutil.MemoryDir(t)
	root = t.TempDir()
	if _, err := installRuntime(t).Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	if _, err := memory.Start(context.Background(), dir, true); err != nil {
		t.Fatalf("memory.Start: %v", err)
	}
	for i := range count {
		if err := recordViaService(context.Background(), dir, fmt.Sprintf("entry %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	return root, dir
}

// setAsideDirs lists the directories uninstall set the memory directory aside into.
func setAsideDirs(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(dir + ".removed-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, path := range found {
			_ = os.RemoveAll(path)
		}
	})
	return found
}

func entryTotal(t *testing.T, dir string) int64 {
	t.Helper()
	store, err := memory.Open(dir)
	if err != nil {
		t.Fatalf("memory.Open(%s): %v", dir, err)
	}
	defer func() { _ = store.Close() }()
	_, total, err := store.Coverage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func requireMemoryGone(t *testing.T, root, dir string, result lifecycle.LifecycleResult) {
	t.Helper()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("memory directory after uninstall: %v, want removed", err)
	}
	if left := setAsideDirs(t, dir); len(left) != 0 {
		t.Errorf("set-aside directories left behind: %v", left)
	}
	if _, err := os.Stat(embed.ModelDir(root)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("model after uninstall: %v, want removed", err)
	}
	if _, err := memory.Probe(context.Background(), dir); !errors.Is(err, memory.ErrUnreachable) {
		t.Errorf("owner after uninstall: %v, want unreachable", err)
	}
	if raw, _ := os.ReadFile(model.OpenCodeConfigPath(root)); strings.Contains(string(raw), memory.ServerKey) {
		t.Errorf("%s is still registered in %s", memory.ServerKey, model.OpenCodeConfigPath(root))
	}
	if len(result.Incomplete) != 0 {
		t.Errorf("Incomplete = %v, want none", result.Incomplete)
	}
}

func TestUninstallRetainsTheMemoryBackup(t *testing.T) {
	root, dir := installedWithMemory(t, 2)
	runtime := lifecycle.Runtime{RetainMemory: true}
	result, err := runtime.Run(context.Background(), "uninstall", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	requireMemoryGone(t, root, dir, result)
	wantDir := filepath.Join(result.RetainedDir, setup.RetainedMemoryDirName)
	if filepath.Dir(result.RetainedMemory) != wantDir || !strings.HasPrefix(result.RetainedDir, filepath.Join(root, setup.RetainedDirName)) {
		t.Fatalf("backup at %s in %s, want %s under %s", result.RetainedMemory, result.RetainedDir, wantDir, filepath.Join(root, setup.RetainedDirName))
	}
	if got := entryTotal(t, wantDir); got != 2 {
		t.Errorf("entries in the backup = %d, want 2", got)
	}
}

func TestUninstallDiscardsTheMemory(t *testing.T) {
	root, dir := installedWithMemory(t, 1)
	result, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	requireMemoryGone(t, root, dir, result)
	if result.RetainedMemory != "" || result.RetainedDir != "" {
		t.Errorf("discard retained %q in %q", result.RetainedMemory, result.RetainedDir)
	}
	if _, err := os.Stat(filepath.Join(root, setup.RetainedDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("retained directory after discard: %v, want none", err)
	}
}

func TestUninstallKeepsTheMemoryWhenTheBackupFails(t *testing.T) {
	root, dir := installedWithMemory(t, 1)
	// A file where the retained directory belongs makes the backup impossible.
	if err := os.WriteFile(filepath.Join(root, setup.RetainedDirName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (lifecycle.Runtime{RetainMemory: true}).Run(context.Background(), "uninstall", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	kept := setAsideDirs(t, dir)
	if len(kept) != 1 {
		t.Fatalf("set-aside directories = %v, want exactly one kept", kept)
	}
	if len(result.Incomplete) != 1 || !strings.Contains(result.Incomplete[0], kept[0]) {
		t.Fatalf("Incomplete = %v, want one entry naming %s", result.Incomplete, kept[0])
	}
	if got := entryTotal(t, kept[0]); got != 1 {
		t.Errorf("entries in the kept directory = %d, want 1", got)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("memory directory at its configured path: %v, want none", err)
	}
	if result.RetainedMemory != "" {
		t.Errorf("RetainedMemory = %q after a failed backup", result.RetainedMemory)
	}
	if !setup.IsInstalled(root) {
		t.Error("the installation was forgotten while its memory stayed set aside")
	}
}

// TestUninstallResumesAMemoryRetirementThatDidNotFinish proves a failed retirement leaves the
// installation recorded, and that the next uninstall finishes the store an earlier run set aside.
func TestUninstallResumesAMemoryRetirementThatDidNotFinish(t *testing.T) {
	root, dir := installedWithMemory(t, 1)
	blocker := filepath.Join(root, setup.RetainedDirName)
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	retain := lifecycle.Runtime{RetainMemory: true}
	if _, err := retain.Run(context.Background(), "uninstall", root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("first Run(uninstall) error = %v", err)
	}
	if !setup.IsInstalled(root) {
		t.Fatal("the installation was forgotten while its memory stayed set aside")
	}
	if kept := setAsideDirs(t, dir); len(kept) != 1 {
		t.Fatalf("set-aside directories = %v, want one kept", kept)
	}

	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	result, err := retain.Run(context.Background(), "uninstall", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("second Run(uninstall) error = %v", err)
	}
	if len(result.Incomplete) != 0 {
		t.Fatalf("Incomplete = %v, want the resumed retirement to finish", result.Incomplete)
	}
	if left := setAsideDirs(t, dir); len(left) != 0 {
		t.Errorf("set-aside directories = %v after the resumed retirement, want none", left)
	}
	if result.RetainedMemory == "" {
		t.Fatal("the resumed retirement did not retain the store")
	}
	if _, err := os.Stat(result.RetainedMemory); err != nil {
		t.Errorf("retained backup: %v", err)
	}
	if setup.IsInstalled(root) {
		t.Error("the installation is still recorded after the resumed retirement finished")
	}
}

// TestUninstallLosesNoAcknowledgedWrite writes through the service while uninstall runs: every write
// the service acknowledged is in the backup, and once the owner is stopped the writer is refused.
func TestUninstallLosesNoAcknowledgedWrite(t *testing.T) {
	root, dir := installedWithMemory(t, 0)
	ctx := context.Background()
	var (
		wg      sync.WaitGroup
		acked   int
		refused = make(chan error, 1)
		started = make(chan struct{})
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			if err := recordViaService(ctx, dir, fmt.Sprintf("concurrent %d", i)); err != nil {
				refused <- err
				return
			}
			acked++
			if acked == 1 {
				close(started)
			}
		}
	}()
	<-started
	result, err := (lifecycle.Runtime{RetainMemory: true}).Run(ctx, "uninstall", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	wg.Wait()
	if err := <-refused; err == nil {
		t.Fatal("the writer ended without a refusal")
	}
	requireMemoryGone(t, root, dir, result)
	// One write may commit without being acknowledged; none acknowledged may be missing.
	if got := entryTotal(t, filepath.Dir(result.RetainedMemory)); got < int64(acked) || got > int64(acked)+1 {
		t.Errorf("entries in the backup = %d, acknowledged writes = %d", got, acked)
	}
}

// TestUninstallRetainsNothingFromAMemoryWithoutAStore covers a memory directory that never got a store:
// retaining it has nothing to back up and is not incomplete work.
func TestUninstallRetainsNothingFromAMemoryWithoutAStore(t *testing.T) {
	dir := filepath.Join(testutil.MemoryDir(t), "m")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(memory.EnvDir, dir)
	result, err := (lifecycle.Runtime{RetainMemory: true}).Run(context.Background(), "uninstall", t.TempDir(), setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	if len(result.Incomplete) != 0 || result.RetainedMemory != "" || result.RetainedDir != "" {
		t.Fatalf("result = %+v, want nothing retained and nothing incomplete", result)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("memory directory: %v, want it retired", err)
	}
}

// TestUninstallRetainsEveryPendingMemory covers a live store plus one an earlier uninstall set aside:
// both are backed up and the earlier backup is still reported.
func TestUninstallRetainsEveryPendingMemory(t *testing.T) {
	root, dir := installedWithMemory(t, 1)
	pending := dir + ".removed-1"
	if _, err := memory.Start(context.Background(), pending, true); err != nil {
		t.Fatalf("memory.Start(%s): %v", pending, err)
	}
	if err := memory.StopOwner(context.Background(), pending); err != nil {
		t.Fatalf("memory.StopOwner(%s): %v", pending, err)
	}
	result, err := (lifecycle.Runtime{RetainMemory: true}).Run(context.Background(), "uninstall", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	if len(result.Incomplete) != 0 || result.RetainedMemory == "" {
		t.Fatalf("result = %+v, want every memory retained", result)
	}
	if !slices.ContainsFunc(result.Actions, func(action string) bool { return strings.HasPrefix(action, "retained memory backup: ") }) {
		t.Fatalf("Actions = %v, want the earlier backup reported", result.Actions)
	}
	if left := setAsideDirs(t, dir); len(left) != 0 {
		t.Errorf("set-aside directories = %v, want none", left)
	}
}

// TestUninstallWithAMemoryDirectoryWithoutParentHasNothingToRetire covers a configured memory path whose
// parent does not exist.
func TestUninstallWithAMemoryDirectoryWithoutParentHasNothingToRetire(t *testing.T) {
	t.Setenv(memory.EnvDir, filepath.Join(t.TempDir(), "absent", "memory"))
	result, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", t.TempDir(), setuputil.TestPlanRequest())
	if err != nil || len(result.Incomplete) != 0 {
		t.Fatalf("Run(uninstall) = %+v, %v; want a completed uninstall", result, err)
	}
}

// TestUninstallTreatsTheMemoryPathLiterally proves a configured memory path with glob characters never
// selects another directory as one of its set-aside copies.
func TestUninstallTreatsTheMemoryPathLiterally(t *testing.T) {
	base := testutil.MemoryDir(t)
	dir := filepath.Join(base, "mem[12]")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(memory.EnvDir, dir)
	// "mem[12].removed-*" read as a pattern would match this unrelated directory.
	unrelated := filepath.Join(base, "mem1"+".removed-1")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", t.TempDir(), setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	if len(result.Incomplete) != 0 {
		t.Fatalf("Incomplete = %v, want none", result.Incomplete)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated directory %s: %v, want it untouched", unrelated, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("memory directory: %v, want it retired", err)
	}
}

// TestUninstallKeepsAMemoryWhoseOwnerHoldsTheLockWithoutASocket covers an owner that took the lock but has
// not bound its socket yet: the store is never deleted under it.
func TestUninstallKeepsAMemoryWhoseOwnerHoldsTheLockWithoutASocket(t *testing.T) {
	flockPath, err := exec.LookPath("flock")
	if err != nil {
		t.Skip("flock(1) is not available")
	}
	root := t.TempDir()
	if _, err := installRuntime(t).Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	dir := filepath.Join(testutil.MemoryDir(t), "m")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(memory.EnvDir, dir)
	lock := filepath.Join(dir, memory.LockFile)
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	holder := exec.Command(flockPath, lock, "sleep", "60")
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Process.Kill(); _ = holder.Wait() })
	// The holder owns the lock once a non-blocking attempt fails.
	for i := 0; exec.Command(flockPath, "-n", lock, "true").Run() == nil; i++ {
		if i == 100 {
			t.Fatal("the holder never took the lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	result, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	kept := setAsideDirs(t, dir)
	if len(kept) != 1 {
		t.Fatalf("set-aside directories = %v, want the store kept", kept)
	}
	if len(result.Incomplete) != 1 || !strings.Contains(result.Incomplete[0], kept[0]) {
		t.Fatalf("Incomplete = %v, want one entry naming %s", result.Incomplete, kept[0])
	}
	if !setup.IsInstalled(root) {
		t.Error("the installation was forgotten while its memory stayed set aside")
	}
}

func TestUninstallKeepsTheMemoryInPlaceWhenTheSetAsidePathIsTooLong(t *testing.T) {
	base := testutil.MemoryDir(t)
	dir := filepath.Join(base, "m")
	// Grow dir to the longest path whose own socket can be bound; the set-aside suffix then exceeds it.
	for {
		if _, err := memory.SocketPath(dir + "a"); err != nil {
			break
		}
		dir += "a"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(memory.EnvDir, dir)
	root := t.TempDir()
	result, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("memory directory: %v, want kept in place", err)
	}
	if left := setAsideDirs(t, dir); len(left) != 0 {
		t.Errorf("set-aside directories = %v, want none", left)
	}
	if len(result.Incomplete) != 1 || !strings.Contains(result.Incomplete[0], "stays in place") {
		t.Fatalf("Incomplete = %v, want one entry stating the directory stays in place", result.Incomplete)
	}
}

func TestArchifyLifecycleInstallSyncDeselectAndPreservedUnmanaged(t *testing.T) {
	t.Run("install acquires and sync deselection cleans Takt-managed Archify", func(t *testing.T) {
		root := t.TempDir()
		rt := installRuntime(t)
		reqWith := setuputil.TestPlanRequest()
		reqWith.Components = []string{string(model.ComponentContext7), string(model.ComponentArchify)}

		res, err := rt.Run(context.Background(), "install", root, reqWith)
		if err != nil {
			t.Fatalf("Run(install) error = %v", err)
		}
		if !slices.Contains(res.Actions, "archify-install") {
			t.Fatalf("Actions = %v, want archify-install", res.Actions)
		}
		if _, found := archify.Resolve(root); !found {
			t.Fatal("archify.Resolve() = false after install")
		}
		manifest, err := setup.LoadOwnershipManifest(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, owned := manifest.Entries[model.ArchifyManagedMarkerKey]; !owned {
			t.Fatalf("manifest missing %s", model.ArchifyManagedMarkerKey)
		}
		bridgeOpenCode := filepath.Join(root, model.OpenCodeSkillsDir, "takt-archify", "SKILL.md")
		if _, err := os.Stat(bridgeOpenCode); err != nil {
			t.Fatalf("bridge skill missing after install: %v", err)
		}

		reqWithout := setuputil.TestPlanRequest()
		reqWithout.Components = []string{string(model.ComponentContext7)}
		if _, err := rt.Run(context.Background(), "sync", root, reqWithout); err != nil {
			t.Fatalf("Run(sync without archify) error = %v", err)
		}
		if _, err := os.Stat(model.ArchifySkillDir(root)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("archify dir after deselection stat = %v, want removed", err)
		}
		if _, err := os.Stat(bridgeOpenCode); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("takt-archify bridge skill after deselection stat = %v, want removed", err)
		}
		manifestAfter, err := setup.LoadOwnershipManifest(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, stillOwned := manifestAfter.Entries[model.ArchifyManagedMarkerKey]; stillOwned {
			t.Fatalf("manifest kept %s after deselection", model.ArchifyManagedMarkerKey)
		}
	})

	t.Run("uninstall preserves reused unmanaged Archify", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(memory.EnvDir, filepath.Join(t.TempDir(), "absent"))
		// Pre-install an unmanaged compatible copy before Takt install.
		preinstallUnmanagedArchify(t, root)
		rt := installRuntime(t)
		req := setuputil.TestPlanRequest()
		req.Components = []string{string(model.ComponentArchify)}
		res, err := rt.Run(context.Background(), "install", root, req)
		if err != nil {
			t.Fatalf("Run(install) error = %v", err)
		}
		if slices.Contains(res.Actions, "archify-install") {
			t.Fatalf("Actions = %v, must not claim archify-install for reused copy", res.Actions)
		}
		if _, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", root, req); err != nil {
			t.Fatalf("Run(uninstall) error = %v", err)
		}
		if _, found := archify.Resolve(root); !found {
			t.Fatal("uninstall removed the user's pre-existing unmanaged Archify installation")
		}
	})

	t.Run("sync replaces stale Takt-managed Archify via backup", func(t *testing.T) {
		root := t.TempDir()
		rt := installRuntime(t)
		req := setuputil.TestPlanRequest()
		req.Components = []string{string(model.ComponentContext7), string(model.ComponentArchify)}
		if _, err := rt.Run(context.Background(), "install", root, req); err != nil {
			t.Fatalf("Run(install) error = %v", err)
		}
		pkgPath := filepath.Join(model.ArchifySkillDir(root), "package.json")
		if err := os.WriteFile(pkgPath, []byte(`{"name":"archify","version":"3.0.0"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := rt.Run(context.Background(), "sync", root, req)
		if err != nil {
			t.Fatalf("Run(sync stale archify) error = %v", err)
		}
		if _, found := archify.Resolve(root); !found {
			t.Fatal("archify.Resolve() = false after stale sync")
		}
		if ver, err := archify.InstalledVersion(root); err != nil || ver != archify.Version {
			t.Fatalf("InstalledVersion() = (%q, %v), want %q", ver, err, archify.Version)
		}
		if len(res.BackedUp) == 0 {
			t.Fatalf("BackedUp = %v, want backup entry for replaced stale archify", res.BackedUp)
		}
	})

	t.Run("install fails before writing when Archify archive is unavailable", func(t *testing.T) {
		root := t.TempDir()
		srv := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(srv.Close)
		rt := installRuntime(t)
		m := *rt.Archify
		m.URL = srv.URL
		rt.Archify = &m
		req := setuputil.TestPlanRequest()
		req.Components = []string{string(model.ComponentArchify)}
		res, err := rt.Run(context.Background(), "install", root, req)
		if err == nil || !strings.Contains(err.Error(), "archify capability unavailable") {
			t.Fatalf("Run(install) error = %v, want archify capability unavailable", err)
		}
		if len(res.Changed) != 0 {
			t.Fatalf("Changed = %v, want none when prerequisite fails before writes", res.Changed)
		}
	})

	t.Run("install cancelled during Archify acquisition applies nothing", func(t *testing.T) {
		root := t.TempDir()
		rt := installRuntime(t)
		if err := rt.Model.Acquire(context.Background(), http.DefaultClient, embed.ModelDir(root)); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		// No response is written: one could race the cancellation and reach
		// Acquire as an HTTP error instead of context.Canceled.
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			cancel()
			<-r.Context().Done()
		}))
		t.Cleanup(srv.Close)
		rt.Archify = &archify.Manifest{
			URL:     srv.URL,
			SHA256:  strings.Repeat("a", 64),
			Size:    10,
			Version: archify.Version,
		}
		req := setuputil.TestPlanRequest()
		req.Components = []string{string(model.ComponentArchify)}
		res, err := rt.Run(ctx, "install", root, req)
		if err != nil {
			t.Fatalf("Run(install) error = %v, want nil for cancellation", err)
		}
		if res.Outcome != lifecycle.OutcomeCancelledNothingApplied {
			t.Fatalf("Outcome = %v, want %v", res.Outcome, lifecycle.OutcomeCancelledNothingApplied)
		}
	})

	t.Run("sync deselection preserves user-edited takt-archify bridge skill", func(t *testing.T) {
		root := t.TempDir()
		rt := installRuntime(t)
		reqWith := setuputil.TestPlanRequest()
		reqWith.Components = []string{string(model.ComponentContext7), string(model.ComponentArchify)}
		if _, err := rt.Run(context.Background(), "install", root, reqWith); err != nil {
			t.Fatalf("Run(install) error = %v", err)
		}
		bridgeOpenCode := filepath.Join(root, model.OpenCodeSkillsDir, "takt-archify", "SKILL.md")
		raw, err := os.ReadFile(bridgeOpenCode)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bridgeOpenCode, append(raw, []byte("\n# custom\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		reqWithout := setuputil.TestPlanRequest()
		reqWithout.Components = []string{string(model.ComponentContext7)}
		if _, err := rt.Run(context.Background(), "sync", root, reqWithout); err != nil {
			t.Fatalf("Run(sync without archify) error = %v", err)
		}
		if _, err := os.Stat(model.ArchifySkillDir(root)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("archify dir after deselection stat = %v, want removed", err)
		}
		got, err := os.ReadFile(bridgeOpenCode)
		if err != nil || !strings.Contains(string(got), "# custom\n") {
			t.Fatalf("user-edited bridge skill = (%q, %v), want preserved custom edit", string(got), err)
		}
	})
}

// preinstallUnmanagedArchify places a compatible Archify copy the way a user
// installing it independently would: the same tree, without Takt's receipt.
func preinstallUnmanagedArchify(t *testing.T, root string) {
	t.Helper()
	if _, installed, err := testutil.FakeArchify(t).Acquire(context.Background(), http.DefaultClient, root); err != nil || !installed {
		t.Fatalf("pre-install unmanaged Archify: %v, %v", installed, err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(model.ArchifyManagedMarkerKey))); err != nil {
		t.Fatal(err)
	}
}

// TestArchifyOwnershipFollowsTheReceipt covers the ownership evidence an
// Archify tree carries: Takt's receipt, recorded in the ownership manifest.
func TestArchifyOwnershipFollowsTheReceipt(t *testing.T) {
	withArchify := func() setup.PlanRequest {
		req := setuputil.TestPlanRequest()
		req.Components = []string{string(model.ComponentArchify)}
		return req
	}
	owned := func(t *testing.T, root string) bool {
		t.Helper()
		manifest, err := setup.LoadOwnershipManifest(root)
		if err != nil {
			t.Fatal(err)
		}
		_, found := manifest.Entries[model.ArchifyManagedMarkerKey]
		return found
	}

	t.Run("a retry records the tree an interrupted install wrote", func(t *testing.T) {
		root := t.TempDir()
		rt := installRuntime(t)
		// An earlier run extracted the tree and failed before recording it.
		if _, installed, err := rt.Archify.Acquire(context.Background(), http.DefaultClient, root); err != nil || !installed {
			t.Fatalf("earlier extraction: %v, %v", installed, err)
		}
		if _, err := rt.Run(context.Background(), "install", root, withArchify()); err != nil {
			t.Fatalf("Run(install) retry error = %v", err)
		}
		if !owned(t, root) {
			t.Fatalf("retry did not record %s for the tree Takt wrote", model.ArchifyManagedMarkerKey)
		}
		if _, err := rt.Run(context.Background(), "sync", root, setuputil.TestPlanRequest()); err != nil {
			t.Fatalf("Run(sync without archify) error = %v", err)
		}
		if _, err := os.Lstat(model.ArchifySkillDir(root)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("archify dir after deselection stat = %v, want removed", err)
		}
	})

	t.Run("an independent copy is never claimed", func(t *testing.T) {
		root := t.TempDir()
		preinstallUnmanagedArchify(t, root)
		rt := installRuntime(t)
		if _, err := rt.Run(context.Background(), "install", root, withArchify()); err != nil {
			t.Fatalf("Run(install) error = %v", err)
		}
		if owned(t, root) {
			t.Fatal("install claimed an Archify copy installed outside Takt")
		}
	})

	t.Run("uninstall leaves an untracked tree without package.json", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(memory.EnvDir, filepath.Join(t.TempDir(), "absent"))
		rt := installRuntime(t)
		if _, err := rt.Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err != nil {
			t.Fatalf("Run(install) error = %v", err)
		}
		notes := filepath.Join(model.ArchifySkillDir(root), "notes.md")
		if err := os.MkdirAll(filepath.Dir(notes), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(notes, []byte("local"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", root, setuputil.TestPlanRequest()); err != nil {
			t.Fatalf("Run(uninstall) error = %v", err)
		}
		if _, err := os.Stat(notes); err != nil {
			t.Fatalf("uninstall removed an untracked Archify tree: %v", err)
		}
	})

	t.Run("uninstall preserves a managed tree changed since extraction", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(memory.EnvDir, filepath.Join(t.TempDir(), "absent"))
		rt := installRuntime(t)
		if _, err := rt.Run(context.Background(), "install", root, withArchify()); err != nil {
			t.Fatalf("Run(install) error = %v", err)
		}
		edited := filepath.Join(model.ArchifySkillDir(root), "SKILL.md")
		if err := os.WriteFile(edited, []byte("---\nname: archify\ndescription: mine\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := (lifecycle.Runtime{}).Run(context.Background(), "uninstall", root, withArchify())
		if err != nil {
			t.Fatalf("Run(uninstall) error = %v", err)
		}
		if _, err := os.Stat(edited); err != nil {
			t.Fatalf("uninstall removed a changed managed Archify tree: %v", err)
		}
		if !slices.Contains(res.Preserved, model.ArchifySkillRelDir) {
			t.Fatalf("Preserved = %v, want %s", res.Preserved, model.ArchifySkillRelDir)
		}
	})

	t.Run("deselection keeps a managed tree with local changes", func(t *testing.T) {
		root := t.TempDir()
		rt := installRuntime(t)
		if _, err := rt.Run(context.Background(), "install", root, withArchify()); err != nil {
			t.Fatalf("Run(install) error = %v", err)
		}
		pkg := filepath.Join(model.ArchifySkillDir(root), "package.json")
		local := []byte(`{"name":"archify","version":"` + archify.Version + `","local":true}`)
		if err := os.WriteFile(pkg, local, 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := rt.Run(context.Background(), "sync", root, setuputil.TestPlanRequest())
		if err != nil {
			t.Fatalf("Run(sync without archify) error = %v", err)
		}
		if got, err := os.ReadFile(pkg); err != nil || !bytes.Equal(got, local) {
			t.Fatalf("package.json after deselection = %q, %v; want the local edit kept", got, err)
		}
		if !slices.Contains(res.Retained, model.ArchifySkillRelDir) {
			t.Fatalf("Retained = %v, want %s", res.Retained, model.ArchifySkillRelDir)
		}
		if owned(t, root) {
			t.Fatal("deselection kept ownership of a tree it left in place")
		}
		// The released tree is no longer Takt's: a later deselection leaves it too.
		if _, err := rt.Run(context.Background(), "sync", root, setuputil.TestPlanRequest()); err != nil {
			t.Fatalf("Run(second sync) error = %v", err)
		}
		if _, err := os.Stat(pkg); err != nil {
			t.Fatalf("second deselection removed the released tree: %v", err)
		}
	})

	t.Run("sync replaces a damaged managed copy", func(t *testing.T) {
		root := t.TempDir()
		rt := installRuntime(t)
		if _, err := rt.Run(context.Background(), "install", root, withArchify()); err != nil {
			t.Fatalf("Run(install) error = %v", err)
		}
		// The copy still resolves, yet no longer matches the extracted tree.
		bin := model.ArchifyBinaryPath(root)
		if err := os.WriteFile(bin, []byte("#!/usr/bin/env node\nthrow new Error('damaged')\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, found := archify.Resolve(root); !found {
			t.Fatal("damaged copy no longer resolves; the scenario needs one that does")
		}
		res, err := rt.Run(context.Background(), "sync", root, withArchify())
		if err != nil {
			t.Fatalf("Run(sync) error = %v", err)
		}
		if !archify.Intact(root) {
			t.Fatal("sync left the damaged managed copy in place")
		}
		backedUp := false
		for _, backup := range res.BackedUp {
			backedUp = backedUp || backup.Path == model.ArchifySkillRelDir
		}
		if !backedUp {
			t.Fatalf("BackedUp = %+v, want the damaged copy set aside", res.BackedUp)
		}
	})
}
