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

package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	artifactutil "github.com/rou-cru/takt-ai/takt/internal/artifacts"
)

// loadOrCreateManifest loads the ownership manifest for rootDir, or returns an empty manifest when none exists.
func loadOrCreateManifest(rootDir string) (*OwnershipManifest, error) {
	manifest, err := LoadOwnershipManifest(rootDir)
	if errors.Is(err, os.ErrNotExist) {
		return NewOwnershipManifest(), nil
	}
	return manifest, err
}

// TargetPlan is the plain setup input supplied by a native adapter.
// Target is opaque to setup; its manifest and artifacts define the target's ownership.
type TargetPlan struct {
	// Target identifies the owning agent; setup treats it opaquely.
	Target string
	// ManagedPaths are the normalized paths this target owns.
	ManagedPaths []string
	// Artifacts is the rendered content ready for deployment.
	Artifacts []Artifact
	// Actions are provider commands that run after a complete deployment.
	Actions []ProviderAction
}

// ApplyContext applies plans with cooperative cancellation; see
// DeployContext. Provider actions run only after a complete deployment.
func ApplyContext(ctx context.Context, rootDir string, plans []TargetPlan, runtime ProviderRuntime, preserve ...string) (DeploymentResult, error) {
	return applyPipeline(ctx, rootDir, plans, skipSet(preserve), nil, runtime)
}

// SyncContext syncs plans with cooperative cancellation; see
// DeployContext.
func SyncContext(ctx context.Context, rootDir string, plans []TargetPlan, runtime ProviderRuntime, preserve ...string) (DeploymentResult, error) {
	if strings.TrimSpace(rootDir) == "" {
		return DeploymentResult{}, fmt.Errorf("deployment root is required")
	}
	_, artifacts, _, err := flattenPlans(plans)
	if err != nil {
		return DeploymentResult{}, err
	}
	manifest, err := loadOrCreateManifest(rootDir)
	if err != nil {
		return DeploymentResult{}, err
	}
	skip, err := expandSkipWithLocalEdits(rootDir, manifest, artifacts, preserve)
	if err != nil {
		return DeploymentResult{}, err
	}
	return applyPipeline(ctx, rootDir, plans, skip, manifest, runtime)
}

// applyPipeline runs the shared preflight→apply→provider-actions spine so
// ApplyContext and SyncContext differ only in how they build the skip set.
func applyPipeline(ctx context.Context, rootDir string, plans []TargetPlan, skip map[string]bool, manifest *OwnershipManifest, runtime ProviderRuntime) (DeploymentResult, error) {
	actions, err := preflightProviderActions(plans, runtime)
	if err != nil {
		return DeploymentResult{}, err
	}
	result, err := applyPlans(ctx, rootDir, plans, skip, manifest)
	if err != nil {
		return notAppliedActions(result, actions), err
	}
	return executeProviderActions(result, actions, runtime)
}

// expandSkipWithLocalEdits adds locally edited managed files to the preserve
// set so sync never overwrites user changes; deleted files stay deployable.
func expandSkipWithLocalEdits(rootDir string, manifest *OwnershipManifest, artifacts []Artifact, preserve []string) (map[string]bool, error) {
	skip := make(map[string]bool, len(preserve))
	for _, path := range preserve {
		skip[path] = true
	}
	for _, artifact := range artifacts {
		entry, managed := manifest.Entries[artifact.Path]
		if !managed {
			continue
		}
		current, err := os.ReadFile(filepath.Join(rootDir, filepath.FromSlash(artifact.Path)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // deleted locally: redeploy it
			}
			return nil, fmt.Errorf("inspect managed file %q: %w", artifact.Path, err)
		}
		if hashOf(current) != entry.SHA256 {
			skip[artifact.Path] = true
		}
	}
	return skip, nil
}

// notAppliedActions adds the provider actions a cancelled deployment never
// reached to its not-applied work.
func notAppliedActions(result DeploymentResult, actions []ProviderAction) DeploymentResult {
	if len(result.NotApplied) == 0 {
		return result
	}
	for _, action := range actions {
		result.NotApplied = append(result.NotApplied, action.ID)
	}
	return result
}

// skipSet builds a skip map from a preserve-path list, or nil when empty so
// applyPlans keeps behaving exactly as it did before preserve existed.
func skipSet(preserve []string) map[string]bool {
	if len(preserve) == 0 {
		return nil
	}
	skip := make(map[string]bool, len(preserve))
	for _, path := range preserve {
		skip[path] = true
	}
	return skip
}

// ConflictEntry is a plan artifact whose on-disk content differs from what
// Takt would deploy, in a case where the automatic resolution is not
// obviously right for every user; see DetectConflicts.
type ConflictEntry struct {
	// Path is the artifact's root-relative path, suitable for passing back
	// into the preserve list of ApplyContext/SyncContext.
	Path string
	// Reason is "pre-existing" (unmanaged content Takt would overwrite), "user-edited", or "missing".
	Reason string
	// Impact is ImpactUnrelated, ImpactUncertain or ImpactIncompatible; see
	// classifyConflict for the rule set.
	Impact string
	// Affects names the capability the file defines, in user terms.
	Affects string
	// Consequence explains, in one sentence, what keeping the file means.
	Consequence string
	// Alternative is the viable route when Impact is ImpactIncompatible.
	Alternative string
	// SHA256 is the hex digest of the content on disk ("" when missing), the
	// content a risk acceptance refers to.
	SHA256 string
	// Accepted reports the user already kept this exact content with this impact, so it must not be asked again.
	Accepted bool
}

// DetectConflicts previews where deploying would overwrite unowned content or
// discard a user edit, without writing or mutating state.
func DetectConflicts(rootDir string, plans []TargetPlan) ([]ConflictEntry, error) {
	_, artifacts, _, err := flattenPlans(plans)
	if err != nil {
		return nil, err
	}
	manifest, err := loadOrCreateManifest(rootDir)
	if err != nil {
		return nil, err
	}
	accepted := loadRiskAcceptances(rootDir)
	var conflicts []ConflictEntry
	for _, artifact := range artifacts {
		conflict, include, err := detectArtifactConflict(rootDir, artifact, manifest, accepted)
		if err != nil {
			return nil, err
		}
		if include {
			conflicts = append(conflicts, conflict)
		}
	}
	return conflicts, nil
}

func detectArtifactConflict(rootDir string, artifact Artifact, manifest *OwnershipManifest, accepted map[string]RiskAcceptance) (ConflictEntry, bool, error) {
	entry, managed := manifest.Entries[artifact.Path]
	conflict, current, include, err := readConflict(rootDir, artifact, entry, managed)
	if err != nil || !include {
		return conflict, include, err
	}
	classifyConflict(&conflict, artifact, entry, managed, current)
	conflict.Accepted = conflictAccepted(conflict, accepted)
	return conflict, true, nil
}

func readConflict(rootDir string, artifact Artifact, entry OwnershipEntry, managed bool) (ConflictEntry, []byte, bool, error) {
	current, readErr := os.ReadFile(filepath.Join(rootDir, filepath.FromSlash(artifact.Path)))
	conflict := ConflictEntry{Path: artifact.Path}
	if errors.Is(readErr, os.ErrNotExist) {
		if !managed {
			return ConflictEntry{}, nil, false, nil
		}
		conflict.Reason = "missing"
		return conflict, nil, true, nil
	}
	if readErr != nil {
		return ConflictEntry{}, nil, false, fmt.Errorf("inspect managed file %q: %w", artifact.Path, readErr)
	}
	if !managed {
		if isMergeableConfig(artifact.Path) || bytes.Equal(current, artifact.Content) {
			return ConflictEntry{}, nil, false, nil
		}
		conflict.Reason, conflict.SHA256 = "pre-existing", hashOf(current)
		return conflict, current, true, nil
	}
	digest := hashOf(current)
	if digest == entry.SHA256 || bytes.Equal(current, artifact.Content) {
		return ConflictEntry{}, nil, false, nil
	}
	conflict.Reason, conflict.SHA256 = "user-edited", digest
	return conflict, current, true, nil
}

func conflictAccepted(conflict ConflictEntry, accepted map[string]RiskAcceptance) bool {
	prior, found := accepted[conflict.Path]
	return found && conflict.Impact == ImpactUncertain && prior.Impact == conflict.Impact && prior.SHA256 == conflict.SHA256
}

// UninstallContext is Uninstall with cooperative cancellation: ctx is checked between entries.
// Cancelled work finalizes handled entries, leaves the rest in NotApplied, and returns ctx.Err().
func UninstallContext(ctx context.Context, rootDir string, targets ...OwnershipTarget) (UninstallResult, error) {
	if strings.TrimSpace(rootDir) == "" {
		return UninstallResult{}, fmt.Errorf("deployment root is required")
	}
	selected, err := selectOwnershipTargets(targets)
	if err != nil {
		return UninstallResult{}, err
	}
	manifest, err := LoadOwnershipManifest(rootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UninstallResult{Removed: []string{}, Preserved: []string{}}, nil
		}
		return UninstallResult{}, err
	}
	root, rootReal, err := uninstallRoots(rootDir)
	if err != nil {
		return UninstallResult{}, err
	}

	keepSharedSkills(manifest, selected)
	plan := planUninstall(manifest, selected)

	result, rollback, err := applyUninstallPlan(ctx, manifest, rootReal, plan)
	if err != nil {
		rollback()
		return UninstallResult{}, err
	}
	result, err = finishUninstall(manifest, root, result)
	if err == nil && len(result.NotApplied) > 0 {
		err = ctx.Err()
	}
	return result, err
}

// PreviewUninstall classifies what Uninstall would do without touching disk.
// Callers present this plan before requiring explicit authorization to apply it.
func PreviewUninstall(rootDir string, targets ...OwnershipTarget) (UninstallResult, error) {
	if strings.TrimSpace(rootDir) == "" {
		return UninstallResult{}, fmt.Errorf("deployment root is required")
	}
	selected, err := selectOwnershipTargets(targets)
	if err != nil {
		return UninstallResult{}, err
	}
	manifest, err := LoadOwnershipManifest(rootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UninstallResult{Removed: []string{}, Preserved: []string{}}, nil
		}
		return UninstallResult{}, err
	}
	_, rootReal, err := uninstallRoots(rootDir)
	if err != nil {
		return UninstallResult{}, err
	}

	result := UninstallResult{Removed: []string{}, Preserved: []string{}, PreservedReasons: map[string]string{}}
	keepSharedSkills(manifest, selected)
	for _, item := range planUninstall(manifest, selected) {
		if err := previewUninstallItem(rootReal, item, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func uninstallRoots(rootDir string) (string, string, error) {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve deployment root: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = root
	}
	return root, rootReal, nil
}

func previewUninstallItem(root string, item uninstallPlanEntry, result *UninstallResult) error {
	if item.action == actionPreserve {
		if restorablePreExisting(root, item.entry) {
			result.Restored = append(result.Restored, item.path)
			return nil
		}
		result.Preserved = append(result.Preserved, item.path)
		result.PreservedReasons[item.path] = "takt-additions"
		if outcome, err := classifyRemoval(root, item); err == nil && outcome == removalMissing {
			result.PreservedReasons[item.path] = "pre-existing"
		}
		return nil
	}
	outcome, err := classifyRemoval(root, item)
	if err != nil {
		return err
	}
	if outcome == removalEdited {
		result.Preserved = append(result.Preserved, item.path)
		result.PreservedReasons[item.path] = "user-edited"
	} else {
		result.Removed = append(result.Removed, item.path)
	}
	return nil
}

// selectOwnershipTargets validates uninstall targets are known and non-empty.
func selectOwnershipTargets(targets []OwnershipTarget) (map[OwnershipTarget]bool, error) {
	selected := make(map[OwnershipTarget]bool, len(targets))
	for _, target := range targets {
		if !ownershipTargets[target] {
			return nil, fmt.Errorf("unknown ownership target %q", target)
		}
		selected[target] = true
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("at least one ownership target is required")
	}
	return selected, nil
}

// keepSharedSkills drops TargetSkills from selected while any unselected harness still owns entries.
// Skills are shared, so a partial uninstall must leave them for the harnesses that remain.
func keepSharedSkills(manifest *OwnershipManifest, selected map[OwnershipTarget]bool) {
	if !selected[TargetSkills] {
		return
	}
	for _, entry := range manifest.Entries {
		for _, owner := range entry.Targets {
			if owner != TargetSkills && !selected[owner] {
				delete(selected, TargetSkills)
				return
			}
		}
	}
}

// sortedEntryPaths lists manifest paths in stable order for deterministic planning.
func sortedEntryPaths(manifest *OwnershipManifest) []string {
	return slices.Sorted(maps.Keys(manifest.Entries))
}

// uninstallAction classifies what Uninstall does with one manifest entry.
type uninstallAction int

const (
	actionKeep     uninstallAction = iota // still owned by others: keep, prune targets
	actionPreserve                        // pre-existing or user-edited: keep file + entry
	actionRemove                          // Takt-created, unedited: delete
)

// uninstallPlanEntry is one manifest entry classified as keep, preserve, or remove.
type uninstallPlanEntry struct {
	path      string
	action    uninstallAction
	entry     OwnershipEntry
	remaining []OwnershipTarget
}

// stagedFile tracks a file moved aside for uninstall so failures can restore it.
type stagedFile struct {
	original string
	staged   string
}

// planUninstall classifies every manifest entry against the selected targets, purely in memory.
// The on-disk edited-file check happens at removal time, so planning cannot fail on transient I/O.
func planUninstall(manifest *OwnershipManifest, selected map[OwnershipTarget]bool) []uninstallPlanEntry {
	plan := make([]uninstallPlanEntry, 0, len(manifest.Entries))
	for _, entryPath := range sortedEntryPaths(manifest) {
		entry := manifest.Entries[entryPath]
		remaining := make([]OwnershipTarget, 0, len(entry.Targets))
		fullySelected := true
		for _, owner := range entry.Targets {
			if selected[owner] {
				continue
			}
			remaining = append(remaining, owner)
			fullySelected = false
		}
		if !fullySelected {
			slices.Sort(remaining)
			plan = append(plan, uninstallPlanEntry{path: entryPath, action: actionKeep, entry: entry, remaining: remaining})
			continue
		}
		if entry.PreExisting {
			plan = append(plan, uninstallPlanEntry{path: entryPath, action: actionPreserve, entry: entry})
			continue
		}
		plan = append(plan, uninstallPlanEntry{path: entryPath, action: actionRemove, entry: entry})
	}
	return plan
}

// stageRemoval classifies one managed file and stages it aside when it is
// clean; a missing file only drops its manifest entry, an edited one is preserved.
func stageRemoval(root string, item uninstallPlanEntry, manifest *OwnershipManifest, result *UninstallResult, staged *[]stagedFile) error {
	outcome, err := classifyRemoval(root, item)
	if err != nil {
		return err
	}
	switch outcome {
	case removalMissing:
		delete(manifest.Entries, item.path)
		result.Removed = append(result.Removed, item.path)
	case removalEdited:
		result.Preserved = append(result.Preserved, item.path)
	case removalClean:
		original, err := SafeJoin(root, item.path)
		if err != nil {
			return err
		}
		stagedPath, err := stageForRemoval(root, original, item.path)
		if err != nil {
			return err
		}
		*staged = append(*staged, stagedFile{original: original, staged: stagedPath})
		delete(manifest.Entries, item.path)
		result.Removed = append(result.Removed, item.path)
	}
	return nil
}

// applyUninstallPlan mutates the in-memory manifest and stages removals aside.
// Nothing persists until staging succeeds, so failures leave manifest and files untouched.
func applyUninstallPlan(ctx context.Context, manifest *OwnershipManifest, root string, plan []uninstallPlanEntry) (UninstallResult, func(), error) {
	result := UninstallResult{Removed: []string{}, Preserved: []string{}}
	staged := make([]stagedFile, 0, len(plan))
	var restores []OwnershipEntry
	rollback := func() {
		for _, s := range staged {
			_ = renameFile(s.staged, s.original)
		}
		removeStagingDir(root)
	}
	for index, item := range plan {
		if ctx.Err() != nil {
			for _, rest := range plan[index:] {
				result.NotApplied = append(result.NotApplied, rest.path)
			}
			break
		}
		restore, err := applyUninstallItem(root, item, manifest, &result, &staged)
		if err != nil {
			return result, rollback, err
		}
		if restore {
			restores = append(restores, item.entry)
		}
	}

	for _, entry := range restores {
		if err := restoreEntry(root, entry); err != nil {
			return result, rollback, err
		}
		result.Restored = append(result.Restored, entry.Path)
	}

	// All removals staged and purged; the manifest is persisted once, by the
	// terminal finishUninstall call in Uninstall, so a failure there never
	// claims a file we could not remove.
	for _, s := range staged {
		_ = os.Remove(s.staged)
		pruneEmptyDirs(root, filepath.Dir(s.original))
	}
	removeStagingDir(root)
	return result, func() {}, nil
}

func applyUninstallItem(root string, item uninstallPlanEntry, manifest *OwnershipManifest, result *UninstallResult, staged *[]stagedFile) (bool, error) {
	switch item.action {
	case actionKeep:
		entry := item.entry
		entry.Targets = item.remaining
		manifest.Entries[item.path] = entry
	case actionPreserve:
		shouldRestore := restorablePreExisting(root, item.entry)
		if !shouldRestore {
			result.Preserved = append(result.Preserved, item.path)
		}
		delete(manifest.Entries, item.path)
		return shouldRestore, nil
	case actionRemove:
		if err := stageRemoval(root, item, manifest, result, staged); err != nil {
			return false, err
		}
	}
	return false, nil
}

// removalOutcome classifies one actionRemove entry against the file
// currently on disk.
type removalOutcome int

const (
	removalMissing removalOutcome = iota // already gone: report removed, nothing to stage
	// removalEdited keeps user-edited files instead of deleting them.
	removalEdited
	// removalClean removes unedited Takt-owned content safely.
	removalClean
)

// classifyRemoval inspects whether a Takt-owned file is still present and unedited, without mutating anything.
// Safe to call from both the apply path and PreviewUninstall.
func classifyRemoval(root string, item uninstallPlanEntry) (removalOutcome, error) {
	original, err := SafeJoin(root, item.path)
	if err != nil {
		return 0, err
	}
	data, readErr := os.ReadFile(original)
	if errors.Is(readErr, os.ErrNotExist) {
		return removalMissing, nil
	}
	if readErr != nil {
		return 0, fmt.Errorf("inspect managed file %q: %w", item.path, readErr)
	}
	if hashOf(data) != item.entry.SHA256 {
		return removalEdited, nil
	}
	return removalClean, nil
}

// stageForRemoval moves a managed file aside within root, staying on one filesystem for rollback.
func stageForRemoval(root, original, rel string) (string, error) {
	// Build the staging destination with the same symlink-escape-safe join
	// used for managed paths, so a symlinked parent outside root is rejected.
	dst, err := SafeJoin(root, filepath.Join(".takt-uninstall-staging", filepath.FromSlash(rel)))
	if err != nil {
		return "", fmt.Errorf("stage managed file %q: %w", rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), ManagedDirectoryMode); err != nil {
		return "", fmt.Errorf("stage managed file %q: %w", rel, err)
	}
	if err := renameFile(original, dst); err != nil {
		return "", fmt.Errorf("stage managed file %q: %w", rel, err)
	}
	return dst, nil
}

// removeStagingDir deletes leftover uninstall staging after success or rollback.
func removeStagingDir(root string) {
	_ = os.RemoveAll(filepath.Join(root, ".takt-uninstall-staging"))
}

// finishUninstall removes the ownership manifest once its last entry is gone;
// otherwise it persists the pruned entries.
func finishUninstall(manifest *OwnershipManifest, root string, result UninstallResult) (UninstallResult, error) {
	if len(manifest.Entries) == 0 {
		if err := os.Remove(filepath.Join(root, OwnershipManifestFilename)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return UninstallResult{}, fmt.Errorf("remove ownership manifest: %w", err)
		}
		return result, nil
	}
	if err := manifest.Save(root); err != nil {
		return UninstallResult{}, err
	}
	return result, nil
}

// UninstallResult reports what manifest-driven removal did per file.
type UninstallResult struct {
	// Removed lists deleted Takt-owned paths.
	Removed []string `json:"removed"`
	// Preserved lists paths kept because the user changed them or never owned them.
	Preserved []string `json:"preserved"`
	// Restored lists pre-existing files put back to their pre-Takt content; see restorablePreExisting.
	Restored []string `json:"restored,omitempty"`
	// NotApplied lists manifest entries a cancellation left installed.
	NotApplied []string `json:"notApplied,omitempty"`
	// PreservedReasons maps a Preserved path to why it stayed: "pre-existing", "takt-additions", or "user-edited".
	// Only PreviewUninstall populates this; Uninstall leaves it nil.
	PreservedReasons map[string]string `json:"preservedReasons,omitempty"`
}

// pruneEmptyDirs removes empty directories upward from directory until reaching root or encountering an unremovable directory.
func pruneEmptyDirs(root, directory string) {
	for directory != root {
		if err := os.Remove(directory); err != nil {
			return
		}
		directory = filepath.Dir(directory)
	}
}

// applyPlans deploys the supplied plans, records ownership for deployed artifacts, and saves the ownership manifest.
// Paths listed in skip are reported as unchanged and are excluded from deployment. If manifest is nil, it is loaded or created.
func applyPlans(ctx context.Context, rootDir string, plans []TargetPlan, skip map[string]bool, manifest *OwnershipManifest) (DeploymentResult, error) {
	activePaths, activeArtifacts, targetByPath, manifest, err := prepareApply(rootDir, plans, skip, manifest)
	if err != nil {
		return DeploymentResult{}, err
	}
	priors, err := collectPriorStates(rootDir, activeArtifacts, manifest)
	if err != nil {
		return DeploymentResult{}, err
	}
	// Validate every ownership entry (target mapping + metadata) before
	// Deploy so an unsupported target fails fast and leaves no artifacts or
	// ownership manifest on disk.
	validated, err := buildOwnershipEntries(activeArtifacts, targetByPath, priors)
	if err != nil {
		return DeploymentResult{}, err
	}

	result, err := DeployContext(ctx, rootDir, activePaths, activeArtifacts)
	cancelled := deploymentCancelled(ctx, err)
	if err != nil && !cancelled {
		return DeploymentResult{}, err
	}
	markSkippedPaths(&result, skip)
	if cancelled && len(result.Changed) == 0 {
		return result, err
	}
	// A cancelled deployment records ownership only for what is on disk now.
	validated = slices.DeleteFunc(validated, func(entry OwnershipEntry) bool { return slices.Contains(result.NotApplied, entry.Path) })
	if err := manifest.Add(validated...); err != nil {
		return DeploymentResult{}, err
	}
	if saveErr := manifest.Save(rootDir); saveErr != nil {
		return DeploymentResult{}, saveErr
	}
	return result, err
}

func deploymentCancelled(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err())
}

// prepareApply flattens and validates the plans, resolves the ownership
// manifest (loading one when nil), and drops skipped paths from deployment.
func prepareApply(rootDir string, plans []TargetPlan, skip map[string]bool, manifest *OwnershipManifest) ([]string, []Artifact, map[string]string, *OwnershipManifest, error) {
	managedPaths, artifacts, targetByPath, err := flattenPlans(plans)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if manifest == nil {
		manifest, err = loadOrCreateManifest(rootDir)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}
	activePaths, activeArtifacts := activeWithoutSkipped(managedPaths, artifacts, skip)
	for index := range activeArtifacts {
		merged, err := mergePreexistingConfig(rootDir, activeArtifacts[index], manifest)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		activeArtifacts[index] = merged
	}
	return activePaths, activeArtifacts, targetByPath, manifest, nil
}

// activeWithoutSkipped drops preserved paths so partial redeploys touch only selected files.
func activeWithoutSkipped(managedPaths []string, artifacts []Artifact, skip map[string]bool) ([]string, []Artifact) {
	activePaths := make([]string, 0, len(managedPaths))
	for _, managedPath := range managedPaths {
		if !skip[managedPath] {
			activePaths = append(activePaths, managedPath)
		}
	}
	activeArtifacts := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !skip[artifact.Path] {
			activeArtifacts = append(activeArtifacts, artifact)
		}
	}
	return activePaths, activeArtifacts
}

// collectPriorStates captures per-artifact takeover state so manifest entries can be upserted after deployment.
func collectPriorStates(rootDir string, artifacts []Artifact, manifest *OwnershipManifest) (map[string]priorState, error) {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve deployment root: %w", err)
	}
	priors := make(map[string]priorState, len(artifacts))
	for _, artifact := range artifacts {
		state, err := priorStateFor(root, artifact, manifest)
		if err != nil {
			return nil, err
		}
		priors[artifact.Path] = state
	}
	return priors, nil
}

// BackupDir holds preserved copies of pre-existing content Takt is about to take over.
// Addressed through SafeJoin so crafted artifact paths can never write outside root.
const BackupDir = ".takt-backups"

// priorStateFor captures an artifact's takeover state, backing up edited bytes before Deploy overwrites them.
func priorStateFor(root string, artifact Artifact, manifest *OwnershipManifest) (priorState, error) {
	if entry, exists := manifest.Entries[artifact.Path]; exists {
		return managedPriorState(root, artifact.Path, entry)
	}
	return preexistingPriorState(root, artifact.Path)
}

func managedPriorState(root, artifactPath string, entry OwnershipEntry) (priorState, error) {
	state := priorState{preExisting: entry.PreExisting, priorSHA256: entry.PriorSHA256, backupPath: entry.BackupPath, mode: os.FileMode(entry.Mode)}
	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifactPath)))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return priorState{}, fmt.Errorf("inspect managed file %q: %w", artifactPath, err)
	}
	if hashOf(current) == entry.SHA256 {
		return state, nil
	}
	edited, err := backedUpState(root, artifactPath, current, state.mode)
	if err != nil {
		return priorState{}, err
	}
	// Uninstall must still restore the original takeover backup, not this edit.
	if !entry.PreExisting {
		state.priorSHA256, state.backupPath = edited.priorSHA256, edited.backupPath
	}
	return state, nil
}

func preexistingPriorState(root, artifactPath string) (priorState, error) {
	state := priorState{mode: ManagedFileMode}
	destination := filepath.Join(root, filepath.FromSlash(artifactPath))
	info, err := os.Stat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return priorState{}, fmt.Errorf("inspect managed file %q: %w", artifactPath, err)
	}
	current, err := os.ReadFile(destination)
	if err != nil {
		return priorState{}, fmt.Errorf("inspect managed file %q: %w", artifactPath, err)
	}
	if info.Mode().IsRegular() {
		state.mode = info.Mode().Perm()
	}
	state, err = backedUpState(root, artifactPath, current, state.mode)
	if err != nil {
		return priorState{}, err
	}
	state.preExisting = true
	return state, nil
}

func backedUpState(root, artifactPath string, content []byte, mode os.FileMode) (priorState, error) {
	backup, err := backupContent(root, artifactPath, content)
	if err != nil {
		return priorState{}, err
	}
	return priorState{priorSHA256: hashOf(content), backupPath: backup, mode: mode}, nil
}

// backupContent copies content to BackupDir/<artifactPath> beneath root and returns the backup path.
// An existing backup with other content is never overwritten: the copy gains a hash suffix instead.
func backupContent(root, artifactPath string, content []byte) (string, error) {
	backupRel := path.Join(BackupDir, artifactPath)
	backupDest, err := SafeJoin(root, filepath.FromSlash(backupRel))
	if err != nil {
		return "", fmt.Errorf("stage backup for %q: %w", artifactPath, err)
	}
	if existing, readErr := os.ReadFile(backupDest); readErr == nil && !bytes.Equal(existing, content) {
		backupRel += "." + hashOf(content)[:8]
		if backupDest, err = SafeJoin(root, filepath.FromSlash(backupRel)); err != nil {
			return "", fmt.Errorf("stage backup for %q: %w", artifactPath, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(backupDest), ManagedDirectoryMode); err != nil {
		return "", fmt.Errorf("stage backup for %q: %w", artifactPath, err)
	}
	if err := os.WriteFile(backupDest, content, ManagedFileMode); err != nil {
		return "", fmt.Errorf("write backup for %q: %w", artifactPath, err)
	}
	return backupRel, nil
}

// markSkippedPaths reports preserved paths as unchanged so results stay complete.
func markSkippedPaths(result *DeploymentResult, skip map[string]bool) {
	for skippedPath := range skip {
		result.Unchanged = append(result.Unchanged, skippedPath)
	}
	slices.Sort(result.Unchanged)
}

// buildOwnershipEntries validates and constructs ownership entries without mutating the manifest.
// Callers reject bad input before any artifact reaches disk.
func buildOwnershipEntries(artifacts []Artifact, targetByPath map[string]string, priors map[string]priorState) ([]OwnershipEntry, error) {
	entries := make([]OwnershipEntry, 0, len(artifacts))
	for _, artifact := range artifacts {
		target, err := OwnershipTargetFor(targetByPath[artifact.Path])
		if err != nil {
			return nil, err
		}
		state := priors[artifact.Path]
		entry, err := NewOwnershipEntry(artifact.Path, artifact.Content, state.mode, state.preExisting, state.priorSHA256, state.backupPath, target)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// priorState carries an artifact's takeover metadata into its new ownership entry.
type priorState struct {
	preExisting bool
	priorSHA256 string
	backupPath  string
	mode        os.FileMode
}

// flattenPlans normalizes and combines plans' managed paths and artifacts with per-path ownership.
// Empty, duplicate, or conflicting plans are an error.
func flattenPlans(plans []TargetPlan) ([]string, []Artifact, map[string]string, error) {
	if len(plans) == 0 {
		return nil, nil, nil, fmt.Errorf("at least one target plan is required")
	}
	flat := flattenedPlans{targetByPath: make(map[string]string)}
	seenTargets := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		if err := flat.addPlan(strings.TrimSpace(plan.Target), plan, seenTargets); err != nil {
			return nil, nil, nil, err
		}
	}
	return flat.managedPaths, flat.artifacts, flat.targetByPath, nil
}

// flattenedPlans accumulates combined managed paths with per-path ownership.
type flattenedPlans struct {
	managedPaths []string
	artifacts    []Artifact
	targetByPath map[string]string
}

// addPlan merges one target's paths and artifacts, rejecting duplicates and cross-target ownership.
func (flat *flattenedPlans) addPlan(target string, plan TargetPlan, seenTargets map[string]struct{}) error {
	if target == "" {
		return fmt.Errorf("target plan identity is required")
	}
	if _, exists := seenTargets[target]; exists {
		return fmt.Errorf("duplicate target plan %q", target)
	}
	seenTargets[target] = struct{}{}
	if len(plan.ManagedPaths) == 0 {
		return fmt.Errorf("target plan %q has no managed paths", target)
	}
	for _, managedPath := range plan.ManagedPaths {
		clean, err := artifactutil.NormalizeRelPath(managedPath)
		if err != nil {
			return fmt.Errorf("invalid managed path: %w", err)
		}
		if err := flat.claim(clean, target, "managed path"); err != nil {
			return err
		}
		flat.managedPaths = append(flat.managedPaths, clean)
	}
	for _, artifact := range plan.Artifacts {
		clean, err := artifactutil.NormalizeRelPath(artifact.Path)
		if err != nil {
			return fmt.Errorf("invalid artifact path: %w", err)
		}
		if err := flat.claim(clean, target, "artifact path"); err != nil {
			return err
		}
		artifact.Path = clean
		flat.artifacts = append(flat.artifacts, artifact)
	}
	return nil
}

// claim assigns one clean path to a target, rejecting paths already owned by another target.
func (flat *flattenedPlans) claim(clean, target, kind string) error {
	if owner, exists := flat.targetByPath[clean]; exists && owner != target {
		return fmt.Errorf("%s %q belongs to targets %q and %q", kind, clean, owner, target)
	}
	flat.targetByPath[clean] = target
	return nil
}
