package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

// ownedFileContent is the byte payload of the files these tests register in the
// ownership manifest; ownership entries require non-empty managed content.
const ownedFileContent = "managed bytes"

// writeFile creates file (and its parents) with content, failing the test on error.
func writeFile(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// corruptOpenCodeConfig makes every MCP injection into or removal from root
// fail: a directory sits where the OpenCode config file should be.
func corruptOpenCodeConfig(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(model.OpenCodeConfigPath(root), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestNativeArtifactPendingMatchesOnlyOpenCodeDirectory(t *testing.T) {
	inside := opencode.ConfigDir() + "/agents/takt.md"
	if !nativeArtifactPending([]string{"skills/x/SKILL.md", inside}) {
		t.Errorf("nativeArtifactPending(%q among others) = false, want true", inside)
	}
	if nativeArtifactPending([]string{"skills/x/SKILL.md", opencode.ConfigDir()}) {
		t.Error("nativeArtifactPending(non-OpenCode paths) = true, want false")
	}
	if nativeArtifactPending(nil) {
		t.Error("nativeArtifactPending(nil) = true, want false")
	}
}

func TestPlannedPathsListsEveryArtifact(t *testing.T) {
	plans := []setup.TargetPlan{
		{Artifacts: []setup.Artifact{{Path: "a"}, {Path: "b"}}},
		{Artifacts: []setup.Artifact{{Path: "c"}}},
	}
	if got := plannedPaths(plans); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("plannedPaths() = %v, want [a b c]", got)
	}
	if got := plannedPaths(nil); len(got) != 0 {
		t.Errorf("plannedPaths(nil) = %v, want none", got)
	}
}

func TestPreviewLifecycleInstallAndSyncAgree(t *testing.T) {
	request := setuputil.TestPlanRequest()
	install, err := PreviewLifecycle("install", t.TempDir(), request)
	if err != nil {
		t.Fatal(err)
	}
	sync, err := PreviewLifecycle("sync", t.TempDir(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(sync.(InstallPreview).Plans), len(install.(InstallPreview).Plans); got != want {
		t.Errorf("sync preview has %d plans, want %d as install", got, want)
	}
}

func TestPreviewLifecycleInstallRejectsUnknownComponent(t *testing.T) {
	request := setuputil.TestPlanRequest()
	request.Components = []string{"no-such-component"}
	if _, err := PreviewLifecycle("install", t.TempDir(), request); err == nil {
		t.Fatal("PreviewLifecycle(install, unknown component) error = nil, want an error")
	}
}

func TestRunInstallRejectsUnknownComponent(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	request.Components = []string{"no-such-component"}
	if _, err := (Runtime{}).Run(context.Background(), "install", root, request); err == nil {
		t.Fatal("Run(install, unknown component) error = nil, want an error")
	}
	if setup.IsInstalled(root) {
		t.Error("a rejected plan was recorded as installed")
	}
}

func TestRunInstallWrapsHandshakeFailure(t *testing.T) {
	sentinel := errors.New("socket closed")
	runtime := Runtime{OpenCodeHandshake: func(context.Context) error { return sentinel }}
	_, err := runtime.Run(context.Background(), "install", t.TempDir(), setuputil.TestPlanRequest())
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "OpenCode V2 preflight failed") {
		t.Fatalf("Run() error = %v, want the handshake error wrapped as a preflight failure", err)
	}
}

func TestRunInstallFailsWhenEngramUnavailable(t *testing.T) {
	root := t.TempDir()
	withAcquire(t, func(context.Context, string) (string, error) { return "", errors.New("offline") })
	result, err := (Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err == nil || !strings.Contains(err.Error(), "engram memory capability unavailable") {
		t.Fatalf("Run() error = %v, want the Engram prerequisite failure", err)
	}
	if len(result.Changed) != 0 {
		t.Errorf("Changed = %v, want none: the prerequisite fails before writes", result.Changed)
	}
}

// TestRunInstallCancelledDuringAcquisitionAppliesNothing verifies a
// cancellation while a prerequisite is being acquired is a typed outcome with a
// nil error and the whole plan listed as not applied.
func TestRunInstallCancelledDuringAcquisitionAppliesNothing(t *testing.T) {
	cancelling := func(ctx context.Context, _ string) (string, error) {
		return "", ctx.Err()
	}
	for _, name := range []string{"engram", "codegraph"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			switch name {
			case "engram":
				withAcquire(t, cancelling)
			default:
				withAcquireCodegraph(t, cancelling)
			}
			result, err := (Runtime{}).Run(ctx, "install", root, setuputil.TestPlanRequest())
			if err != nil {
				t.Fatalf("Run() error = %v, want nil for a cancellation", err)
			}
			if result.Outcome != OutcomeCancelledNothingApplied {
				t.Errorf("Outcome = %v, want %v", result.Outcome, OutcomeCancelledNothingApplied)
			}
			if len(result.NotApplied) == 0 {
				t.Error("NotApplied is empty, want the planned artifacts")
			}
			if setup.IsInstalled(root) {
				t.Error("a cancelled install was recorded as installed")
			}
		})
	}
}

func TestRunInstallReportsProgressAndReload(t *testing.T) {
	root := t.TempDir()
	var messages []string
	reloaded := 0
	runtime := Runtime{
		Progress: func(progress setup.DeploymentProgress) { messages = append(messages, progress.Message) },
		Reload:   func(context.Context) error { reloaded++; return nil },
	}
	result, err := runtime.Run(context.Background(), "install", root, setuputil.TestPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded != 1 || !result.ReloadAttempted || result.ReloadError != nil {
		t.Errorf("reloaded = %d, ReloadAttempted = %v, ReloadError = %v; want one clean reload", reloaded, result.ReloadAttempted, result.ReloadError)
	}
	for _, want := range []string{"Preparing installation plan", "Installing CodeGraph integration", "Recording installation", "Reloading OpenCode"} {
		if !slices.Contains(messages, want) {
			t.Errorf("progress messages = %v, missing %q", messages, want)
		}
	}
}

// TestRunInstallReloadFailureDoesNotRollBack verifies a reload failure is
// reported separately: the files are valid on disk and stay recorded.
func TestRunInstallReloadFailureDoesNotRollBack(t *testing.T) {
	root := t.TempDir()
	sentinel := errors.New("reload refused")
	runtime := Runtime{Reload: func(context.Context) error { return sentinel }}
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

// TestRunInstallInjectionFailureKeepsDeployedResult verifies files written
// before a failed MCP injection are still reported alongside the error.
func TestRunInstallInjectionFailureKeepsDeployedResult(t *testing.T) {
	root := t.TempDir()
	corruptOpenCodeConfig(t, root)
	if _, err := (Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err == nil {
		t.Fatal("Run() error = nil, want the injection failure")
	}
	if setup.IsInstalled(root) {
		t.Error("a failed install was recorded as installed")
	}
}

func TestRecordCancelledWithNothingChangedRecordsNothing(t *testing.T) {
	root := t.TempDir()
	got, err := recordCancelled(root, setuputil.TestPlanRequest(), LifecycleResult{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != OutcomeCancelledNothingApplied {
		t.Errorf("Outcome = %v, want %v", got.Outcome, OutcomeCancelledNothingApplied)
	}
	if setup.IsInstalled(root) {
		t.Error("an empty cancelled run was recorded as installed")
	}
}

func TestRecordCancelledRecordsOnlyFullyAppliedRuns(t *testing.T) {
	request := setuputil.TestPlanRequest()
	pending := opencode.ConfigDir() + "/agents/takt.md"
	cases := []struct {
		name       string
		notApplied []string
		recorded   bool
	}{
		{"complete deployment is recorded", nil, true},
		{"leftover OpenCode artifact is not recorded", []string{pending}, false},
		{"leftover skill artifact alone is recorded", []string{"skills/x/SKILL.md"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			deployed := LifecycleResult{Changed: []string{"a"}, NotApplied: tc.notApplied}
			got, err := recordCancelled(root, request, deployed, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if got.Outcome != OutcomeCancelledPartial {
				t.Errorf("Outcome = %v, want %v", got.Outcome, OutcomeCancelledPartial)
			}
			if setup.IsInstalled(root) != tc.recorded {
				t.Errorf("IsInstalled() = %v, want %v", setup.IsInstalled(root), tc.recorded)
			}
		})
	}
}

func TestRunUninstallCancelledReturnsTypedOutcome(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	if _, err := (Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (Runtime{}).Run(ctx, "uninstall", root, request)
	if err != nil {
		t.Fatalf("Run(uninstall) error = %v, want nil for a cancellation", err)
	}
	if result.Outcome == OutcomeCompleted {
		t.Errorf("Outcome = %v, want a cancelled outcome", result.Outcome)
	}
	if !setup.IsInstalled(root) {
		t.Error("a cancelled uninstall forgot the installation, want it kept: part of it remains")
	}
}

func TestInstallCodegraphPropagatesAcquisitionFailure(t *testing.T) {
	sentinel := errors.New("no codegraph")
	withAcquireCodegraph(t, func(context.Context, string) (string, error) { return "", sentinel })
	if err := InstallCodegraph(context.Background(), t.TempDir()); !errors.Is(err, sentinel) {
		t.Fatalf("InstallCodegraph() error = %v, want %v", err, sentinel)
	}
}

func TestInjectServersReportInjectionFailure(t *testing.T) {
	root := t.TempDir()
	corruptOpenCodeConfig(t, root)
	if err := InjectEngram(root, "/bin/engram"); err == nil || !strings.Contains(err.Error(), "inject engram") {
		t.Errorf("InjectEngram() error = %v, want an 'inject engram' failure", err)
	}
	if err := InjectCodegraph(root, "/bin/codegraph"); err == nil || !strings.Contains(err.Error(), "inject codegraph") {
		t.Errorf("InjectCodegraph() error = %v, want an 'inject codegraph' failure", err)
	}
}

func TestInjectServerSkipsRegistrationWhenInjectionFails(t *testing.T) {
	registered := false
	err := injectServer("x", t.TempDir(), "/bin/x",
		func(string, string) (model.InjectionResult, error) {
			return model.InjectionResult{}, errors.New("boom")
		},
		func(string, string) error { registered = true; return nil })
	if err == nil || registered {
		t.Fatalf("injectServer() error = %v, registered = %v; want a failure and no registration", err, registered)
	}
}

func TestInjectServerPropagatesRegistrationResult(t *testing.T) {
	sentinel := errors.New("register failed")
	err := injectServer("x", t.TempDir(), "/bin/x",
		func(string, string) (model.InjectionResult, error) { return model.InjectionResult{}, nil },
		func(string, string) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("injectServer() error = %v, want %v", err, sentinel)
	}
}

func TestRegisterManagedPathRecordsOnlyTheManagedBinary(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "bin", "tool")
	writeFile(t, managed, ownedFileContent)

	if err := registerManagedPath(root, "/usr/bin/tool", managed, managed); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.LoadOwnershipManifest(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a reused binary created a manifest: err = %v", err)
	}

	if err := registerManagedPath(root, managed, managed, managed); err != nil {
		t.Fatal(err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := manifest.Entries["bin/tool"]
	if !ok || !slices.Contains(entry.Targets, setup.TargetOpenCode) {
		t.Fatalf("manifest entries = %v, want bin/tool owned by OpenCode", manifest.Entries)
	}
}

func TestRegisterOwnedFileIsIdempotentAndKeepsOtherOwners(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "bin", "tool")
	writeFile(t, file, ownedFileContent)

	// Seed the manifest with a skills-owned entry for the same file.
	seeded := setup.NewOwnershipManifest()
	entry, err := setup.NewOwnershipEntry("bin/tool", []byte(ownedFileContent), managedFileMode, false, "", "", setup.TargetSkills)
	if err != nil {
		t.Fatal(err)
	}
	if err := seeded.Add(entry); err != nil {
		t.Fatal(err)
	}
	if err := seeded.Save(root); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := registerOwnedFile(root, file, managedFileMode); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	targets := manifest.Entries["bin/tool"].Targets
	if len(targets) != 2 || !slices.Contains(targets, setup.TargetSkills) || !slices.Contains(targets, setup.TargetOpenCode) {
		t.Errorf("Targets = %v, want skills and opencode, each once", targets)
	}
}

func TestRegisterOwnedFileErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		root := t.TempDir()
		err := registerOwnedFile(root, filepath.Join(root, "absent"), managedFileMode)
		if err == nil || !strings.Contains(err.Error(), "record ownership of") {
			t.Fatalf("error = %v, want a record-ownership failure", err)
		}
	})
	t.Run("corrupt manifest", func(t *testing.T) {
		root := t.TempDir()
		file := filepath.Join(root, "tool")
		writeFile(t, file, ownedFileContent)
		writeFile(t, filepath.Join(root, setup.OwnershipManifestFilename), "{ not json")
		err := registerOwnedFile(root, file, managedFileMode)
		if err == nil || !strings.Contains(err.Error(), "load ownership manifest") {
			t.Fatalf("error = %v, want a manifest load failure", err)
		}
	})
	t.Run("empty file", func(t *testing.T) {
		root := t.TempDir()
		file := filepath.Join(root, "tool")
		writeFile(t, file, "")
		if err := registerOwnedFile(root, file, managedFileMode); err == nil {
			t.Fatal("error = nil, want ownership entries to require content")
		}
	})
}

func TestRegisterManagedBinariesOnlyRecordTaktInstalls(t *testing.T) {
	root := t.TempDir()
	if err := registerManagedEngram(root, "/usr/bin/engram"); err != nil {
		t.Fatalf("registerManagedEngram(reused) error = %v", err)
	}
	if err := registerManagedCodegraph(root, "/usr/bin/codegraph"); err != nil {
		t.Fatalf("registerManagedCodegraph(reused) error = %v", err)
	}
	if _, err := setup.LoadOwnershipManifest(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reused binaries created a manifest: err = %v", err)
	}

	engramBinary := engram.ManagedBinaryPath(root)
	writeFile(t, engramBinary, ownedFileContent)
	if err := registerManagedEngram(root, engramBinary); err != nil {
		t.Fatal(err)
	}
	codegraphBinary := codegraph.ManagedBinaryPath(root)
	writeFile(t, codegraphBinary, ownedFileContent)
	writeFile(t, filepath.Join(root, filepath.FromSlash(codegraph.ManagedMarkerKey())), ownedFileContent)
	if err := registerManagedCodegraph(root, codegraphBinary); err != nil {
		t.Fatal(err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := manifest.Entries[codegraph.ManagedMarkerKey()]; !ok {
		t.Errorf("manifest entries = %v, want the codegraph marker recorded", manifest.Entries)
	}
}

func TestRefreshManagedOwnership(t *testing.T) {
	t.Run("no manifest is a no-op", func(t *testing.T) {
		if err := refreshManagedOwnership(t.TempDir(), []string{"x"}); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
	})
	t.Run("corrupt manifest fails", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, setup.OwnershipManifestFilename), "{ not json")
		if err := refreshManagedOwnership(root, []string{"x"}); err == nil {
			t.Fatal("error = nil, want a manifest load failure")
		}
	})
	t.Run("re-baselines managed files and skips the rest", func(t *testing.T) {
		root := t.TempDir()
		managed := filepath.Join(root, "managed.json")
		writeFile(t, managed, ownedFileContent)
		manifest := setup.NewOwnershipManifest()
		entry, err := setup.NewOwnershipEntry("managed.json", []byte(ownedFileContent), 0o644, false, "", "", setup.TargetOpenCode)
		if err != nil {
			t.Fatal(err)
		}
		if err := manifest.Add(entry); err != nil {
			t.Fatal(err)
		}
		if err := manifest.Save(root); err != nil {
			t.Fatal(err)
		}
		writeFile(t, managed, "edited after injection")

		outside := filepath.Join(filepath.Dir(root), "outside.json")
		unmanaged := filepath.Join(root, "unmanaged.json")
		if err := refreshManagedOwnership(root, []string{outside, unmanaged, managed}); err != nil {
			t.Fatal(err)
		}
		reloaded, err := setup.LoadOwnershipManifest(root)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.Entries["managed.json"].SHA256 == entry.SHA256 {
			t.Error("SHA256 baseline unchanged, want it refreshed to the post-injection bytes")
		}
		if len(reloaded.Entries) != 1 {
			t.Errorf("entries = %v, want unmanaged files left out", reloaded.Entries)
		}
	})
	t.Run("only skipped files leave the manifest untouched", func(t *testing.T) {
		root := t.TempDir()
		manifest := setup.NewOwnershipManifest()
		if err := manifest.Save(root); err != nil {
			t.Fatal(err)
		}
		before, _ := os.Stat(filepath.Join(root, setup.OwnershipManifestFilename))
		if err := refreshManagedOwnership(root, []string{filepath.Join(root, "other")}); err != nil {
			t.Fatal(err)
		}
		after, _ := os.Stat(filepath.Join(root, setup.OwnershipManifestFilename))
		if !after.ModTime().Equal(before.ModTime()) {
			t.Error("manifest rewritten although nothing was managed")
		}
	})
	t.Run("unreadable managed file fails", func(t *testing.T) {
		root := t.TempDir()
		manifest := setup.NewOwnershipManifest()
		entry, err := setup.NewOwnershipEntry("gone.json", []byte(ownedFileContent), 0o644, false, "", "", setup.TargetOpenCode)
		if err != nil {
			t.Fatal(err)
		}
		if err := manifest.Add(entry); err != nil {
			t.Fatal(err)
		}
		if err := manifest.Save(root); err != nil {
			t.Fatal(err)
		}
		err = refreshManagedOwnership(root, []string{filepath.Join(root, "gone.json")})
		if err == nil || !strings.Contains(err.Error(), "refresh ownership for") {
			t.Fatalf("error = %v, want a refresh-ownership failure", err)
		}
	})
}

func TestRemoveServerWrapsFailure(t *testing.T) {
	sentinel := errors.New("cannot remove")
	err := removeServer("engram", t.TempDir(), func(string) (model.InjectionResult, error) { return model.InjectionResult{}, sentinel })
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "remove engram") {
		t.Fatalf("removeServer() error = %v, want %v wrapped as 'remove engram'", err, sentinel)
	}
	if err := removeServer("engram", t.TempDir(), func(string) (model.InjectionResult, error) { return model.InjectionResult{}, nil }); err != nil {
		t.Fatalf("removeServer() success case error = %v", err)
	}
}

func TestRemoveEngramAndCodegraphReportCorruptConfig(t *testing.T) {
	root := t.TempDir()
	corruptOpenCodeConfig(t, root)
	if err := removeEngram(root); err == nil {
		t.Error("removeEngram(corrupt config) error = nil, want a failure")
	}
	if err := removeCodegraph(root); err == nil {
		t.Error("removeCodegraph(corrupt config) error = nil, want a failure")
	}
}

func TestRemoveCodegraphClearsManagedTreeOnlyWhenMarkerIsGone(t *testing.T) {
	root := t.TempDir()
	leftover := filepath.Join(codegraph.ManagedPrefix(root), "node_modules", "pkg", "index.js")
	writeFile(t, leftover, "x")
	marker := filepath.Join(root, filepath.FromSlash(codegraph.ManagedMarkerKey()))

	// Marker still present: the manifest-driven uninstall has not dropped it,
	// so the tree must stay.
	writeFile(t, marker, "x")
	if err := removeCodegraph(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(leftover); err != nil {
		t.Fatalf("managed tree removed while its marker exists: %v", err)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := removeCodegraph(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(codegraph.ManagedPrefix(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed tree still present after the marker was dropped: %v", err)
	}
}

func TestInstallPlansIncludesSkillsPlan(t *testing.T) {
	plans, _, err := installPlans(setuputil.TestPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) < 2 {
		t.Fatalf("plans = %d, want the OpenCode plan plus the skills plan", len(plans))
	}
}

func TestRunInstallProceedsAfterSuccessfulHandshake(t *testing.T) {
	handshakes := 0
	runtime := Runtime{OpenCodeHandshake: func(context.Context) error { handshakes++; return nil }}
	if _, err := runtime.Run(context.Background(), "install", t.TempDir(), setuputil.TestPlanRequest()); err != nil {
		t.Fatal(err)
	}
	if handshakes != 1 {
		t.Errorf("handshake ran %d times, want exactly once before deployment", handshakes)
	}
}
