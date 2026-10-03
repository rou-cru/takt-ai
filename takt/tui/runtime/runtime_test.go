package runtime_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	skillsutil "github.com/rou-cru/takt-ai/takt/skills/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	// modernc.org/sqlite registers the "sqlite" database/sql driver via init.
	_ "modernc.org/sqlite"
)

func TestAdapterLifecycleActions(t *testing.T) {
	root := t.TempDir()
	adapter := testAdapter(nil)

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

func TestAdapterInstallDeploysSkills(t *testing.T) {
	root := t.TempDir()
	result, err := testAdapter(nil).Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root})
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
	adapter := testAdapter(nil)

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
	adapter := testAdapter(nil)
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
	adapter := testAdapter(nil)
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
	adapter := testAdapter(nil)
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
	adapter := testAdapter(nil)
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

// TestAdapterUninstallEngramLeaveIsDefault verifies that an empty or Leave
// choice changes nothing about the uninstall.
func TestAdapterUninstallEngramLeaveIsDefault(t *testing.T) {
	for _, choice := range []string{"", lifecycle.EngramLeave} {
		root := t.TempDir()
		adapter := testAdapter(nil)
		if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
			t.Fatalf("install error = %v", err)
		}
		result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionUninstall, RootDir: root, EngramChoice: choice})
		if err != nil {
			t.Fatalf("uninstall error = %v", err)
		}
		if len(result.Removed) == 0 {
			t.Fatal("uninstall removed nothing")
		}
		for _, entry := range result.Incomplete {
			if strings.Contains(entry, "Engram") {
				t.Fatalf("leave reported Engram incomplete work: %v", result.Incomplete)
			}
		}
	}
}

// TestAdapterUninstallEngramRemoveReportsIncomplete verifies that the
// config footprint is removed as usual, and the database without a supported
// remover is honest Incomplete, never a failure.
func TestAdapterUninstallEngramRemoveReportsIncomplete(t *testing.T) {
	root := t.TempDir()
	adapter := testAdapter(nil)
	if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
		t.Fatalf("install error = %v", err)
	}
	result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionUninstall, RootDir: root, EngramChoice: lifecycle.EngramRemove})
	if err != nil {
		t.Fatalf("remove choice must not fail: %v", err)
	}
	if len(result.Removed) == 0 {
		t.Fatal("uninstall removed nothing")
	}
	found := false
	for _, entry := range result.Incomplete {
		if strings.Contains(entry, "Engram database") {
			found = true
		}
	}
	if !found {
		t.Fatalf("incomplete = %v, want an Engram database entry", result.Incomplete)
	}
}

// TestAdapterUninstallEngramRetainDeliversMemoryBesideKeptFiles verifies the
// memory handoff lands in the same retained directory as kept files, its
// location reaches the result, and nothing is reported as incomplete.
func TestAdapterUninstallEngramRetainDeliversMemoryBesideKeptFiles(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ENGRAM_DATA_DIR", dataDir)
	fixture, err := sql.Open("sqlite", filepath.Join(dataDir, "engram.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"CREATE TABLE observations (title TEXT)", "INSERT INTO observations VALUES ('alpha decision')"} {
		if _, err := fixture.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	_ = fixture.Close()

	root := t.TempDir()
	adapter := testAdapter(nil)
	if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
		t.Fatalf("install error = %v", err)
	}
	kept := ".config/opencode/takt/agents/takt-analyst/OPERATIONS.md"
	if err := os.WriteFile(filepath.Join(root, kept), []byte("user edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionUninstall, RootDir: root, RetainPaths: []string{kept}, EngramChoice: lifecycle.EngramRetain})
	if err != nil {
		t.Fatalf("uninstall error = %v", err)
	}
	if len(result.Incomplete) != 0 {
		t.Fatalf("incomplete = %v, want none", result.Incomplete)
	}
	want := filepath.Join(result.RetainedDir, "engram", "engram.db")
	if result.RetainedDir == "" || result.RetainedMemory != want {
		t.Fatalf("RetainedMemory = %q, RetainedDir = %q; want the database inside the retained directory", result.RetainedMemory, result.RetainedDir)
	}
	delivered, err := sql.Open("sqlite", "file:"+want+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = delivered.Close() }()
	var title string
	if err := delivered.QueryRow("SELECT title FROM observations").Scan(&title); err != nil || title != "alpha decision" {
		t.Fatalf("delivered row = %q, %v", title, err)
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

func testAdapter(calls *[][]string) runtime.Adapter {
	return runtime.NewTestAdapter(lifecycle.Runtime{ProviderActions: setup.ProviderRuntime{
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
	adapter := runtime.NewTestAdapter(lifecycle.Runtime{ProviderActions: setup.ProviderRuntime{
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
			if _, err := testAdapter(nil).Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
				t.Fatal(err)
			}
			path := ".config/opencode/opencode.json"
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			adapter := runtime.NewTestAdapter(lifecycle.Runtime{ProviderActions: setup.ProviderRuntime{
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
				if err != nil || !strings.Contains(string(data), "engram") {
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
