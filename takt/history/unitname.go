// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// MaxUnitNameCells bounds a unit name's display width (PR-DAG-REP-8). The DAG
// view sizes every box to it, so a name is never cut to fit.
const MaxUnitNameCells = 26

// ValidateUnitName is the one contract a unit name keeps, in a plan declaration
// and in the admission of work without a plan (PR-DAG-REP-8): a single line of
// at most MaxUnitNameCells display cells with no control characters or Unicode
// line and paragraph separators. The refusal names the rule and how to satisfy it.
func ValidateUnitName(name string) error {
	breaks := func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp) }
	if strings.IndexFunc(name, breaks) >= 0 {
		return fmt.Errorf("unit name %q has a control character or line break; a unit name is one line of at most %d cells: rename it as a short label of the work", name, MaxUnitNameCells)
	}
	if cells := ansi.StringWidth(name); cells > MaxUnitNameCells {
		return fmt.Errorf("unit name %q is %d cells wide; a unit name is one line of at most %d cells: rename it as a short label of the work", name, cells, MaxUnitNameCells)
	}
	return nil
}
