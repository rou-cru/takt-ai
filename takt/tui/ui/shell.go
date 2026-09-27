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
	// Home renders the entry screen: no panel and no step, since the logo in
	// Body already identifies the product.
	Home bool
	// CenterBody vertically centers short content in the available body area.
	CenterBody bool
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
	// shellFooterGap reserves the blank row before footer actions.
	shellFooterGap = 1
	// centerDivisor splits remaining width evenly around centered content.
	centerDivisor = 2
	// wideMargin is the lateral margin on wide terminals.
	wideMargin = 2
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

	sections := []string{headerRow(stepLabel(frame.Header), inner), ""}

	// The paneled body keeps its border intact: content wraps to the inner
	// width minus border and padding, and the border is applied last.
	footer := strings.TrimRight(frame.Footer, "\n")
	reserved := shellHeaderRows + shellFooterGap
	if footer != "" {
		reserved += footerHeight(footer, inner)
	}
	bodyHeight := max(1, frame.Height-reserved)

	panel := !frame.Home && strings.TrimRight(frame.Body, "\n") != ""
	contentWidth := inner
	if panel {
		contentWidth = max(1, inner-panelPad)
	}
	body := wrapBody(strings.TrimRight(frame.Body, "\n"), contentWidth)
	lines := strings.Split(body, "\n")
	windowHeight := bodyHeight
	if panel {
		windowHeight = max(1, bodyHeight-panelVerticalPad)
	}
	start := visibleStart(lines, frame.Scroll, windowHeight)
	visible := lines[start:]
	if frame.CenterBody && len(visible) < windowHeight {
		pad := (windowHeight - len(visible)) / 2
		visible = append(make([]string, pad), visible...)
	}
	bodyOut := fitLines(strings.Join(visible, "\n"), windowHeight)
	if panel {
		bodyOut = lipgloss.PlaceHorizontal(inner, lipgloss.Center, theme.Panel.Render(bodyOut))
	}
	sections = append(sections, bodyOut)
	if footer != "" {
		sections = append(sections, "", centerBlock(wrapBody(footer, inner), inner))
	}
	return paint(indent(strings.Join(sections, "\n"), frame.Width), frame.Width, frame.Height)
}

func footerHeight(footer string, inner int) int { return lipgloss.Height(wrapBody(footer, inner)) + 1 }
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

// panelPad is the horizontal cost of the panel border and padding.
const panelPad = 4

// panelVerticalPad is the vertical cost of the panel border (top and bottom).
const panelVerticalPad = 2

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
	sig := theme.Signature.Render(TextBrand)
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
		hang := hangingIndent(ansi.Strip(line))
		if 2*hang >= width {
			hang = 0
		}
		segments := strings.Split(ansi.Wrap(line, width-hang, ""), "\n")
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
		return 1
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

// chromeHeight reserves rows around the body so content never overlaps chrome.
const chromeHeight = 6

// BodyHeight returns usable content height.
func BodyHeight(height int) int {
	if height <= 0 {
		return 0
	}
	if height -= chromeHeight; height > 1 {
		return height
	}
	return 1
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
