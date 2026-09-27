// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package catalog

import (
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestCapabilitiesManifestLoads(t *testing.T) {
	manifest, err := LoadCapabilities()
	if err != nil {
		t.Fatalf("LoadCapabilities() error = %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("Version = %d, want 1", manifest.Version)
	}
}

func TestCapabilitiesReservedCore(t *testing.T) {
	manifest, err := LoadCapabilities()
	if err != nil {
		t.Fatalf("LoadCapabilities() error = %v", err)
	}
	reserved := map[string]bool{}
	for _, entry := range manifest.Components {
		if entry.Selectable {
			if entry.Core {
				t.Errorf("selectable entry %q must not be core", entry.ID)
			}
			continue
		}
		reserved[entry.ID] = entry.Core
		if !entry.Core {
			t.Errorf("reserved entry %q must be core", entry.ID)
		}
		if !strings.Contains(entry.Note, "non-omittable") {
			t.Errorf("reserved entry %q note = %q, want non-omittable marker", entry.ID, entry.Note)
		}
		if strings.TrimSpace(entry.Purpose) == "" {
			t.Errorf("reserved entry %q has no purpose", entry.ID)
		}
		if len(entry.Deps) != 0 {
			t.Errorf("reserved entry %q deps = %v, want empty", entry.ID, entry.Deps)
		}
	}
	for _, id := range []string{"engram", "skills", "specialists"} {
		if !reserved[id] {
			t.Errorf("missing reserved core entry %q", id)
		}
	}
}

func TestParseCapabilitiesRejectsBadInput(t *testing.T) {
	for name, data := range map[string]string{
		"corrupt":             "{not yaml",
		"unsupported version": "version: 99\ncomponents:\n  - id: a\n    purpose: p\n    selectable: true\n",
		"empty":               "version: 1\ncomponents: []\n",
		"missing purpose":     "version: 1\ncomponents:\n  - id: a\n    selectable: true\n",
		"duplicate id":        "version: 1\ncomponents:\n  - id: a\n    purpose: p\n    selectable: true\n  - id: a\n    purpose: q\n    selectable: true\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCapabilities([]byte(data)); err == nil {
				t.Fatal("parseCapabilities() error = nil, want error")
			}
		})
	}
}

func TestReconcile(t *testing.T) {
	m := Capabilities{Components: []Capability{
		{ID: "core", Core: true, Selectable: false},
		{ID: "a", Selectable: true, Deps: []string{"core"}},
		{ID: "b", Selectable: true, Deps: []string{"a"}},
		{ID: "c", Selectable: true},
	}}

	kept, removals := Reconcile(m, []model.ComponentID{"a", "b", "c"})
	if !slices.Equal(kept, []model.ComponentID{"a", "b", "c"}) || len(removals) != 0 {
		t.Fatalf("kept=%v removals=%v, want all three kept", kept, removals)
	}

	kept, removals = Reconcile(m, []model.ComponentID{"b", "c"})
	if !slices.Equal(kept, []model.ComponentID{"c"}) {
		t.Fatalf("kept = %v, want [c] (b needs a)", kept)
	}
	if len(removals) != 1 || removals[0].Component != "b" || !strings.Contains(removals[0].Reason, "a") {
		t.Fatalf("removals = %v, want b removed naming a", removals)
	}

	kept, removals = Reconcile(m, []model.ComponentID{"a"})
	if !slices.Equal(kept, []model.ComponentID{"a"}) || len(removals) != 0 {
		t.Fatalf("kept=%v removals=%v, want [a] kept (core satisfies dep)", kept, removals)
	}

	kept, removals = Reconcile(m, []model.ComponentID{"unknown"})
	if !slices.Equal(kept, []model.ComponentID{"unknown"}) || len(removals) != 0 {
		t.Fatalf("kept=%v removals=%v, want unknown passed through", kept, removals)
	}
}
