// Package ui contains reusable terminal UI primitives.
package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// Frame holds everything needed to draw one screen.
type Frame struct {
	// Header names the task and step ("Install · Review"); Shell renders the
	// step after " · " on the right of the header, the signature on the left.
	Header string
	Body   string
	// Footer is the row of action buttons rendered below the panel.
	Footer string
	Width  int
	Height int
	Scroll int
	// Home renders the entry screen: no header, no panel and no wrapping; Body
	// is the screen's own composition sized by HomeRows and InnerWidth.
	Home bool
}

// MinWidth and MinHeight bound the smallest usable screen.
const (
	// MinWidth is the narrowest supported terminal width in cells.
	MinWidth = 60
	// MinHeight is the shortest supported terminal height in cells.
	MinHeight = 20
	// DefaultWidth is the fallback terminal width used by the shared shell.
	DefaultWidth = 80
	// DefaultHeight is the fallback terminal height used by the shared shell.
	DefaultHeight = 24
)

const (
	// shellHeaderRows accounts for the header and its following blank row.
	shellHeaderRows = 2
	// shellBottomMargin keeps one blank row under the last block.
	shellBottomMargin = 1
	// footerGap is the blank row between the panel and the action row; it also
	// carries the scroll position when the panel overflows.
	footerGap = 1
	// centerDivisor splits remaining space evenly around centered content.
	centerDivisor = 2
	// wideMargin and narrowMargin are the lateral margins (PR-UX-8): three
	// columns when they fit, two at reduced width.
	wideMargin, narrowMargin = 3, 2
	// panelBorder is the border cost of a panel on each axis (both sides).
	panelBorder = 2
	// panelPadding is the horizontal padding inside a panel (both sides).
	panelPadding = 2
	// panelMaxWidth bounds every panel so it never stretches text across a
	// wide terminal (PR-UX-2) and never changes width with its content.
	panelMaxWidth = 96
)

// Shell composes the signature header, the paneled body and the action row so
// every screen shares one layout. No key bar is rendered.
func Shell(frame Frame) string {
	if frame.Width <= 0 {
		frame.Width = DefaultWidth
	}
	if frame.Height <= 0 {
		frame.Height = DefaultHeight
	}
	inner := max(1, InnerWidth(frame.Width))
	if frame.Width < MinWidth || frame.Height < MinHeight {
		return smallShell(frame, inner)
	}
	return fullShell(frame, inner)
}

func smallShell(frame Frame, inner int) string {
	notice := fmt.Sprintf(TextShellTooSmallFmt, MinWidth, MinHeight, frame.Width, frame.Height)
	return paint(indent(ansi.Wrap(theme.Label.Render(notice), inner, ""), frame.Width), frame.Width, frame.Height)
}

func fullShell(frame Frame, inner int) string {
	available := max(1, frame.Height-shellHeaderRows-shellBottomMargin)
	footer := ""
	if trimmed := strings.TrimRight(frame.Footer, "\n"); trimmed != "" {
		footer = centerBlock(wrapBody(trimmed, inner), inner)
	}
	body := strings.TrimRight(frame.Body, "\n")

	if frame.Home {
		// The home composes its own centered block (logo, signature, menu) for
		// HomeRows; it carries the signature, so no header row repeats it.
		return paint(indent(fitLines(body, HomeRows(frame.Height)), frame.Width), frame.Width, frame.Height)
	}
	var block string
	if body != "" {
		block = panelBlock(body, footer, frame.Width, frame.Scroll, available, inner)
		// A short block sits in the middle of the body area instead of
		// piling against the header.
		if height := lipgloss.Height(block); height < available {
			block = strings.Repeat("\n", (available-height)/centerDivisor) + block
		}
	} else {
		block = fitLines(wrapBody(body, inner), max(1, available-footerRows(footer)))
		if footer != "" {
			block += "\n\n" + footer
		}
	}
	sections := headerRow(stepLabel(frame.Header), inner) + "\n\n" + block
	return paint(indent(sections, frame.Width), frame.Width, frame.Height)
}

// panelBlock renders the bordered panel at its stable width, the gap row
// (scroll position when the panel overflows) and the action row.
func panelBlock(body, footer string, width, scroll, available, inner int) string {
	panelWidth := PanelWidth(width)
	lines := strings.Split(wrapBody(body, panelWidth-panelBorder-panelPadding), "\n")
	window := max(1, available-panelBorder-footerRows(footer))
	start := visibleStart(lines, scroll, window)
	end := min(len(lines), start+window)
	panel := theme.Panel.Width(panelWidth).Render(paintSurface(strings.Join(lines[start:end], "\n")))
	rows := []string{lipgloss.PlaceHorizontal(inner, lipgloss.Center, panel)}
	gap := ""
	if len(lines) > window {
		gap = positionRow(start, end, len(lines), inner, panelWidth)
	}
	if footer != "" {
		rows = append(rows, gap, footer)
	} else if gap != "" {
		rows = append(rows, gap)
	}
	return strings.Join(rows, "\n")
}

// footerRows is the height the action row and its gap take.
func footerRows(footer string) int {
	if footer == "" {
		return 0
	}
	return footerGap + lipgloss.Height(footer)
}

// positionRow shows which rows of an overflowing panel are visible,
// right-aligned under the panel (PR-UX-29).
func positionRow(start, end, total, inner, panelWidth int) string {
	position := theme.Caption.Render(fmt.Sprintf(TextScrollPosFmt, start+1, end, total))
	right := (inner-panelWidth)/centerDivisor + panelWidth
	return strings.Repeat(" ", max(0, right-lipgloss.Width(position))) + position
}

// paintSurface keeps the panel background behind styled spans. Every span
// ends in an SGR reset, which would otherwise expose the terminal background
// until the end of the line; the surface colors are re-applied after each.
func paintSurface(content string) string {
	if theme.Mono() {
		return content
	}
	surface := ansi.Style{}.BackgroundColor(theme.Surface).ForegroundColor(theme.TextPrimary).String()
	content = strings.ReplaceAll(content, "\x1b[0m", ansi.ResetStyle)
	return strings.ReplaceAll(content, ansi.ResetStyle, ansi.ResetStyle+surface)
}

func visibleStart(lines []string, scroll, height int) int {
	start := min(max(0, scroll), max(0, len(lines)-height))
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(ansi.Strip(line)), strings.TrimSpace(theme.Icon.Cursor)) {
			if i < start {
				start = i
			}
			if i >= start+height {
				start = i - height + 1
			}
			break
		}
	}
	return start
}

// centerBlock centers each line of a block within width.
func centerBlock(block string, width int) string {
	var out []string
	for line := range strings.SplitSeq(block, "\n") {
		pad := (width - lipgloss.Width(line)) / centerDivisor
		if pad < 0 {
			pad = 0
		}
		out = append(out, strings.Repeat(" ", pad)+line)
	}
	return strings.Join(out, "\n")
}

// stepLabel isolates the step from a "Task · Step" header.
func stepLabel(header string) string {
	_, step, found := strings.Cut(header, " · ")
	if !found {
		return header
	}
	return step
}

// headerRow renders the signature on the left and the step on the right.
func headerRow(step string, width int) string {
	sig := theme.SignatureText.Render(TextBrand)
	if step == "" {
		return sig
	}
	step = theme.Title.Render(step)
	pad := max(0, width-lipgloss.Width(sig)-lipgloss.Width(step))
	return sig + strings.Repeat(" ", pad) + step
}

// wrapBody wraps lines with hanging indents so wrapped text stays attached to its item.
func wrapBody(body string, width int) string {
	var out []string
	for line := range strings.SplitSeq(body, "\n") {
		if ansi.StringWidth(line) <= width {
			out = append(out, line)
			continue
		}
		hang := hangingIndent(ansi.Strip(line))
		if 2*hang >= width {
			hang = 0
		}
		// Wrap only the text after the indent, at the width it really has, so
		// the first segment is not cut hang columns short.
		head, text := ansi.Truncate(line, hang, ""), ansi.TruncateLeft(line, hang, "")
		segments := strings.Split(ansi.Wrap(text, width-hang, ""), "\n")
		segments[0] = head + segments[0]
		for index := 1; index < len(segments); index++ {
			segments[index] = strings.Repeat(" ", hang) + segments[index]
		}
		out = append(out, segments...)
	}
	return strings.Join(out, "\n")
}

// hangingIndent finds where a line's text starts so continuations align under it.
func hangingIndent(line string) int {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	rest := line[indent:]
	for _, marker := range []string{theme.Icon.Cursor, theme.Icon.CheckboxOn, theme.Icon.CheckboxOff} {
		if after, found := strings.CutPrefix(rest, marker); found {
			indent += len(marker)
			rest = after
			if trimmed, spaced := strings.CutPrefix(rest, " "); spaced {
				indent, rest = indent+1, trimmed
			}
		}
	}
	return indent
}

// margin sets the lateral margin so narrow screens keep more content width.
func margin(width int) int {
	if width < DefaultWidth {
		return narrowMargin
	}
	return wideMargin
}

// indent applies the left margin so content never touches the terminal edge.
func indent(block string, width int) string {
	pad := strings.Repeat(" ", margin(width))
	return pad + strings.ReplaceAll(block, "\n", "\n"+pad)
}

// paint sizes the block to the terminal: every line is truncated to width
// and padded with spaces, and short blocks grow to height. Color is never
// injected here: the controller paints the owned backgrounds through the
// declarative view fields, so Content stays geometry plus content styling.
func paint(block string, width, height int) string {
	lines := strings.Split(block, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines[:height] {
		line = ansi.Truncate(line, width, "")
		lines[i] = line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	}
	return strings.Join(lines[:height], "\n")
}

// BodyHeight returns the visible rows inside a panel above a one-line action
// row: the window Shell scrolls for most screens.
func BodyHeight(height int) int {
	return ContentRows(height, 1)
}

// ContentRows returns the visible rows inside a panel above an action row of
// footerLines lines (0 for none); Shell uses the same arithmetic.
func ContentRows(height, footerLines int) int {
	if height <= 0 {
		return 0
	}
	reserved := shellHeaderRows + shellBottomMargin + panelBorder
	if footerLines > 0 {
		reserved += footerGap + footerLines
	}
	return max(1, height-reserved)
}

// HomeRows returns the rows the home composition may fill: everything above
// the bottom margin, since the home has no header.
func HomeRows(height int) int {
	return max(1, height-shellBottomMargin)
}

// PanelWidth returns the stable panel width for a terminal width.
func PanelWidth(width int) int {
	return min(max(1, InnerWidth(width)), panelMaxWidth)
}

// InnerWidth returns width inside margins.
func InnerWidth(width int) int {
	if width <= 0 {
		return 0
	}
	return width - 2*margin(width)
}

// fitLines caps a block to the visible rows so overflow never pushes chrome away.
func fitLines(block string, count int) string {
	lines := strings.Split(block, "\n")
	if len(lines) > count {
		return strings.Join(lines[:count], "\n")
	}
	return block
}
