// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package setup

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/catalog"
)

// TestAllComponentsMatchesManifest pins that AllComponents mirrors the
// manifest-driven selectable order exactly.
func TestAllComponentsMatchesManifest(t *testing.T) {
	manifestOrder, err := catalog.SelectableOrder()
	if err != nil {
		t.Fatalf("SelectableOrder() error = %v", err)
	}
	got, err := AllComponents()
	if err != nil {
		t.Fatalf("AllComponents() error = %v", err)
	}
	if !slices.Equal(got, manifestOrder) {
		t.Fatalf("AllComponents() = %v, manifest order = %v", got, manifestOrder)
	}
}
