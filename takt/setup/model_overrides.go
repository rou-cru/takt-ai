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
	"maps"

	"github.com/rou-cru/takt-ai/takt/model"
)

// ApplyModelOverrideChange reassigns one sub-agent's model starting from the
// real installed configuration, redeploying only the artifacts it changes.
func ApplyModelOverrideChange(ctx context.Context, rootDir string, subAgentID string, assignment model.ModelAssignment, runtime ProviderRuntime) (DeploymentResult, error) {
	request, err := LoadInstalledConfig(rootDir)
	if err != nil {
		return DeploymentResult{}, err
	}
	before, _, err := BuildTargetPlans(request)
	if err != nil {
		return DeploymentResult{}, err
	}

	request.OpenCodeModelOverrides = mergedOverrides(request.OpenCodeModelOverrides, subAgentID, assignment)

	after, _, err := BuildTargetPlans(request)
	if err != nil {
		return DeploymentResult{}, err
	}

	managedPaths, _, _, err := flattenPlans(after)
	if err != nil {
		return DeploymentResult{}, err
	}
	preserve := unchangedPaths(managedPaths, changedArtifactPathSet(before, after))

	result, err := ApplyContext(ctx, rootDir, after, runtime, preserve...)
	if err != nil {
		if ctx.Err() != nil {
			return result, err
		}
		return DeploymentResult{}, err
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

// mergedOverrides copies existing and applies one change: set the override, or clear it when Model is empty.
func mergedOverrides(existing map[string]model.ModelAssignment, subAgentID string, assignment model.ModelAssignment) map[string]model.ModelAssignment {
	merged := make(map[string]model.ModelAssignment, len(existing)+1)
	maps.Copy(merged, existing)
	if assignment.Model == "" {
		delete(merged, subAgentID)
	} else {
		merged[subAgentID] = assignment
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
