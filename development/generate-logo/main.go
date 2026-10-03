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
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// chafaVersion pins the renderer so logo output stays identical everywhere.
	chafaVersion = "1.18.3"
	// whiteFloor marks near-white pixels so border cleanup ignores shading.
	whiteFloor = 245
	// borderBlackLuma is the luminance ceiling for border padding: the source
	// artwork's dark background clears to transparent so the logo blends with
	// the terminal canvas instead of drawing its own rectangle.
	borderBlackLuma = 24.0
	// innerRingCrop is the radius, as a fraction of half the image's shorter
	// side, inside which artwork is kept. The canonical PNG has a dark outer
	// band (luma ~46), a black gap, then the bright inner ring; measured on
	// both axes the gap sits at ~0.86. Cutting there keeps one crisp ring: at
	// terminal resolution the outer band only renders as a ragged fringe.
	innerRingCrop = 0.86
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
	// petrolHue is the hue of the brand's petrol family (P400–P600, ~188–193°).
	// Blue-to-indigo artwork (blueHueMin..blueHueMax, at least
	// recolorMinSaturation) is rotated onto it, keeping lightness and
	// saturation, so the terminal mark speaks the brand's single blue.
	petrolHue, blueHueMin, blueHueMax = 190.0, 200.0, 280.0
	recolorMinSaturation              = 0.12
	// fullSize, compactSize and installerSize are the Chafa boxes of the
	// datasets: the home logo, its variant for short terminals, and the mark
	// the installer prints.
	fullSize, compactSize, installerSize = "48x20", "24x10", "32x14"
	// installerBegin and installerEnd delimit the generated mark in install.sh.
	installerBegin = "# BEGIN GENERATED LOGO (development/generate-logo; DO NOT EDIT)"
	installerEnd   = "# END GENERATED LOGO"
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
	installer := flag.String("installer", "", "install.sh to embed the installer mark into (optional)")
	flag.Parse()
	if *input == "" {
		fatal(errors.New("-input is required"))
	}
	if err := generate(*input, *output, *blocksOutput, *installer); err != nil {
		fatal(err)
	}
}

// fatal reports generation errors so failures surface before bad files are used.
func fatal(err error) {
	fmt.Fprintln(os.Stderr, "generate-logo:", err)
	os.Exit(1)
}

// generate rebuilds logo data so checked-in branding matches the source image.
// Each Go file holds a full and a compact dataset: Braille art whose shape
// survives mono mode, and half-block art where every cell carries foreground
// and background truecolor. installer, when set, receives a small half-block
// mark between its generated-logo markers.
func generate(input, output, blocksOutput, installer string) error {
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
	render := func(symbols string, fgOnly bool, size string) ([][]span, error) {
		return renderChafa(chafaPath, temporaryName, symbols, fgOnly, size)
	}
	files := []struct {
		output, name, symbols string
		fgOnly                bool
	}{
		{output, "generatedLogo", "braille", true},
		{blocksOutput, "generatedLogoBlocks", "block", false},
	}
	for _, file := range files {
		full, err := render(file.symbols, file.fgOnly, fullSize)
		if err != nil {
			return err
		}
		compact, err := render(file.symbols, file.fgOnly, compactSize)
		if err != nil {
			return err
		}
		if err := writeGenerated(file.output, dataset{file.name, full}, dataset{file.name + "Compact", compact}); err != nil {
			return err
		}
	}
	if installer == "" {
		return nil
	}
	mark, err := render("block", false, installerSize)
	if err != nil {
		return err
	}
	return embedInstallerMark(installer, mark)
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
	if err := png.Encode(temporary, recolorToPetrol(removeBorderPadding(cropToInnerRing(source)))); err != nil {
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
func renderChafa(chafaPath, source, symbols string, fgOnly bool, size string) ([][]span, error) {
	args := []string{"--format=symbols", "--symbols=" + symbols, "--colors=full"}
	if fgOnly {
		args = append(args, "--fg-only")
	}
	args = append(args, "--size="+size, source)
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
		return "", fmt.Errorf("chafa %s is required to regenerate the logo; install it outside the release binary", chafaVersion)
	}
	output, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("chafa %s is required to regenerate the logo; install it outside the release binary", chafaVersion)
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

// cropToInnerRing clears everything outside the inner ring (innerRingCrop) so
// the outer band never reaches the terminal.
func cropToInnerRing(source image.Image) *image.NRGBA {
	bounds := source.Bounds()
	result := image.NewNRGBA(bounds)
	centerX := float64(bounds.Min.X+bounds.Max.X) / 2
	centerY := float64(bounds.Min.Y+bounds.Max.Y) / 2
	radius := innerRingCrop * float64(min(bounds.Dx(), bounds.Dy())) / 2
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if math.Hypot(float64(x)+0.5-centerX, float64(y)+0.5-centerY) <= radius {
				result.Set(x, y, source.At(x, y))
			}
		}
	}
	return result
}

// recolorToPetrol rotates the artwork's blue-to-indigo tones onto the petrol
// hue, keeping lightness and saturation; neutrals (the white body, the dark
// interior) and transparent padding are untouched.
func recolorToPetrol(source *image.NRGBA) *image.NRGBA {
	bounds := source.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := source.NRGBAAt(x, y)
			if pixel.A == 0 {
				continue
			}
			hue, saturation, lightness := toHSL(pixel)
			if saturation < recolorMinSaturation || hue < blueHueMin || hue > blueHueMax {
				continue
			}
			r, g, b := fromHSL(petrolHue, saturation, lightness)
			source.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: pixel.A})
		}
	}
	return source
}

// toHSL converts an 8-bit color to hue (degrees), saturation and lightness.
func toHSL(c color.NRGBA) (float64, float64, float64) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	high, low := max(r, g, b), min(r, g, b)
	lightness := (high + low) / 2
	if high == low {
		return 0, 0, lightness
	}
	delta := high - low
	saturation := delta / (1 - math.Abs(2*lightness-1))
	var hue float64
	switch high {
	case r:
		hue = math.Mod((g-b)/delta, 6)
	case g:
		hue = (b-r)/delta + 2
	default:
		hue = (r-g)/delta + 4
	}
	hue *= 60
	if hue < 0 {
		hue += 360
	}
	return hue, saturation, lightness
}

// fromHSL converts hue (degrees), saturation and lightness to 8-bit RGB.
func fromHSL(hue, saturation, lightness float64) (uint8, uint8, uint8) {
	chroma := (1 - math.Abs(2*lightness-1)) * saturation
	x := chroma * (1 - math.Abs(math.Mod(hue/60, 2)-1))
	offset := lightness - chroma/2
	var r, g, b float64
	switch {
	case hue < 60:
		r, g, b = chroma, x, 0
	case hue < 120:
		r, g, b = x, chroma, 0
	case hue < 180:
		r, g, b = 0, chroma, x
	case hue < 240:
		r, g, b = 0, x, chroma
	case hue < 300:
		r, g, b = x, 0, chroma
	default:
		r, g, b = chroma, 0, x
	}
	channel := func(v float64) uint8 { return uint8(math.Round(min(1, max(0, v+offset)) * 255)) }
	return channel(r), channel(g), channel(b)
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

// dataset is one generated logo variable.
type dataset struct {
	name  string
	lines [][]span
}

// writeGenerated saves formatted Go so checked-in output never drifts from
// gofmt. The logoSpan type is hand-written in logo.go; generated files carry
// only data.
func writeGenerated(output string, datasets ...dataset) error {
	if err := os.MkdirAll(filepath.Dir(output), generatedDirectoryMode); err != nil {
		return err
	}
	var b bytes.Buffer
	b.WriteString("// Code generated by development/generate-logo; DO NOT EDIT.\n\npackage styles\n")
	for _, data := range datasets {
		fmt.Fprintf(&b, "\nvar %s = [][]logoSpan{\n", data.name)
		for _, line := range data.lines {
			b.WriteString("\t{")
			for _, span := range line {
				fmt.Fprintf(&b, "{Text: %q, Color: %q, Bg: %q},", span.Text, span.Color, span.Bg)
			}
			b.WriteString("},\n")
		}
		b.WriteString("}\n")
	}
	// Emit gofmt-clean output so the checked-in file never drifts from the
	// formatter's expectation.
	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return fmt.Errorf("format generated Go: %w", err)
	}
	return os.WriteFile(output, formatted, generatedFileMode)
}

// embedInstallerMark replaces the block between the installer's markers with
// a function printing the mark as truecolor half-blocks.
func embedInstallerMark(path string, lines [][]span) error {
	script, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	before, rest, found := strings.Cut(string(script), installerBegin)
	_, after, closed := strings.Cut(rest, installerEnd)
	if !found || !closed {
		return fmt.Errorf("%s lacks the generated-logo markers", path)
	}
	var b strings.Builder
	b.WriteString(installerBegin + "\nprint_logo() {\n")
	for _, line := range lines {
		var row strings.Builder
		for _, span := range line {
			row.WriteString(sgrFor(span) + span.Text)
		}
		row.WriteString("\\033[0m")
		fmt.Fprintf(&b, "    printf '%%b\\n' '%s'\n", strings.ReplaceAll(row.String(), "'", "'\\''"))
	}
	b.WriteString("}\n" + installerEnd)
	return os.WriteFile(path, []byte(before+b.String()+after), generatedFileMode)
}

// sgrFor is the escape selecting a span's colors, written as printf %b text.
func sgrFor(s span) string {
	codes := []string{"0"}
	for _, selector := range []struct {
		code  int
		color string
	}{{sgrForegroundTrueColor, s.Color}, {sgrBackgroundTrueColor, s.Bg}} {
		if len(selector.color) != len("#rrggbb") {
			continue
		}
		r, _ := strconv.ParseUint(selector.color[1:3], 16, 8)
		g, _ := strconv.ParseUint(selector.color[3:5], 16, 8)
		b, _ := strconv.ParseUint(selector.color[5:7], 16, 8)
		codes = append(codes, fmt.Sprintf("%d;2;%d;%d;%d", selector.code, r, g, b))
	}
	return "\\033[" + strings.Join(codes, ";") + "m"
}
