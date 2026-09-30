package runtime_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

const (
	// driftedConfig is a managed file whose content the tests replace to create drift.
	driftedConfig = ".config/opencode/opencode.json"
	// reassignedModel is the model the tests assign to it.
	reassignedModel = "fake/fake-model"
)

// reassignedAgent names the first deployed specialist instance, the one whose
// model the tests reassign.
func reassignedAgent(t *testing.T) string {
	t.Helper()
	loaded, err := catalog.LoadPackages()
	if err != nil {
		t.Fatalf("LoadPackages() error = %v", err)
	}
	for _, def := range loaded.Agents {
		if len(def.Instances) > 0 {
			return def.Instances[0]
		}
	}
	t.Fatal("catalog defines no specialist instances")
	return ""
}

// installedAdapterRoot installs a real root through the fake-provider adapter.
func installedAdapterRoot(t *testing.T) (string, runtime.Adapter) {
	t.Helper()
	root := t.TempDir()
	adapter := testAdapter(nil)
	if _, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionInstall, RootDir: root}); err != nil {
		t.Fatalf("install error = %v", err)
	}
	return root, adapter
}

func TestCorrectDriftRestoresSelectedFileAndReinjectsIntegrations(t *testing.T) {
	root, adapter := installedAdapterRoot(t)
	full := filepath.Join(root, filepath.FromSlash(driftedConfig))
	if err := os.WriteFile(full, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionCorrectDrift, RootDir: root, SelectedDriftPaths: []string{driftedConfig}})
	if err != nil {
		t.Fatalf("correct drift error = %v", err)
	}
	if result.Action != runtime.ActionCorrectDrift || result.Outcome != lifecycle.OutcomeCompleted || !slices.Contains(result.Changed, driftedConfig) {
		t.Fatalf("result = %+v, want a completed correction that restored %s", result, driftedConfig)
	}
	data, err := os.ReadFile(full)
	if err != nil || string(data) == "{}" {
		t.Fatalf("restored file = %q, %v; want the drifted content replaced", data, err)
	}
	if !strings.Contains(string(data), "engram") {
		t.Errorf("restored file lacks the re-injected Engram integration:\n%s", data)
	}
}

func TestCorrectDriftLeavesUnplannedPathsUnresolvedAndSkipsReinjection(t *testing.T) {
	root, adapter := installedAdapterRoot(t)
	before, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(driftedConfig)))
	if err != nil {
		t.Fatal(err)
	}

	const unplanned = "not/a/managed/path.md"
	result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionCorrectDrift, RootDir: root, SelectedDriftPaths: []string{unplanned}})
	if err != nil {
		t.Fatalf("correct drift error = %v", err)
	}
	if !slices.Equal(result.Unresolved, []string{unplanned}) || len(result.Changed) != 0 {
		t.Fatalf("result = %+v, want the unplanned path unresolved and nothing changed", result)
	}
	after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(driftedConfig)))
	if err != nil || string(after) != string(before) {
		t.Errorf("managed file changed although nothing was restored: %v", err)
	}
}

func TestCorrectDriftCancelledBeforeAnyWriteChangesNothing(t *testing.T) {
	root, adapter := installedAdapterRoot(t)
	full := filepath.Join(root, filepath.FromSlash(driftedConfig))
	if err := os.WriteFile(full, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	message := adapter.Command(ctx, runtime.ActionRequest{Action: runtime.ActionCorrectDrift, RootDir: root, SelectedDriftPaths: []string{driftedConfig}})().(runtime.ActionResultMsg)
	if message.Err != nil {
		if !errors.Is(message.Err, context.Canceled) {
			t.Fatalf("error = %v, want cancellation", message.Err)
		}
		return
	}
	if message.Result.Outcome != lifecycle.OutcomeCancelledNothingApplied || len(message.Result.Changed) != 0 || !message.Result.CancelRequested {
		t.Fatalf("result = %+v, want a cancelled correction that applied nothing", message.Result)
	}
	if data, err := os.ReadFile(full); err != nil || string(data) != "{}" {
		t.Errorf("cancelled correction wrote the file: %q, %v", data, err)
	}
}

func TestReassignModelsRedeploysOnlyTheChangedSpecialist(t *testing.T) {
	root, adapter := installedAdapterRoot(t)
	agent := reassignedAgent(t)
	overrides := map[string]model.ModelAssignment{agent: {Model: reassignedModel}}

	result, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionReassignModels, RootDir: root, ReassignOverrides: overrides})
	if err != nil {
		t.Fatalf("reassign error = %v", err)
	}
	if result.Action != runtime.ActionReassignModels || result.Outcome != lifecycle.OutcomeCompleted || len(result.Changed) == 0 {
		t.Fatalf("result = %+v, want a completed reassignment that changed files", result)
	}
	if len(result.Unchanged) == 0 {
		t.Errorf("result = %+v, want the other specialists left unchanged", result)
	}
	installed, err := setup.LoadInstalledConfig(root)
	if err != nil || installed.OpenCodeModelOverrides[agent].Model != reassignedModel {
		t.Fatalf("installed overrides = %+v, %v; want the assignment recorded", installed.OpenCodeModelOverrides, err)
	}

	// Repeating the same assignment has no changed specialist to redeploy.
	again, err := adapter.Execute(runtime.ActionRequest{Action: runtime.ActionReassignModels, RootDir: root, ReassignOverrides: overrides})
	if err != nil || len(again.Changed) != 0 {
		t.Errorf("repeat = %+v, %v; want no changes", again, err)
	}
}

func TestReassignModelsReportsEverythingNotAppliedWhenCancelledUpFront(t *testing.T) {
	root, adapter := installedAdapterRoot(t)
	agent := reassignedAgent(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := adapter.ExecuteContext(ctx, runtime.ActionRequest{
		Action: runtime.ActionReassignModels, RootDir: root,
		ReassignOverrides: map[string]model.ModelAssignment{agent: {Model: reassignedModel}},
	})
	if err != nil {
		t.Fatalf("error = %v, want a cancelled outcome instead", err)
	}
	if !slices.Equal(result.NotApplied, []string{agent}) || !result.Cancelled() || len(result.Changed) != 0 {
		t.Errorf("result = %+v, want %s reported as not applied", result, agent)
	}
}

func TestReassignModelsFailsWithoutAnInstallation(t *testing.T) {
	_, err := testAdapter(nil).Execute(runtime.ActionRequest{
		Action: runtime.ActionReassignModels, RootDir: t.TempDir(),
		ReassignOverrides: map[string]model.ModelAssignment{reassignedAgent(t): {Model: reassignedModel}},
	})
	if err == nil {
		t.Fatal("reassigning models with nothing installed returned no error")
	}
}

func TestCommandReportsProgressToTheObserverAndVerifiesInstalls(t *testing.T) {
	var events []setup.DeploymentProgress
	request := runtime.ActionRequest{ID: 21, Action: runtime.ActionInstall, RootDir: t.TempDir()}
	message := testAdapter(nil).Command(context.Background(), request, func(event setup.DeploymentProgress) {
		events = append(events, event)
	})().(runtime.ActionResultMsg)
	if message.Err != nil || message.Result.Verify == nil {
		t.Fatalf("result = %+v, %v; want a verified install", message.Result, message.Err)
	}
	if message.Request.ID != request.ID {
		t.Errorf("result request ID = %d, want %d", message.Request.ID, request.ID)
	}
	if len(events) == 0 {
		t.Fatal("observer received no progress events")
	}
	if last := events[len(events)-1]; last.Stage != "preparing" || last.Message != "Verifying installation" {
		t.Errorf("last progress event = %+v, want the verification stage", last)
	}
}

func TestPreviewPlanClassifiesPlannedFilesAgainstDisk(t *testing.T) {
	root, adapter := installedAdapterRoot(t)
	const edited, removed = ".config/opencode/opencode.json", ".config/opencode/takt/README.md"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(edited)), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(removed))); err != nil {
		t.Fatal(err)
	}

	plan, err := adapter.PreviewPlan(runtime.PreviewRequest{Action: runtime.ActionSync, RootDir: root})
	if err != nil {
		t.Fatalf("PreviewPlan() error = %v", err)
	}
	if !slices.Contains(plan.Modify, edited) || slices.Contains(plan.Add, edited) {
		t.Errorf("Modify = %v Add = %v, want %s classified as modified", plan.Modify, plan.Add, edited)
	}
	if !slices.Contains(plan.Add, removed) || slices.Contains(plan.Modify, removed) {
		t.Errorf("Add = %v Modify = %v, want %s classified as added", plan.Add, plan.Modify, removed)
	}
}

func TestPreviewPlanReportsAnUninspectablePlannedPath(t *testing.T) {
	root, adapter := installedAdapterRoot(t)
	const blocked = ".config/opencode/takt/README.md"
	full := filepath.Join(root, filepath.FromSlash(blocked))
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	// A directory where a file is planned cannot be read as content.
	if err := os.Mkdir(full, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.PreviewPlan(runtime.PreviewRequest{Action: runtime.ActionSync, RootDir: root})
	if err == nil || !strings.Contains(err.Error(), "inspect") || !strings.Contains(err.Error(), blocked) {
		t.Errorf("PreviewPlan() error = %v, want an inspect error naming %s", err, blocked)
	}
}
