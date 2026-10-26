package setup_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// plantManifestSaveFailure returns a progress callback that obstructs the
// ownership-manifest save once deployment is under way: applyPlans loads the
// manifest before deploying, so the planted directory breaks only the save
// that follows the commit.
func plantManifestSaveFailure(t *testing.T, root string) func(setup.DeploymentProgress) {
	t.Helper()
	manifestDir := filepath.Join(root, setup.OwnershipManifestFilename)
	return func(setup.DeploymentProgress) {
		if _, err := os.Stat(manifestDir); errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(manifestDir, setup.ManagedDirectoryMode); err != nil {
				t.Errorf("plant manifest obstruction: %v", err)
			}
		}
	}
}

// TestManifestSaveFailureAfterCommitIsNonSuccess covers the failure case: a
// deployment whose files committed and whose ownership-manifest save then
// fails reports non-success listing the committed changes in the returned
// result, and the committed inventory
// stays observable on disk.
func TestManifestSaveFailureAfterCommitIsNonSuccess(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("hello")
	result, err := setup.ApplyContextProgress(context.Background(), root, plans, setup.ProviderRuntime{}, plantManifestSaveFailure(t, root))
	if err == nil {
		t.Fatal("ApplyContextProgress() error = nil, want the manifest save failure as non-success")
	}
	if !slices.Equal(result.Changed, []string{"a.txt"}) {
		t.Errorf("Changed = %v, want [a.txt]: the committed inventory stays beside the manifest-save error", result.Changed)
	}
	content, readErr := os.ReadFile(filepath.Join(root, "a.txt"))
	if readErr != nil || string(content) != "hello" {
		t.Errorf("a.txt = %q, %v, want the committed content %q", content, readErr, "hello")
	}
	if _, manifestErr := setup.LoadOwnershipManifest(root); manifestErr == nil {
		t.Errorf("LoadOwnershipManifest() error = nil, want the unsaved manifest to leave tracking limited to disk")
	}
}

// TestRetryAfterManifestFailureKeepsCommittedWork covers the
// bookkeeping failure: retrying after the manifest save failed reports the
// committed files as completed work instead of deploying them again, and the
// retry records their ownership.
func TestRetryAfterManifestFailureKeepsCommittedWork(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("hello")
	if _, err := setup.ApplyContextProgress(context.Background(), root, plans, setup.ProviderRuntime{}, plantManifestSaveFailure(t, root)); err == nil {
		t.Fatal("first ApplyContextProgress() error = nil, want the manifest save failure")
	}
	if err := os.Remove(filepath.Join(root, setup.OwnershipManifestFilename)); err != nil {
		t.Fatalf("remove manifest obstruction: %v", err)
	}
	result, err := setup.ApplyContextProgress(context.Background(), root, plans, setup.ProviderRuntime{}, nil)
	if err != nil {
		t.Fatalf("retry ApplyContextProgress() error = %v", err)
	}
	if len(result.Changed) != 0 {
		t.Errorf("retry Changed = %v, want none: committed work is not deployed again", result.Changed)
	}
	if len(result.Unchanged) != 1 || result.Unchanged[0] != "a.txt" {
		t.Errorf("retry Unchanged = %v, want [a.txt] as the completed work", result.Unchanged)
	}
	manifest, manifestErr := setup.LoadOwnershipManifest(root)
	if manifestErr != nil {
		t.Fatalf("LoadOwnershipManifest() after retry error = %v", manifestErr)
	}
	if _, owned := manifest.Entries["a.txt"]; !owned {
		t.Errorf("ownership entries = %v, want a.txt recorded after the retry", manifest.Entries)
	}
}

// TestRetryAfterCancelledDeployReportsCompletedAndRemaining covers the retry
// inventory: a stop between artifacts leaves committed work on
// disk, and the retry reports that work unchanged and the rest changed.
func TestRetryAfterCancelledDeployReportsCompletedAndRemaining(t *testing.T) {
	root := t.TempDir()
	artifacts := []setup.Artifact{
		{Path: "a.txt", Content: []byte("one")},
		{Path: "b.txt", Content: []byte("two")},
	}
	managed := []string{"a.txt", "b.txt"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopAfterFirstCommit := func(progress setup.DeploymentProgress) {
		if progress.Stage == "applied" && progress.Completed == 1 {
			cancel()
		}
	}
	first, err := setup.DeployContextProgress(ctx, root, managed, artifacts, stopAfterFirstCommit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DeployContextProgress() error = %v, want the cancellation as the reported cause", err)
	}
	if !slices.Equal(first.Changed, []string{"a.txt"}) || !slices.Equal(first.NotApplied, []string{"b.txt"}) {
		t.Errorf("stopped result Changed = %v NotApplied = %v, want [a.txt] committed and [b.txt] remaining", first.Changed, first.NotApplied)
	}
	if content, readErr := os.ReadFile(filepath.Join(root, "a.txt")); readErr != nil || string(content) != "one" {
		t.Errorf("a.txt = %q, %v, want the committed content %q", content, readErr, "one")
	}
	if _, statErr := os.Stat(filepath.Join(root, "b.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("b.txt exists after the stop: %v, want the remaining work not deployed", statErr)
	}
	retry, err := setup.DeployContextProgress(context.Background(), root, managed, artifacts, nil)
	if err != nil {
		t.Fatalf("retry DeployContextProgress() error = %v", err)
	}
	if !slices.Equal(retry.Unchanged, []string{"a.txt"}) {
		t.Errorf("retry Unchanged = %v, want [a.txt] as the completed work", retry.Unchanged)
	}
	if !slices.Equal(retry.Changed, []string{"b.txt"}) {
		t.Errorf("retry Changed = %v, want [b.txt] as the remaining work", retry.Changed)
	}
}

// TestSyncReportsRetainedDriftAsSuccess covers PR-INS-24/25: a locally
// edited managed file is kept, listed in Retained, and the sync succeeds.
func TestSyncReportsRetainedDriftAsSuccess(t *testing.T) {
	root := t.TempDir()
	plans := simplePlan("deployed")
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	edited := []byte("edited")
	path := plans[0].Artifacts[0].Path
	if err := os.WriteFile(filepath.Join(root, path), edited, setup.ManagedFileMode); err != nil {
		t.Fatal(err)
	}
	result, err := setup.SyncContextProgress(context.Background(), root, plans, setup.ProviderRuntime{}, nil)
	if err != nil {
		t.Fatalf("SyncContextProgress() error = %v, want success with retained drift", err)
	}
	if !slices.Equal(result.Retained, []string{path}) {
		t.Errorf("Retained = %v, want [%s]", result.Retained, path)
	}
	if content, readErr := os.ReadFile(filepath.Join(root, path)); readErr != nil || string(content) != string(edited) {
		t.Errorf("%s = %q, %v, want the edit kept", path, content, readErr)
	}
}

// A result with no incomplete or conflicting work carries no such keys, so a
// strict --json consumer never meets null where it expects a list.
func TestResultsOmitEmptyOutcomeLists(t *testing.T) {
	for name, result := range map[string]any{
		"deployment": setup.DeploymentResult{Changed: []string{"a.txt"}, Unchanged: []string{}},
		"restore":    setup.RestoreResult{Restored: []string{"a.txt"}},
	} {
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("%s: Marshal() error = %v", name, err)
		}
		for _, key := range []string{"incomplete", "conflicts", "preserved"} {
			if strings.Contains(string(encoded), `"`+key+`"`) {
				t.Errorf("%s: %s carries %q for an empty list", name, encoded, key)
			}
		}
	}
}
