// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package setup

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// AllComponents returns every selectable component in display and planning
// order from the capability manifest. It is the single source of truth for
// "install everything applicable"; Engram and skills are not selectable.
func AllComponents() ([]model.ComponentID, error) {
	order, err := catalog.SelectableOrder()
	if err != nil {
		return nil, fmt.Errorf("load selectable component order: %w", err)
	}
	return order, nil
}

// ValidateComponents validates custom-setup component names: every name must
// be known, non-empty, and unique. The returned list follows the canonical
// component order regardless of input order.
func ValidateComponents(names []string) ([]model.ComponentID, error) {
	order, err := AllComponents()
	if err != nil {
		return nil, err
	}
	seen := make(map[model.ComponentID]struct{}, len(order))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("component name is empty")
		}
		component := model.ComponentID(name)
		if !slices.Contains(order, component) {
			return nil, fmt.Errorf("unknown component %q", name)
		}
		if _, exists := seen[component]; exists {
			return nil, fmt.Errorf("duplicate component %q", name)
		}
		seen[component] = struct{}{}
	}
	ordered := make([]model.ComponentID, 0, len(seen))
	for _, component := range order {
		if _, ok := seen[component]; ok {
			ordered = append(ordered, component)
		}
	}
	return ordered, nil
}

// ResolveComponents validates custom-setup names and drops components whose dependencies are unsatisfied.
// A dependency the user explicitly deselected is never re-added.
func ResolveComponents(names []string) ([]model.ComponentID, []catalog.Removal, error) {
	components, err := ValidateComponents(names)
	if err != nil {
		return nil, nil, err
	}
	kept, removals, err := catalog.ReconcileSelection(components)
	if err != nil {
		return nil, nil, fmt.Errorf("reconcile components: %w", err)
	}
	return kept, removals, nil
}

// openCodeComponentArtifacts applies the selected components to the OpenCode
// plan: config merges plus the standalone DAG, memory and VFS plugin
// artifacts. The artifacts are home-rooted.
func openCodeComponentArtifacts(components []model.ComponentID, config *opencode.ConfigRequest) ([]Artifact, error) {
	config.Permissions = true
	binary, err := taktAIExecutable()
	if err != nil {
		return nil, fmt.Errorf("resolve takt-ai executable for the OpenCode memory and VFS plugins: %w", err)
	}
	for _, component := range components {
		switch component {
		case model.ComponentContext7:
			config.Context7 = true
		}
	}
	rendered := []opencode.Artifact{opencode.TaktDagPluginArtifact(binary), opencode.OpenCodePluginPackageArtifact(), opencode.TaktMemoryPluginArtifact(binary), opencode.TaktVFSPluginArtifact(binary, true), opencode.TaktSandboxAdapterArtifact()}
	artifacts := make([]Artifact, 0, len(rendered))
	for _, r := range rendered {
		artifacts = append(artifacts, Artifact{Path: r.Path, Content: r.Content})
	}
	return artifacts, nil
}

// taktAIExecutable is the absolute takt-ai path rendered into the session-start hooks and plugins; tests replace it.
var taktAIExecutable = os.Executable
