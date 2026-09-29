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
	"cmp"
	"slices"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// OpenCodePlanOptions contains explicit OpenCode global projection inputs.
type OpenCodePlanOptions struct {
	// Model is the fallback model for sub-agents without an override.
	Model string `json:"model"`
	// GlobalPrompt is the shared crew prompt every OpenCode agent reads. Empty
	// skips the artifact so callers that only project models stay valid.
	GlobalPrompt string `json:"global_prompt"`
}

// PlanRequest contains explicit native content and model overrides;
// assignments resolve from the semantic catalog.
type PlanRequest struct {
	// OpenCode holds the OpenCode fallback model.
	OpenCode OpenCodePlanOptions `json:"opencode"`
	// OpenCodeModelOverrides reassigns OpenCode sub-agent models by ID.
	OpenCodeModelOverrides map[string]model.ModelAssignment `json:"opencode_model_overrides"`
	// Components names artifact-only components for a custom setup; empty means defaults.
	Components []string `json:"components"`
}

// BuildTargetPlans validates the request and builds the OpenCode plan with
// path-sorted artifacts. The returned removals are the manifest dependencies
// dropped from the selection, if any.
func BuildTargetPlans(request PlanRequest) ([]TargetPlan, []catalog.Removal, error) {
	components, removals, err := ResolveComponents(request.Components)
	if err != nil {
		return nil, nil, err
	}
	plan, err := buildOpenCodePlan(request.OpenCode, request.OpenCodeModelOverrides, components)
	if err != nil {
		return nil, nil, err
	}
	return []TargetPlan{plan}, removals, nil
}

// buildOpenCodePlan builds an OpenCode configuration plan from the declarative catalog and the
// selected model, using the default model when none is specified. Per-instance overrides take
// precedence over the fallback model. OpenCode does not compile a per-agent
// prompt file: it deploys the catalog tree as-is and points each agent's prompt at it via {file:...}
// references (opencode.ComposePrompt), so no global prompt artifact is generated here (see README.md).
func buildOpenCodePlan(options OpenCodePlanOptions, overrides map[string]model.ModelAssignment, components []model.ComponentID) (TargetPlan, error) {
	modelID := options.Model
	pack, err := catalog.LoadPackages()
	if err != nil {
		return TargetPlan{}, err
	}
	specs := make([]opencode.AgentSpec, 0, len(pack.Agents))
	for _, def := range pack.Agents {
		for _, instanceID := range def.Instances {
			assignment, ok := overrides[instanceID]
			if !ok {
				assignment = model.ModelAssignment{Model: modelID}
			}
			profile := def.Profile(instanceID)
			vfsCapabilities, _ := def.VFSCapabilities(instanceID) // the catalog rejects an undeclared grant on load
			specs = append(specs, opencode.AgentSpec{
				ID:              instanceID,
				Description:     profile.Description,
				Mode:            opencode.AgentMode(profile.Role),
				System:          opencode.ComposePrompt(def, instanceID),
				Model:           assignment.Model,
				Variant:         assignment.Effort,
				Role:            profile.Role,
				VFSCapabilities: vfsCapabilities,
				Skills:          def.Skills,
			})
		}
	}
	configRequest := opencode.ConfigRequest{
		Assignment: model.ModelAssignment{Model: modelID},
		Agents:     specs,
	}
	componentArtifacts, err := openCodeComponentArtifacts(components, &configRequest)
	if err != nil {
		return TargetPlan{}, err
	}
	config, err := opencode.RenderConfig(configRequest)
	if err != nil {
		return TargetPlan{}, err
	}

	artifacts := make([]Artifact, 0, len(pack.Files)+len(componentArtifacts)+1)
	generatedPaths := make([]string, 0, len(pack.Files)+len(componentArtifacts))
	for _, f := range pack.Files {
		artifacts = append(artifacts, Artifact{Path: opencode.DeployPath(f.Path), Content: f.Content})
	}
	artifacts = append(artifacts, componentArtifacts...)
	for _, artifact := range artifacts {
		generatedPaths = append(generatedPaths, artifact.Path)
	}
	artifacts = append(artifacts, Artifact{Path: config.Path, Content: config.Content})
	sortArtifacts(artifacts)
	managedPaths, err := shared.NewManagedPaths(generatedPaths)
	if err != nil {
		return TargetPlan{}, err
	}
	return TargetPlan{Target: model.AgentOpenCode, ManagedPaths: managedPaths, Artifacts: artifacts}, nil
}

// sortArtifacts orders artifacts lexicographically by their relative path.
func sortArtifacts(artifacts []Artifact) {
	slices.SortFunc(artifacts, func(a, b Artifact) int { return cmp.Compare(a.Path, b.Path) })
}
