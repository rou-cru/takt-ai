// Package runtime provides Bubble Tea lifecycle action boundaries.
package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// Action names a lifecycle operation.
type Action string

// Actions are the closed set of lifecycle operations the UI can request.
const (
	// ActionInstall deploys managed artifacts so a fresh root becomes usable.
	ActionInstall Action = "install"
	// ActionSync redeploys changed artifacts while preserving local edits.
	ActionSync Action = "sync"
	// ActionUninstall removes managed configuration so a root returns to user content.
	ActionUninstall Action = "uninstall"
	// ActionCorrectDrift restores selected paths so drift correction stays scoped.
	ActionCorrectDrift Action = "correct-drift"
	// ActionReassignModels updates one target's overrides so unchanged agents stay untouched.
	ActionReassignModels Action = "reassign-models"
)

// ActionRequest carries a screen's operation to the runtime.
type ActionRequest struct {
	// ID matches a result to its request so stale results are ignored.
	ID                     uint64
	Action                 Action
	RootDir                string
	Components             []string
	OpenCodeModelOverrides map[string]model.ModelAssignment
	// PreservePaths excludes artifacts from deployment so a "keep mine" choice survives.
	PreservePaths []string
	// AcceptedRisks records conflicts the user kept so a later audit can explain them.
	AcceptedRisks []setup.RiskAcceptance
	// SelectedDriftPaths scopes drift correction so untouched paths stay untouched.
	SelectedDriftPaths []string
	// RetainPaths and RemovePaths carry uninstall decisions so kept files move aside instead of vanishing.
	RetainPaths []string
	RemovePaths []string
	// EngramChoice carries the Engram database decision (lifecycle.EngramLeave, EngramRetain or EngramRemove) so uninstall honors it without new plumbing.
	EngramChoice string
	// ReassignOverrides names the reassignment the adapter diffs.
	ReassignOverrides map[string]model.ModelAssignment
}

// ActionResult reports affected paths from a lifecycle operation.
type ActionResult struct {
	Action    Action
	Changed   []string
	Unchanged []string
	Removed   []string
	Preserved []string
	Actions   []string
	// Unresolved lists drift paths missing from the installed plan so review can explain them.
	Unresolved []string
	// Restored lists files put back to pre-install content so users see what returned.
	Restored []string
	// Retained, RetainedDir and Incomplete describe the uninstall handoff so kept files stay findable.
	Retained    []string
	RetainedDir string
	// RetainedMemory is the delivered Engram database inside RetainedDir, empty when none was handed off.
	RetainedMemory string
	Incomplete     []string
	Verify         *verify.Report
	// Outcome, NotApplied, BackupDir and CancelRequested describe cancellation so screens report honestly.
	Outcome         lifecycle.Outcome
	NotApplied      []string
	BackupDir       string
	CancelRequested bool
	ReloadAttempted bool
	ReloadError     error
}

// ErrVersionMismatch blocks drift correction built from the wrong definitions.
var ErrVersionMismatch = errors.New("the installed version's definitions are not available in this build")

// lastID seeds request IDs so every action stays matchable to its result.
var lastID atomic.Uint64

// NextID allocates a process-unique request ID.
func NextID() uint64 { return lastID.Add(1) }

// CancelRequest asks a running action to stop.
type CancelRequest struct{ ID uint64 }

// ActionResultMsg delivers an action result to the event loop.
type ActionResultMsg struct {
	Request ActionRequest
	Result  ActionResult
	Err     error
}

// ActionProgressMsg carries one matching action's progress to its screen.
type ActionProgressMsg struct {
	Request  ActionRequest
	Progress setup.DeploymentProgress
}

// Adapter invokes setup's lifecycle APIs.
type Adapter struct {
	lifecycle lifecycle.Runtime
}

// NewAdapter wires the production lifecycle, including OpenCode V2 preflight
// and reload. Tests may continue using Adapter{} or NewTestAdapter with a
// zero lifecycle to avoid external processes.
func NewAdapter() Adapter {
	return Adapter{lifecycle: lifecycle.NewRuntime()}
}

// Command defers a request to a Bubble Tea command.
func (adapter Adapter) Command(ctx context.Context, request ActionRequest, observers ...func(setup.DeploymentProgress)) tea.Cmd {
	return func() tea.Msg {
		var progress func(setup.DeploymentProgress)
		if len(observers) > 0 {
			progress = observers[0]
		}
		result, err := adapter.ExecuteContextProgress(ctx, request, progress)
		result.CancelRequested = ctx.Err() != nil
		return ActionResultMsg{Request: request, Result: result, Err: err}
	}
}

// Execute runs a lifecycle operation to completion.
func (adapter Adapter) Execute(request ActionRequest) (ActionResult, error) {
	return adapter.ExecuteContext(context.Background(), request)
}

// ExecuteContext runs a request with cancellation.
func (adapter Adapter) ExecuteContext(ctx context.Context, request ActionRequest) (ActionResult, error) {
	return adapter.ExecuteContextProgress(ctx, request, nil)
}

// ExecuteContextProgress executes a request while reporting lifecycle progress.
func (adapter Adapter) ExecuteContextProgress(ctx context.Context, request ActionRequest, progress func(setup.DeploymentProgress)) (ActionResult, error) {
	started := time.Now()
	result, err := adapter.execute(ctx, request, progress)
	if result.Outcome == "" && err == nil {
		result.Outcome = lifecycle.OutcomeCompleted
	}
	if result.Outcome == lifecycle.OutcomeCancelledPartial && result.BackupDir == "" {
		result.BackupDir = setup.BackupsSince(request.RootDir, started)
	}
	return result, err
}

// execute routes a request to its handler so each action keeps one code path.
func (adapter Adapter) execute(ctx context.Context, request ActionRequest, progress func(setup.DeploymentProgress)) (ActionResult, error) {
	if request.Action == ActionCorrectDrift {
		return adapter.executeCorrectDrift(ctx, request)
	}
	if request.Action == ActionReassignModels {
		return adapter.executeReassignModels(ctx, request)
	}
	planRequest, err := adapter.planRequest(request)
	if err != nil {
		return ActionResult{}, err
	}
	// The choice travels on the adapter copy so Run's signature
	// stays shared with the CLI, whose zero value is Leave.
	adapter.lifecycle.EngramChoice = request.EngramChoice
	adapter.lifecycle.Progress = progress
	result, err := adapter.lifecycle.Run(ctx, string(request.Action), request.RootDir, planRequest, request.PreservePaths...)
	out := actionResult(request.Action, result)
	if err == nil && out.Outcome == lifecycle.OutcomeCompleted && progress != nil && (request.Action == ActionInstall || request.Action == ActionSync) {
		progress(setup.DeploymentProgress{Stage: "preparing", Message: "Verifying installation"})
	}
	return adapter.finishExecution(ctx, request, result, out, err)
}

func (adapter Adapter) finishExecution(ctx context.Context, request ActionRequest, result lifecycle.LifecycleResult, out ActionResult, err error) (ActionResult, error) {
	if err != nil {
		// Work done before the failure is still reported: a failed operation
		// must not hide what it already changed on disk.
		return out, err
	}
	if out.Outcome != lifecycle.OutcomeCompleted {
		// Stopped at a stable point: retention choices, risk records and
		// verification belong to a completed operation only.
		out.NotApplied = slices.Concat(out.NotApplied, request.RemovePaths, request.RetainPaths)
		return out, nil
	}

	if request.Action == ActionUninstall {
		end, err := setup.BeginOperation(request.RootDir, string(request.Action))
		if err != nil {
			return out, err
		}
		defer end()
		choices := setup.RetentionChoices{Keep: request.RetainPaths, Remove: request.RemovePaths}
		if request.EngramChoice == lifecycle.EngramRetain {
			// The handoff shares the retained directory with kept files, so it goes through the same call.
			if dir, dirErr := setup.EngramDataDir(); dirErr != nil {
				out.Incomplete = append(out.Incomplete, "Engram memory: "+dirErr.Error())
			} else {
				choices.MemoryFrom = dir
			}
		}
		retention, err := setup.ApplyUninstallRetention(ctx, request.RootDir, choices, time.Now())
		out.Removed = append(out.Removed, retention.Removed...)
		out.Retained, out.RetainedDir, out.RetainedMemory, out.NotApplied = retention.Retained, retention.RetainedDir, retention.Memory, retention.NotApplied
		out.Incomplete = append(out.Incomplete, retention.Incomplete...)
		if lifecycle.Cancelled(ctx, err) {
			out.Outcome = lifecycle.CancelledOutcome(len(out.Removed) + len(out.Restored) + len(out.Retained))
			return out, nil
		}
		return out, err
	}

	// Run post-deployment verification for install and sync.
	if request.Action == ActionInstall || request.Action == ActionSync {
		// A failed record only means the same question is asked again; it must not turn a completed operation into a failure.
		_ = setup.RecordRiskAcceptances(request.RootDir, request.AcceptedRisks)
		report := verify.CollectWithReload(ctx, request.RootDir, verify.ReloadStatus{
			Attempted: result.ReloadAttempted,
			Err:       result.ReloadError,
		})
		out.Verify = &report
	}

	return out, nil
}

func (adapter Adapter) planRequest(request ActionRequest) (setup.PlanRequest, error) {
	if request.Action != ActionInstall && request.Action != ActionSync {
		return setup.PlanRequest{}, nil
	}
	built, err := buildPlanRequest(request.Components, request.OpenCodeModelOverrides)
	if err != nil {
		return setup.PlanRequest{}, err
	}
	preserveModelChoices(request.RootDir, &built)
	return built, nil
}

func actionResult(action Action, result lifecycle.LifecycleResult) ActionResult {
	return ActionResult{Action: action, Changed: result.Changed, Unchanged: result.Unchanged, Removed: result.Removed, Preserved: result.Preserved, Restored: result.Restored, Actions: result.Actions, Incomplete: result.Incomplete, Outcome: result.Outcome, NotApplied: result.NotApplied, BackupDir: result.BackupDir, ReloadAttempted: result.ReloadAttempted, ReloadError: result.ReloadError}
}

// executeCorrectDrift restores selected paths from the installed record so other paths survive.
func (adapter Adapter) executeCorrectDrift(ctx context.Context, request ActionRequest) (ActionResult, error) {
	if !SameInstalledVersion(request.RootDir) {
		return ActionResult{}, fmt.Errorf("correct drift: %w", ErrVersionMismatch)
	}
	preview, err := installedPreview(request.RootDir)
	if err != nil {
		return ActionResult{}, err
	}
	// Restored files are reinjected with Engram below, so its binary is acquired before anything is written.
	engramCommand, err := engram.Acquire(ctx, request.RootDir)
	if err != nil {
		return ActionResult{}, fmt.Errorf("correct drift: Engram memory capability unavailable: requires the engram binary %s or newer: %w", engram.EngramVersion, err)
	}
	end, err := setup.BeginOperation(request.RootDir, string(request.Action))
	if err != nil {
		return ActionResult{}, err
	}
	defer end()
	result, err := setup.CorrectDriftContext(ctx, request.RootDir, preview.Plans, request.SelectedDriftPaths, adapter.lifecycle.ProviderActions)
	out := ActionResult{
		Action:     ActionCorrectDrift,
		Changed:    result.Changed,
		Unchanged:  result.Unchanged,
		Unresolved: result.Unresolved,
		NotApplied: result.NotApplied,
	}
	if lifecycle.Cancelled(ctx, err) {
		out.Outcome = lifecycle.CancelledOutcome(len(result.Changed))
		return out, reinjectDrift(ctx, request.RootDir, engramCommand, len(result.Changed))
	}
	if err != nil {
		return ActionResult{}, err
	}
	return out, reinjectDrift(ctx, request.RootDir, engramCommand, len(result.Changed))
}

// reinjectDrift restores installed integrations only after files changed.
// Both Engram and CodeGraph are required harness capabilities.
func reinjectDrift(ctx context.Context, root, engramCommand string, changed int) error {
	if changed == 0 {
		return nil
	}
	if err := lifecycle.InjectEngram(root, engramCommand); err != nil {
		return err
	}
	return lifecycle.InstallCodegraph(ctx, root)
}

// SameInstalledVersion checks whether the installed version matches this build.
func SameInstalledVersion(rootDir string) bool {
	return setup.IsCurrentVersion(setup.InstalledVersion(rootDir))
}

// installedPreview resolves the installed record's plans so drift judges what was installed.
func installedPreview(rootDir string) (lifecycle.InstallPreview, error) {
	installed, err := setup.LoadInstalledConfig(rootDir)
	if err != nil {
		return lifecycle.InstallPreview{}, err
	}
	// DefaultPlanRequest never populates OpenCode, so this always resets the
	// recorded fallback model/prompt to defaults, keeping the rest of the
	// recorded selection (see TestInstalledPreviewResetsOpenCodeModelToDefault).
	defaults, err := setup.DefaultPlanRequest()
	if err != nil {
		return lifecycle.InstallPreview{}, err
	}
	installed.OpenCode = defaults.OpenCode
	preview, err := lifecycle.PreviewLifecycle(string(ActionInstall), rootDir, installed)
	if err != nil {
		return lifecycle.InstallPreview{}, err
	}
	installPreview, _ := preview.(lifecycle.InstallPreview)
	return installPreview, nil
}

// ScanDrift reports drift without writing.
func (adapter Adapter) ScanDrift(rootDir string) ([]setup.ConflictEntry, error) {
	preview, err := installedPreview(rootDir)
	return preview.Conflicts, err
}

// executeReassignModels redeploys only changed sub-agents so untouched ones keep running.
func (adapter Adapter) executeReassignModels(ctx context.Context, request ActionRequest) (ActionResult, error) {
	installed, err := setup.LoadInstalledConfig(request.RootDir)
	if err != nil {
		return ActionResult{}, err
	}
	before := installed.OpenCodeModelOverrides
	end, err := setup.BeginOperation(request.RootDir, string(request.Action))
	if err != nil {
		return ActionResult{}, err
	}
	defer end()

	out := ActionResult{Action: ActionReassignModels}
	ids := changedOverrideIDs(before, request.ReassignOverrides)
	for index, subAgentID := range ids {
		if ctx.Err() != nil {
			out.NotApplied = append(out.NotApplied, ids[index:]...)
			break
		}
		result, err := setup.ApplyModelOverrideChange(ctx, request.RootDir, subAgentID, request.ReassignOverrides[subAgentID], adapter.lifecycle.ProviderActions)
		out.Changed = append(out.Changed, result.Changed...)
		out.Unchanged = append(out.Unchanged, result.Unchanged...)
		if lifecycle.Cancelled(ctx, err) {
			out.NotApplied = append(append(out.NotApplied, result.NotApplied...), ids[index+1:]...)
			break
		}
		if err != nil {
			return ActionResult{}, err
		}
	}
	if len(out.NotApplied) > 0 {
		out.Outcome = lifecycle.CancelledOutcome(len(out.Changed))
	}
	return out, nil
}

// changedOverrideIDs lists sub-agents whose assignment differs so redeploys stay minimal.
func changedOverrideIDs(before, after map[string]model.ModelAssignment) []string {
	var ids []string
	for id, assignment := range after {
		if before[id] != assignment {
			ids = append(ids, id)
		}
	}
	for id := range before {
		if _, present := after[id]; !present {
			ids = append(ids, id)
		}
	}
	return ids
}

// buildPlanRequest assembles one plan so preview and execution cannot diverge.
func buildPlanRequest(components []string, opencodeOverrides map[string]model.ModelAssignment) (setup.PlanRequest, error) {
	planRequest, err := setup.DefaultPlanRequest()
	if err != nil {
		return setup.PlanRequest{}, err
	}
	planRequest.Components = components
	planRequest.OpenCodeModelOverrides = opencodeOverrides
	return planRequest, nil
}

// PreviewRequest asks what install/sync would face so review shows real conflicts.
type PreviewRequest struct {
	Action                 Action
	RootDir                string
	Components             []string
	OpenCodeModelOverrides map[string]model.ModelAssignment
}

// InstallPlan is the resolved install/sync plan a review screen shows before
// commitment: the lifecycle preview plus how each planned file relates to
// what is on disk now.
type InstallPlan struct {
	lifecycle.InstallPreview
	// Add lists planned paths absent on disk; Modify lists present paths whose
	// bytes differ. Files already identical appear in neither.
	Add, Modify []string
}

// PreviewPlan resolves request's plan and classifies every planned file
// against disk, without writing anything: previewing stays read-only.
func (adapter Adapter) PreviewPlan(request PreviewRequest) (InstallPlan, error) {
	planRequest, err := buildPlanRequest(request.Components, request.OpenCodeModelOverrides)
	if err != nil {
		return InstallPlan{}, err
	}
	preserveModelChoices(request.RootDir, &planRequest)
	preview, err := lifecycle.PreviewLifecycle(string(request.Action), request.RootDir, planRequest)
	if err != nil {
		return InstallPlan{}, err
	}
	installPreview, ok := preview.(lifecycle.InstallPreview)
	if !ok {
		return InstallPlan{}, nil
	}
	plan := InstallPlan{InstallPreview: installPreview}
	for _, target := range installPreview.Plans {
		for _, artifact := range target.Artifacts {
			current, err := os.ReadFile(filepath.Join(request.RootDir, filepath.FromSlash(artifact.Path)))
			switch {
			case errors.Is(err, fs.ErrNotExist):
				plan.Add = append(plan.Add, artifact.Path)
			case err != nil:
				return InstallPlan{}, fmt.Errorf("inspect %q: %w", artifact.Path, err)
			case !bytes.Equal(current, artifact.Content):
				// Configuration artifacts always count as modified; a merge-aware
				// diff belongs in setup.
				plan.Modify = append(plan.Modify, artifact.Path)
			}
		}
	}
	return plan, nil
}

// PreviewUninstall computes what uninstalling would remove or preserve without
// touching disk.
func (adapter Adapter) PreviewUninstall(rootDir string) (setup.UninstallResult, error) {
	preview, err := lifecycle.PreviewLifecycle(string(ActionUninstall), rootDir, setup.PlanRequest{})
	if err != nil {
		return setup.UninstallResult{}, err
	}
	result, _ := preview.(setup.UninstallResult)
	return result, nil
}

// OpenCodeModels reports the models the local OpenCode installation offers;
// asking the binary is execution, so it stays at the runtime boundary.
func (adapter Adapter) OpenCodeModels() ([]string, error) {
	return opencode.AvailableModels(context.Background())
}

// preserveModelChoices keeps preview and execution on the same installed baseline.
func preserveModelChoices(root string, built *setup.PlanRequest) {
	if installed, err := setup.LoadInstalledConfig(root); err == nil {
		built.OpenCode.Model = installed.OpenCode.Model
		if built.OpenCodeModelOverrides == nil {
			built.OpenCodeModelOverrides = installed.OpenCodeModelOverrides
		}
	}
}
