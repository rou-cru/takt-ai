package setup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// shortActionTimeout keeps the timeout test fast while far exceeding process start-up jitter.
const shortActionTimeout = 200 * time.Millisecond

// longSleepSeconds is far beyond shortActionTimeout so the sleep can only end by cancellation.
const longSleepSeconds = "30"

// floodBytes is far larger than any sane capture limit for one action's output.
const floodBytes = 200000

func actionPlan(actions ...setup.ProviderAction) []setup.TargetPlan {
	plans := simplePlan("hello")
	plans[0].Actions = actions
	return plans
}

func foundLookup(program string) (string, error) { return "/stub/" + program, nil }

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertNoStagedFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && path != root && strings.HasPrefix(entry.Name(), ".takt-setup-") {
			t.Errorf("staged file remains: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestApplyContextRejectsInvalidProviderActionsBeforeWriting(t *testing.T) {
	tests := map[string]setup.ProviderAction{
		"blank ID":        {ID: " ", Program: "p"},
		"padded ID":       {ID: " a", Program: "p"},
		"NUL in ID":       {ID: "a\x00", Program: "p"},
		"blank program":   {ID: "a", Program: ""},
		"padded program":  {ID: "a", Program: "p "},
		"NUL in argument": {ID: "a", Program: "p", Args: []string{"ok", "b\x00"}},
	}
	for name, action := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			_, err := setup.ApplyContext(context.Background(), root, actionPlan(action), setup.ProviderRuntime{LookPath: foundLookup})
			if err == nil {
				t.Fatal("invalid action accepted")
			}
			if _, statErr := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(statErr) {
				t.Errorf("files written despite invalid action: %v", statErr)
			}
		})
	}
}

func TestApplyContextReportsActionProgressAndFailureDetail(t *testing.T) {
	var events []setup.DeploymentProgress
	failure := errors.New("exit status 3")
	runtime := setup.ProviderRuntime{
		LookPath: foundLookup,
		Run: func(_ context.Context, program string, _ ...string) ([]byte, error) {
			if program == "bad" {
				return []byte("  boom detail\n"), failure
			}
			return nil, nil
		},
	}
	actions := []setup.ProviderAction{{ID: "one", Program: "good"}, {ID: "two", Program: "bad"}, {ID: "three", Program: "good"}}

	result, err := setup.ApplyContextProgress(context.Background(), t.TempDir(), actionPlan(actions...), runtime,
		func(event setup.DeploymentProgress) { events = append(events, event) })
	if !errors.Is(err, failure) || !strings.HasSuffix(err.Error(), ": boom detail") || !strings.Contains(err.Error(), "two") {
		t.Fatalf("error = %v, want wrapped failure naming the action with trimmed output", err)
	}
	if len(result.Actions) != 1 || result.Actions[0] != "one" {
		t.Errorf("Actions = %v, want only the completed action", result.Actions)
	}
	var twoEvents int
	for _, event := range events {
		if event.Path == "two" {
			twoEvents++
			if event.Completed != 1 || event.Total != len(actions) {
				t.Errorf("progress for failing action = %+v", event)
			}
		}
		if event.Path == "three" {
			t.Errorf("progress reported for an action after the failure: %+v", event)
		}
	}
	if twoEvents != 1 {
		t.Errorf("progress events for failing action = %d, want 1", twoEvents)
	}
}

func TestApplyContextActionFailureWithoutOutput(t *testing.T) {
	failure := errors.New("nope")
	runtime := setup.ProviderRuntime{
		LookPath: foundLookup,
		Run:      func(context.Context, string, ...string) ([]byte, error) { return []byte(" \n"), failure },
	}
	_, err := setup.ApplyContext(context.Background(), t.TempDir(), actionPlan(setup.ProviderAction{ID: "a", Program: "p"}), runtime)
	if !errors.Is(err, failure) || strings.HasSuffix(err.Error(), ":") || strings.HasSuffix(strings.TrimSpace(err.Error()), ": ") {
		t.Fatalf("error = %v, want the failure without an output suffix", err)
	}
}

func TestApplyContextDefaultRunnerCapturesOutputAndExitStatus(t *testing.T) {
	action := setup.ProviderAction{ID: "a", Program: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}}
	_, err := setup.ApplyContext(context.Background(), t.TempDir(), actionPlan(action), setup.ProviderRuntime{})
	if err == nil {
		t.Fatal("non-zero exit reported success")
	}
	for _, want := range []string{"out", "err", "exit status 3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestApplyContextDefaultRunnerPrefersContextErrorOnTimeout(t *testing.T) {
	action := setup.ProviderAction{ID: "a", Program: "sleep", Args: []string{longSleepSeconds}}
	_, err := setup.ApplyContext(context.Background(), t.TempDir(), actionPlan(action), setup.ProviderRuntime{Timeout: shortActionTimeout})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestApplyContextBoundsCapturedActionOutput(t *testing.T) {
	action := setup.ProviderAction{ID: "a", Program: "sh", Args: []string{"-c", "head -c 200000 /dev/zero | tr '\\0' x; exit 1"}}
	_, err := setup.ApplyContext(context.Background(), t.TempDir(), actionPlan(action), setup.ProviderRuntime{})
	if err == nil {
		t.Fatal("failing action reported success")
	}
	if !strings.Contains(err.Error(), "xxxx") || len(err.Error()) >= floodBytes/2 {
		t.Errorf("error length = %d, want a non-empty prefix far below the %d bytes emitted", len(err.Error()), floodBytes)
	}
}

func TestManifestSaveFailures(t *testing.T) {
	manifest := setup.NewOwnershipManifest()
	if err := manifest.Save(" "); err == nil {
		t.Error("blank root saved")
	}
	blocked := t.TempDir()
	mustWriteFile(t, filepath.Join(blocked, "blocker"), "file")
	if err := manifest.Save(filepath.Join(blocked, "blocker", "sub")); err == nil {
		t.Error("save beneath a regular file succeeded")
	}
	t.Run("staging in read-only root", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission checks do not apply to root")
		}
		root := t.TempDir()
		if err := os.Chmod(root, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
		if err := manifest.Save(root); err == nil {
			t.Error("save into read-only root succeeded")
		}
	})
	t.Run("rename failure removes the staged file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, setup.OwnershipManifestFilename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := manifest.Save(root); err == nil {
			t.Fatal("save over a directory succeeded")
		}
		assertNoStagedFiles(t, root)
	})
}

func conflictsByPath(t *testing.T, root string, plans []setup.TargetPlan) map[string]setup.ConflictEntry {
	t.Helper()
	conflicts, err := setup.DetectConflicts(root, plans)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]setup.ConflictEntry{}
	for _, conflict := range conflicts {
		byPath[conflict.Path] = conflict
	}
	return byPath
}

func TestRiskAcceptanceIsUpsertedAndSurvivesCorruption(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "kept a")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "kept b")
	plans := []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{"a.txt", "b.txt"},
		Artifacts:    []setup.Artifact{{Path: "a.txt", Content: []byte("new a")}, {Path: "b.txt", Content: []byte("new b")}},
	}}
	conflicts := conflictsByPath(t, root, plans)
	accept := func(path string) setup.RiskAcceptance {
		return setup.RiskAcceptance{Path: path, SHA256: conflicts[path].SHA256, Impact: conflicts[path].Impact}
	}
	if err := setup.RecordRiskAcceptances(root, []setup.RiskAcceptance{accept("a.txt")}); err != nil {
		t.Fatal(err)
	}
	if err := setup.RecordRiskAcceptances(root, []setup.RiskAcceptance{accept("b.txt")}); err != nil {
		t.Fatal(err)
	}
	got := conflictsByPath(t, root, plans)
	if !got["a.txt"].Accepted || !got["b.txt"].Accepted {
		t.Fatalf("accepted = a:%v b:%v, want a kept after recording b", got["a.txt"].Accepted, got["b.txt"].Accepted)
	}

	stale := accept("b.txt")
	stale.SHA256 = "stale"
	if err := setup.RecordRiskAcceptances(root, []setup.RiskAcceptance{stale}); err != nil {
		t.Fatal(err)
	}
	got = conflictsByPath(t, root, plans)
	if got["b.txt"].Accepted || !got["a.txt"].Accepted {
		t.Errorf("accepted = a:%v b:%v, want b replaced and a kept", got["a.txt"].Accepted, got["b.txt"].Accepted)
	}

	mustWriteFile(t, filepath.Join(root, setup.RiskAcceptanceFilename), "not json")
	for path, conflict := range conflictsByPath(t, root, plans) {
		if conflict.Accepted {
			t.Errorf("%s accepted from a corrupt record", path)
		}
	}
}

func TestRecordRiskAcceptancesWriteFailures(t *testing.T) {
	accepted := []setup.RiskAcceptance{{Path: "a"}}
	if err := setup.RecordRiskAcceptances(filepath.Join(t.TempDir(), "absent"), accepted); err == nil {
		t.Error("missing root recorded")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, setup.RiskAcceptanceFilename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := setup.RecordRiskAcceptances(root, accepted); err == nil {
		t.Fatal("record over a directory succeeded")
	}
	assertNoStagedFiles(t, root)
}
