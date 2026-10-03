package ui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func TestListNavigationAndRendering(t *testing.T) {
	if got := ui.MoveCursor(0, 3, -1); got != 2 {
		t.Fatalf("MoveCursor() = %d, want 2", got)
	}
	out := ui.Options([]string{"first", "second"}, 1, true)
	if strings.Count(out, theme.Icon.Cursor) != 1 || !strings.Contains(out, "second") {
		t.Fatalf("focused list output = %q", out)
	}
}

// A bar only measures real progress: without a known total the spinner
// marker carries activity and no bar pretends to measure anything.
func TestBusyShowsABarOnlyForAKnownTotal(t *testing.T) {
	unknown := ansi.Strip(ui.Busy("Installation", "*", ui.Progress{Frame: 1}))
	if strings.ContainsAny(unknown, "█░%") {
		t.Fatalf("a bar appeared without a known total:\n%s", unknown)
	}
	known := ansi.Strip(ui.Busy("Installation", "*", ui.Progress{Completed: 1, Total: 4}))
	if !strings.Contains(known, "25%") || !strings.Contains(known, "█") {
		t.Fatalf("a known total must show a bar and its percentage:\n%s", known)
	}
}

func TestHorizontalNavigationUsesLeftAndRight(t *testing.T) {
	keymap := keys.Default()
	cursor := 0
	if !ui.NudgeHorizontal(&cursor, 3, keymap, tea.KeyPressMsg{Code: tea.KeyRight}) || cursor != 1 {
		t.Fatalf("right cursor = %d, want 1", cursor)
	}
	if !ui.NudgeHorizontal(&cursor, 3, keymap, tea.KeyPressMsg{Code: tea.KeyLeft}) || cursor != 0 {
		t.Fatalf("left cursor = %d, want 0", cursor)
	}
	if ui.NudgeHorizontal(&cursor, 3, keymap, tea.KeyPressMsg{Code: tea.KeyDown}) {
		t.Fatal("down must not navigate a horizontal option row")
	}
}

func TestShellRendersWithinWidth(t *testing.T) {
	frame := ui.Frame{Header: "Install · Harnesses", Body: "Select AI Agents", Width: 80, Height: 24}
	out := ui.Shell(frame)
	if !strings.Contains(out, "Select AI Agents") {
		t.Fatalf("shell output = %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > frame.Width {
			t.Fatalf("shell line exceeds frame width: %q", line)
		}
	}
}

// A panel's width depends on the terminal only: content arriving or
// scrolling never resizes it, and it stays centered under a fixed header.
func TestShellPanelKeepsAStableCenteredWidth(t *testing.T) {
	const width = 140
	border := func(body string) (left, size int, header string) {
		lines := strings.Split(ansi.Strip(ui.Shell(ui.Frame{Header: "Configure", Body: body, Width: width, Height: 24})), "\n")
		for _, line := range lines {
			if left := strings.Index(line, "┌"); left >= 0 {
				return lipgloss.Width(line[:left]), lipgloss.Width(strings.TrimRight(line, " ")) - lipgloss.Width(line[:left]), lines[0]
			}
		}
		t.Fatalf("panel border not found:\n%s", strings.Join(lines, "\n"))
		return 0, 0, ""
	}
	shortLeft, shortSize, header := border("Short content")
	longLeft, longSize, _ := border(strings.Repeat("a much longer line of content ", 3))
	if shortSize != ui.PanelWidth(width) || longSize != shortSize || longLeft != shortLeft {
		t.Fatalf("panel width %d/%d at %d/%d, want a stable %d", shortSize, longSize, shortLeft, longLeft, ui.PanelWidth(width))
	}
	if inner := ui.InnerWidth(width); shortLeft != (width-inner)/2+(inner-shortSize)/2 {
		t.Fatalf("panel left edge %d, want it centered", shortLeft)
	}
	if !strings.HasPrefix(header, "   Takt AI") {
		t.Fatalf("header must sit at the three-column wide margin: %q", header)
	}
}

// Styled spans end in an SGR reset; inside a panel the surface background
// must come back right after it instead of exposing the terminal background.
func TestPanelBackgroundSurvivesStyledSpans(t *testing.T) {
	theme.SetMode(theme.ModeColor)
	body := theme.StatusWarning.Render("Warning:") + " rest of the line"
	out := ui.Shell(ui.Frame{Header: "Install", Body: body, Width: 80, Height: 24})
	surface := ansi.Style{}.BackgroundColor(theme.Surface).ForegroundColor(theme.TextPrimary).String()
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "Warning:") {
			continue
		}
		after := line[strings.Index(line, "Warning:"):]
		if !strings.Contains(after, ansi.ResetStyle+surface) {
			t.Fatalf("panel background not restored after the styled span: %q", after)
		}
		return
	}
	t.Fatal("styled span not rendered")
}

// An overflowing panel shows which rows are visible (PR-UX-29).
func TestOverflowingPanelShowsItsPosition(t *testing.T) {
	body := strings.Repeat("row\n", 80)
	out := ansi.Strip(ui.Shell(ui.Frame{Header: "Install", Body: body, Footer: "Back", Width: 80, Height: 24}))
	want := fmt.Sprintf(ui.TextScrollPosFmt, 1, ui.BodyHeight(24), 80)
	if !strings.Contains(out, want) {
		t.Fatalf("overflow position %q missing:\n%s", want, out)
	}
	if fits := ansi.Strip(ui.Shell(ui.Frame{Header: "Install", Body: "row", Footer: "Back", Width: 80, Height: 24})); strings.Contains(fits, "/ 1") {
		t.Fatalf("a panel that fits must not show a position:\n%s", fits)
	}
}

// A short panel sits in the middle of the body area, not against the header.
func TestShortPanelIsVerticallyCentered(t *testing.T) {
	lines := strings.Split(ansi.Strip(ui.Shell(ui.Frame{Header: "Install", Body: "row", Footer: "Back", Width: 80, Height: 24})), "\n")
	top := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "┌") })
	bottom := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "Back") })
	above, below := top-2, len(lines)-1-bottom
	if top < 0 || bottom < 0 || above-below > 1 || below-above > 1 {
		t.Fatalf("block rows above %d / below %d, want it centered:\n%s", above, below, strings.Join(lines, "\n"))
	}
}

// TestShellHeaderShowsSignatureAndStep proves the header renders the Takt AI
// signature on the left and the step (after " · ") on the right, never the task.
func TestShellHeaderShowsSignatureAndStep(t *testing.T) {
	out := ui.Shell(ui.Frame{Header: "Install · Review", Body: "Body", Width: 80, Height: 24})
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "Takt AI") {
		t.Fatalf("shell output missing signature:\n%s", plain)
	}
	if !strings.Contains(plain, "Review") || strings.Contains(plain, "Install · Review") {
		t.Fatalf("shell header must show the step only, not the full title:\n%s", plain)
	}
}

// TestShellOmitsPanelOnHome proves the entry screen is not wrapped in a panel.
func TestShellOmitsPanelOnHome(t *testing.T) {
	paneled := ansi.Strip(ui.Shell(ui.Frame{Body: "Select AI Agents", Width: 80, Height: 24}))
	home := ansi.Strip(ui.Shell(ui.Frame{Body: "Select AI Agents", Width: 80, Height: 24, Home: true}))
	if !strings.Contains(paneled, "┌") {
		t.Fatalf("operational body must be paneled:\n%s", paneled)
	}
	if strings.Contains(home, "┌") {
		t.Fatalf("home body must not be paneled:\n%s", home)
	}
}

func TestShellBelowMinimumRendersSizeNotice(t *testing.T) {
	out := ui.Shell(ui.Frame{Header: "Install", Body: "Body", Width: 50, Height: 24})
	if strings.Contains(out, "Body") || !strings.Contains(ansi.Strip(out), "50x24") {
		t.Fatalf("small shell = %q", out)
	}
}

// TestShellFillsTerminalAndCapsOverflow checks that the painted canvas always
// fills the terminal height, and that overflowing content is capped at it.
func TestShellFillsTerminalAndCapsOverflow(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 24}, {120, 36}} {
		short := ui.Shell(ui.Frame{Header: "Install", Body: "Short", Footer: "Home", Width: size[0], Height: size[1]})
		if lipgloss.Height(short) != size[1] || !strings.Contains(short, "Home") {
			t.Fatalf("short body height = %d, want it to fill the painted canvas (%d) with footer", lipgloss.Height(short), size[1])
		}

		long := ui.Shell(ui.Frame{Header: "Install", Body: strings.Repeat("long-content ", 500), Footer: "Home", Width: size[0], Height: size[1]})
		if lipgloss.Width(long) > size[0] || lipgloss.Height(long) != size[1] {
			t.Fatalf("long body dimensions %dx%d, want %v", lipgloss.Width(long), lipgloss.Height(long), size)
		}
		if !strings.Contains(long, "Home") {
			t.Fatal("footer was clipped")
		}
	}
}

// TestShellSizesEveryLine checks the canvas geometry: every line is exactly
// the terminal width and the block fills the height. Owned backgrounds are
// painted through the declarative view fields by the controller, not as
// per-cell sequences inside Content.
func TestShellSizesEveryLine(t *testing.T) {
	out := ui.Shell(ui.Frame{Header: "Install", Body: ui.CheckList([]ui.Item{{Label: "OpenCode", Checked: true}}, 0, true), Width: 80, Height: 24})
	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Fatalf("painted height = %d, want 24", len(lines))
	}
	for _, line := range lines {
		if lipgloss.Width(line) != 80 {
			t.Fatalf("unsized line %q", line)
		}
	}
}

func ansiFree(s string) string { return ansi.Strip(s) }

// TestShellKeepsWrappedLinesIndentedUnderTheirItem pins the hanging-indent
// rule: a long consequence wraps under its own item instead of restarting at
// the margin.
func TestShellKeepsWrappedLinesIndentedUnderTheirItem(t *testing.T) {
	body := "Remove:\n  " + strings.Repeat("path/segment/", 12) + "file.md"
	out := ansiFree(ui.Shell(ui.Frame{Header: "Uninstall · Review", Body: body, Width: 60, Height: 20}))
	wrapped := 0
	for index, line := range strings.Split(out, "\n") {
		_, content, inPanel := strings.Cut(line, "│ ")
		if !inPanel || !strings.Contains(content, "path/segment/") {
			continue
		}
		wrapped++
		if !strings.HasPrefix(content, "  ") {
			t.Fatalf("line %d is not indented under its item: %q", index+1, line)
		}
	}
	if wrapped < 2 {
		t.Fatalf("expected the path to wrap, got:\n%s", out)
	}
}

// TestMonoShellEmitsNoAnsi proves NO_COLOR degrades to plain text with the
// focus markers and labels intact.
func TestMonoShellEmitsNoAnsi(t *testing.T) {
	theme.SetMode(theme.ModeMono)
	defer theme.SetMode(theme.ModeColor)
	out := ui.Shell(ui.Frame{Header: "Install · Review", Body: ui.Options([]string{"first", "second"}, 1, true), Width: 80, Height: 24})
	if strings.Contains(out, "\x1b") {
		t.Fatalf("mono shell must emit no ANSI:\n%q", out)
	}
	if !strings.Contains(out, "> second") {
		t.Fatalf("mono shell must keep the focus marker:\n%s", out)
	}
}
