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
	"fmt"
	"maps"
	"slices"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/model"
)

// ApplyModelOverrideChanges reassigns every changed sub-agent in one
// deployment, starting from the real installed configuration and redeploying
// only the artifacts the changes touch. An empty Model clears an override.
func ApplyModelOverrideChanges(ctx context.Context, rootDir string, changes map[string]model.ModelAssignment, runtime ProviderRuntime) (DeploymentResult, error) {
	request, err := LoadInstalledConfig(rootDir)
	if err != nil {
		return DeploymentResult{}, err
	}
	before, _, err := BuildTargetPlans(request)
	if err != nil {
		return DeploymentResult{}, err
	}

	request.OpenCodeModelOverrides = mergedOverrides(request.OpenCodeModelOverrides, changes)

	after, _, err := BuildTargetPlans(request)
	if err != nil {
		return DeploymentResult{}, err
	}

	managedPaths, _, _, err := flattenPlans(after)
	if err != nil {
		return DeploymentResult{}, err
	}
	changed := changedArtifactPathSet(before, after)
	// The installed record can disagree with the file (edits made outside
	// Takt), so a request the record already satisfies may still change it.
	deployed, err := LoadDeployedModels(rootDir)
	if err != nil {
		return DeploymentResult{}, err
	}
	for id := range changes {
		wanted, ok := request.OpenCodeModelOverrides[id]
		if !ok {
			wanted = model.ModelAssignment{Model: request.OpenCode.Model}
		}
		if deployed[id].Model != wanted.Model {
			changed[opencode.ConfigPath()] = true
		}
	}
	preserve := unchangedPaths(managedPaths, changed)

	// A reassignment changes only the models asked for. When the user edited
	// the config since Takt wrote it, patch just those models in place instead
	// of redeploying the rendered file over the edits.
	patch, err := planEditedConfigPatch(rootDir, after, changed, slices.Sorted(maps.Keys(changes)))
	if err != nil {
		return DeploymentResult{}, err
	}
	if patch != nil {
		preserve = append(preserve, patch.path)
	}

	result, err := ApplyContext(ctx, rootDir, after, runtime, preserve...)
	if err != nil {
		if ctx.Err() != nil {
			return result, err
		}
		return DeploymentResult{}, err
	}
	if patch != nil {
		// The patch was computed from the bytes planned above; apply backs those
		// bytes up first, so an edit landing in between stays recoverable.
		if err := patch.apply(rootDir); err != nil {
			return DeploymentResult{}, fmt.Errorf("the rest of the deployment is applied but the edited config was not patched; repeat the reassignment: %w", err)
		}
		result.Unchanged = slices.DeleteFunc(result.Unchanged, func(path string) bool { return path == patch.path })
		result.Changed = append(result.Changed, patch.path)
	}
	if err := SaveInstalledConfig(rootDir, request); err != nil {
		return DeploymentResult{}, err
	}
	return result, nil
}

func unchangedPaths(paths []string, changed map[string]bool) []string {
	preserve := make([]string, 0, len(paths))
	for _, path := range paths {
		if !changed[path] {
			preserve = append(preserve, path)
		}
	}
	return preserve
}

// mergedOverrides copies existing and applies changes: set each override, or clear it when Model is empty.
func mergedOverrides(existing, changes map[string]model.ModelAssignment) map[string]model.ModelAssignment {
	merged := make(map[string]model.ModelAssignment, len(existing)+len(changes))
	maps.Copy(merged, existing)
	for subAgentID, assignment := range changes {
		if assignment.Model == "" {
			delete(merged, subAgentID)
		} else {
			merged[subAgentID] = assignment
		}
	}
	return merged
}

// changedArtifactPathSet reports paths whose rendered content differs, so partial redeploys preserve the rest.
func changedArtifactPathSet(before, after []TargetPlan) map[string]bool {
	priorContent := make(map[string][]byte)
	for _, plan := range before {
		for _, artifact := range plan.Artifacts {
			priorContent[artifact.Path] = artifact.Content
		}
	}
	changed := make(map[string]bool)
	for _, plan := range after {
		for _, artifact := range plan.Artifacts {
			prior, existed := priorContent[artifact.Path]
			if !existed || !bytes.Equal(prior, artifact.Content) {
				changed[artifact.Path] = true
			}
		}
	}
	return changed
}
