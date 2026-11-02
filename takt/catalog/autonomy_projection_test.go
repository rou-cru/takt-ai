// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/setup"
)

// Projection preserves the applicable guidance and resolves existing policy
// markers; it does not certify how a model follows those instructions.
func TestAutonomyGuidanceSurvivesDeployment(t *testing.T) {
	request, err := setup.DefaultPlanRequest()
	if err != nil {
		t.Fatal(err)
	}
	plans, _, err := setup.BuildTargetPlans(request)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		catalog.GuidelinesPath,
		"agents/takt/OPERATIONS.md", "agents/takt/PERSONA.md",
		"skills/takt-failure-handling/SKILL.md",
		"skills/takt-workflow-selection/SKILL.md",
		"skills/takt-workflow-selection-exceptions/SKILL.md",
		"skills/takt-sdd-recovery/SKILL.md",
	}
	artifacts := map[string]setup.Artifact{}
	for _, plan := range plans {
		for _, artifact := range plan.Artifacts {
			artifacts[artifact.Path] = artifact
		}
	}
	root := t.TempDir()
	for _, path := range paths {
		source, err := fs.ReadFile(catalog.AssetFS(), path)
		if err != nil {
			t.Fatal(err)
		}
		want, err := dispatch.ResolvePolicyPlaceholders(source)
		if err != nil {
			t.Fatal(err)
		}
		destination := opencode.DeployPath(path)
		artifact, ok := artifacts[destination]
		if !ok {
			t.Fatalf("missing projection: %s", destination)
		}
		if _, err := setup.DeployContextProgress(context.Background(), root, []string{destination}, []setup.Artifact{artifact}, nil); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(root, destination))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("deployed guidance differs at %s: %v", destination, err)
		}
		if strings.Contains(string(got), "{{policy.") {
			t.Fatalf("unresolved policy in %s", destination)
		}
	}
}
