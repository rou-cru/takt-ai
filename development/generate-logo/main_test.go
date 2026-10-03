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
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"image"
	"testing"
)

func TestGenerateWritesBothLogoDatasets(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	chafa := filepath.Join(bin, "chafa")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Chafa version 1.18.3'; exit 0; fi\nprintf '\\033[38;2;1;2;3mX\\033[0m\\n'\n"
	if err := os.WriteFile(chafa, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	input := filepath.Join(root, "source.png")
	writeLogoInput(t, input)
	mono := filepath.Join(root, "mono", "logo.go")
	blocks := filepath.Join(root, "blocks", "logo.go")
	installer := filepath.Join(root, "install.sh")
	if err := os.WriteFile(installer, []byte("before\n"+installerBegin+"\nold\n"+installerEnd+"\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generate(input, mono, blocks, installer); err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	for path, variable := range map[string]string{mono: "generatedLogo", blocks: "generatedLogoBlocks"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read generated file %s: %v", path, err)
		}
		for _, name := range []string{variable, variable + "Compact"} {
			if !strings.Contains(string(content), "var "+name+" = [][]logoSpan{") || !strings.Contains(string(content), `Text: "X"`) {
				t.Errorf("generated file %s does not contain %s logo data: %s", path, name, content)
			}
		}
	}
	embedded, err := os.ReadFile(installer)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(embedded); !strings.HasPrefix(got, "before\n") || !strings.HasSuffix(got, "after\n") || strings.Contains(got, "\nold\n") ||
		!strings.Contains(got, "print_logo() {") || !strings.Contains(got, `\033[0;38;2;1;2;3mX\033[0m`) {
		t.Errorf("installer mark not embedded between the markers:\n%s", got)
	}
}

// Blue-to-indigo artwork moves onto the petrol hue; neutrals stay.
func TestRecolorToPetrolRotatesBluesOnly(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	indigo, white, black := color.NRGBA{R: 0x41, G: 0x52, B: 0x86, A: 255}, color.NRGBA{R: 240, G: 240, B: 240, A: 255}, color.NRGBA{A: 255}
	img.SetNRGBA(0, 0, indigo)
	img.SetNRGBA(1, 0, white)
	img.SetNRGBA(2, 0, black)
	recolorToPetrol(img)
	if hue, _, _ := toHSL(img.NRGBAAt(0, 0)); hue < petrolHue-1 || hue > petrolHue+1 {
		t.Errorf("indigo hue = %.1f, want petrol %.0f", hue, petrolHue)
	}
	_, _, before := toHSL(indigo)
	if _, _, after := toHSL(img.NRGBAAt(0, 0)); after < before-0.01 || after > before+0.01 {
		t.Errorf("lightness %.3f -> %.3f, want it kept", before, after)
	}
	if img.NRGBAAt(1, 0) != white || img.NRGBAAt(2, 0) != black {
		t.Error("neutral pixels were recolored")
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

func TestRequireChafaRejectsIncompatibleVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "chafa"), []byte("#!/bin/sh\necho 'Chafa version 9.9.9'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	if _, err := requireChafa(); err == nil || !strings.Contains(err.Error(), "got \"Chafa version 9.9.9\"") {
		t.Fatalf("requireChafa() error = %v, want incompatible version", err)
	}
}

func TestRenderChafaRejectsProcessAndOutputFailures(t *testing.T) {
	root := t.TempDir()
	chafa := filepath.Join(root, "chafa")
	for name, scenario := range map[string]struct{ script, want string }{
		"process":      {"#!/bin/sh\nexit 7\n", "run chafa"},
		"invalid utf8": {"#!/bin/sh\nprintf '\\377'\n", "invalid UTF-8"},
		"empty logo":   {"#!/bin/sh\nexit 0\n", "generated logo has no lines"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(chafa, []byte(scenario.script), 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := renderChafa(chafa, "source.png", "braille", true, fullSize); err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("renderChafa() error = %v, want %q", err, scenario.want)
			}
		})
	}
}

func TestWriteGeneratedRejectsInvalidGoAndOutputPath(t *testing.T) {
	root := t.TempDir()
	lines := [][]span{{{Text: "X"}}}
	if err := writeGenerated(filepath.Join(root, "valid", "logo.go"), dataset{"not-a-valid-go-name", lines}); err == nil || !strings.Contains(err.Error(), "format generated Go") {
		t.Fatalf("writeGenerated(invalid identifier) error = %v, want formatting error", err)
	}
	blocked := filepath.Join(root, "file")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeGenerated(filepath.Join(blocked, "logo.go"), dataset{"generatedLogo", lines}); err == nil {
		t.Fatal("writeGenerated(path below a file) returned no error")
	}
}

func writeLogoInput(t *testing.T, path string) {
	t.Helper()
	input := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			input.SetNRGBA(x, y, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
		}
	}
	input.SetNRGBA(2, 2, color.NRGBA{R: 20, G: 80, B: 140, A: 255})
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

// Only the inner ring survives: pixels past innerRingCrop become transparent.
func TestCropToInnerRingClearsTheOuterBand(t *testing.T) {
	const size = 100
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, A: 255})
		}
	}
	cropped := cropToInnerRing(img)
	if cropped.NRGBAAt(size/2, size/2).A == 0 {
		t.Error("the center was cleared")
	}
	if cropped.NRGBAAt(1, size/2).A != 0 || cropped.NRGBAAt(0, 0).A != 0 {
		t.Error("pixels beyond the inner ring were kept")
	}
}
