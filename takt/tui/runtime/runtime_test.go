package runtime_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/setup"
	skillsutil "github.com/rou-cru/takt-ai/takt/skills/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

func TestAdapterLifecycleActions(t *testing.T) {
	root := t.TempDir()
	adapter := testAdapter(t, nil)

	install, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root})
	if err != nil || len(install.Changed) == 0 {
		t.Fatalf("install = %+v, %v", install, err)
	}
	sync, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionSync, RootDir: root})
	if err != nil || len(sync.Unchanged) == 0 {
		t.Fatalf("sync = %+v, %v", sync, err)
	}
	uninstall, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionUninstall, RootDir: root})
	if err != nil || len(uninstall.Removed) == 0 {
		t.Fatalf("uninstall = %+v, %v", uninstall, err)
	}
}

// An upgrade is the install of the recorded choices onto this build: a managed
// file the user edited after an older install is replaced unless preserved,
// and the installed version becomes this build's.
func TestAdapterUpgradeHonorsReplaceAndPreserve(t *testing.T) {
	previous := setup.BuildVersion
	t.Cleanup(func() { setup.BuildVersion = previous })
	for _, preserve := range []bool{false, true} {
		root := t.TempDir()
		adapter := testAdapter(t, nil)
		setup.BuildVersion = "0.0.25"
		install, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root})
		if err != nil {
			t.Fatalf("install error = %v", err)
		}
		path := managedMarkdown(t, install.Changed)
		full := filepath.Join(root, filepath.FromSlash(path))
		released, err := os.ReadFile(full)
		if err != nil {
			t.Fatal(err)
		}
		olderInstallThenEdit(t, root, path)

		setup.BuildVersion = "0.0.27"
		request := runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}
		if preserve {
			request.PreservePaths = []string{path}
		}
		if _, err := adapter.Execute(request); err != nil {
			t.Fatalf("upgrade error = %v", err)
		}
		got, err := os.ReadFile(full)
		if err != nil {
			t.Fatal(err)
		}
		if want := map[bool][]byte{false: released, true: []byte("user edit")}[preserve]; !bytes.Equal(got, want) {
			t.Errorf("preserve=%v: %s = %q, want %q", preserve, path, got, want)
		}
		if got := setup.InstalledVersion(root); got != "0.0.27" {
			t.Errorf("preserve=%v: InstalledVersion() = %q, want the upgraded build", preserve, got)
		}
	}
}

// managedMarkdown returns one deployed Markdown path.
func managedMarkdown(t *testing.T, changed []string) string {
	t.Helper()
	for _, path := range changed {
		if filepath.Ext(path) == ".md" {
			return path
		}
	}
	t.Fatalf("no Markdown file among %v", changed)
	return ""
}

// olderInstallThenEdit records path as written by an older build with other
// content, then edits it as the user would.
func olderInstallThenEdit(t *testing.T, root, path string) {
	t.Helper()
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entries[path]
	older := sha256.Sum256([]byte("older build content"))
	entry.SHA256 = hex.EncodeToString(older[:])
	manifest.Entries[path] = entry
	if err := manifest.Save(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("user edit"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterInstallDeploysSkills(t *testing.T) {
	root := t.TempDir()
	result, err := testAdapter(t, nil).Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root})
	if err != nil {
		t.Fatalf("install error = %v", err)
	}
	skillPath, _ := skillsutil.FirstSkill(t)
	if !slices.Contains(result.Changed, skillPath) {
		t.Fatalf("install changed = %v, want %q", result.Changed, skillPath)
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
}

func TestAdapterSyncPreservesModifiedAndRedeploysDeletedSkills(t *testing.T) {
	skillPath, embedded := skillsutil.FirstSkill(t)
	adapter := testAdapter(t, nil)

	t.Run("locally modified skill is preserved", func(t *testing.T) {
		root := t.TempDir()
		if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
			t.Fatalf("install error = %v", err)
		}
		local := append(append([]byte(nil), embedded...), []byte("\nlocal edit")...)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(skillPath)), local, 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionSync, RootDir: root})
		if err != nil {
			t.Fatalf("sync error = %v", err)
		}
		if slices.Contains(result.Changed, skillPath) {
			t.Fatalf("sync changed = %v, want locally modified skill preserved", result.Changed)
		}
		current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(skillPath)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, local) {
			t.Fatal("sync did not preserve the locally modified skill content")
		}
	})

	t.Run("locally deleted skill is redeployed", func(t *testing.T) {
		root := t.TempDir()
		if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
			t.Fatalf("install error = %v", err)
		}
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(skillPath))); err != nil {
			t.Fatal(err)
		}
		result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionSync, RootDir: root})
		if err != nil {
			t.Fatalf("sync error = %v", err)
		}
		if !slices.Contains(result.Changed, skillPath) {
			t.Fatalf("sync changed = %v, want redeployed %q", result.Changed, skillPath)
		}
		current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(skillPath)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, embedded) {
			t.Fatal("redeployed skill content does not match the embedded skill")
		}
	})
}

func TestAdapterUninstallRemovesSkills(t *testing.T) {
	root := t.TempDir()
	adapter := testAdapter(t, nil)
	if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
		t.Fatalf("install error = %v", err)
	}
	skillPath, _ := skillsutil.FirstSkill(t)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(skillPath))); err != nil {
		t.Fatalf("precondition: installed skill file: %v", err)
	}
	if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionUninstall, RootDir: root}); err != nil {
		t.Fatalf("uninstall error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(skillPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skill file after uninstall stat error = %v, want removed", err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err == nil {
		if _, ok := manifest.Entries[skillPath]; ok {
			t.Fatalf("manifest still records %q after uninstall", skillPath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("load manifest: %v", err)
	}
}

func TestActionCommandReturnsErrorMessage(t *testing.T) {
	message := runtime.Adapter{}.Command(context.Background(), runtime.ActionRequest{Action: runtime.ActionCorrectDrift, RootDir: t.TempDir()})()
	result, ok := message.(runtime.ActionResultMsg)
	if !ok || result.Err == nil {
		t.Fatalf("command message = %#v, want ActionResultMsg with error", message)
	}
}

func TestAdapterExecuteCarriesComponents(t *testing.T) {
	root := t.TempDir()
	adapter := testAdapter(t, nil)
	if _, err := adapter.Execute(runtime.ActionRequest{
		Action:     runtime.ActionInstall,
		RootDir:    root,
		Components: []string{"context7"},
	}); err != nil {
		t.Fatalf("install with components error = %v", err)
	}
	config, err := os.ReadFile(filepath.Join(root, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	if !bytes.Contains(config, []byte(`"context7"`)) {
		t.Fatalf("opencode.json missing component merge:\n%s", config)
	}

	plain, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionSync, RootDir: root})
	if err != nil {
		t.Fatalf("sync without components error = %v", err)
	}
	if len(plain.Unchanged) == 0 {
		t.Fatalf("sync without components changed %v, want byte-identical redeploy", plain.Changed)
	}
}

// TestCorrectDriftBlockedOnVersionMismatch verifies that this build's
// definitions are not substituted for a different installed version.
func TestCorrectDriftBlockedOnVersionMismatch(t *testing.T) {
	root := t.TempDir()
	adapter := testAdapter(t, nil)
	previous := setup.BuildVersion
	setup.BuildVersion = "1.0.0"
	t.Cleanup(func() { setup.BuildVersion = previous })
	if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
		t.Fatalf("install error = %v", err)
	}
	agentPath := filepath.Join(root, ".config", "opencode", "takt", "agents", "takt-analyst", "OPERATIONS.md")
	if err := os.WriteFile(agentPath, []byte("user edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	setup.BuildVersion = "2.0.0"
	if runtime.SameInstalledVersion(root) {
		t.Fatal("SameInstalledVersion = true across 1.0.0 -> 2.0.0")
	}
	_, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionCorrectDrift, RootDir: root, SelectedDriftPaths: []string{".config/opencode/takt/agents/takt-analyst/OPERATIONS.md"}})
	if !errors.Is(err, runtime.ErrVersionMismatch) {
		t.Fatalf("error = %v, want ErrVersionMismatch", err)
	}
	if got, _ := os.ReadFile(agentPath); string(got) != "user edit" {
		t.Fatalf("file = %q, want the edit untouched", got)
	}
}

// TestAdapterUninstallAppliesRetentionChoices verifies that the retention
// handoff runs inside the runtime action.
func TestAdapterUninstallAppliesRetentionChoices(t *testing.T) {
	root := t.TempDir()
	adapter := testAdapter(t, nil)
	if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
		t.Fatalf("install error = %v", err)
	}
	kept, removed := ".config/opencode/takt/agents/takt-analyst/OPERATIONS.md", ".config/opencode/takt/agents/takt-dev/OPERATIONS.md"
	for _, rel := range []string{kept, removed} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("user edit"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionUninstall, RootDir: root, RetainPaths: []string{kept}, RemovePaths: []string{removed}})
	if err != nil {
		t.Fatalf("uninstall error = %v", err)
	}
	if !slices.Equal(result.Retained, []string{kept}) || result.RetainedDir == "" || len(result.Incomplete) != 0 {
		t.Fatalf("result = %+v, want %s retained with a directory", result, kept)
	}
	if !slices.Contains(result.Removed, removed) {
		t.Fatalf("removed = %v, want %s", result.Removed, removed)
	}
	if got, err := os.ReadFile(filepath.Join(result.RetainedDir, filepath.FromSlash(kept))); err != nil || string(got) != "user edit" {
		t.Fatalf("retained copy = %q, err = %v", got, err)
	}
	if setup.IsInstalled(root) {
		t.Fatal("IsInstalled = true after full uninstall")
	}
}

// installedWithMemory installs into a fresh root and starts the memory service in a private directory
// holding one entry.
func installedWithMemory(t *testing.T, adapter runtime.Adapter) (root, dir string) {
	t.Helper()
	dir = testutil.MemoryDir(t)
	root = t.TempDir()
	if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
		t.Fatalf("install error = %v", err)
	}
	if _, err := memory.Start(context.Background(), dir, true); err != nil {
		t.Fatalf("memory.Start: %v", err)
	}
	store, err := memory.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	entry := memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeGlobal, Title: "alpha decision", Content: "body", Evidence: "seen"}
	if _, err := store.Record(context.Background(), memory.Identity{Principal: memory.PrincipalExternal, HostSession: "k1"}, entry); err != nil {
		t.Fatal(err)
	}
	return root, dir
}

// TestAdapterUninstallDiscardsTheMemoryByDefault verifies that a request that does not retain the
// memory deletes it and reports nothing retained.
func TestAdapterUninstallDiscardsTheMemoryByDefault(t *testing.T) {
	adapter := testAdapter(t, nil)
	root, dir := installedWithMemory(t, adapter)
	result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionUninstall, RootDir: root})
	if err != nil {
		t.Fatalf("uninstall error = %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("memory directory after uninstall: %v, want removed", err)
	}
	if result.RetainedMemory != "" || result.RetainedDir != "" || len(result.Incomplete) != 0 {
		t.Errorf("result = %+v, want nothing retained and nothing incomplete", result)
	}
}

// TestAdapterUninstallRetainDeliversMemoryBesideKeptFiles verifies the memory backup lands in the same
// retained directory as kept files, its location reaches the result, and nothing is reported as incomplete.
func TestAdapterUninstallRetainDeliversMemoryBesideKeptFiles(t *testing.T) {
	adapter := testAdapter(t, nil)
	root, dir := installedWithMemory(t, adapter)
	kept := ".config/opencode/takt/agents/takt-analyst/OPERATIONS.md"
	if err := os.WriteFile(filepath.Join(root, kept), []byte("user edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionUninstall, RootDir: root, RetainPaths: []string{kept}, RetainMemory: true})
	if err != nil {
		t.Fatalf("uninstall error = %v", err)
	}
	if len(result.Incomplete) != 0 {
		t.Fatalf("incomplete = %v, want none", result.Incomplete)
	}
	want := filepath.Join(result.RetainedDir, setup.RetainedMemoryDirName, memory.StoreFile)
	if result.RetainedDir == "" || result.RetainedMemory != want {
		t.Fatalf("RetainedMemory = %q, RetainedDir = %q; want the backup inside the retained directory", result.RetainedMemory, result.RetainedDir)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("memory directory after uninstall: %v, want removed", err)
	}
	store, err := memory.Open(filepath.Dir(want))
	if err != nil {
		t.Fatalf("memory.Open(backup): %v", err)
	}
	defer func() { _ = store.Close() }()
	if _, total, err := store.Coverage(context.Background()); err != nil || total != 1 {
		t.Fatalf("backup entries = %d, %v; want 1", total, err)
	}
	if _, err := os.Stat(filepath.Join(result.RetainedDir, filepath.FromSlash(kept))); err != nil {
		t.Fatalf("kept file is not beside the memory: %v", err)
	}
}

// installedPreview always resets the recorded fallback model to defaults
// (DefaultPlanRequest never sets OpenCode), independent of native content —
// this locks that behavior down so a later cleanup of the dead Content guard
// cannot change it.
func TestInstalledPreviewResetsOpenCodeModelToDefault(t *testing.T) {
	root := t.TempDir()
	installed, err := setup.DefaultPlanRequest()
	if err != nil {
		t.Fatal(err)
	}
	installed.OpenCode.Model = "custom-model"
	if err := setup.RecordInstallation(root, installed); err != nil {
		t.Fatal(err)
	}
	preview, err := runtime.InstalledPreview(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range preview.Plans {
		for _, artifact := range plan.Artifacts {
			if strings.Contains(string(artifact.Content), "custom-model") {
				t.Fatalf("artifact %s carries the recorded fallback model; installedPreview must reset it to defaults", artifact.Path)
			}
		}
	}
}

func testAdapter(t *testing.T, calls *[][]string) runtime.Adapter {
	t.Helper()
	// An uninstall deletes the memory directory: a test that does not choose its own never reaches the user's.
	t.Setenv(memory.EnvDir, filepath.Join(t.TempDir(), "absent"))
	return runtime.NewAdapterFor(lifecycle.Runtime{Model: testutil.FakeModel(t), ProviderActions: setup.ProviderRuntime{
		LookPath: func(program string) (string, error) { return "/fake/" + program, nil },
		Run: func(_ context.Context, program string, args ...string) ([]byte, error) {
			if calls != nil {
				*calls = append(*calls, append([]string{program}, args...))
			}
			return nil, nil
		},
	}})
}

/*
// Ctrl+C reaches the running action's context once; the model stays busy
// until the typed result arrives.
func TestCommandReportsCompletedWhenCancelArrivesLate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter := runtime.NewAdapterFor(lifecycle.Runtime{ProviderActions: setup.ProviderRuntime{
		LookPath: func(program string) (string, error) { return "/fake/" + program, nil },
		Run: func(context.Context, string, ...string) ([]byte, error) {
			cancel()
			return nil, nil
		},
	}})
	request := runtime.ActionRequest{ID: 42, Action: runtime.ActionInstall, RootDir: t.TempDir()}
	message := adapter.Command(ctx, request)().(runtime.ActionResultMsg)
	if message.Err != nil || message.Result.Outcome != lifecycle.OutcomeCompleted || !message.Result.CancelRequested || message.Result.Cancelled() {
		t.Fatalf("result = %+v, %v; want completed after a late cancellation", message.Result, message.Err)
	}
}

// Cancellation before writes must not reinject; cancellation after a provider
// failure keeps changed paths and restores the required memory integration.
func TestCorrectDriftCancellationBoundaries(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "no changes", true: "partial"}[partial], func(t *testing.T) {
			root := t.TempDir()
			if _, err := testAdapter(t, nil).Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
				t.Fatal(err)
			}
			path := ".config/opencode/opencode.json"
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			adapter := runtime.NewAdapterFor(lifecycle.Runtime{ProviderActions: setup.ProviderRuntime{
				LookPath: func(string) (string, error) { return "/fake/opencode", nil },
				Run:      func(context.Context, string, ...string) ([]byte, error) { cancel(); return nil, context.Canceled },
			}})
			if !partial {
				cancel()
			}
			msg := adapter.Command(ctx, runtime.ActionRequest{Action: runtime.ActionCorrectDrift, RootDir: root, SelectedDriftPaths: []string{path}})().(runtime.ActionResultMsg)
			if msg.Err != nil {
				t.Fatal(msg.Err)
			}
			if partial {
				if msg.Result.Outcome != lifecycle.OutcomeCancelledPartial || !slices.Contains(msg.Result.Changed, path) {
					t.Fatalf("result=%+v", msg.Result)
				}
				data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
				if err != nil || !strings.Contains(string(data), memory.ServerKey) {
					t.Fatalf("missing reinjection: %s, %v", data, err)
				}
			} else {
				if msg.Result.Outcome != lifecycle.OutcomeCancelledNothingApplied || len(msg.Result.Changed) != 0 {
					t.Fatalf("result=%+v", msg.Result)
				}
				data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
				if err != nil || string(data) != "{}" {
					t.Fatalf("cancelled operation wrote config: %s, %v", data, err)
				}
			}
		})
	}
}
*/
