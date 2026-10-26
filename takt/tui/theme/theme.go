// Package theme is the single source of visual truth for the TUI: renderers
// ask for a role (TextPrimary, SuccessFg, Caption), never a hex or palette
// name. Dark is the only theme; when the terminal cannot render color,
// every role degrades to plain text while markers and labels carry the
// semantics. Glyphs follow the same rule one capability up: when the
// terminal cannot render Unicode, the central glyph map degrades to ASCII
// while words and layout keep the meaning, so no screen shows a replacement
// character (PR-UX-6).
package theme

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Mode names the rendering capability the surface targets.
type Mode int

// Rendering modes are the closed set of surface capabilities.
const (
	// ModeColor paints the single dark theme with true-color SGR sequences.
	ModeColor Mode = iota
	// ModeColor256 paints the dark theme through the declared xterm-256
	// mapping (Dark256), whose contrast pairs are verified on the mapped values.
	ModeColor256
	// ModeMono renders plain text with no ANSI styling; markers and labels
	// carry every semantic role so focus, selection and state stay readable.
	ModeMono
)

// mode is the active capability; resolve() maps every role through it.
var mode = ModeColor

// SetMode switches the capability and rebuilds every style.
func SetMode(m Mode) { mode = m; resolve() }

// DetectMode reads the environment for the documented no-color fallback. It
// seeds the first frame and stays authoritative afterwards: the program's
// reported color profile (ModeFor) picks the palette, but NO_COLOR and
// TERM=dumb always win over it.
func DetectMode() Mode {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return ModeMono
	}
	return ModeColor
}

// ModeFor maps the terminal's color profile onto a declared mode. Only true
// color and the verified 256-color table are colored: an unverified
// nearest-color downsample is not contrast evidence, so anything below 256
// colors renders monochrome.
func ModeFor(profile colorprofile.Profile) Mode {
	switch profile {
	case colorprofile.TrueColor:
		return ModeColor
	case colorprofile.ANSI256:
		return ModeColor256
	default:
		return ModeMono
	}
}

// Mono reports whether the surface renders without color.
func Mono() bool { return mode == ModeMono }

// Animation reports whether animated indicators may run. Set
// TAKT_NO_ANIMATION to any non-empty value for the documented
// animation-free mode; the ASCII baseline renders either way.
func Animation() bool { return os.Getenv("TAKT_NO_ANIMATION") == "" }

// GlyphMode names the glyph capability the surface targets, independent of
// color and animation.
type GlyphMode int

// Glyph modes are the closed set of glyph capabilities.
const (
	// GlyphUnicode draws box-drawing borders and pictographic markers where
	// the terminal renders them.
	GlyphUnicode GlyphMode = iota
	// GlyphASCII draws every border, marker and bar from plain ASCII, so a
	// terminal that cannot render Unicode stays legible with no replacement
	// characters (PR-UX-6).
	GlyphASCII
)

// glyphMode is the active glyph capability; the glyph map and the panel
// border resolve through it.
var glyphMode = GlyphUnicode

// DetectGlyphMode reads the environment for the documented glyph fallback
// (PR-UX-6). TERM=dumb is explicit terminal-capability information that
// proves an ASCII-only surface and prevails over the locale. Otherwise the
// first set variable of LC_ALL, then LC_CTYPE, then LANG decides, and a
// locale that does not declare the UTF-8 codeset falls back to ASCII: a
// wrong Unicode guess draws replacement characters, while a wrong ASCII
// guess only draws plainer text.
func DetectGlyphMode() GlyphMode {
	if os.Getenv("TERM") == "dumb" {
		return GlyphASCII
	}
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if locale := os.Getenv(name); locale != "" {
			if !localeDeclaresUTF8(locale) {
				return GlyphASCII
			}
			return GlyphUnicode
		}
	}
	return GlyphUnicode
}

// localeDeclaresUTF8 reports whether a locale names the UTF-8 codeset:
// either the whole value ("UTF-8", as some terminals set LC_CTYPE) or the
// codeset after the territory suffix, before any "@" modifier
// ("en_US.UTF-8", "C.utf8").
func localeDeclaresUTF8(locale string) bool {
	codeset := locale
	if _, after, found := strings.Cut(locale, "."); found {
		codeset = after
	}
	codeset, _, _ = strings.Cut(codeset, "@")
	switch strings.ToUpper(codeset) {
	case "UTF-8", "UTF8":
		return true
	}
	return false
}

// SetGlyphMode switches the glyph capability and rebuilds the glyph map and
// every style. It is the entrypoint for proven capability: a caller that
// has proven the terminal ASCII-only sets GlyphASCII, and that proof
// prevails over the locale-derived mode. No color profile proves glyphs,
// because NO_COLOR also produces an ASCII color profile.
func SetGlyphMode(m GlyphMode) { glyphMode = m; resolve() }

// Palette holds one theme's tokens so renderers ask for roles, never hex values.
type Palette struct {
	Canvas, Surface, TextPrimary, TextSecondary, TextMuted, Signature,
	FocusRing, InfoFg, BorderControl, BorderSubtle, SelectionFg, SelectionBg,
	SuccessFg, WarningFg, DangerFg, DisabledBg, DisabledFg,
	ActionDangerBg, ActionDangerFg color.Color
}

// Dark maps the brand's dark semantic tokens onto the terminal (brand.md
// §5.2–5.3 dark column; canvas and signature per VDS_TUI.md §3).
var Dark = Palette{
	// The terminal canvas is the VDS's neutral black, darker than bg.canvas.
	Canvas:  lipgloss.Color("#0D0D0D"),
	Surface: lipgloss.Color("#18262C"), // N900
	// text.primary, text.secondary, text.muted: N50, N200, N300.
	TextPrimary: lipgloss.Color("#F5F7F7"), TextSecondary: lipgloss.Color("#D2DCDD"), TextMuted: lipgloss.Color("#ADBDC0"),
	// The signature is P400, kept apart from focus.ring (P300).
	Signature: lipgloss.Color("#4BA3B0"),
	FocusRing: lipgloss.Color("#80C3CB"), InfoFg: lipgloss.Color("#80C3CB"),
	BorderControl: lipgloss.Color("#82979C"), BorderSubtle: lipgloss.Color("#35464D"),
	SelectionFg: lipgloss.Color("#D9EEF0"), SelectionBg: lipgloss.Color("#1E3B46"),
	SuccessFg: lipgloss.Color("#6EE7B7"), WarningFg: lipgloss.Color("#FCD34D"), DangerFg: lipgloss.Color("#FCA5A5"),
	DisabledBg: lipgloss.Color("#25343A"), DisabledFg: lipgloss.Color("#ADBDC0"),
	ActionDangerBg: lipgloss.Color("#FCA5A5"), ActionDangerFg: lipgloss.Color("#450A0A"),
}

// Dark256 is the declared xterm-256 mapping of Dark: each token's nearest
// palette index, kept only because every contrast pair still holds on the
// mapped values (theme_test.go recomputes them).
var Dark256 = Palette{
	Canvas: lipgloss.ANSIColor(232), Surface: lipgloss.ANSIColor(235),
	TextPrimary: lipgloss.ANSIColor(255), TextSecondary: lipgloss.ANSIColor(253), TextMuted: lipgloss.ANSIColor(250),
	Signature: lipgloss.ANSIColor(73),
	FocusRing: lipgloss.ANSIColor(110), InfoFg: lipgloss.ANSIColor(110),
	BorderControl: lipgloss.ANSIColor(246), BorderSubtle: lipgloss.ANSIColor(238),
	SelectionFg: lipgloss.ANSIColor(254), SelectionBg: lipgloss.ANSIColor(236),
	SuccessFg: lipgloss.ANSIColor(79), WarningFg: lipgloss.ANSIColor(221), DangerFg: lipgloss.ANSIColor(217),
	DisabledBg: lipgloss.ANSIColor(236), DisabledFg: lipgloss.ANSIColor(250),
	ActionDangerBg: lipgloss.ANSIColor(217), ActionDangerFg: lipgloss.ANSIColor(52),
}

// Role colors resolve from the active palette so screens never repeat tokens.
var (
	Canvas, Surface, TextPrimary, TextSecondary, TextMuted, Signature,
	FocusRing, InfoFg, BorderControl, BorderSubtle, SelectionFg, SelectionBg,
	SuccessFg, WarningFg, DangerFg, DisabledBg, DisabledFg,
	ActionDangerBg, ActionDangerFg color.Color

	// SignatureText marks the "Takt AI" wordmark.
	SignatureText lipgloss.Style
	// Title marks headings with primary weight.
	Title lipgloss.Style
	// Label marks content text.
	Label lipgloss.Style
	// Secondary marks supporting content one step below Label.
	Secondary lipgloss.Style
	// Caption marks supporting text so it stays readable without faint styling.
	Caption lipgloss.Style
	// Focus marks the focus gutter so the cursor has one color.
	Focus lipgloss.Style
	// Selected marks persistent selection so choice survives focus moves.
	Selected lipgloss.Style
	// Checked marks a checked checklist label; CheckMarker its [x]/[ ] marker.
	Checked, CheckMarker lipgloss.Style
	// StatusSuccess, StatusWarning, StatusDanger and StatusInfo color a state
	// label with weight; SuccessText, WarningText and DangerText color running
	// text (a consequence, a notice) without it.
	StatusSuccess, StatusWarning, StatusDanger, StatusInfo lipgloss.Style
	SuccessText, WarningText, DangerText                   lipgloss.Style
	// Strong marks an inline identifier that leads its block (a file path).
	Strong lipgloss.Style
	// Panel wraps a body in an untitled bordered panel (bg.surface, border.control).
	Panel lipgloss.Style
	// Button marks a secondary action: text.secondary, no fill.
	Button lipgloss.Style
	// ButtonFocus marks the focused action (canvas text on focus.ring fill).
	ButtonFocus lipgloss.Style
	// ButtonDanger marks an unfocused destructive action: danger.fg, no fill.
	ButtonDanger lipgloss.Style
	// ButtonDangerFocus marks a focused destructive action (action.danger pair).
	ButtonDangerFocus lipgloss.Style
	// ButtonDisabled marks an unavailable action (disabled pair), focused or not.
	ButtonDisabled lipgloss.Style
)

// active is the palette the current mode paints with.
func active() Palette {
	if mode == ModeColor256 {
		return Dark256
	}
	return Dark
}

// role resolves a raw color through the active mode so mono is plain text.
func role(c color.Color) color.Color {
	if Mono() {
		return lipgloss.NoColor{}
	}
	return c
}

// init resolves roles, styles and glyphs so no renderer repeats tokens.
func init() {
	SetMode(DetectMode())
	SetGlyphMode(DetectGlyphMode())
}

// resolve rebuilds every exported role, style and glyph from the active
// palette and glyph set.
func resolve() {
	Icon = glyphSet()
	p := active()
	Canvas, Surface = role(p.Canvas), role(p.Surface)
	TextPrimary, TextSecondary, TextMuted = role(p.TextPrimary), role(p.TextSecondary), role(p.TextMuted)
	Signature, FocusRing, InfoFg = role(p.Signature), role(p.FocusRing), role(p.InfoFg)
	BorderControl, BorderSubtle = role(p.BorderControl), role(p.BorderSubtle)
	SelectionFg, SelectionBg = role(p.SelectionFg), role(p.SelectionBg)
	SuccessFg, WarningFg, DangerFg = role(p.SuccessFg), role(p.WarningFg), role(p.DangerFg)
	DisabledBg, DisabledFg = role(p.DisabledBg), role(p.DisabledFg)
	ActionDangerBg, ActionDangerFg = role(p.ActionDangerBg), role(p.ActionDangerFg)

	// Bold marks hierarchy; in mono only order and labels carry it.
	bold := !Mono()
	text := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	SignatureText = text(Signature).Bold(bold)
	Title = text(TextPrimary).Bold(bold)
	Label = text(TextPrimary)
	Secondary = text(TextSecondary)
	Caption = text(TextMuted)
	Focus = text(FocusRing)
	Selected = text(SelectionFg).Background(SelectionBg)
	Checked = text(SuccessFg)
	CheckMarker = text(TextMuted)
	StatusSuccess = text(SuccessFg).Bold(bold)
	StatusWarning = text(WarningFg).Bold(bold)
	StatusDanger = text(DangerFg).Bold(bold)
	StatusInfo = text(InfoFg).Bold(bold)
	SuccessText, WarningText, DangerText = text(SuccessFg), text(WarningFg), text(DangerFg)
	Strong = text(TextPrimary).Bold(bold)
	Panel = text(TextPrimary).Background(Surface).Padding(0, 1).
		Border(Icon.Border).BorderForeground(BorderControl)
	Button = text(TextSecondary)
	ButtonFocus = text(Canvas).Background(FocusRing).Bold(bold)
	ButtonDanger = text(DangerFg)
	ButtonDangerFocus = text(ActionDangerFg).Background(ActionDangerBg).Bold(bold)
	ButtonDisabled = text(DisabledFg).Background(DisabledBg)
}

// Glyphs is the central glyph map: the one definition of every border,
// marker and bar character the TUI renders, so one state never renders as
// two marks and a capability change never edits a screen. Words and layout
// carry the meaning whenever two glyphs of a set coincide.
type Glyphs struct {
	// Cursor is the focus-gutter marker; FieldBar marks a focused text field.
	Cursor, FieldBar string
	// Done, Failed and Unchecked mark completed, failed and unverified steps.
	Done, Failed, Unchecked string
	// Chosen and Unchosen mark the selected and the unselected value of one
	// single choice; CheckboxOn and CheckboxOff mark multiple selection.
	Chosen, Unchosen, CheckboxOn, CheckboxOff string
	// BarFill and BarEmpty draw a progress bar's completed and open cells.
	BarFill, BarEmpty rune
	// Border draws a panel.
	Border lipgloss.Border
}

// unicodeGlyphs is the enhancement set: box-drawing borders and
// pictographic markers, no Nerd Font glyphs.
var unicodeGlyphs = Glyphs{
	Cursor:      "> ",
	Done:        "✓",
	Failed:      "✗",
	Unchecked:   "?",
	Chosen:      "• ",
	Unchosen:    "  ",
	CheckboxOn:  "[x]",
	CheckboxOff: "[ ]",
	FieldBar:    "┃ ",
	BarFill:     '█',
	BarEmpty:    '░',
	Border: lipgloss.Border{
		Top: "─", Bottom: "─", Left: "│", Right: "│",
		TopLeft: "┌", TopRight: "┐", BottomLeft: "└", BottomRight: "┘",
	},
}

// asciiGlyphs is the fallback set (PR-UX-6): every character is plain ASCII,
// on the approved control vocabulary — > focus, [x]/[ ] multiple selection,
// | field focus, x completed step, * active value — with borders, markers
// and bars in their legible ASCII forms. Done and Failed keep distinct
// marks, and the state words stay distinct beside them.
var asciiGlyphs = Glyphs{
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

// Icon is the active glyph map; screens read it at render time, so a glyph
// mode change reaches every screen without editing it.
var Icon Glyphs

// glyphSet returns the glyph map of the active glyph mode.
func glyphSet() Glyphs {
	if glyphMode == GlyphASCII {
		return asciiGlyphs
	}
	return unicodeGlyphs
}
