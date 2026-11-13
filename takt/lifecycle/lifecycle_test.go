package lifecycle_test

import (
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

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	return lifecycle.Runtime{Model: testutil.FakeModel(t)}
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
