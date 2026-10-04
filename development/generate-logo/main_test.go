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
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateWritesEveryLogoDataset(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "source.png")
	writeSquarePNG(t, input, 200, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
	mono := filepath.Join(root, "mono", "logo.go")
	blocks := filepath.Join(root, "blocks", "logo.go")
	mark := filepath.Join(root, "image", "logo.png")
	installer := filepath.Join(root, "install.sh")
	if err := os.WriteFile(installer, []byte("before\n"+installerBegin+"\nold\n"+installerEnd+"\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generate(input, mono, blocks, mark, installer); err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	for path, variables := range map[string][]string{mono: {"generatedLogoBraille"}, blocks: {"generatedLogoQuadrants"}} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read generated file %s: %v", path, err)
		}
		for _, name := range variables {
			if !strings.Contains(string(content), "var "+name+" = [][][]logoSpan{") {
				t.Errorf("generated file %s does not declare %s", path, name)
			}
		}
	}
	if _, err := decodeInputImage(mark); err != nil {
		t.Errorf("generated image: %v", err)
	}
	embedded, err := os.ReadFile(installer)
	if err != nil {
		t.Fatal(err)
	}
	got := string(embedded)
	if !strings.HasPrefix(got, "before\n") || !strings.HasSuffix(got, "after\n") || strings.Contains(got, "\nold\n") || !strings.Contains(got, "print_logo() {") {
		t.Errorf("installer mark not embedded between the markers:\n%s", got)
	}
	if rows := strings.Count(got, "printf '%b\\n'"); rows != installerRows {
		t.Errorf("installer mark has %d rows, want %d", rows, installerRows)
	}
}

func TestDecodeInputImageDistinguishesMissingAndInvalidPNG(t *testing.T) {
	if _, err := decodeInputImage(filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Fatal("decodeInputImage(missing) returned no error")
	}
	invalid := filepath.Join(t.TempDir(), "invalid.png")
	if err := os.WriteFile(invalid, []byte("not a PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeInputImage(invalid); err == nil || !strings.Contains(err.Error(), "decode PNG") {
		t.Fatalf("decodeInputImage(invalid) error = %v, want PNG decode error", err)
	}
}

// The rings come from geometry, the interior from the artwork, and the
// result is cropped to the outer band.
func TestClassifyDrawsRingsAndKeepsInterior(t *testing.T) {
	const size = 1000
	source := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			source.SetNRGBA(x, y, color.NRGBA{R: 240, G: 238, B: 236, A: 255})
		}
	}
	art, err := classify(source)
	if err != nil {
		t.Fatal(err)
	}
	if art.width != art.height || art.width != 946 {
		t.Fatalf("classify() size = %dx%d, want 946x946", art.width, art.height)
	}
	center := art.width / 2
	at := func(fraction float64) ink { return art.at(center+int(fraction*size/2), center) }
	for _, check := range []struct {
		fraction float64
		want     ink
	}{
		{0, inkBody},
		{0.78, inkInnerRing},
		{0.86, inkBlack},
		{0.92, inkOuterBand},
	} {
		if got := at(check.fraction); got != check.want {
			t.Errorf("ink at radius %.2f = %d, want %d", check.fraction, got, check.want)
		}
	}
	if got := art.at(0, 0); got != inkClear {
		t.Errorf("corner ink = %d, want clear", got)
	}
	if _, err := classify(image.NewNRGBA(image.Rect(0, 0, 4, 3))); err == nil {
		t.Error("classify(non-square) returned no error")
	}
}

func TestNearestInkKeepsInteriorPalette(t *testing.T) {
	for _, check := range []struct {
		color rgb
		want  ink
	}{
		{rgb{0x10, 0x10, 0x10}, inkBlack},
		{rgb{0xf0, 0xf0, 0xf0}, inkBody},
		{rgb{0xb3, 0xbd, 0xc9}, inkChest},
	} {
		if got := nearestInk(check.color, interiorInks); got != check.want {
			t.Errorf("nearestInk(%v) = %d, want %d", check.color, got, check.want)
		}
	}
}

// A dark minority still wins its box, so thin dark features survive
// downsampling.
func TestDownsampleFavorsDarkFeatures(t *testing.T) {
	art := gridOf(3, []ink{
		inkBlack, inkBlack, inkBody,
		inkBlack, inkBody, inkBody,
		inkBlack, inkBody, inkBody,
	})
	if got := downsample(art, 1, 1).at(0, 0); got != inkBlack {
		t.Errorf("box with 4 dark of 9 = %d, want dark", got)
	}
	art.pixels[0] = inkBody
	if got := downsample(art, 1, 1).at(0, 0); got != inkBody {
		t.Errorf("box with 3 dark of 9 = %d, want body", got)
	}
}

func TestRenderHalfBlocksDrawsTwoPixelsPerCell(t *testing.T) {
	art := gridOf(4, []ink{
		inkClear, inkBody, inkBody, inkBody,
		inkClear, inkClear, inkBody, inkChest,
	})
	got := renderHalfBlocks(&grid{width: 4, height: 2, pixels: art.pixels[:8]})
	want := []span{
		{Text: " "},
		{Text: "▀█", Color: inkBody.hex()},
		{Text: "▀", Color: inkBody.hex(), Bg: inkChest.hex()},
	}
	if len(got) != 1 || !equalSpans(got[0], want) {
		t.Fatalf("renderHalfBlocks() = %#v, want %#v", got, want)
	}
}

// A cell keeps its two most common inks; a third folds into the closer one,
// and a clear pixel always becomes the background.
func TestRenderQuadrantsKeepsTwoInksPerCell(t *testing.T) {
	art := gridOf(4, []ink{
		inkBody, inkChest, inkBody, inkBody,
		inkBlack, inkBlack, inkBody, inkBlack,
	})
	got := renderQuadrants(art)
	// Left cell: body on top (the chest pixel folds into it). Right cell: three
	// body pixels over one black, so body is the background and the lone
	// black pixel the foreground.
	want := []span{
		{Text: "▀", Color: inkBody.hex(), Bg: inkBlack.hex()},
		{Text: "▗", Color: inkBlack.hex(), Bg: inkBody.hex()},
	}
	if len(got) != 1 || !equalSpans(got[0], want) {
		t.Fatalf("renderQuadrants() = %#v, want %#v", got, want)
	}

	art = gridOf(2, []ink{
		inkClear, inkBody,
		inkClear, inkClear,
	})
	want = []span{{Text: "▝", Color: inkBody.hex()}}
	if got := renderQuadrants(art); len(got) != 1 || !equalSpans(got[0], want) {
		t.Fatalf("renderQuadrants(clear cell) = %#v, want %#v", got, want)
	}
}

// Each 2×2 pattern maps to its own character, the empty and full ones
// included.
func TestQuadrantRunesCoverEveryPattern(t *testing.T) {
	if len(quadrantRunes) != 16 || quadrantRunes[0] != ' ' || quadrantRunes[15] != '█' {
		t.Fatalf("quadrantRunes = %q", string(quadrantRunes))
	}
	seen := map[rune]bool{}
	for _, r := range quadrantRunes {
		if seen[r] {
			t.Fatalf("quadrantRunes repeats %q", r)
		}
		seen[r] = true
	}
}

// The mark keeps its colors inside the outer band and nothing outside it, so
// a terminal drawing it shows a round logo instead of a square.
func TestMaskedMarkClearsBeyondTheOuterBand(t *testing.T) {
	const size = 1000
	fill := color.NRGBA{R: 40, G: 45, B: 85, A: 255}
	source := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			source.SetNRGBA(x, y, fill)
		}
	}
	mark := maskedMark(source, 100)
	if got := mark.Bounds().Size(); got != image.Pt(100, 100) {
		t.Fatalf("maskedMark() size = %v, want 100x100", got)
	}
	if got := mark.NRGBAAt(0, 0); got.A != 0 {
		t.Errorf("corner = %v, want transparent", got)
	}
	if got := mark.NRGBAAt(50, 50); got != fill {
		t.Errorf("center = %v, want %v", got, fill)
	}
	if edge := mark.NRGBAAt(0, 50); edge.A == 0 || edge.A == 255 || edge.R != fill.R {
		t.Errorf("edge = %v, want a partly covered pixel of the fill color", edge)
	}
}

func TestRenderBrailleDotsLitPixels(t *testing.T) {
	art := gridOf(2, []ink{
		inkBody, inkBlack,
		inkClear, inkInnerRing,
		inkClear, inkClear,
		inkChest, inkClear,
	})
	got := renderBraille(art)
	// Dots 1 (upper left), 5 (second row right) and 7 (lower left).
	if want := string(rune(brailleBase + 0x01 + 0x10 + 0x40)); len(got) != 1 || got[0][0].Text != want {
		t.Fatalf("renderBraille() = %#v, want %q", got, want)
	}
}

func TestWriteGeneratedRejectsInvalidGoAndOutputPath(t *testing.T) {
	root := t.TempDir()
	variants := [][][]span{{{{Text: "X"}}}}
	if err := writeGenerated(filepath.Join(root, "valid", "logo.go"), dataset{"not-a-valid-go-name", variants}); err == nil || !strings.Contains(err.Error(), "format generated Go") {
		t.Fatalf("writeGenerated(invalid identifier) error = %v, want formatting error", err)
	}
	blocked := filepath.Join(root, "file")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeGenerated(filepath.Join(blocked, "logo.go"), dataset{"generatedLogo", variants}); err == nil {
		t.Fatal("writeGenerated(path below a file) returned no error")
	}
}

func gridOf(width int, pixels []ink) *grid {
	return &grid{width: width, height: len(pixels) / width, pixels: pixels}
}

func equalSpans(got, want []span) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func writeSquarePNG(t *testing.T, path string, size int, fill color.NRGBA) {
	t.Helper()
	input := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			input.SetNRGBA(x, y, fill)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, input); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
