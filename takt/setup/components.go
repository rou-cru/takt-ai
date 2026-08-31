// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package setup

import (
	"fmt"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/claude"
	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/model"
)

const taktTheme = "takt-kanagawa"

// componentOrder lists the selectable artifact-only components in canonical
// display and planning order. ComponentEngram and ComponentSkills stay
// reserved but unselectable: engram requires runtime actions that the plan
// pipeline cannot express, and skills are deployed unconditionally by every
// install and sync.
var componentOrder = []model.ComponentID{
	model.ComponentContext7,
	model.ComponentPermission,
	model.ComponentTheme,
	model.ComponentClaudeTheme,
	model.ComponentOpenCodeTaktLogo,
}

// ValidateComponents validates custom-setup component names: every name must
// be known, non-empty, and unique. The returned list follows the canonical
// component order regardless of input order.
func ValidateComponents(names []string) ([]model.ComponentID, error) {
	seen := make(map[model.ComponentID]struct{}, len(componentOrder))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("component name is empty")
		}
		component, ok := componentByName(name)
		if !ok {
			return nil, fmt.Errorf("unknown component %q", name)
		}
		if _, exists := seen[component]; exists {
			return nil, fmt.Errorf("duplicate component %q", name)
		}
		seen[component] = struct{}{}
	}
	ordered := make([]model.ComponentID, 0, len(seen))
	for _, component := range componentOrder {
		if _, ok := seen[component]; ok {
			ordered = append(ordered, component)
		}
	}
	return ordered, nil
}

func componentByName(name string) (model.ComponentID, bool) {
	for _, component := range componentOrder {
		if string(component) == name {
			return component, true
		}
	}
	return "", false
}

// claudeComponentArtifacts applies the selected components to the Claude plan:
// it returns the standalone artifacts to append and mutates the settings
// request for components that merge into settings.json. Components scoped to
// other targets are skipped, mirroring the reference adapter-level no-ops.
func claudeComponentArtifacts(components []model.ComponentID, settings *claude.SettingsRequest) []Artifact {
	artifacts := make([]Artifact, 0, len(components))
	for _, component := range components {
		switch component {
		case model.ComponentContext7:
			rendered := claude.Context7ServerArtifact()
			artifacts = append(artifacts, Artifact{Path: rendered.Path, Content: rendered.Content})
		case model.ComponentPermission:
			permissions := claude.DefaultPermissions()
			settings.Permissions = &permissions
		case model.ComponentTheme:
			if settings.Settings == nil {
				settings.Settings = make(map[string]any)
			}
			settings.Settings["theme"] = taktTheme
		case model.ComponentClaudeTheme:
			rendered := claude.TaktThemeArtifact()
			artifacts = append(artifacts, Artifact{Path: rendered.Path, Content: rendered.Content})
		default:
			// opencode-takt-logo is OpenCode-only.
		}
	}
	return artifacts
}

// codexComponentArtifacts applies the selected components to the Codex config
// request; every applicable Codex component merges into config.toml.
// Components scoped to other targets are skipped.
func codexComponentArtifacts(components []model.ComponentID, config *codex.ConfigRequest) {
	for _, component := range components {
		switch component {
		case model.ComponentContext7:
			config.Context7 = true
		case model.ComponentPermission:
			config.Permissions = true
		default:
			// theme, claude-theme, and opencode-takt-logo have no Codex
			// projection.
		}
	}
}

// openCodeComponentArtifacts applies the selected components to the OpenCode
// plan: config merges plus the standalone Takt logo plugin artifacts.
// Components scoped to other targets are skipped.
func openCodeComponentArtifacts(components []model.ComponentID, config *opencode.ConfigRequest) []Artifact {
	artifacts := make([]Artifact, 0, len(components))
	for _, component := range components {
		switch component {
		case model.ComponentContext7:
			config.Context7 = true
		case model.ComponentPermission:
			config.Permissions = true
		case model.ComponentTheme:
			config.Theme = opencode.TaktTheme
		case model.ComponentOpenCodeTaktLogo:
			for _, rendered := range []opencode.Artifact{
				opencode.TaktLogoPluginArtifact(),
				opencode.TaktLogoRegistrationArtifact(),
			} {
				artifacts = append(artifacts, Artifact{Path: rendered.Path, Content: rendered.Content})
			}
		default:
			// claude-theme is Claude-only.
		}
	}
	return artifacts
}
