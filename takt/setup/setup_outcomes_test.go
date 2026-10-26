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

	"github.com/rou-cru/takt-ai/takt/doctor"
	"github.com/rou-cru/takt-ai/takt/model"
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

// TestProjectionKeepsIncompleteFacts covers the deployment
// result's projection keeps the incomplete cause, the completed work, and
// the remaining work.
func TestProjectionKeepsIncompleteFacts(t *testing.T) {
	result := setup.DeploymentResult{
		Changed:    []string{"a.txt"},
		Unchanged:  []string{"b.txt"},
		NotApplied: []string{"c.txt"},
		Incomplete: []string{"sandbox adapter dependency was not installed: npm unavailable"},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var projection map[string]json.RawMessage
	if err := json.Unmarshal(data, &projection); err != nil {
		t.Fatalf("unmarshal projection: %v", err)
	}
	for _, key := range []string{"changed", "unchanged", "notApplied", "incomplete"} {
		if _, ok := projection[key]; !ok {
			t.Errorf("projection %s lacks key %q", data, key)
		}
	}
	if !strings.Contains(string(projection["incomplete"]), "sandbox adapter dependency was not installed") {
		t.Errorf("incomplete = %s, want the dependency failure cause kept", projection["incomplete"])
	}
	// The keys survive a zero result, so no projection can hide the facts.
	empty, err := json.Marshal(setup.DeploymentResult{})
	if err != nil {
		t.Fatalf("marshal empty result: %v", err)
	}
	if !strings.Contains(string(empty), `"incomplete":`) {
		t.Errorf("empty result %s lacks key %q", empty, "incomplete")
	}
}

// TestDoctorWarnsDriftReadinessSeparatelyFromOutcome covers the readiness case:
// drift on fully deployed work keeps the deployment outcome a pass while
// readiness warns on its own line.
func TestDoctorWarnsDriftReadinessSeparatelyFromOutcome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// An empty PATH and a dead Engram address keep the unrelated checks
	// deterministic; only the deployment rows are asserted.
	t.Setenv("PATH", "")
	t.Setenv(model.EnvEngramURL, "http://127.0.0.1:1")
	t.Chdir(t.TempDir())

	manifest := setup.NewOwnershipManifest()
	deployed := []struct{ path, recorded, onDisk string }{
		{"opencode/kept.md", "deployed", "deployed"},
		{"opencode/drifted.md", "deployed", "edited"},
	}
	for _, file := range deployed {
		entry, err := setup.NewOwnershipEntry(file.path, []byte(file.recorded), 0o644, false, "", "", setup.TargetOpenCode)
		if err != nil {
			t.Fatalf("ownership entry %s: %v", file.path, err)
		}
		if err := manifest.Add(entry); err != nil {
			t.Fatalf("manifest add %s: %v", file.path, err)
		}
		path := filepath.Join(home, filepath.FromSlash(file.path))
		if err := os.MkdirAll(filepath.Dir(path), setup.ManagedDirectoryMode); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(file.onDisk), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := manifest.Save(home); err != nil {
		t.Fatalf("manifest save: %v", err)
	}

	var out strings.Builder
	// Missing tools make the verdict unhealthy; the deployment rows are
	// what this run asserts.
	_ = doctor.Run(&out)
	report := out.String()
	if !strings.Contains(report, "[!!]  deployment:readiness") || !strings.Contains(report, "1 of 2 managed files differ from deployed content") {
		t.Errorf("report lacks the separate readiness warning:\n%s", report)
	}
	if !strings.Contains(report, "[ok]  deployment:manifest") || !strings.Contains(report, "2 managed files deployed") {
		t.Errorf("report lacks the passing deployment outcome:\n%s", report)
	}
}
