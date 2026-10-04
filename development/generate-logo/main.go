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
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// rgbaShift converts the 16-bit color channels returned by RGBA to 8-bit channels.
	rgbaShift = 8
	// interiorEdge, innerRingEdge, gapEdge and outerBandEdge are the outer
	// radii, as fractions of half the image side, of the interior, the inner
	// ring, the dark gap and the outer band, measured on the canonical PNG
	// (each edge is sharp to within 3 px over all angles).
	interiorEdge, innerRingEdge, gapEdge, outerBandEdge = 0.724, 0.836, 0.889, 0.946
	// darkVoteNumerator/darkVoteDenominator is the weight of a dark pixel's
	// vote when downsampling (see downsample).
	darkVoteNumerator, darkVoteDenominator = 8, 5
	// generatedDirectoryMode and generatedFileMode keep generated branding traversable and readable.
	generatedDirectoryMode os.FileMode = 0o755
	generatedFileMode      os.FileMode = 0o644
	// sgr color selectors are the ANSI SGR values written into the installer mark.
	sgrForegroundTrueColor, sgrBackgroundTrueColor = 38, 48
	// installerRows is the height of the mark the installer prints, drawn
	// with half-blocks so any true-color terminal shows it as intended.
	installerRows = 14
	// installerBegin and installerEnd delimit the generated mark in install.sh.
	installerBegin = "# BEGIN GENERATED LOGO (development/generate-logo; DO NOT EDIT)"
	installerEnd   = "# END GENERATED LOGO"
)

// logoRows are the heights, in terminal rows, of the generated variants,
// largest first: the home draws the largest one that fits the terminal. Every
// variant is twice as wide as it is tall, so the ring stays round in cells
// about twice as tall as they are wide.
var logoRows = []int{32, 26, 20, 16, 12, 8}

// ink is one class of the flat brand palette every source pixel is reduced
// to. The volume and gloss of the artwork are dropped on purpose: at terminal
// resolution only flat regions keep clean edges.
type ink uint8

const (
	inkClear ink = iota
	inkBlack
	inkOuterBand
	inkInnerRing
	inkBody
	inkChest
	inkCount
)

// rgb is an 8-bit color.
type rgb struct{ r, g, b uint8 }

// palette holds the flat colors sampled from the canonical PNG: the dark
// interior (also the eyes, the nose and the gap between the rings), the outer
// band, the inner ring, the white body and the pale chest. inkClear has no
// color.
var palette = [inkCount]rgb{
	inkBlack:     {0x0e, 0x0e, 0x10},
	inkOuterBand: {0x28, 0x2d, 0x55},
	inkInnerRing: {0x35, 0x42, 0x76},
	inkBody:      {0xf3, 0xef, 0xee},
	inkChest:     {0xb8, 0xc4, 0xd0},
}

// hex is the #rrggbb form of a palette ink; inkClear has none.
func (i ink) hex() string {
	if i == inkClear {
		return ""
	}
	c := palette[i]
	return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
}

// span holds one colored text run so logo rows stay compact.
type span struct {
	Text  string
	Color string
	Bg    string
}

// grid is a square or rectangular raster of palette inks.
type grid struct {
	width, height int
	pixels        []ink
}

func newGrid(width, height int) *grid {
	return &grid{width: width, height: height, pixels: make([]ink, width*height)}
}

func (g *grid) at(x, y int) ink     { return g.pixels[y*g.width+x] }
func (g *grid) set(x, y int, i ink) { g.pixels[y*g.width+x] = i }

// main parses flags so missing input fails before bad output is written.
func main() {
	input := flag.String("input", "", "input PNG path")
	output := flag.String("output", "takt/tui/styles/logo_generated.go", "generated Go file path (Braille mono datasets)")
	blocksOutput := flag.String("blocks-output", "takt/tui/styles/logo_blocks_generated.go", "generated Go file path (octant and half-block color datasets)")
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
// The mono file holds Braille variants whose shape survives the loss of
// color; the color file holds octant variants for terminals known to draw
// Unicode 16 octants and half-block variants for every other terminal.
// installer, when set, receives a half-block mark between its markers.
func generate(input, output, blocksOutput, installer string) error {
	source, err := decodeInputImage(input)
	if err != nil {
		return err
	}
	art, err := classify(source)
	if err != nil {
		return err
	}
	var braille, octant, half [][][]span
	for _, rows := range logoRows {
		fine := downsample(art, 4*rows)
		braille = append(braille, renderBraille(fine))
		octant = append(octant, renderOctants(fine))
		half = append(half, renderHalfBlocks(downsample(art, 2*rows)))
	}
	if err := writeGenerated(output, dataset{"generatedLogoBraille", braille}); err != nil {
		return err
	}
	if err := writeGenerated(blocksOutput, dataset{"generatedLogoOctants", octant}, dataset{"generatedLogoHalfBlocks", half}); err != nil {
		return err
	}
	if installer == "" {
		return nil
	}
	return embedInstallerMark(installer, renderHalfBlocks(downsample(art, 2*installerRows)))
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

// classify reduces the square source to palette inks. The rings are drawn
// geometrically from the radii measured on the canonical PNG, so their
// shading never mottles them; inside the inner ring each pixel takes the
// nearest interior ink, and everything beyond the outer band is clear. The
// result is cropped to the outer band, so the mark is centered and symmetric.
func classify(source image.Image) (*grid, error) {
	bounds := source.Bounds()
	if bounds.Dx() != bounds.Dy() || bounds.Dx() == 0 {
		return nil, fmt.Errorf("input image must be square, got %dx%d", bounds.Dx(), bounds.Dy())
	}
	half := float64(bounds.Dx()) / 2
	side := int(math.Ceil(2 * outerBandEdge * half))
	origin := half - float64(side)/2
	result := newGrid(side, side)
	for y := range side {
		for x := range side {
			sx, sy := origin+float64(x)+0.5, origin+float64(y)+0.5
			radius := math.Hypot(sx-half, sy-half) / half
			switch {
			case radius > outerBandEdge:
			case radius > gapEdge:
				result.set(x, y, inkOuterBand)
			case radius > innerRingEdge:
				result.set(x, y, inkBlack)
			case radius > interiorEdge:
				result.set(x, y, inkInnerRing)
			default:
				r, g, b, _ := source.At(bounds.Min.X+int(sx), bounds.Min.Y+int(sy)).RGBA()
				result.set(x, y, nearestInk(rgb{uint8(r >> rgbaShift), uint8(g >> rgbaShift), uint8(b >> rgbaShift)}, interiorInks))
			}
		}
	}
	return result, nil
}

// interiorInks are the inks drawn inside the inner ring.
var interiorInks = []ink{inkBlack, inkBody, inkChest}

// nearestInk picks the candidate ink closest to c.
func nearestInk(c rgb, candidates []ink) ink {
	best, bestDistance := candidates[0], -1
	for _, candidate := range candidates {
		if distance := colorDistance(c, palette[candidate]); bestDistance < 0 || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

// colorDistance is a cheap perceptual distance ("redmean") between two colors.
func colorDistance(a, b rgb) int {
	meanRed := (int(a.r) + int(b.r)) / 2
	dr, dg, db := int(a.r)-int(b.r), int(a.g)-int(b.g), int(a.b)-int(b.b)
	return ((512+meanRed)*dr*dr)>>8 + 4*dg*dg + ((767-meanRed)*db*db)>>8
}

// downsample shrinks a square grid to size×size by majority vote, so edges
// stay crisp and no in-between shade appears that the palette lacks. Dark
// votes weigh more: the eyes, nose, whiskers and chest outline are thin and
// would otherwise vanish from the smaller variants.
func downsample(art *grid, size int) *grid {
	result := newGrid(size, size)
	for y := range size {
		y0, y1 := y*art.height/size, max((y+1)*art.height/size, y*art.height/size+1)
		for x := range size {
			x0, x1 := x*art.width/size, max((x+1)*art.width/size, x*art.width/size+1)
			var votes [inkCount]int
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					votes[art.at(sx, sy)]++
				}
			}
			votes[inkBlack] = votes[inkBlack] * darkVoteNumerator / darkVoteDenominator
			result.set(x, y, majority(votes))
		}
	}
	return result
}

// majority returns the ink with the most votes; ties go to the lower ink so
// the output is deterministic.
func majority(votes [inkCount]int) ink {
	best := inkClear
	for candidate := inkClear + 1; candidate < inkCount; candidate++ {
		if votes[candidate] > votes[best] {
			best = candidate
		}
	}
	return best
}

// renderHalfBlocks draws one cell per 1×2 pixels: the upper pixel is the
// foreground of ▀ and the lower one its background. Only ▀, ▄, █ and space
// appear, which every terminal font draws.
func renderHalfBlocks(art *grid) [][]span {
	lines := make([][]span, 0, art.height/2)
	for y := 0; y+1 < art.height; y += 2 {
		var line []span
		for x := range art.width {
			top, bottom := art.at(x, y), art.at(x, y+1)
			switch {
			case top == bottom:
				line = appendSpan(line, solidCell(top), top.hex(), "")
			case bottom == inkClear:
				line = appendSpan(line, "▀", top.hex(), "")
			case top == inkClear:
				line = appendSpan(line, "▄", bottom.hex(), "")
			default:
				line = appendSpan(line, "▀", top.hex(), bottom.hex())
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// solidCell is the character filling a cell with one ink: a full block, or a
// blank for clear so the terminal canvas shows through.
func solidCell(i ink) string {
	if i == inkClear {
		return " "
	}
	return "█"
}

// renderOctants draws one cell per 2×4 pixels with the Unicode 16 block
// octants: each cell keeps its two most common inks, as foreground and
// background, and folds any other pixel into the closer of the two.
func renderOctants(art *grid) [][]span {
	lines := make([][]span, 0, art.height/4)
	for y := 0; y+3 < art.height; y += 4 {
		var line []span
		for x := 0; x+1 < art.width; x += 2 {
			cell := cellPixels(art, x, y)
			back, fore := twoInks(cell)
			if back == fore {
				line = appendSpan(line, solidCell(back), back.hex(), "")
				continue
			}
			pattern := 0
			for index, pixel := range cell {
				if closerInk(pixel, back, fore) == fore {
					pattern |= 1 << index
				}
			}
			line = appendSpan(line, string(octantRune(pattern)), fore.hex(), back.hex())
		}
		lines = append(lines, line)
	}
	return lines
}

// renderBraille draws one cell per 2×4 pixels as Braille dots wherever the
// artwork is lit (body, chest or ring), without color: the mono-mode mark.
func renderBraille(art *grid) [][]span {
	lines := make([][]span, 0, art.height/4)
	for y := 0; y+3 < art.height; y += 4 {
		var text strings.Builder
		for x := 0; x+1 < art.width; x += 2 {
			dots := 0
			for index, pixel := range cellPixels(art, x, y) {
				if pixel != inkClear && pixel != inkBlack {
					dots |= brailleDots[index]
				}
			}
			text.WriteRune(rune(brailleBase + dots))
		}
		lines = append(lines, []span{{Text: text.String()}})
	}
	return lines
}

// cellPixels lists the 2×4 pixels of the cell at (x, y) in octant order:
// left to right, top to bottom, so index i is octant i+1.
func cellPixels(art *grid, x, y int) [8]ink {
	var cell [8]ink
	for row := range 4 {
		cell[2*row] = art.at(x, y+row)
		cell[2*row+1] = art.at(x+1, y+row)
	}
	return cell
}

// twoInks returns the cell's most common ink as background and the next as
// foreground; ties go to the lower ink. A clear pixel is always the
// background when present, since a terminal cannot draw a clear foreground.
// A single-ink cell returns that ink twice.
func twoInks(cell [8]ink) (back, fore ink) {
	var counts [inkCount]int
	for _, pixel := range cell {
		counts[pixel]++
	}
	// Scanning upward with a strict comparison leaves ties to the lower ink.
	back = inkClear
	for candidate := inkClear + 1; candidate < inkCount; candidate++ {
		if counts[candidate] > counts[back] {
			back = candidate
		}
	}
	fore = back
	for candidate := inkClear; candidate < inkCount; candidate++ {
		if candidate != back && counts[candidate] > 0 && (fore == back || counts[candidate] > counts[fore]) {
			fore = candidate
		}
	}
	if fore == inkClear {
		return fore, back
	}
	return back, fore
}

// closerInk folds a pixel into whichever of back and fore it resembles more;
// clear only absorbs clear, so stray colored pixels never punch holes.
func closerInk(pixel, back, fore ink) ink {
	switch {
	case pixel == back || pixel == fore:
		return pixel
	case back == inkClear:
		return fore
	case fore == inkClear:
		return back
	case colorDistance(palette[pixel], palette[fore]) < colorDistance(palette[pixel], palette[back]):
		return fore
	default:
		return back
	}
}

// brailleBase is U+2800, the empty Braille pattern; brailleDots maps octant
// order (left to right, top to bottom) onto Braille's dot bits.
const brailleBase = 0x2800

var brailleDots = [8]int{0x01, 0x08, 0x02, 0x10, 0x04, 0x20, 0x40, 0x80}

// octantBase is U+1CD00, BLOCK OCTANT-3: Unicode 16 encodes, in increasing
// pattern order, every octant pattern not already drawn by an older block
// character. octantLegacy holds those 26 older characters by pattern (bit i
// set means octant i+1 is filled).
const octantBase = 0x1CD00

var octantLegacy = map[int]rune{
	0x00: ' ', 0xFF: '█',
	0x0F: '▀', 0xF0: '▄', 0x55: '▌', 0xAA: '▐',
	0x05: '▘', 0x0A: '▝', 0x50: '▖', 0xA0: '▗',
	0xA5: '▚', 0x5A: '▞', 0x5F: '▛', 0xAF: '▜', 0xF5: '▙', 0xFA: '▟',
	0x03: '\U0001FB82', 0xC0: '▂', 0x3F: '\U0001FB85', 0xFC: '▆',
	0x14: '\U0001FBE6', 0x28: '\U0001FBE7',
	0x01: '\U0001CEA8', 0x02: '\U0001CEAB', 0x40: '\U0001CEA3', 0x80: '\U0001CEA0',
}

// octantRune returns the character drawing an octant pattern.
func octantRune(pattern int) rune {
	if legacy, ok := octantLegacy[pattern]; ok {
		return legacy
	}
	skipped := 0
	for older := range octantLegacy {
		if older < pattern {
			skipped++
		}
	}
	return rune(octantBase + pattern - skipped)
}

// appendSpan adds one cell, merging it into the previous run when both share
// colors so generated rows stay compact.
func appendSpan(line []span, text, color, bg string) []span {
	if count := len(line); count > 0 && line[count-1].Color == color && line[count-1].Bg == bg {
		line[count-1].Text += text
		return line
	}
	return append(line, span{Text: text, Color: color, Bg: bg})
}

// dataset is one generated logo variable: variants largest first.
type dataset struct {
	name     string
	variants [][][]span
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
		fmt.Fprintf(&b, "\nvar %s = [][][]logoSpan{\n", data.name)
		for _, variant := range data.variants {
			b.WriteString("\t{\n")
			for _, line := range variant {
				b.WriteString("\t\t{")
				for _, span := range line {
					fmt.Fprintf(&b, "{Text: %q, Color: %q, Bg: %q},", span.Text, span.Color, span.Bg)
				}
				b.WriteString("},\n")
			}
			b.WriteString("\t},\n")
		}
		b.WriteString("}\n")
	}
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
