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
// returning an empty manifest when none exists yet.
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
// paths are intentionally left untouched; lifecycle policy owns deletion.
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
// redeployed; paths without manifest entries deploy as a fresh install.
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
// second uninstall is also a no-op.
func Uninstall(rootDir string, targets ...OwnershipTarget) (UninstallResult, error) {
	if strings.TrimSpace(rootDir) == "" {
		return UninstallResult{}, fmt.Errorf("deployment root is required")
	}
	if len(targets) == 0 {
		return UninstallResult{}, fmt.Errorf("at least one ownership target is required")
	}
	selected := make(map[OwnershipTarget]bool, len(targets))
	for _, target := range targets {
		if !ownershipTargets[target] {
			return UninstallResult{}, fmt.Errorf("unknown ownership target %q", target)
		}
		selected[target] = true
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
	entryPaths := make([]string, 0, len(manifest.Entries))
	for entryPath := range manifest.Entries {
		entryPaths = append(entryPaths, entryPath)
	}
	sort.Strings(entryPaths)
	for _, entryPath := range entryPaths {
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
			continue
		}
		if entry.PreExisting {
			result.Preserved = append(result.Preserved, entryPath)
		} else {
			destination := filepath.Join(root, filepath.FromSlash(entryPath))
			if err := os.Remove(destination); err == nil {
				result.Removed = append(result.Removed, entryPath)
				pruneEmptyDirs(root, filepath.Dir(destination))
			} else if !os.IsNotExist(err) {
				return UninstallResult{}, fmt.Errorf("remove managed file %q: %w", entryPath, err)
			}
		}
		delete(manifest.Entries, entryPath)
	}
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
// removal failing means something inside is still wanted).
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
// loaded or created here; Sync passes its already-loaded instance.
func applyPlans(rootDir string, plans []TargetPlan, skip map[string]bool, manifest *OwnershipManifest) (DeploymentResult, error) {
	managedPaths, artifacts, targetByPath, err := flattenPlans(plans)
	if err != nil {
		return DeploymentResult{}, err
	}
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

	root, err := filepath.Abs(rootDir)
	if err != nil {
		return DeploymentResult{}, fmt.Errorf("resolve deployment root: %w", err)
	}
	priors := make(map[string]priorState, len(activeArtifacts))
	for _, artifact := range activeArtifacts {
		state := priorState{mode: 0o644}
		destination := filepath.Join(root, filepath.FromSlash(artifact.Path))
		info, statErr := os.Stat(destination)
		switch {
		case statErr == nil:
			current, readErr := os.ReadFile(destination)
			if readErr != nil {
				return DeploymentResult{}, fmt.Errorf("inspect managed file %q: %w", artifact.Path, readErr)
			}
			digest := sha256.Sum256(current)
			state.preExisting = true
			state.priorSHA256 = hex.EncodeToString(digest[:])
			if info.Mode().IsRegular() {
				state.mode = info.Mode().Perm()
			}
		case os.IsNotExist(statErr):
		default:
			return DeploymentResult{}, fmt.Errorf("inspect managed file %q: %w", artifact.Path, statErr)
		}
		priors[artifact.Path] = state
	}

	result, err := Deploy(rootDir, activePaths, activeArtifacts)
	if err != nil {
		return DeploymentResult{}, err
	}
	for skippedPath := range skip {
		result.Unchanged = append(result.Unchanged, skippedPath)
	}
	sort.Strings(result.Unchanged)

	if manifest == nil {
		manifest, err = loadOrCreateManifest(rootDir)
		if err != nil {
			return DeploymentResult{}, err
		}
	}
	entries := make([]OwnershipEntry, 0, len(activeArtifacts))
	for _, artifact := range activeArtifacts {
		target, err := OwnershipTargetFor(targetByPath[artifact.Path])
		if err != nil {
			return DeploymentResult{}, err
		}
		state := priors[artifact.Path]
		entry, err := NewOwnershipEntry(artifact.Path, artifact.Content, state.mode, state.preExisting, state.priorSHA256, "", target)
		if err != nil {
			return DeploymentResult{}, err
		}
		entries = append(entries, entry)
	}
	if err := manifest.Add(entries...); err != nil {
		return DeploymentResult{}, err
	}
	return result, manifest.Save(rootDir)
}

type priorState struct {
	preExisting bool
	priorSHA256 string
	mode        os.FileMode
}

// flattenPlans rejects duplicate targets and cross-target ownership conflicts,
// normalizes every path once, and reports which target owns each path.
func flattenPlans(plans []TargetPlan) ([]string, []Artifact, map[string]string, error) {
	if len(plans) == 0 {
		return nil, nil, nil, fmt.Errorf("at least one target plan is required")
	}
	seenTargets := make(map[string]struct{}, len(plans))
	owners := make(map[string]string)
	targetByPath := make(map[string]string)
	var managedPaths []string
	var artifacts []Artifact
	for _, plan := range plans {
		target := strings.TrimSpace(plan.Target)
		if target == "" {
			return nil, nil, nil, fmt.Errorf("target plan identity is required")
		}
		if _, exists := seenTargets[target]; exists {
			return nil, nil, nil, fmt.Errorf("duplicate target plan %q", target)
		}
		seenTargets[target] = struct{}{}
		if len(plan.ManagedPaths) == 0 {
			return nil, nil, nil, fmt.Errorf("target plan %q has no managed paths", target)
		}
		for _, managedPath := range plan.ManagedPaths {
			clean, err := artifactutil.NormalizeRelPath(managedPath)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("invalid managed path: %w", err)
			}
			if owner, exists := owners[clean]; exists && owner != target {
				return nil, nil, nil, fmt.Errorf("managed path %q belongs to targets %q and %q", clean, owner, target)
			}
			owners[clean] = target
			targetByPath[clean] = target
			managedPaths = append(managedPaths, clean)
		}
		for _, artifact := range plan.Artifacts {
			clean, err := artifactutil.NormalizeRelPath(artifact.Path)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("invalid artifact path: %w", err)
			}
			if owner, exists := owners[clean]; exists && owner != target {
				return nil, nil, nil, fmt.Errorf("artifact path %q belongs to targets %q and %q", clean, owner, target)
			}
			owners[clean] = target
			targetByPath[clean] = target
			artifact.Path = clean
			artifacts = append(artifacts, artifact)
		}
	}
	return managedPaths, artifacts, targetByPath, nil
}
