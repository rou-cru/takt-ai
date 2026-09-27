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

import "context"

// DriftCorrectionResult reports what CorrectDrift applied, plus requested paths missing from plans.
type DriftCorrectionResult struct {
	DeploymentResult
	// Unresolved lists selected paths absent from plans, e.g. a sub-agent the catalog no longer renders.
	Unresolved []string `json:"unresolved,omitempty"`
}

// CorrectDriftContext is CorrectDrift with cooperative cancellation; cancellation returns restored work with ctx.Err().
func CorrectDriftContext(ctx context.Context, rootDir string, plans []TargetPlan, selectedPaths []string, runtime ProviderRuntime) (DriftCorrectionResult, error) {
	managedPaths, _, _, err := flattenPlans(plans)
	if err != nil {
		return DriftCorrectionResult{}, err
	}
	managed := make(map[string]bool, len(managedPaths))
	for _, path := range managedPaths {
		managed[path] = true
	}

	resolved := make(map[string]bool, len(selectedPaths))
	var unresolved []string
	for _, path := range selectedPaths {
		if managed[path] {
			resolved[path] = true
		} else {
			unresolved = append(unresolved, path)
		}
	}

	preserve := make([]string, 0, len(managedPaths))
	for _, path := range managedPaths {
		if !resolved[path] {
			preserve = append(preserve, path)
		}
	}

	result, err := ApplyContext(ctx, rootDir, plans, runtime, preserve...)
	if err != nil {
		if ctx.Err() != nil {
			return DriftCorrectionResult{DeploymentResult: result, Unresolved: unresolved}, err
		}
		return DriftCorrectionResult{}, err
	}
	return DriftCorrectionResult{DeploymentResult: result, Unresolved: unresolved}, nil
}
