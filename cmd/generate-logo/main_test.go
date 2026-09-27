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

// TestRemoveBorderPaddingClearsWhiteAndBlackBorders verifies border cleanup
// clears both padding colors while enclosed pixels survive.
func TestRemoveBorderPaddingClearsWhiteAndBlackBorders(t *testing.T) {
	for _, padding := range []color.NRGBA{
		{R: 250, G: 249, B: 248, A: 255},
		{R: 10, G: 10, B: 10, A: 255},
	} {
		input := image.NewNRGBA(image.Rect(0, 0, 5, 5))
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				input.SetNRGBA(x, y, padding)
			}
		}
		art := color.NRGBA{R: 30, G: 90, B: 130, A: 255}
		for _, point := range [...]image.Point{image.Pt(1, 1), image.Pt(2, 1), image.Pt(3, 1), image.Pt(1, 2), image.Pt(3, 2), image.Pt(1, 3), image.Pt(2, 3), image.Pt(3, 3)} {
			input.SetNRGBA(point.X, point.Y, art)
		}
		output := removeBorderPadding(input)
		if got := output.NRGBAAt(0, 0).A; got != 0 {
			t.Fatalf("padding %v: border alpha = %d, want 0", padding, got)
		}
		if got := output.NRGBAAt(1, 1); got.A != 255 || got.R != art.R {
			t.Fatalf("padding %v: artwork ring eaten: %#v", padding, got)
		}
		if output.NRGBAAt(2, 2).A != 255 {
			t.Fatalf("padding %v: enclosed padding-colored pixel destroyed", padding)
		}
	}
}

// TestParseANSIProducesForegroundAndBackgroundSpans verifies Chafa output
// becomes clean spans carrying both colors.
func TestParseANSIProducesForegroundAndBackgroundSpans(t *testing.T) {
	lines, err := parseANSI([]byte("\x1b[38;2;1;2;3;48;2;4;5;6m▀▄\x1b[0m\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0]) != 1 {
		t.Fatalf("spans = %#v", lines)
	}
	if got := lines[0][0]; got.Text != "▀▄" || got.Color != "#010203" || got.Bg != "#040506" {
		t.Fatalf("span = %#v", got)
	}
}

// TestParseANSIKeepsBackgroundAcrossForegroundChanges verifies the background
// persists through foreground-only updates and clears on reset.
func TestParseANSIKeepsBackgroundAcrossForegroundChanges(t *testing.T) {
	lines, err := parseANSI([]byte("\x1b[38;2;1;2;3;48;2;4;5;6m▀\x1b[38;2;7;8;9m▄\x1b[0m█\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0]) != 3 {
		t.Fatalf("spans = %#v", lines)
	}
	want := []span{
		{Text: "▀", Color: "#010203", Bg: "#040506"},
		{Text: "▄", Color: "#070809", Bg: "#040506"},
		{Text: "█"},
	}
	for i, w := range want {
		if got := lines[0][i]; got != w {
			t.Fatalf("span %d = %#v, want %#v", i, got, w)
		}
	}
}

// TestTrimBlankRows verifies padding rows are removed without losing logo content.
func TestTrimBlankRows(t *testing.T) {
	lines := trimBlankRows([][]span{{{Text: "  "}}, {{Text: "▀", Color: "#010203", Bg: "#040506"}}, {{Text: " "}}})
	if len(lines) != 1 || lines[0][0].Text != "▀" {
		t.Fatalf("trimBlankRows() = %#v", lines)
	}
}

// TestParseANSIRejectsBrokenEscapes verifies malformed escapes fail loudly
// instead of leaking bytes into spans.
func TestParseANSIRejectsBrokenEscapes(t *testing.T) {
	for input, want := range map[string]string{
		"\x1bX":       "unsupported escape sequence",
		"\x1b":        "unsupported escape sequence",
		"\x1b[38;2;1": "unterminated escape sequence",
	} {
		if _, err := parseANSI([]byte(input)); err == nil || err.Error() != want {
			t.Errorf("parseANSI(%q) error = %v, want %q", input, err, want)
		}
	}
}
