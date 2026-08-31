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

// Command generate-logo creates structured TUI branding data from the canonical PNG.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
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
	chafaVersion = "1.18.2"
	whiteFloor   = 245
)

type span struct {
	Text  string
	Color string
}

func main() {
	input := flag.String("input", "", "input PNG path")
	output := flag.String("output", "takt/tui/styles/logo_generated.go", "generated Go file path")
	flag.Parse()
	if *input == "" {
		fatal(errors.New("-input is required"))
	}
	if err := generate(*input, *output); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "generate-logo:", err)
	os.Exit(1)
}

func generate(input, output string) error {
	if err := requireChafa(); err != nil {
		return err
	}
	file, err := os.Open(input)
	if err != nil {
		return err
	}
	defer file.Close()
	inputImage, err := png.Decode(file)
	if err != nil {
		return fmt.Errorf("decode PNG: %w", err)
	}
	temporary, err := os.CreateTemp("", "takt-logo-*.png")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := png.Encode(temporary, removeBorderWhite(inputImage)); err != nil {
		temporary.Close()
		return fmt.Errorf("encode processed PNG: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	ansi, err := exec.Command("chafa", "--format=symbols", "--symbols=braille", "--colors=full", "--fg-only", "--size=32x20", temporaryName).Output()
	if err != nil {
		return fmt.Errorf("run chafa: %w", err)
	}
	lines, err := parseANSI(ansi)
	if err != nil {
		return fmt.Errorf("parse chafa output: %w", err)
	}
	lines = trimBlankRows(lines)
	if err := validateLogo(lines); err != nil {
		return err
	}
	return writeGenerated(output, lines)
}

// trimBlankRows removes Chafa's grid padding without changing the brand mark.
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

func requireChafa() error {
	output, err := exec.Command("chafa", "--version").Output()
	if err != nil {
		return errors.New("Chafa 1.18.2 is required to regenerate the logo; install it outside the release binary")
	}
	if !strings.Contains(string(output), "Chafa version "+chafaVersion) {
		return fmt.Errorf("Chafa %s is required, got %q", chafaVersion, strings.TrimSpace(string(output)))
	}
	return nil
}

func removeBorderWhite(source image.Image) *image.NRGBA {
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
		if !seen[point] && nearWhite(result.At(point.X, point.Y)) {
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

func nearWhite(c color.Color) bool {
	r, g, b, a := c.RGBA()
	return a > 0 && r>>8 >= whiteFloor && g>>8 >= whiteFloor && b>>8 >= whiteFloor
}

func parseANSI(input []byte) ([][]span, error) {
	var lines [][]span
	var line []span
	foreground := ""
	for len(input) > 0 {
		if input[0] == '\x1b' {
			if len(input) < 2 || input[1] != '[' {
				return nil, errors.New("unsupported escape sequence")
			}
			end := 2
			for end < len(input) && (input[end] < 0x40 || input[end] > 0x7e) {
				end++
			}
			if end == len(input) {
				return nil, errors.New("unterminated escape sequence")
			}
			if input[end] == 'm' {
				var err error
				foreground, err = applySGR(string(input[2:end]), foreground)
				if err != nil {
					return nil, err
				}
			}
			input = input[end+1:]
			continue
		}
		r, size := utf8.DecodeRune(input)
		if r == utf8.RuneError && size == 1 {
			return nil, errors.New("invalid UTF-8 in Chafa output")
		}
		input = input[size:]
		switch r {
		case '\r':
		case '\n':
			lines = append(lines, line)
			line = nil
		default:
			line = appendSpan(line, string(r), foreground)
		}
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}
	return lines, nil
}

func applySGR(sequence, foreground string) (string, error) {
	if sequence == "" {
		return "", nil
	}
	parts := strings.Split(sequence, ";")
	for i := 0; i < len(parts); i++ {
		code, err := strconv.Atoi(parts[i])
		if err != nil {
			return "", fmt.Errorf("invalid SGR code %q", parts[i])
		}
		switch code {
		case 0, 39:
			foreground = ""
		case 38:
			if i+4 >= len(parts) || parts[i+1] != "2" {
				return "", fmt.Errorf("expected truecolor foreground SGR in %q", sequence)
			}
			r, err := strconv.Atoi(parts[i+2])
			if err != nil {
				return "", err
			}
			g, err := strconv.Atoi(parts[i+3])
			if err != nil {
				return "", err
			}
			b, err := strconv.Atoi(parts[i+4])
			if err != nil {
				return "", err
			}
			if r < 0 || r > 255 || g < 0 || g > 255 || b < 0 || b > 255 {
				return "", errors.New("truecolor component out of range")
			}
			foreground = fmt.Sprintf("#%02x%02x%02x", r, g, b)
			i += 4
		case 48:
			if i+4 >= len(parts) || parts[i+1] != "2" {
				return "", fmt.Errorf("expected truecolor background SGR in %q", sequence)
			}
			i += 4
		}
	}
	return foreground, nil
}

func appendSpan(line []span, text, color string) []span {
	if len(line) > 0 && line[len(line)-1].Color == color {
		line[len(line)-1].Text += text
		return line
	}
	return append(line, span{Text: text, Color: color})
}

func validateLogo(lines [][]span) error {
	for _, line := range lines {
		for _, span := range line {
			if span.Text == "" || strings.ContainsRune(span.Text, '\x1b') {
				return errors.New("generated logo contains an invalid text span")
			}
			if span.Color != "" && (len(span.Color) != 7 || span.Color[0] != '#') {
				return fmt.Errorf("invalid foreground color %q", span.Color)
			}
		}
	}
	if len(lines) == 0 {
		return errors.New("generated logo has no lines")
	}
	return nil
}

func writeGenerated(output string, lines [][]span) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/generate-logo; DO NOT EDIT.\n\npackage styles\n\n")
	b.WriteString("type logoSpan struct {\n\tText string\n\tColor string\n}\n\nvar generatedLogo = [][]logoSpan{\n")
	for _, line := range lines {
		b.WriteString("\t{")
		for _, span := range line {
			fmt.Fprintf(&b, "{Text: %q, Color: %q},", span.Text, span.Color)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	return os.WriteFile(output, b.Bytes(), 0o644)
}
