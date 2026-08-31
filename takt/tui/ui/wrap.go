// Package ui contains reusable terminal UI primitives.
package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Wrap folds text to width while preserving existing line breaks.
func Wrap(text string, width int) string { return ansi.Wrap(text, width, "") }

// WrapLine folds one line into display-width-aware rows.
func WrapLine(line string, width int) []string { return strings.Split(Wrap(line, width), "\n") }
