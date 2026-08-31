// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"image"
	"image/color"
	"testing"
)

func TestRemoveBorderWhitePreservesEnclosedWhite(t *testing.T) {
	input := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			input.SetNRGBA(x, y, color.NRGBA{R: 250, G: 249, B: 248, A: 255})
		}
	}
	for _, point := range [...]image.Point{image.Pt(1, 1), image.Pt(2, 1), image.Pt(3, 1), image.Pt(1, 2), image.Pt(3, 2), image.Pt(1, 3), image.Pt(2, 3), image.Pt(3, 3)} {
		input.SetNRGBA(point.X, point.Y, color.NRGBA{R: 20, G: 20, B: 20, A: 255})
	}
	output := removeBorderWhite(input)
	if got := output.NRGBAAt(0, 0).A; got != 0 {
		t.Fatalf("border white alpha = %d, want 0", got)
	}
	if got := output.NRGBAAt(2, 2); got.A != 255 || got.R != 250 {
		t.Fatalf("enclosed white = %#v, want preserved", got)
	}
	if got := output.NRGBAAt(1, 1).A; got != 255 {
		t.Fatalf("non-white boundary alpha = %d, want 255", got)
	}
}

func TestParseANSIProducesForegroundOnlySpans(t *testing.T) {
	lines, err := parseANSI([]byte("\x1b[38;2;1;2;3;48;2;4;5;6m⣿⣀\x1b[0m\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0]) != 1 {
		t.Fatalf("spans = %#v", lines)
	}
	if got := lines[0][0]; got.Text != "⣿⣀" || got.Color != "#010203" {
		t.Fatalf("span = %#v", got)
	}
}

func TestTrimBlankRows(t *testing.T) {
	lines := trimBlankRows([][]span{{{Text: "  "}}, {{Text: "⣿", Color: "#010203"}}, {{Text: " "}}})
	if len(lines) != 1 || lines[0][0].Text != "⣿" {
		t.Fatalf("trimBlankRows() = %#v", lines)
	}
}
