package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestValidateComponentsRejectsBadNames(t *testing.T) {
	all, err := AllComponents()
	if err != nil || len(all) == 0 {
		t.Fatalf("AllComponents() = %v, %v", all, err)
	}
	valid := string(all[0])
	tests := []struct {
		name    string
		input   []string
		wantErr string
	}{
		{"empty", []string{" "}, "component name is empty"},
		{"unknown", []string{"no-such-component"}, `unknown component "no-such-component"`},
		{"duplicate", []string{valid, valid}, "duplicate component"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ValidateComponents(tt.input); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateComponents() error = %v, want containing %q", err, tt.wantErr)
			}
			if _, _, err := ResolveComponents(tt.input); err == nil {
				t.Error("ResolveComponents accepted the same invalid input")
			}
			request := PlanRequest{Components: tt.input}
			if _, _, err := BuildTargetPlans(request); err == nil {
				t.Error("BuildTargetPlans accepted the same invalid input")
			}
		})
	}
}

func TestValidateComponentsFollowsCanonicalOrder(t *testing.T) {
	all, err := AllComponents()
	if err != nil || len(all) < 2 {
		t.Skipf("need two selectable components, have %v (%v)", all, err)
	}
	got, err := ValidateComponents([]string{string(all[1]), string(all[0])})
	if err != nil || !slices.Equal(got, all[:2]) {
		t.Fatalf("ValidateComponents() = %v, %v; want canonical order %v", got, err, all[:2])
	}
}

func TestMergedOverridesSetsAndClearsWithoutMutatingInput(t *testing.T) {
	existing := map[string]model.ModelAssignment{"a": {Model: "m1"}, "b": {Model: "m2"}}
	set := mergedOverrides(existing, map[string]model.ModelAssignment{"c": {Model: "m3"}})
	cleared := mergedOverrides(existing, map[string]model.ModelAssignment{"a": {}})
	if len(set) != 3 || set["c"].Model != "m3" {
		t.Errorf("set = %v", set)
	}
	if _, has := cleared["a"]; has || len(cleared) != 1 {
		t.Errorf("cleared = %v", cleared)
	}
	if len(existing) != 2 || existing["a"].Model != "m1" {
		t.Errorf("input mutated: %v", existing)
	}
}

func TestChangedArtifactPathSetAndUnchangedPaths(t *testing.T) {
	before := []TargetPlan{{Artifacts: []Artifact{{Path: "same", Content: []byte("1")}, {Path: "edit", Content: []byte("1")}}}}
	after := []TargetPlan{{Artifacts: []Artifact{{Path: "same", Content: []byte("1")}, {Path: "edit", Content: []byte("2")}, {Path: "new", Content: []byte("1")}}}}
	changed := changedArtifactPathSet(before, after)
	if len(changed) != 2 || !changed["edit"] || !changed["new"] {
		t.Fatalf("changed = %v", changed)
	}
	if got := unchangedPaths([]string{"same", "edit", "new"}, changed); !slices.Equal(got, []string{"same"}) {
		t.Errorf("unchangedPaths = %v", got)
	}
}

func TestCorrectDriftContextRejectsEmptyPlansAndReportsCancellation(t *testing.T) {
	if _, err := CorrectDriftContext(context.Background(), t.TempDir(), nil, nil, ProviderRuntime{}); err == nil {
		t.Error("CorrectDriftContext without plans succeeded")
	}
	root := t.TempDir()
	plans := opsPlan("opencode", map[string]string{"a.txt": "a"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := CorrectDriftContext(ctx, root, plans, []string{"a.txt", "ghost.txt"}, ProviderRuntime{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !slices.Equal(result.Unresolved, []string{"ghost.txt"}) || !slices.Equal(result.NotApplied, []string{"a.txt"}) {
		t.Errorf("result = %+v", result)
	}
}

func TestCorrectDriftContextPropagatesNonCancellationFailure(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, OwnershipManifestFilename), "{")
	_, err := CorrectDriftContext(context.Background(), root, opsPlan("opencode", map[string]string{"a.txt": "a"}), []string{"a.txt"}, ProviderRuntime{})
	if err == nil || !strings.Contains(err.Error(), "parse ownership manifest") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyModelOverrideChangeSurfacesApplyFailure(t *testing.T) {
	root := t.TempDir()
	if err := RecordInstallation(root, defaultRequestForTest(t)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, OwnershipManifestFilename), "{")
	_, err := ApplyModelOverrideChanges(context.Background(), root, map[string]model.ModelAssignment{"takt-dev": {Model: "provider/model"}}, ProviderRuntime{})
	if err == nil || !strings.Contains(err.Error(), "parse ownership manifest") {
		t.Fatalf("error = %v", err)
	}
}

func TestMergePreexistingConfigReadFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "cfg.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := mergePreexistingConfig(root, Artifact{Path: "cfg.json", Content: []byte("{}")}, NewOwnershipManifest())
	if err == nil || !strings.Contains(err.Error(), "read existing config") {
		t.Fatalf("error = %v", err)
	}
}

func TestMergePreexistingConfigMergesUserKeys(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "cfg.json"), `{"user":1}`)
	merged, err := mergePreexistingConfig(root, Artifact{Path: "cfg.json", Content: []byte(`{"takt":2}`)}, NewOwnershipManifest())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged.Content), `"user"`) || !strings.Contains(string(merged.Content), `"takt"`) {
		t.Errorf("merged = %s, want both keys", merged.Content)
	}
}

func TestForgetInstallationAndVersionHelpers(t *testing.T) {
	root := t.TempDir()
	if got := InstalledVersion(root); got != "" {
		t.Errorf("InstalledVersion(empty) = %q", got)
	}
	if err := RecordInstallation(root, defaultRequestForTest(t)); err != nil {
		t.Fatal(err)
	}
	if got := InstalledVersion(root); got != BuildVersion || !IsCurrentVersion(got) || IsCurrentVersion("other") {
		t.Errorf("version helpers disagree: %q vs %q", got, BuildVersion)
	}
	if err := ForgetInstallation(root); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadInstalledConfig(root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("installed config survived ForgetInstallation: %v", err)
	}
	if err := ForgetInstallation(root); err != nil {
		t.Errorf("forgetting twice: %v", err)
	}
	writeFile(t, filepath.Join(root, InstalledConfigFilename, "child"), "x")
	if err := ForgetInstallation(root); err == nil || !strings.Contains(err.Error(), "remove installed config") {
		t.Errorf("error = %v, want removal failure", err)
	}
}

func TestOperationRecordFailuresAndNotice(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "blocker"), "file")
	if _, err := BeginOperation(filepath.Join(dir, "blocker", "root"), "install"); err == nil {
		t.Error("BeginOperation under a regular file succeeded")
	}
	if err := os.Mkdir(filepath.Join(dir, OperationRecordFilename), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginOperation(dir, "install"); err == nil {
		t.Error("BeginOperation over a directory succeeded")
	}

	record, found := IncompleteOperation(func() string {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, OperationRecordFilename), "torn{")
		return root
	}())
	if !found || record.Action != "" {
		t.Errorf("torn record = %+v, %v; want found with empty action", record, found)
	}

	notice := OperationRecord{Action: "mystery"}.Notice(t.TempDir())
	if len(notice) != 2 || !strings.Contains(notice[0], "previous operation did not finish.") || strings.Contains(notice[0], "started") {
		t.Errorf("notice = %q", notice)
	}
	if !strings.Contains(notice[1], "not automatic") {
		t.Errorf("recovery = %q", notice[1])
	}
	known := OperationRecord{Action: "reassign-models", Started: record.Started}.Notice(t.TempDir())
	if !strings.Contains(known[0], "model assignment") {
		t.Errorf("notice = %q", known)
	}
}

func defaultRequestForTest(t *testing.T) PlanRequest {
	t.Helper()
	request, err := DefaultPlanRequest()
	if err != nil {
		t.Fatal(err)
	}
	return request
}
