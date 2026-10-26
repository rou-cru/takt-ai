package tui_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// canonicalModes restores the modes the suite's other tests assume, so a
// glyph or color experiment stays inside one test.
func canonicalModes(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		theme.SetMode(theme.ModeColor)
		theme.SetGlyphMode(theme.GlyphUnicode)
	})
}

// asciiOnly reports whether a rendered string holds no byte above ASCII.
func asciiOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// TestGlyphDetectionReadsTerminalCapability pins the detection matrix
// TERM=dumb proves an ASCII surface, the first set of LC_ALL, then
// LC_CTYPE, then LANG decides, and a locale that does not declare the UTF-8
// codeset falls back to ASCII while an unset locale defaults to Unicode.
// NO_COLOR proves nothing about glyphs (T-06).
func TestGlyphDetectionReadsTerminalCapability(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want theme.GlyphMode
	}{
		{"dumb terminal wins over a UTF-8 locale", map[string]string{"TERM": "dumb", "LC_ALL": "en_US.UTF-8"}, theme.GlyphASCII},
		{"C locale", map[string]string{"LC_ALL": "C"}, theme.GlyphASCII},
		{"POSIX locale", map[string]string{"LC_ALL": "POSIX"}, theme.GlyphASCII},
		{"ISO-8859-1 codeset", map[string]string{"LC_ALL": "en_US.ISO-8859-1"}, theme.GlyphASCII},
		{"territory without a codeset", map[string]string{"LC_ALL": "en_US"}, theme.GlyphASCII},
		{"UTF-8 codeset", map[string]string{"LC_ALL": "en_US.UTF-8"}, theme.GlyphUnicode},
		{"C.UTF-8", map[string]string{"LC_ALL": "C.UTF-8"}, theme.GlyphUnicode},
		{"lowercase utf8", map[string]string{"LC_ALL": "en_US.utf8"}, theme.GlyphUnicode},
		{"bare UTF-8 as terminals set LC_CTYPE", map[string]string{"LC_CTYPE": "UTF-8"}, theme.GlyphUnicode},
		{"modifier after the codeset", map[string]string{"LC_ALL": "en_US.UTF-8@euro"}, theme.GlyphUnicode},
		{"LC_ALL decides over LANG", map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, theme.GlyphASCII},
		{"LC_CTYPE decides over LANG", map[string]string{"LC_CTYPE": "en_US.UTF-8", "LANG": "C"}, theme.GlyphUnicode},
		{"non-UTF-8 LC_CTYPE decides over LANG", map[string]string{"LC_CTYPE": "C", "LANG": "en_US.UTF-8"}, theme.GlyphASCII},
		{"unset locale defaults to Unicode", nil, theme.GlyphUnicode},
		{"NO_COLOR proves nothing about glyphs", map[string]string{"NO_COLOR": "1"}, theme.GlyphUnicode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear every input first so ambient values cannot leak between cases.
			for _, name := range []string{"TERM", "LC_ALL", "LC_CTYPE", "LANG", "NO_COLOR"} {
				t.Setenv(name, "")
			}
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			if got := theme.DetectGlyphMode(); got != tc.want {
				t.Fatalf("DetectGlyphMode() under %v = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

// TestProvenGlyphCapabilityOverridesTheLocale pins SetGlyphMode as the
// entrypoint for proven capability: explicit terminal-capability
// information takes precedence over what the locale implies, in both
// directions.
func TestProvenGlyphCapabilityOverridesTheLocale(t *testing.T) {
	canonicalModes(t)
	t.Setenv("LC_ALL", "en_US.UTF-8")

	theme.SetGlyphMode(theme.GlyphASCII)
	if theme.Icon.BarFill != '#' || theme.Icon.Border.TopLeft != "+" {
		t.Fatalf("proven ASCII capability must prevail over the UTF-8 locale: Icon = %+v", theme.Icon)
	}

	theme.SetGlyphMode(theme.GlyphUnicode)
	if theme.Icon.BarFill != '█' || theme.Icon.Border.TopLeft != "┌" {
		t.Fatalf("proven Unicode capability must prevail over the locale too: Icon = %+v", theme.Icon)
	}
}

// TestASCIIRendersStatusFocusProgressAndBorders pins the unit-level
// half: the glyph map holds the approved ASCII vocabulary (T-06), and the
// shared renderers draw status words, focus, choices, progress and panel
// borders through it with no byte above ASCII and no replacement character.
// Verification rows are checked for their markers only; their state suffixes
// come from shared text constants outside this unit's files.
func TestASCIIRendersStatusFocusProgressAndBorders(t *testing.T) {
	canonicalModes(t)
	theme.SetMode(theme.ModeColor)
	theme.SetGlyphMode(theme.GlyphASCII)

	vocabulary := theme.Glyphs{
		Cursor:      "> ",
		Done:        "x",
		Failed:      "X",
		Unchecked:   "?",
		Chosen:      "* ",
		Unchosen:    "  ",
		CheckboxOn:  "[x]",
		CheckboxOff: "[ ]",
		FieldBar:    "| ",
		BarFill:     '#',
		BarEmpty:    '.',
		Border: lipgloss.Border{
			Top: "-", Bottom: "-", Left: "|", Right: "|",
			TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
		},
	}
	if theme.Icon != vocabulary {
		t.Fatalf("ASCII glyph map = %+v, want %+v", theme.Icon, vocabulary)
	}

	status := ui.Status(ui.StateFailed, "cannot reach OpenCode")
	if plain := ansi.Strip(status); !strings.Contains(plain, "Failed: cannot reach OpenCode") {
		t.Errorf("Status() = %q, want the state word and the message", plain)
	}
	if !asciiOnly(status) {
		t.Errorf("Status() = %q, want ASCII only", status)
	}

	busy := ui.Busy(ui.TextBusyInstallation, "*", ui.Progress{
		Done:      []string{"Checking OpenCode connection"},
		Message:   "Applying installation files",
		Current:   "agents.md",
		Completed: 1,
		Total:     4,
	})
	plainBusy := ansi.Strip(busy)
	for _, want := range []string{
		"x Checking OpenCode connection", // the completed step mark
		"* Applying installation files",  // the active step keeps its marker
		"25%",
		"agents.md",
		"#.", // the bar fills with # and leaves open cells as .
	} {
		if !strings.Contains(plainBusy, want) {
			t.Errorf("ASCII Busy() missing %q:\n%s", want, plainBusy)
		}
	}
	if !asciiOnly(busy) {
		t.Errorf("ASCII Busy() = %q, want no byte above ASCII", busy)
	}

	// T-06: user-provided names keep their actual content; the glyph map
	// governs generated characters only, so a non-ASCII file name is not
	// transliterated and draws no replacement character.
	verbatim := ui.Busy(ui.TextBusyInstallation, "*", ui.Progress{Message: "Applying", Current: "配置.md", Completed: 1, Total: 4})
	if !strings.Contains(verbatim, "配置.md") || strings.Contains(verbatim, "\uFFFD") {
		t.Errorf("ASCII Busy() = %q, want the file name verbatim with no replacement character", verbatim)
	}

	options := ui.Options([]string{"first", "second"}, 1, true)
	if plain := ansi.Strip(options); !strings.Contains(plain, "> second") {
		t.Errorf("Options() = %q, want the ASCII focus marker", plain)
	}
	if !asciiOnly(options) {
		t.Errorf("Options() = %q, want ASCII only", options)
	}

	checklist := ui.CheckList([]ui.Item{{Label: "OpenCode", Checked: true}, {Label: "Skills"}}, 0, true)
	if plain := ansi.Strip(checklist); !strings.Contains(plain, "[x] OpenCode") || !strings.Contains(plain, "[ ] Skills") {
		t.Errorf("CheckList() = %q, want the ASCII checkbox markers", plain)
	}
	if !asciiOnly(checklist) {
		t.Errorf("CheckList() = %q, want ASCII only", checklist)
	}

	selector := ui.Selector([]string{"first", "second"}, 0, 1, true)
	if plain := ansi.Strip(selector); !strings.Contains(plain, "* second") {
		t.Errorf("Selector() = %q, want the ASCII chosen marker", plain)
	}
	if !asciiOnly(selector) {
		t.Errorf("Selector() = %q, want ASCII only", selector)
	}

	report := verify.Report{Checks: []verify.CheckResult{
		{ID: "mcp:opencode:engram", State: verify.Verified},
		{ID: "mcp:opencode:codegraph", State: verify.NotVerified, Explanation: "no evidence"},
		{ID: "skills:opencode:takt", State: verify.NotVerifiable, Explanation: "cannot query"},
	}}
	verification := ui.Verification(report)
	plainVerification := ansi.Strip(verification)
	for _, want := range []string{"x Memory", "X Code navigation", "? Skills"} {
		if !strings.Contains(plainVerification, want) {
			t.Errorf("ASCII Verification() missing %q:\n%s", want, plainVerification)
		}
	}
	for _, unwanted := range []string{"✓", "✗"} {
		if strings.Contains(verification, unwanted) {
			t.Errorf("ASCII Verification() must not draw %q:\n%s", unwanted, verification)
		}
	}
	if strings.Contains(verification, "\uFFFD") {
		t.Errorf("ASCII Verification() = %q, want no replacement character", verification)
	}

	shell := ui.Shell(ui.Frame{Header: "Install", Body: "Select AI Agents", Footer: "Back", Width: 80, Height: 24})
	plainShell := ansi.Strip(shell)
	for _, want := range []string{"Takt AI", "Install", "Select AI Agents", "Back", "+"} {
		if !strings.Contains(plainShell, want) {
			t.Errorf("ASCII Shell() missing %q:\n%s", want, plainShell)
		}
	}
	for _, unwanted := range []string{"┌", "┐", "└", "┘", "─", "│"} {
		if strings.Contains(plainShell, unwanted) {
			t.Errorf("ASCII Shell() must not draw box-drawing %q:\n%s", unwanted, plainShell)
		}
	}
	if !asciiOnly(shell) {
		t.Errorf("ASCII Shell() = %q, want no byte above ASCII", shell)
	}
}

// TestASCIIRendersAtEverySupportedSize pins the size half of what a
// unit render can show: at 120x32, 80x24, 60x20 and 160x24, and below the
// minimum, one composed screen keeps its signature, focus, choices, progress,
// border and footer in ASCII bytes. The real-terminal inventory behind the size contract
// stays environment-gated evidence this test does not claim.
func TestASCIIRendersAtEverySupportedSize(t *testing.T) {
	canonicalModes(t)
	theme.SetMode(theme.ModeColor)
	theme.SetGlyphMode(theme.GlyphASCII)

	body := strings.Join([]string{
		ui.Status(ui.StateSuccess, "ready to work"),
		ui.CheckList([]ui.Item{{Label: "OpenCode", Checked: true}, {Label: "Skills"}}, 0, true),
		ui.Busy(ui.TextBusyInstallation, "*", ui.Progress{
			Done:      []string{"Checking OpenCode connection"},
			Message:   "Applying installation files",
			Completed: 1,
			Total:     4,
		}),
	}, "\n")
	footer := ui.FooterActions(ui.Actions(ui.TextActionBack), 0, true)

	for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}, {160, 24}} {
		out := ui.Shell(ui.Frame{
			Header: "Install",
			Body:   body,
			Footer: footer,
			Width:  size[0],
			Height: size[1],
		})
		plain := ansi.Strip(out)
		for _, want := range []string{"Takt AI", "> ", "[x]", "25%", "+", "Back"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%dx%d Shell() missing %q:\n%s", size[0], size[1], want, plain)
			}
		}
		if !asciiOnly(out) {
			t.Errorf("%dx%d Shell() holds a byte above ASCII:\n%s", size[0], size[1], out)
		}
	}

	tooSmall := ui.Shell(ui.Frame{Header: "Install", Body: body, Width: 50, Height: 24})
	if plain := ansi.Strip(tooSmall); !strings.Contains(plain, "Terminal too small") || !strings.Contains(plain, "Resize to continue") {
		t.Errorf("below-minimum Shell() = %q, want the size notice and its fix", plain)
	}
	if !asciiOnly(tooSmall) {
		t.Errorf("below-minimum Shell() = %q, want ASCII only", tooSmall)
	}
}

// TestUnicodeModeKeepsTheEnhancedGlyphs pins the other half of the glyph
// map (T-06: Unicode is an enhancement, not a dependency): a surface that
// can render it keeps box-drawing borders, pictographic markers and bar
// cells, and the baseline vocabulary stays ASCII in both sets.
func TestUnicodeModeKeepsTheEnhancedGlyphs(t *testing.T) {
	canonicalModes(t)
	theme.SetMode(theme.ModeColor)
	theme.SetGlyphMode(theme.GlyphUnicode)

	if theme.Icon.Done != "✓" || theme.Icon.Chosen != "• " || theme.Icon.FieldBar != "┃ " ||
		theme.Icon.BarFill != '█' || theme.Icon.BarEmpty != '░' {
		t.Fatalf("Unicode glyph map lost its enhancement set: %+v", theme.Icon)
	}

	busy := ansi.Strip(ui.Busy(ui.TextBusyInstallation, "*", ui.Progress{
		Done:      []string{"Checking OpenCode connection"},
		Completed: 1,
		Total:     4,
	}))
	for _, want := range []string{"✓ Checking OpenCode connection", "25%", "█", "░"} {
		if !strings.Contains(busy, want) {
			t.Errorf("Unicode Busy() missing %q:\n%s", want, busy)
		}
	}

	shell := ansi.Strip(ui.Shell(ui.Frame{Header: "Install", Body: "Select AI Agents", Footer: "Back", Width: 80, Height: 24}))
	for _, want := range []string{"┌", "┐", "└", "┘"} {
		if !strings.Contains(shell, want) {
			t.Errorf("Unicode Shell() missing border corner %q:\n%s", want, shell)
		}
	}

	checklist := ansi.Strip(ui.CheckList([]ui.Item{{Label: "OpenCode", Checked: true}}, 0, true))
	if !strings.Contains(checklist, "[x] OpenCode") {
		t.Errorf("Unicode CheckList() = %q, want the baseline ASCII checkbox vocabulary", checklist)
	}
}

// TestNoColorIsNotGlyphProof pins T-06's independence rule: NO_COLOR
// drops color but proves nothing about glyphs, so the same surface renders
// monochrome text with Unicode glyphs intact.
func TestNoColorIsNotGlyphProof(t *testing.T) {
	canonicalModes(t)
	for _, name := range []string{"TERM", "LC_ALL", "LC_CTYPE", "LANG"} {
		t.Setenv(name, "")
	}
	t.Setenv("NO_COLOR", "1")

	if got := theme.DetectGlyphMode(); got != theme.GlyphUnicode {
		t.Fatalf("DetectGlyphMode() under NO_COLOR = %v, want Unicode: no-color is not glyph proof", got)
	}
	if got := theme.DetectMode(); got != theme.ModeMono {
		t.Fatalf("DetectMode() under NO_COLOR = %v, want mono", got)
	}

	theme.SetMode(theme.DetectMode())
	theme.SetGlyphMode(theme.DetectGlyphMode())
	busy := ansi.Strip(ui.Busy(ui.TextBusyInstallation, "*", ui.Progress{
		Done:      []string{"phase one"},
		Completed: 1,
		Total:     4,
	}))
	for _, want := range []string{"✓ phase one", "25%", "█"} {
		if !strings.Contains(busy, want) {
			t.Errorf("mono Busy() missing %q: color fell but glyphs must stay:\n%s", want, busy)
		}
	}
}
