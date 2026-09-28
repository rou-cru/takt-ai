// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package lifecycle dispatches the install, sync, and uninstall orchestration
// shared by the CLI (takt/cli) and the TUI runtime. It sits above setup,
// skills and engram and composes them (hosting this dispatch in setup would
// create an import cycle through skills).
package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/skills"
)

// LifecycleResult is the union of deploy and uninstall outcomes.
type LifecycleResult struct {
	Changed   []string
	Unchanged []string
	Removed   []string
	Preserved []string
	// Restored lists pre-existing files uninstall put back to their
	// pre-install content (setup.UninstallResult.Restored).
	Restored []string
	Actions  []string
	// Outcome says whether the operation completed or a cancellation stopped
	// it at a stable point; NotApplied lists the planned work a
	// cancellation left undone.
	Outcome    Outcome
	NotApplied []string
	// BackupDir is set when a cancelled operation replaced files whose
	// copies were written to setup.BackupDir during this run.
	BackupDir string
	// Incomplete lists requested work that has no supported removal
	// mechanism, reported honestly instead of failing the operation:
	// the uninstall completed, this entry did not.
	Incomplete []string
	// ReloadAttempted and ReloadError describe the post-deployment OpenCode
	// handoff separately from file deployment. A reload failure is functional
	// evidence, not a rollback trigger: the files are already valid on disk.
	ReloadAttempted bool
	ReloadError     error
}

// Outcome is the typed end state of a mutating operation.
type Outcome string

// Outcomes are the closed set of end states a mutating operation can report.
const (
	// OutcomeCompleted means every planned change was applied, even if a
	// cancellation arrived too late to take effect.
	OutcomeCompleted Outcome = "completed"
	// OutcomeCancelledPartial means a cancellation stopped the operation at a
	// stable point after some changes were applied; they stay in place.
	OutcomeCancelledPartial Outcome = "cancelled-partial"
	// OutcomeCancelledNothingApplied means a cancellation stopped the
	// operation before any change was applied.
	OutcomeCancelledNothingApplied Outcome = "cancelled-nothing-applied"
)

// Cancelled reports whether err is ctx's own cancellation, i.e. the
// operation stopped cooperatively rather than failed.
func Cancelled(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err())
}

// CancelledOutcome types a cancelled operation by whether it applied anything.
func CancelledOutcome(applied int) Outcome {
	if applied == 0 {
		return OutcomeCancelledNothingApplied
	}
	return OutcomeCancelledPartial
}

// nativeArtifactPending reports whether any OpenCode artifact stayed
// unapplied, mirroring the renderer's directory (takt/agents/opencode).
func nativeArtifactPending(notApplied []string) bool {
	prefix := opencode.ConfigDir() + "/"
	return slices.ContainsFunc(notApplied, func(path string) bool { return strings.HasPrefix(path, prefix) })
}

// Engram retention choices for uninstall: the Engram
// database lives outside Takt's footprint, so uninstall never touches it
// unless the user explicitly chooses EngramRemove. Empty means EngramLeave.
const (
	// EngramLeave keeps the Engram database where it is: zero behavior change.
	EngramLeave = "leave"
	// EngramRetain hands a consistent copy of the Engram database to the
	// retained directory so files and memory share one directory. The database
	// stays in place.
	EngramRetain = "retain"
	// EngramRemove asks for the Engram database to be removed too.
	EngramRemove = "remove"

	// managedFileMode makes Takt-acquired executable files usable by their owner
	// and group while keeping other users from writing them.
	managedFileMode os.FileMode = 0o755
)

// Runtime carries external provider-action execution seams.
type Runtime struct {
	ProviderActions setup.ProviderRuntime
	// OpenCodeHandshake is an install preflight. Production constructors set it
	// to the native V2 API; nil keeps low-level lifecycle tests deterministic.
	OpenCodeHandshake func(context.Context) error
	// Reload is the final handoff after a completed OpenCode deployment.
	Reload func(context.Context) error
	// EngramChoice is EngramLeave (default), EngramRetain or EngramRemove; see above.
	EngramChoice string
}

// NewRuntime wires the production OpenCode V2 preflight and handoff while
// keeping Runtime's zero value useful for isolated lifecycle tests.
func NewRuntime() Runtime {
	return Runtime{
		OpenCodeHandshake: func(ctx context.Context) error {
			_, err := opencode.Handshake(ctx)
			return err
		},
		Reload: opencode.Reload,
	}
}

// InstallPreview is the install/sync preview payload: the target plans
// BuildTargetPlans would apply, plus any conflicts DetectConflicts finds
// between those plans and what is already on disk.
type InstallPreview struct {
	Plans     []setup.TargetPlan
	Conflicts []setup.ConflictEntry
	// Removals lists manifest dependencies dropped from the selection, with
	// the reason each was removed.
	Removals []catalog.Removal
}

// PreviewLifecycle computes what one lifecycle action would do without
// changing the environment.
func PreviewLifecycle(action string, rootDir string, request setup.PlanRequest) (any, error) {
	switch action {
	case "install", "sync":
		plans, removals, err := installPlans(request)
		if err != nil {
			return nil, err
		}
		conflicts, err := setup.DetectConflicts(rootDir, plans)
		if err != nil {
			return nil, err
		}
		return InstallPreview{Plans: plans, Conflicts: conflicts, Removals: removals}, nil
	case "uninstall":
		return setup.PreviewUninstall(rootDir, uninstallTargets()...)
	default:
		return nil, fmt.Errorf("unsupported action %q", action)
	}
}

// installPlans is every plan install and sync deploy: the OpenCode plan plus
// the skills, with the removals dropped from the selection.
func installPlans(request setup.PlanRequest) ([]setup.TargetPlan, []catalog.Removal, error) {
	plans, removals, err := setup.BuildTargetPlans(request)
	if err != nil {
		return nil, nil, err
	}
	skillPlan, err := skills.BuildSkillPlan()
	if err != nil {
		return nil, nil, err
	}
	return append(plans, skillPlan), removals, nil
}

// uninstallTargets is what an uninstall removes: OpenCode plus the skills.
func uninstallTargets() []setup.OwnershipTarget {
	return []setup.OwnershipTarget{setup.TargetOpenCode, setup.TargetSkills}
}

// Run executes one lifecycle operation with this runtime's provider-action
// seams. preserve names paths to exclude from deployment. A cancellation is a
// typed Outcome with a nil error, never a failure.
func (runtime Runtime) Run(ctx context.Context, action string, rootDir string, request setup.PlanRequest, preserve ...string) (LifecycleResult, error) {
	switch action {
	case "install", "sync":
		return runtime.install(ctx, action, rootDir, request, preserve)
	case "uninstall":
		return runtime.uninstall(ctx, rootDir, request)
	default:
		return LifecycleResult{}, fmt.Errorf("unsupported action %q", action)
	}
}

func (runtime Runtime) install(ctx context.Context, action, rootDir string, request setup.PlanRequest, preserve []string) (LifecycleResult, error) {
	started := time.Now()
	// Removals were already surfaced to the user by PreviewLifecycle before
	// this request was authorized; applying does not need to see them again.
	plans, _, err := installPlans(request)
	if err != nil {
		return LifecycleResult{}, err
	}
	if err := runtime.handshake(ctx, request); err != nil {
		return LifecycleResult{}, err
	}
	// Engram is acquired before anything is written: a missing prerequisite leaves root untouched.
	engramCommand, err := acquireEngram(ctx, rootDir)
	if Cancelled(ctx, err) {
		return LifecycleResult{Outcome: OutcomeCancelledNothingApplied, NotApplied: plannedPaths(plans)}, nil
	}
	if err != nil {
		return LifecycleResult{}, fmt.Errorf("engram memory capability unavailable: requires the engram binary %s or newer, which is not on PATH and could not be installed: %w", engram.EngramVersion, err)
	}
	codegraphCommand, err := acquireCodegraph(ctx, rootDir)
	if Cancelled(ctx, err) {
		return LifecycleResult{Outcome: OutcomeCancelledNothingApplied, NotApplied: plannedPaths(plans)}, nil
	}
	if err != nil {
		return LifecycleResult{}, fmt.Errorf("codegraph capability unavailable: requires CodeGraph %s or newer, which is not on PATH and could not be installed: %w", codegraph.CodegraphVersion, err)
	}
	end, err := setup.BeginOperation(rootDir, action)
	if err != nil {
		return LifecycleResult{}, err
	}
	defer end()
	result, err := deploy(ctx, action, rootDir, plans, runtime.ProviderActions, preserve...)
	// Files are written from here on: a later failure still reports them
	// alongside the error, so callers can show partial work honestly.
	deployed := LifecycleResult{Changed: result.Changed, Unchanged: result.Unchanged, Actions: result.Actions, NotApplied: result.NotApplied, Outcome: OutcomeCompleted}
	if Cancelled(ctx, err) {
		return recordCancelled(rootDir, request, deployed, started)
	}
	if err != nil {
		return deployed, err
	}
	return runtime.finishInstall(ctx, rootDir, request, engramCommand, codegraphCommand, deployed)
}

func (runtime Runtime) handshake(ctx context.Context, request setup.PlanRequest) error {
	if runtime.OpenCodeHandshake == nil {
		return nil
	}
	if err := runtime.OpenCodeHandshake(ctx); err != nil {
		return fmt.Errorf("OpenCode V2 preflight failed: %w", err)
	}
	return nil
}

func deploy(ctx context.Context, action, rootDir string, plans []setup.TargetPlan, provider setup.ProviderRuntime, preserve ...string) (setup.DeploymentResult, error) {
	if action == "install" {
		return setup.ApplyContext(ctx, rootDir, plans, provider, preserve...)
	}
	return setup.SyncContext(ctx, rootDir, plans, provider, preserve...)
}

func (runtime Runtime) finishInstall(ctx context.Context, rootDir string, request setup.PlanRequest, engramCommand, codegraphCommand string, deployed LifecycleResult) (LifecycleResult, error) {
	if err := InjectEngram(rootDir, engramCommand); err != nil {
		return deployed, err
	}
	if err := InjectCodegraph(rootDir, codegraphCommand); err != nil {
		return deployed, fmt.Errorf("inject CodeGraph MCP server: %w", err)
	}
	deployed.Actions = append(deployed.Actions, "codegraph-install")
	if err := installSandboxDependency(ctx, rootDir); err == nil {
		deployed.Actions = append(deployed.Actions, "sandbox-adapter-npm-install")
	}
	if err := setup.RecordInstallation(rootDir, request); err != nil {
		return deployed, err
	}
	if runtime.Reload != nil {
		deployed.ReloadAttempted = true
		deployed.ReloadError = runtime.Reload(ctx)
	}
	return deployed, nil
}

// recordCancelled finishes a cancelled deployment, recording it as installed
// only when it was applied in full.
func recordCancelled(rootDir string, request setup.PlanRequest, deployed LifecycleResult, started time.Time) (LifecycleResult, error) {
	deployed.Outcome = CancelledOutcome(len(deployed.Changed))
	if len(deployed.Changed) == 0 {
		return deployed, nil
	}
	// A deployment with leftover NotApplied artifacts is not recorded as
	// installed: the record would claim .takt-installed-config.json matches
	// while those files are absent from the ownership manifest (never-applied
	// paths are excluded there too, applyPlans above), so drift could never
	// catch the gap and a later reassign/drift flow would treat the
	// installation as complete. A run recorded here reapplies cleanly if it
	// happens again; a run not recorded is retried in full next time.
	deployed.BackupDir = setup.BackupsSince(rootDir, started)
	if nativeArtifactPending(deployed.NotApplied) {
		return deployed, nil
	}
	return deployed, setup.RecordInstallation(rootDir, request)
}

func (runtime Runtime) uninstall(ctx context.Context, rootDir string, request setup.PlanRequest) (LifecycleResult, error) {
	end, err := setup.BeginOperation(rootDir, "uninstall")
	if err != nil {
		return LifecycleResult{}, err
	}
	defer end()
	result, err := setup.UninstallContext(ctx, rootDir, uninstallTargets()...)
	removed := LifecycleResult{Removed: result.Removed, Preserved: result.Preserved, Restored: result.Restored, NotApplied: result.NotApplied, Outcome: OutcomeCompleted}
	if Cancelled(ctx, err) {
		// Targets stay recorded as installed: part of them remains.
		removed.Outcome = CancelledOutcome(len(result.Removed) + len(result.Restored))
		return removed, nil
	}
	if err != nil {
		return LifecycleResult{}, err
	}
	if err := removeEngram(rootDir); err != nil {
		return removed, err
	}
	if err := removeCodegraph(rootDir); err != nil {
		return removed, err
	}
	if runtime.EngramChoice == EngramRemove {
		// engram.Remove above covers the MCP config footprint; the
		// database itself has no supported remover, so a Remove choice
		// is reported as incomplete work, never a fatal error:
		// deliberate retention is not a failure.
		removed.Incomplete = append(removed.Incomplete, "Engram database was not removed: Takt has no supported database remover; it lives outside Takt's footprint and was left in place")
	}
	return removed, setup.ForgetInstallation(rootDir)
}

// acquireEngram is the Engram prerequisite seam; tests replace it to avoid PATH and network.
var acquireEngram = engram.Acquire

// acquireCodegraph is the codegraph prerequisite seam; tests replace it to avoid PATH and network.
var acquireCodegraph = codegraph.Acquire

// installSandboxDependency is the sandbox adapter's npm dependency seam; tests replace it to avoid PATH and network.
var installSandboxDependency = opencode.InstallSandboxDependency

// InstallCodegraph acquires the pinned CodeGraph tool and writes the MCP entry
// with its resolved absolute path. CodeGraph is a required workspace capability;
// acquisition or injection errors must be reported to the caller.
func InstallCodegraph(ctx context.Context, rootDir string) error {
	command, err := acquireCodegraph(ctx, rootDir)
	if err != nil {
		return err
	}
	return InjectCodegraph(rootDir, command)
}

// plannedPaths lists every artifact a plan set would write, for a run stopped before writing any.
func plannedPaths(plans []setup.TargetPlan) []string {
	var paths []string
	for _, plan := range plans {
		for _, artifact := range plan.Artifacts {
			paths = append(paths, artifact.Path)
		}
	}
	return paths
}

// InjectEngram wires the engram MCP server into OpenCode, using engramCommand
// verbatim. `engram setup` is never run: Takt's memory contract skill is the
// only source of memory rules.
func InjectEngram(rootDir, engramCommand string) error {
	return injectServer("engram", rootDir, engramCommand, engram.Inject, registerManagedEngram)
}

// injectServer runs one MCP server's injection plus its ownership bookkeeping
// so engram and codegraph share one path instead of two mirrors.
func injectServer(name, rootDir, command string,
	inject func(string, string) (model.InjectionResult, error),
	register func(string, string) error) error {
	injected, err := inject(rootDir, command)
	if err != nil {
		return fmt.Errorf("inject %s: %w", name, err)
	}
	if err := refreshManagedOwnership(rootDir, injected.Files); err != nil {
		return err
	}
	return register(rootDir, command)
}

// registerManagedEngram records a Takt-acquired binary in the ownership
// manifest so the manifest-driven uninstall removes it. A reused user binary is
// never recorded.
func registerManagedEngram(rootDir, engramCommand string) error {
	managed, err := filepath.Abs(engram.ManagedBinaryPath(rootDir))
	if err != nil {
		return nil
	}
	return registerManagedPath(rootDir, engramCommand, managed, managed)
}

// registerManagedCodegraph records the Takt-acquired codegraph install the
// same way, through one marker file of the npm package: the manifest tracks
// files, and uninstall clears the rest of the managed tree with it. A reused
// user binary is never recorded.
func registerManagedCodegraph(rootDir, codegraphCommand string) error {
	managed, err := filepath.Abs(codegraph.ManagedBinaryPath(rootDir))
	if err != nil {
		return nil
	}
	return registerManagedPath(rootDir, codegraphCommand, managed,
		filepath.Join(rootDir, filepath.FromSlash(codegraph.ManagedMarkerKey())))
}

// registerManagedPath records recorded in the ownership manifest when command
// is the Takt-managed binary, not a reused user binary.
func registerManagedPath(rootDir, command, managed, recorded string) error {
	if command != managed {
		return nil
	}
	return registerOwnedFile(rootDir, recorded, managedFileMode)
}

// registerOwnedFile adds file to the ownership manifest, owned by OpenCode.
func registerOwnedFile(rootDir, file string, mode os.FileMode) error {
	manifest, err := setup.LoadOwnershipManifest(rootDir)
	if errors.Is(err, os.ErrNotExist) {
		manifest, err = setup.NewOwnershipManifest(), nil
	}
	if err != nil {
		return fmt.Errorf("load ownership manifest: %w", err)
	}
	relative, err := filepath.Rel(rootDir, file)
	if err != nil {
		return err
	}
	relative = filepath.ToSlash(relative)
	owners := manifest.Entries[relative].Targets
	if !slices.Contains(owners, setup.TargetOpenCode) {
		owners = append(owners, setup.TargetOpenCode)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("record ownership of %q: %w", relative, err)
	}
	entry, err := setup.NewOwnershipEntry(relative, content, mode, false, "", "", owners...)
	if err != nil {
		return err
	}
	if err := manifest.Add(entry); err != nil {
		return err
	}
	return manifest.Save(rootDir)
}

// refreshManagedOwnership keeps the ownership manifest's SHA-256 baseline in
// step with the post-injection bytes. Servers inject after the deploy records
// ownership, so without this refresh a later uninstall would see the modified
// managed file as user-edited content and preserve it instead of removing it.
// Files without a manifest entry (server-only files) are not Takt-owned and are skipped.
func refreshManagedOwnership(rootDir string, files []string) error {
	manifest, err := setup.LoadOwnershipManifest(rootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load ownership manifest: %w", err)
	}
	changed := false
	for _, file := range files {
		relative, relErr := filepath.Rel(rootDir, file)
		if relErr != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		entry, managed := manifest.Entries[relative]
		if !managed {
			continue
		}
		current, readErr := os.ReadFile(file)
		if readErr != nil {
			return fmt.Errorf("refresh ownership for %q: %w", relative, readErr)
		}
		digest := sha256.Sum256(current)
		entry.SHA256 = hex.EncodeToString(digest[:])
		manifest.Entries[relative] = entry
		changed = true
	}
	if !changed {
		return nil
	}
	return manifest.Save(rootDir)
}

// removeEngram strips the engram footprint from the uninstalled harness.
func removeEngram(rootDir string) error {
	return removeServer("engram", rootDir, engram.Remove)
}

// removeServer strips one MCP server's footprint so engram and codegraph share
// one path instead of two mirrors.
func removeServer(name, rootDir string, remove func(string) (model.InjectionResult, error)) error {
	if _, err := remove(rootDir); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}

// InjectCodegraph wires the codegraph MCP server into OpenCode, using
// codegraphCommand (from codegraph.Acquire) verbatim.
func InjectCodegraph(rootDir, codegraphCommand string) error {
	return injectServer("codegraph", rootDir, codegraphCommand, codegraph.Inject, registerManagedCodegraph)
}

// removeCodegraph strips the codegraph MCP entry from the uninstalled harness
// and, once the manifest-driven uninstall dropped the managed install's marker
// file, clears the rest of that tree.
func removeCodegraph(rootDir string) error {
	if err := removeServer("codegraph", rootDir, codegraph.Remove); err != nil {
		return err
	}
	marker := filepath.Join(rootDir, filepath.FromSlash(codegraph.ManagedMarkerKey()))
	if _, err := os.Lstat(marker); errors.Is(err, os.ErrNotExist) {
		if err := os.RemoveAll(codegraph.ManagedPrefix(rootDir)); err != nil {
			return fmt.Errorf("remove managed codegraph: %w", err)
		}
	}
	return nil
}
