// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package runtime

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestChangedOverrideIDsIncludesChangedAndRemovedAssignments(t *testing.T) {
	before := map[string]model.ModelAssignment{
		"same":    {Model: "provider/model-a"},
		"changed": {Model: "provider/model-old"},
		"removed": {Model: "provider/model-gone"},
	}
	after := map[string]model.ModelAssignment{
		"same":    {Model: "provider/model-a"},
		"changed": {Model: "provider/model-new"},
		"added":   {Model: "provider/model-added"},
	}

	got := changedOverrideIDs(before, after)
	slices.Sort(got)
	want := []string{"added", "changed", "removed"}
	if !slices.Equal(got, want) {
		t.Fatalf("changedOverrideIDs() = %v, want %v", got, want)
	}
}
