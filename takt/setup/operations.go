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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	artifactutil "github.com/rou-cru/takt-ai/takt/internal/artifacts"
)

// loadOrCreateManifest loads the deployment root's ownership manifest,
// loadOrCreateManifest loads the ownership manifest for rootDir, or returns an empty manifest when none exists.
func loadOrCreateManifest(rootDir string) (*OwnershipManifest, error) {
	manifest, err := LoadOwnershipManifest(rootDir)
	if errors.Is(err, os.ErrNotExist) {
		return NewOwnershipManifest(), nil
	}
	return manifest, err
}

// TargetPlan is the plain setup input supplied by a native adapter. Target is
// opaque to setup; its manifest and artifacts define the target's ownership.
type TargetPlan struct {
	Target       string
	ManagedPaths []string
	Artifacts    []Artifact
}

// Apply deploys all target plans through the shared deployment path and
// records ownership for every deployed file in the ownership manifest. Stale
// Apply deploys all managed paths and artifacts in the supplied plans without skipping existing files.
func Apply(rootDir string, plans []TargetPlan) (DeploymentResult, error) {
	return applyPlans(rootDir, plans, nil, nil)
}

// Sync re-runs the deploy for the requested targets and reconciles the
// ownership manifest.
//
// Preservation rule: when a planned artifact path already has a manifest
// entry, sync hashes the on-disk bytes; a mismatch against entry.SHA256 means
// the user edited the file since install, so sync skips redeploying it (the
// path is reported unchanged) and leaves its manifest entry untouched — user
// edits to previously installed files are never clobbered. A missing file is
// Sync deploys target plans while preserving managed files that have been modified locally. Missing files and paths without manifest entries are redeployed.
func Sync(rootDir string, plans []TargetPlan) (DeploymentResult, error) {
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
	skip := make(map[string]bool)
	for _, artifact := range artifacts {
		entry, managed := manifest.Entries[artifact.Path]
		if !managed {
			continue
		}
		current, err := os.ReadFile(filepath.Join(rootDir, filepath.FromSlash(artifact.Path)))
		if err != nil {
			if os.IsNotExist(err) {
				continue // deleted locally: redeploy it
			}
			return DeploymentResult{}, fmt.Errorf("inspect managed file %q: %w", artifact.Path, err)
		}
		digest := sha256.Sum256(current)
		if hex.EncodeToString(digest[:]) != entry.SHA256 {
			skip[artifact.Path] = true
		}
	}
	return applyPlans(rootDir, plans, skip, manifest)
}

// Uninstall removes Takt's footprint for the given ownership targets, driven
// entirely by the ownership manifest.
//
// Preservation policy for pre-existing files: an entry with PreExisting set is
// NEVER modified or deleted on uninstall. Its current bytes stay in place as
// user content (BackupPath is ignored); only files Takt created outright are
// removed, followed by pruning of newly emptied parent directories beneath
// rootDir. Ownership is released per target: entries shared with unselected
// targets keep their files and remaining owners. When no entries remain the
// manifest itself is deleted. A missing manifest is a successful no-op, so a
// Uninstall removes files owned exclusively by the specified targets and updates the ownership manifest.
// Pre-existing files and files still owned by other targets are preserved. A missing manifest is treated as a successful no-op.
// It returns the paths removed and preserved during the operation.
func Uninstall(rootDir string, targets ...OwnershipTarget) (UninstallResult, error) {
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
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return UninstallResult{}, fmt.Errorf("resolve deployment root: %w", err)
	}

	result := UninstallResult{Removed: []string{}, Preserved: []string{}}
	for _, entryPath := range sortedEntryPaths(manifest) {
		if err := uninstallEntry(manifest, entryPath, selected, root, &result); err != nil {
			return UninstallResult{}, err
		}
	}
	return finishUninstall(manifest, root, result)
}

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

func sortedEntryPaths(manifest *OwnershipManifest) []string {
	entryPaths := make([]string, 0, len(manifest.Entries))
	for entryPath := range manifest.Entries {
		entryPaths = append(entryPaths, entryPath)
	}
	sort.Strings(entryPaths)
	return entryPaths
}

// uninstallEntry drops one manifest entry: partially unselected entries keep
// their remaining owners, pre-existing files are preserved, and fully
// Takt-owned files are removed from disk.
func uninstallEntry(manifest *OwnershipManifest, entryPath string, selected map[OwnershipTarget]bool, root string, result *UninstallResult) error {
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
		entry.Targets = remaining
		manifest.Entries[entryPath] = entry
		return nil
	}
	if entry.PreExisting {
		result.Preserved = append(result.Preserved, entryPath)
	} else if err := removeManagedEntry(root, entryPath); err != nil {
		return err
	} else {
		result.Removed = append(result.Removed, entryPath)
	}
	delete(manifest.Entries, entryPath)
	return nil
}

func removeManagedEntry(root, entryPath string) error {
	destination := filepath.Join(root, filepath.FromSlash(entryPath))
	err := os.Remove(destination)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove managed file %q: %w", entryPath, err)
	}
	pruneEmptyDirs(root, filepath.Dir(destination))
	return nil
}

// finishUninstall removes the ownership manifest once its last entry is gone;
// otherwise it persists the pruned entries.
func finishUninstall(manifest *OwnershipManifest, root string, result UninstallResult) (UninstallResult, error) {
	if len(manifest.Entries) == 0 {
		if err := os.Remove(filepath.Join(root, OwnershipManifestFilename)); err != nil && !os.IsNotExist(err) {
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
	Removed   []string `json:"removed"`
	Preserved []string `json:"preserved"`
}

// pruneEmptyDirs removes now-empty directories walking upward from directory,
// stopping at the deployment root or at the first non-empty directory (its
// pruneEmptyDirs removes empty directories upward from directory until reaching root or encountering an unremovable directory.
func pruneEmptyDirs(root, directory string) {
	for directory != root {
		if err := os.Remove(directory); err != nil {
			return
		}
		directory = filepath.Dir(directory)
	}
}

// applyPlans flattens and validates the plans, deploys every artifact except
// those named in skip (skipped paths are reported unchanged), then upserts
// ownership manifest entries for everything it deployed. A nil manifest is
// applyPlans deploys the supplied plans, records ownership for deployed artifacts, and saves the ownership manifest.
// Paths listed in skip are reported as unchanged and are excluded from deployment. If manifest is nil, it is loaded or created.
func applyPlans(rootDir string, plans []TargetPlan, skip map[string]bool, manifest *OwnershipManifest) (DeploymentResult, error) {
	activePaths, activeArtifacts, targetByPath, manifest, err := prepareApply(rootDir, plans, skip, manifest)
	if err != nil {
		return DeploymentResult{}, err
	}
	priors, err := collectPriorStates(rootDir, activeArtifacts, manifest)
	if err != nil {
		return DeploymentResult{}, err
	}

	result, err := Deploy(rootDir, activePaths, activeArtifacts)
	if err != nil {
		return DeploymentResult{}, err
	}
	markSkippedPaths(&result, skip)
	if err := recordOwnershipEntries(manifest, activeArtifacts, targetByPath, priors); err != nil {
		return DeploymentResult{}, err
	}
	return result, manifest.Save(rootDir)
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
	return activePaths, activeArtifacts, targetByPath, manifest, nil
}

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

// collectPriorStates captures, per artifact, the ownership entry when present
// or the on-disk bytes otherwise, so manifest entries can be upserted after
// deployment.
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

func priorStateFor(root string, artifact Artifact, manifest *OwnershipManifest) (priorState, error) {
	state := priorState{mode: 0o644}
	if entry, exists := manifest.Entries[artifact.Path]; exists {
		state.preExisting = entry.PreExisting
		state.priorSHA256 = entry.PriorSHA256
		state.backupPath = entry.BackupPath
		state.mode = os.FileMode(entry.Mode)
		return state, nil
	}
	destination := filepath.Join(root, filepath.FromSlash(artifact.Path))
	info, statErr := os.Stat(destination)
	switch {
	case statErr == nil:
		current, readErr := os.ReadFile(destination)
		if readErr != nil {
			return priorState{}, fmt.Errorf("inspect managed file %q: %w", artifact.Path, readErr)
		}
		digest := sha256.Sum256(current)
		state.preExisting = true
		state.priorSHA256 = hex.EncodeToString(digest[:])
		if info.Mode().IsRegular() {
			state.mode = info.Mode().Perm()
		}
	case os.IsNotExist(statErr):
	default:
		return priorState{}, fmt.Errorf("inspect managed file %q: %w", artifact.Path, statErr)
	}
	return state, nil
}

func markSkippedPaths(result *DeploymentResult, skip map[string]bool) {
	for skippedPath := range skip {
		result.Unchanged = append(result.Unchanged, skippedPath)
	}
	sort.Strings(result.Unchanged)
}

func recordOwnershipEntries(manifest *OwnershipManifest, artifacts []Artifact, targetByPath map[string]string, priors map[string]priorState) error {
	entries := make([]OwnershipEntry, 0, len(artifacts))
	for _, artifact := range artifacts {
		target, err := OwnershipTargetFor(targetByPath[artifact.Path])
		if err != nil {
			return err
		}
		state := priors[artifact.Path]
		entry, err := NewOwnershipEntry(artifact.Path, artifact.Content, state.mode, state.preExisting, state.priorSHA256, state.backupPath, target)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
	}
	return manifest.Add(entries...)
}

type priorState struct {
	preExisting bool
	priorSHA256 string
	backupPath  string
	mode        os.FileMode
}

// flattenPlans rejects duplicate targets and cross-target ownership conflicts,
// flattenPlans normalizes and combines managed paths and artifacts from target plans,
// returning the target associated with each path. It reports an error for invalid,
// incomplete, duplicate, or conflicting plans.
func flattenPlans(plans []TargetPlan) ([]string, []Artifact, map[string]string, error) {
	if len(plans) == 0 {
		return nil, nil, nil, fmt.Errorf("at least one target plan is required")
	}
	flat := flattenedPlans{
		owners:       make(map[string]string),
		targetByPath: make(map[string]string),
	}
	seenTargets := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		if err := flat.addPlan(strings.TrimSpace(plan.Target), plan, seenTargets); err != nil {
			return nil, nil, nil, err
		}
	}
	return flat.managedPaths, flat.artifacts, flat.targetByPath, nil
}

type flattenedPlans struct {
	managedPaths []string
	artifacts    []Artifact
	targetByPath map[string]string
	owners       map[string]string
}

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

func (flat *flattenedPlans) claim(clean, target, kind string) error {
	if owner, exists := flat.owners[clean]; exists && owner != target {
		return fmt.Errorf("%s %q belongs to targets %q and %q", kind, clean, owner, target)
	}
	flat.owners[clean] = target
	flat.targetByPath[clean] = target
	return nil
}
