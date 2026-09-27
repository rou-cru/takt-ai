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

// Package main rebuilds logo data so TUI branding stays matched to the canonical PNG.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// chafaVersion pins the renderer so logo output stays identical everywhere.
	chafaVersion = "1.18.2"
	// whiteFloor marks near-white pixels so border cleanup ignores shading.
	whiteFloor = 245
	// borderBlackLuma is the luminance ceiling for border padding: the source
	// artwork's dark background clears to transparent so the logo blends with
	// the terminal canvas instead of drawing its own rectangle.
	borderBlackLuma = 24.0
	// rgbaShift converts the 16-bit color channels returned by RGBA to 8-bit channels.
	rgbaShift = 8
	// redLumaWeight, greenLumaWeight, and blueLumaWeight implement the sRGB luma approximation.
	redLumaWeight, greenLumaWeight, blueLumaWeight = 0.2126, 0.7152, 0.0722
	// generatedDirectoryMode and generatedFileMode keep generated branding traversable and readable.
	generatedDirectoryMode os.FileMode = 0o755
	generatedFileMode      os.FileMode = 0o644
	// sgrReset and sgr color selectors are the ANSI SGR values emitted by Chafa.
	sgrReset, sgrDefaultForeground, sgrDefaultBackground = 0, 39, 49
	sgrForegroundTrueColor, sgrBackgroundTrueColor       = 38, 48
	// sgrTrueColorParameters is the number of parameters consumed by a truecolor selector.
	sgrTrueColorParameters = 4
)

// span holds one colored text run so logo rows stay compact.
type span struct {
	Text  string
	Color string
	Bg    string
}

// main parses flags so missing input fails before bad output is written.
func main() {
	input := flag.String("input", "", "input PNG path")
	output := flag.String("output", "takt/tui/styles/logo_generated.go", "generated Go file path (Braille mono dataset)")
	blocksOutput := flag.String("blocks-output", "takt/tui/styles/logo_blocks_generated.go", "generated Go file path (half-block color dataset)")
	flag.Parse()
	if *input == "" {
		fatal(errors.New("-input is required"))
	}
	if err := generate(*input, *output, *blocksOutput); err != nil {
		fatal(err)
	}
}

// fatal reports generation errors so failures surface before bad files are used.
func fatal(err error) {
	fmt.Fprintln(os.Stderr, "generate-logo:", err)
	os.Exit(1)
}

// generate rebuilds logo data so checked-in branding matches the source image.
func generate(input, output, blocksOutput string) error {
	chafaPath, err := requireChafa()
	if err != nil {
		return err
	}
	inputImage, err := decodeInputImage(input)
	if err != nil {
		return err
	}
	temporaryName, err := writeTemporaryImage(inputImage)
	if err != nil {
		return err
	}
	defer removeTemporaryImage(temporaryName)
	// Height capped at 20 rows regardless of source aspect ratio: the welcome
	// screen only shows the logo when terminal height covers rows + tagline +
	// menu, so a square/portrait source must not render taller than a
	// landscape one did. Two datasets ship from one source: Braille art whose
	// shape survives mono mode, and half-block art where every cell carries
	// foreground and background truecolor.
	brailleLines, err := renderChafa(chafaPath, temporaryName, "braille", true)
	if err != nil {
		return err
	}
	blockLines, err := renderChafa(chafaPath, temporaryName, "block", false)
	if err != nil {
		return err
	}
	if err := writeGenerated(output, brailleLines, "generatedLogo"); err != nil {
		return err
	}
	if err := writeGenerated(blocksOutput, blockLines, "generatedLogoBlocks"); err != nil {
		return err
	}
	return nil
}

func decodeInputImage(input string) (image.Image, error) {
	file, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "warning: closing input file: %v\n", cerr)
		}
	}()
	decoded, err := png.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode PNG: %w", err)
	}
	return decoded, nil
}

func writeTemporaryImage(source image.Image) (string, error) {
	temporary, err := os.CreateTemp("", "takt-logo-*.png")
	if err != nil {
		return "", err
	}
	name := temporary.Name()
	if err := png.Encode(temporary, removeBorderPadding(source)); err != nil {
		if cerr := temporary.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "warning: closing temp file: %v\n", cerr)
		}
		removeTemporaryImage(name)
		return "", fmt.Errorf("encode processed PNG: %w", err)
	}
	if err := temporary.Close(); err != nil {
		removeTemporaryImage(name)
		return "", err
	}
	return name, nil
}

func removeTemporaryImage(name string) {
	if err := os.Remove(name); err != nil {
		fmt.Fprintf(os.Stderr, "warning: removing temp file: %v\n", err)
	}
}

// renderChafa runs Chafa with one symbol set and returns validated, trimmed
// logo lines so both datasets share the same pipeline.
func renderChafa(chafaPath, source, symbols string, fgOnly bool) ([][]span, error) {
	args := []string{"--format=symbols", "--symbols=" + symbols, "--colors=full"}
	if fgOnly {
		args = append(args, "--fg-only")
	}
	args = append(args, "--size=48x20", source)
	ansi, err := exec.Command(chafaPath, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("run chafa: %w", err)
	}
	lines, err := parseANSI(ansi)
	if err != nil {
		return nil, fmt.Errorf("parse chafa output: %w", err)
	}
	lines = trimBlankRows(lines)
	if err := validateLogo(lines); err != nil {
		return nil, err
	}
	return lines, nil
}

// trimBlankRows removes padding so the logo keeps a stable size in the TUI.
func trimBlankRows(lines [][]span) [][]span {
	isBlank := func(line []span) bool {
		for _, span := range line {
			if strings.TrimSpace(span.Text) != "" {
				return false
			}
		}
		return true
	}
	start := 0
	for start < len(lines) && isBlank(lines[start]) {
		start++
	}
	end := len(lines)
	for end > start && isBlank(lines[end-1]) {
		end--
	}
	return lines[start:end]
}

// requireChafa checks the renderer so bad versions never produce drifting logos.
func requireChafa() (string, error) {
	path, err := exec.LookPath("chafa")
	if err != nil {
		return "", errors.New("chafa 1.18.2 is required to regenerate the logo; install it outside the release binary")
	}
	output, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "", errors.New("chafa 1.18.2 is required to regenerate the logo; install it outside the release binary")
	}
	if !strings.Contains(string(output), "Chafa version "+chafaVersion) {
		return "", fmt.Errorf("chafa %s is required, got %q", chafaVersion, strings.TrimSpace(string(output)))
	}
	return path, nil
}

// removeBorderPadding clears edge padding — near-white or near-black pixels
// connected to the border — so the logo blends with the terminal canvas.
// Artwork colors between the two floors stop the flood.
func removeBorderPadding(source image.Image) *image.NRGBA {
	bounds := source.Bounds()
	result := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			result.Set(x, y, source.At(x, y))
		}
	}
	seen := make(map[image.Point]bool)
	queue := make([]image.Point, 0, 2*bounds.Dx()+2*bounds.Dy())
	add := func(point image.Point) {
		if !seen[point] && isBorderPadding(result.At(point.X, point.Y)) {
			seen[point] = true
			queue = append(queue, point)
		}
	}
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		add(image.Pt(x, bounds.Min.Y))
		add(image.Pt(x, bounds.Max.Y-1))
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		add(image.Pt(bounds.Min.X, y))
		add(image.Pt(bounds.Max.X-1, y))
	}
	for len(queue) > 0 {
		point := queue[0]
		queue = queue[1:]
		result.SetNRGBA(point.X, point.Y, color.NRGBA{})
		for _, neighbor := range [...]image.Point{image.Pt(point.X-1, point.Y), image.Pt(point.X+1, point.Y), image.Pt(point.X, point.Y-1), image.Pt(point.X, point.Y+1)} {
			if neighbor.In(bounds) {
				add(neighbor)
			}
		}
	}
	return result
}

// isBorderPadding spots padding pixels — transparent, near-white, or
// near-black — so the flood never eats artwork.
func isBorderPadding(c color.Color) bool {
	r, g, b, a := c.RGBA()
	if a == 0 {
		return true
	}
	r8, g8, b8 := uint8(r>>rgbaShift), uint8(g>>rgbaShift), uint8(b>>rgbaShift)
	if r8 >= whiteFloor && g8 >= whiteFloor && b8 >= whiteFloor {
		return true
	}
	luma := redLumaWeight*float64(r8) + greenLumaWeight*float64(g8) + blueLumaWeight*float64(b8)
	return luma <= borderBlackLuma
}

// splitCSI splits the escape sequence at the start of input into its
// parameters and final byte, and returns the input after it.
func splitCSI(input []byte) (params string, final byte, rest []byte, err error) {
	if len(input) < 2 || input[1] != '[' {
		return "", 0, nil, errors.New("unsupported escape sequence")
	}
	end := 2
	for end < len(input) && (input[end] < 0x40 || input[end] > 0x7e) {
		end++
	}
	if end == len(input) {
		return "", 0, nil, errors.New("unterminated escape sequence")
	}
	return string(input[2:end]), input[end], input[end+1:], nil
}

// parseANSI reads Chafa output so later steps work with plain spans.
func parseANSI(input []byte) ([][]span, error) {
	var lines [][]span
	var line []span
	foreground := ""
	background := ""
	for len(input) > 0 {
		if input[0] == '\x1b' {
			var err error
			input, foreground, background, err = parseANSIEscape(input, foreground, background)
			if err != nil {
				return nil, err
			}
			continue
		}
		r, size := utf8.DecodeRune(input)
		if r == utf8.RuneError && size == 1 {
			return nil, errors.New("invalid UTF-8 in Chafa output")
		}
		input = input[size:]
		lines, line = appendRune(lines, line, r, foreground, background)
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}
	return lines, nil
}

func parseANSIEscape(input []byte, foreground, background string) ([]byte, string, string, error) {
	params, final, rest, err := splitCSI(input)
	if err != nil {
		return nil, "", "", err
	}
	if final != 'm' {
		return rest, foreground, background, nil
	}
	foreground, background, err = applySGR(params, foreground, background)
	return rest, foreground, background, err
}

func appendRune(lines [][]span, line []span, r rune, foreground, background string) ([][]span, []span) {
	switch r {
	case '\r':
	case '\n':
		lines = append(lines, line)
		line = nil
	default:
		line = appendSpan(line, string(r), foreground, background)
	}
	return lines, line
}

// applySGR tracks color codes so text keeps the right colors after escapes.
// Both states persist across escapes until an explicit reset, matching how
// Chafa emits foreground-only updates over a standing background.
func applySGR(sequence, foreground, background string) (string, string, error) {
	if sequence == "" {
		return "", "", nil
	}
	parts := strings.Split(sequence, ";")
	for i := 0; i < len(parts); i++ {
		code, err := strconv.Atoi(parts[i])
		if err != nil {
			return "", "", fmt.Errorf("invalid SGR code %q", parts[i])
		}
		var consumed int
		foreground, background, consumed, err = applySGRCode(parts, i, code, foreground, background)
		if err != nil {
			return "", "", err
		}
		i += consumed
	}
	return foreground, background, nil
}

func applySGRCode(parts []string, i, code int, foreground, background string) (string, string, int, error) {
	switch code {
	case sgrReset:
		return "", "", 0, nil
	case sgrDefaultForeground:
		return "", background, 0, nil
	case sgrDefaultBackground:
		return foreground, "", 0, nil
	case sgrForegroundTrueColor:
		color, err := sgrTrueColor(parts, i)
		if err != nil {
			return "", "", 0, fmt.Errorf("foreground: %w", err)
		}
		return color, background, sgrTrueColorParameters, nil
	case sgrBackgroundTrueColor:
		color, err := sgrTrueColor(parts, i)
		if err != nil {
			return "", "", 0, fmt.Errorf("background: %w", err)
		}
		return foreground, color, sgrTrueColorParameters, nil
	default:
		return foreground, background, 0, nil
	}
}

// sgrTrueColor parses the "2;r;g;b" payload following a 38/48 SGR code.
func sgrTrueColor(parts []string, i int) (string, error) {
	if i+4 >= len(parts) || parts[i+1] != "2" {
		return "", fmt.Errorf("expected truecolor SGR in %q", strings.Join(parts, ";"))
	}
	r, g, b, err := colorComponents(parts, i)
	if err != nil {
		return "", err
	}
	if r < 0 || r > 255 || g < 0 || g > 255 || b < 0 || b > 255 {
		return "", errors.New("truecolor component out of range")
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, b), nil
}

func colorComponents(parts []string, i int) (int, int, int, error) {
	values := [3]int{}
	for offset := range values {
		value, err := strconv.Atoi(parts[i+2+offset])
		if err != nil {
			return 0, 0, 0, err
		}
		values[offset] = value
	}
	return values[0], values[1], values[2], nil
}

// appendSpan merges same-color text so generated files stay small.
func appendSpan(line []span, text, color, bg string) []span {
	if len(line) > 0 && line[len(line)-1].Color == color && line[len(line)-1].Bg == bg {
		line[len(line)-1].Text += text
		return line
	}
	return append(line, span{Text: text, Color: color, Bg: bg})
}

// validateLogo rejects bad logos so broken branding never reaches the TUI.
func validateLogo(lines [][]span) error {
	for _, line := range lines {
		for _, span := range line {
			if span.Text == "" || strings.ContainsRune(span.Text, '\x1b') {
				return errors.New("generated logo contains an invalid text span")
			}
			if err := validateColor(span.Color, "foreground"); err != nil {
				return err
			}
			if err := validateColor(span.Bg, "background"); err != nil {
				return err
			}
		}
	}
	if len(lines) == 0 {
		return errors.New("generated logo has no lines")
	}
	return nil
}

func validateColor(color, name string) error {
	if color != "" && (len(color) != 7 || color[0] != '#') {
		return fmt.Errorf("invalid %s color %q", name, color)
	}
	return nil
}

// writeGenerated saves formatted Go so checked-in output never drifts from
// gofmt. The logoSpan type is hand-written in logo.go; generated files carry
// only data.
func writeGenerated(output string, lines [][]span, varName string) error {
	if err := os.MkdirAll(filepath.Dir(output), generatedDirectoryMode); err != nil {
		return err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by cmd/generate-logo; DO NOT EDIT.\n\npackage styles\n\nvar %s = [][]logoSpan{\n", varName)
	for _, line := range lines {
		b.WriteString("\t{")
		for _, span := range line {
			fmt.Fprintf(&b, "{Text: %q, Color: %q, Bg: %q},", span.Text, span.Color, span.Bg)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	// Emit gofmt-clean output so the checked-in file never drifts from the
	// formatter's expectation.
	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return fmt.Errorf("format generated Go: %w", err)
	}
	return os.WriteFile(output, formatted, generatedFileMode)
}
