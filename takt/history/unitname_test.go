// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history_test

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

// TestValidateUnitName proves the unit-name contract of PR-DAG-REP-8: one line
// within the cell bound, measured in display cells and not in bytes or runes.
func TestValidateUnitName(t *testing.T) {
	atBound := strings.Repeat("a", history.MaxUnitNameCells)
	wideAtBound := strings.Repeat("界", history.MaxUnitNameCells/2)
	cases := []struct {
		name   string
		unit   string
		accept bool
	}{
		{"short label", "architecture", true},
		{"exactly the bound", atBound, true},
		{"wide runes at the bound", wideAtBound, true},
		{"one cell over", atBound + "a", false},
		{"wide runes over the bound", wideAtBound + "界", false},
		{"line break", "two\nlines", false},
		{"escape sequence", "red\x1b[31m", false},
		{"tab", "a\tb", false},
		{"line separator", "a\u2028b", false},
		{"paragraph separator", "a\u2029b", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := history.ValidateUnitName(c.unit)
			if c.accept && err != nil {
				t.Fatalf("name %q refused: %v", c.unit, err)
			}
			if !c.accept && err == nil {
				t.Fatalf("name %q accepted", c.unit)
			}
		})
	}
}
