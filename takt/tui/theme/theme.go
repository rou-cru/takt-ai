// Package theme is the single source of visual truth for the TUI: renderers
// ask for a role (TextPrimary, SuccessFg, Caption), never a hex or palette
// name. Dark is the only theme; when the terminal cannot render color,
// every role degrades to plain text while markers and labels carry the semantics.
package theme

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// Mode names the rendering capability the surface targets.
type Mode int

// Rendering modes are the closed set of surface capabilities.
const (
	// ModeColor paints the single dark theme with true-color SGR sequences.
	ModeColor Mode = iota
	// ModeMono renders plain text with no ANSI styling; markers and labels
	// carry every semantic role so focus, selection and state stay readable.
	ModeMono
)

// mode is the active capability; resolve() maps every role through it.
var mode = ModeColor

// SetMode switches the capability and rebuilds every style. Call once at
// startup before any rendering; the environment default is DetectMode.
func SetMode(m Mode) { mode = m; resolve() }

// DetectMode reads the environment for the documented no-color fallback.
func DetectMode() Mode {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return ModeMono
	}
	return ModeColor
}

// Mono reports whether the surface renders without color.
func Mono() bool { return mode == ModeMono }

// Animation reports whether animated indicators may run. Set
// TAKT_NO_ANIMATION to any non-empty value for the documented
// animation-free mode; the ASCII baseline renders either way.
func Animation() bool { return os.Getenv("TAKT_NO_ANIMATION") == "" }

// Palette holds one theme's tokens so renderers ask for roles, never hex values.
type Palette struct {
	Canvas, Surface, TextPrimary, TextMuted, BrandInk, FocusRing,
	BorderControl, BorderSubtle, SelectionFg, SelectionBg, SuccessFg,
	WarningFg, DangerFg, DisabledBg, DisabledFg,
	ActionDangerBg, ActionDangerFg color.Color
}

// hexBrandTeal is the brand's teal accent (brand.md section 5.3, dark
// column), shared by every role that carries brand emphasis.
const hexBrandTeal = "#80C3CB"

// Dark maps the brand's dark semantic token set onto the terminal. Hex
// values come from brand.md section 5.3 (dark column).
var Dark = Palette{
	Canvas: lipgloss.Color("#101B20"), Surface: lipgloss.Color("#18262C"),
	TextPrimary: lipgloss.Color("#F5F7F7"), TextMuted: lipgloss.Color("#ADBDC0"),
	BrandInk: lipgloss.Color(hexBrandTeal), FocusRing: lipgloss.Color(hexBrandTeal),
	BorderControl: lipgloss.Color("#82979C"), BorderSubtle: lipgloss.Color("#35464D"),
	SelectionFg: lipgloss.Color("#D9EEF0"), SelectionBg: lipgloss.Color("#1E3B46"),
	SuccessFg: lipgloss.Color("#6EE7B7"),
	WarningFg: lipgloss.Color("#FCD34D"),
	DangerFg: lipgloss.Color("#FCA5A5"),
	DisabledBg: lipgloss.Color("#25343A"), DisabledFg: lipgloss.Color("#ADBDC0"),
	ActionDangerBg: lipgloss.Color("#FCA5A5"), ActionDangerFg: lipgloss.Color("#450A0A"),
}

// Role colors resolve once from Dark so screens never repeat hex values.
var (
	Canvas, Surface, TextPrimary, TextMuted, BrandInk, FocusRing,
	BorderControl, BorderSubtle, SelectionFg, SelectionBg, SuccessFg,
	WarningFg, DangerFg, DisabledBg, DisabledFg,
	ActionDangerBg, ActionDangerFg color.Color

	// Signature marks the "Takt AI" wordmark in the header.
	Signature lipgloss.Style
	// Title marks headings with primary weight.
	Title lipgloss.Style
	// Label marks content text.
	Label lipgloss.Style
	// Caption marks supporting text so it stays readable without faint styling.
	Caption lipgloss.Style
	// Focus marks the focus gutter so the cursor has one color.
	Focus lipgloss.Style
	// Selected marks persistent selection so choice survives focus moves.
	Selected lipgloss.Style
	// Panel wraps a body in an untitled bordered panel (bg.surface, border.control).
	Panel lipgloss.Style
	// Button marks a secondary action (text.primary on border.subtle fill).
	Button lipgloss.Style
	// ButtonFocus marks the focused action (canvas text on focus.ring fill).
	ButtonFocus lipgloss.Style
	// ButtonDanger marks a destructive action (action.danger pair).
	ButtonDanger lipgloss.Style
	// ButtonDisabled marks an unavailable action (disabled pair).
	ButtonDisabled lipgloss.Style
)

// role resolves a raw color through the active mode so mono is plain text.
func role(c color.Color) color.Color {
	if Mono() {
		return lipgloss.NoColor{}
	}
	return c
}

// init resolves roles and styles from Dark so no renderer repeats tokens.
func init() {
	SetMode(DetectMode())
}

// resolve rebuilds every exported style from Dark and the active mode.
func resolve() {
	Canvas, Surface = role(Dark.Canvas), role(Dark.Surface)
	TextPrimary, TextMuted = role(Dark.TextPrimary), role(Dark.TextMuted)
	BrandInk, FocusRing = role(Dark.BrandInk), role(Dark.FocusRing)
	BorderControl, BorderSubtle = role(Dark.BorderControl), role(Dark.BorderSubtle)
	SelectionFg, SelectionBg = role(Dark.SelectionFg), role(Dark.SelectionBg)
	SuccessFg = role(Dark.SuccessFg)
	WarningFg = role(Dark.WarningFg)
	DangerFg = role(Dark.DangerFg)
	DisabledBg, DisabledFg = role(Dark.DisabledBg), role(Dark.DisabledFg)
	ActionDangerBg, ActionDangerFg = role(Dark.ActionDangerBg), role(Dark.ActionDangerFg)

	// Bold marks hierarchy; in mono only order and labels carry it.
	bold := !Mono()
	Signature = lipgloss.NewStyle().Foreground(BrandInk).Bold(bold)
	Title = lipgloss.NewStyle().Foreground(TextPrimary).Bold(bold)
	Label = lipgloss.NewStyle().Foreground(TextPrimary)
	Caption = lipgloss.NewStyle().Foreground(TextMuted)
	Focus = lipgloss.NewStyle().Foreground(FocusRing)
	Selected = lipgloss.NewStyle().Foreground(SelectionFg).Background(SelectionBg)
	Panel = lipgloss.NewStyle().Foreground(TextPrimary).Background(Surface).
		Border(lipgloss.Border{
			Top: "─", Bottom: "─", Left: "│", Right: "│",
			TopLeft: "┌", TopRight: "┐", BottomLeft: "└", BottomRight: "┘",
		}).BorderForeground(BorderControl)
	Button = lipgloss.NewStyle().Foreground(TextPrimary).Background(BorderSubtle)
	ButtonFocus = lipgloss.NewStyle().Foreground(Canvas).Background(FocusRing).Bold(bold)
	ButtonDanger = lipgloss.NewStyle().Foreground(ActionDangerFg).Background(ActionDangerBg)
	ButtonDisabled = lipgloss.NewStyle().Foreground(DisabledFg).Background(DisabledBg)
}

// Icon centralizes glyphs so one state never renders as two marks. The
// baseline vocabulary stays ASCII; FieldBar is a box-drawing bar, not a
// Nerd Font glyph.
var Icon = struct {
	Cursor      string
	CheckboxOn  string
	CheckboxOff string
	FieldBar    string
}{
	Cursor:      "> ",
	CheckboxOn:  "[x]",
	CheckboxOff: "[ ]",
	FieldBar:    "┃ ",
}
