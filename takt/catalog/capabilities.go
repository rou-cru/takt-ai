// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// Package catalog is the single declarative source of Takt's installable
// content: agent definitions, skill packages, shared files and the versioned
// install manifest. Loaders validate before any deployment can be planned.
package catalog

import (
	// Required blank import: the embed directive below depends on this package.
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/rou-cru/takt-ai/takt/model"
	"gopkg.in/yaml.v2"
)

//go:embed capabilities.yaml
var capabilitiesYAML []byte

// loadCapabilities shares one parsed manifest because the embedded content never changes at runtime.
var loadCapabilities = sync.OnceValues(func() (Capabilities, error) {
	return parseCapabilities(capabilitiesYAML)
})

// Capability describes one installable unit and its dependencies so installers can explain choices.
type Capability struct {
	ID         string   `yaml:"id"`
	Purpose    string   `yaml:"purpose"`
	Core       bool     `yaml:"core"`
	Selectable bool     `yaml:"selectable"`
	Deps       []string `yaml:"deps"`
}

// Capabilities is the versioned install manifest installers reconcile user selections against.
type Capabilities struct {
	Version    int          `yaml:"version"`
	Components []Capability `yaml:"components"`
}

// LoadCapabilities returns the shared parsed manifest; callers must not mutate the result.
func LoadCapabilities() (Capabilities, error) {
	return loadCapabilities()
}

// parseCapabilities rejects unknown fields and invalid manifests so broken builds fail early.
func parseCapabilities(data []byte) (Capabilities, error) {
	var manifest Capabilities
	if err := yaml.UnmarshalStrict(data, &manifest); err != nil {
		return Capabilities{}, fmt.Errorf("capabilities YAML: %w", err)
	}
	if err := validateCapabilities(manifest); err != nil {
		return Capabilities{}, err
	}
	return manifest, nil
}

// validateCapabilities rejects a manifest no installer may act on: an
// unsupported version, empty or duplicate component identities, missing
// purposes, unknown dependencies and dependency cycles.
func validateCapabilities(m Capabilities) error {
	if m.Version != 1 {
		return fmt.Errorf("capabilities: unsupported version %d", m.Version)
	}
	if len(m.Components) == 0 {
		return fmt.Errorf("capabilities: no components")
	}
	seen := make(map[string]struct{}, len(m.Components))
	for i, entry := range m.Components {
		if strings.TrimSpace(entry.ID) == "" {
			return fmt.Errorf("capabilities entry %d: missing id", i)
		}
		if _, exists := seen[entry.ID]; exists {
			return fmt.Errorf("capabilities entry %d: duplicate id %q", i, entry.ID)
		}
		seen[entry.ID] = struct{}{}
		if strings.TrimSpace(entry.Purpose) == "" {
			return fmt.Errorf("capabilities entry %q: missing purpose", entry.ID)
		}
	}
	return validateDependencyClosure(m)
}

// validateDependencyClosure rejects dependencies on components the manifest
// does not declare, and dependency cycles.
func validateDependencyClosure(m Capabilities) error {
	declared := make(map[string]bool, len(m.Components))
	deps := make(map[string][]string, len(m.Components))
	for _, entry := range m.Components {
		declared[entry.ID] = true
		deps[entry.ID] = entry.Deps
	}
	for _, entry := range m.Components {
		for _, dep := range entry.Deps {
			if !declared[dep] {
				return fmt.Errorf("capabilities entry %q depends on unknown component %q", entry.ID, dep)
			}
		}
	}
	const visiting, done = 1, 2
	state := make(map[string]int, len(m.Components))
	var visit func(id string, stack []string) error
	visit = func(id string, stack []string) error {
		switch state[id] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("capabilities: dependency cycle %s", strings.Join(append(stack, id), " -> "))
		}
		state[id] = visiting
		for _, dep := range deps[id] {
			if err := visit(dep, append(slices.Clone(stack), id)); err != nil {
				return err
			}
		}
		state[id] = done
		return nil
	}
	for _, entry := range m.Components {
		if err := visit(entry.ID, nil); err != nil {
			return err
		}
	}
	return nil
}

// SelectableOrder lists user-choosable components in manifest order so prompts stay stable.
func SelectableOrder() ([]model.ComponentID, error) {
	manifest, err := LoadCapabilities()
	if err != nil {
		return nil, err
	}
	order := make([]model.ComponentID, 0, len(manifest.Components))
	for _, entry := range manifest.Components {
		if !entry.Selectable {
			continue
		}
		order = append(order, model.ComponentID(entry.ID))
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("capabilities: no selectable components")
	}
	return order, nil
}

// ComponentPurpose explains a component in plain words, falling back to its ID when unknown.
func ComponentPurpose(id model.ComponentID) string {
	manifest, _ := LoadCapabilities()
	for _, entry := range manifest.Components {
		if model.ComponentID(entry.ID) == id {
			return entry.Purpose
		}
	}
	return string(id)
}

// Removal explains why a chosen capability was dropped so users can fix their selection.
type Removal struct {
	Component model.ComponentID
	Reason    string
}

// Reconcile drops selected capabilities whose dependencies are missing, without re-adding anything the user removed.
func Reconcile(m Capabilities, selected []model.ComponentID) ([]model.ComponentID, []Removal) {
	selectedSet := make(map[model.ComponentID]bool, len(selected))
	for _, c := range selected {
		selectedSet[c] = true
	}
	kept := make([]model.ComponentID, 0, len(selected))
	var removals []Removal
	for _, c := range selected {
		capability, ok := capByID(m, c)
		if !ok {
			kept = append(kept, c)
			continue
		}
		var missing []string
		for _, dep := range capability.Deps {
			if selectedSet[model.ComponentID(dep)] || isCore(m, model.ComponentID(dep)) {
				continue
			}
			missing = append(missing, dep)
		}
		if len(missing) > 0 {
			removals = append(removals, Removal{
				Component: c,
				Reason:    fmt.Sprintf("requires %s, which is not selected", strings.Join(missing, ", ")),
			})
			continue
		}
		kept = append(kept, c)
	}
	return kept, removals
}

// ReconcileSelection reconciles a selection against the embedded manifest for callers that hold no manifest.
func ReconcileSelection(selected []model.ComponentID) ([]model.ComponentID, []Removal, error) {
	m, err := LoadCapabilities()
	if err != nil {
		return nil, nil, err
	}
	kept, removals := Reconcile(m, selected)
	return kept, removals, nil
}

// capByID finds one capability by ID for dependency checks.
func capByID(m Capabilities, id model.ComponentID) (Capability, bool) {
	for _, c := range m.Components {
		if model.ComponentID(c.ID) == id {
			return c, true
		}
	}
	return Capability{}, false
}

// isCore reports whether an ID is mandatory so its dependents are never dropped.
func isCore(m Capabilities, id model.ComponentID) bool {
	c, ok := capByID(m, id)
	return ok && c.Core
}
