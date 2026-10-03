package theme_test

import (
	"image/color"
	"math"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

func TestRolesResolveToDarkTokens(t *testing.T) {
	theme.SetMode(theme.ModeColor)
	if theme.TextPrimary != theme.Dark.TextPrimary || theme.SuccessFg != theme.Dark.SuccessFg {
		t.Fatal("role colors did not resolve to the dark palette")
	}
}

func TestModeForOnlyColorsVerifiedProfiles(t *testing.T) {
	for profile, want := range map[colorprofile.Profile]theme.Mode{
		colorprofile.TrueColor: theme.ModeColor,
		colorprofile.ANSI256:   theme.ModeColor256,
		colorprofile.ANSI:      theme.ModeMono,
		colorprofile.ASCII:     theme.ModeMono,
		colorprofile.NoTTY:     theme.ModeMono,
	} {
		if got := theme.ModeFor(profile); got != want {
			t.Errorf("ModeFor(%v) = %v, want %v", profile, got, want)
		}
	}
}

// wcagMinText and wcagMinGraphic are the internal targets of brand.md §5.4.
const (
	wcagMinText    = 4.5
	wcagMinGraphic = 3.0
)

// Each palette the TUI paints with must hold every pair it renders, measured
// on the values it actually emits: for Dark256 that is the xterm palette
// entry, not the true-color token it approximates.
func TestPalettesHoldTheirContrastPairs(t *testing.T) {
	for name, p := range map[string]theme.Palette{"Dark": theme.Dark, "Dark256": theme.Dark256} {
		pairs := []struct {
			fg, bg  color.Color
			minimum float64
			label   string
		}{
			{p.TextPrimary, p.Canvas, wcagMinText, "text.primary/canvas"},
			{p.TextPrimary, p.Surface, wcagMinText, "text.primary/surface"},
			{p.TextSecondary, p.Canvas, wcagMinText, "text.secondary/canvas"},
			{p.TextSecondary, p.Surface, wcagMinText, "text.secondary/surface"},
			{p.TextMuted, p.Canvas, wcagMinText, "text.muted/canvas"},
			{p.TextMuted, p.Surface, wcagMinText, "text.muted/surface"},
			{p.Signature, p.Canvas, wcagMinText, "signature/canvas"},
			{p.Canvas, p.FocusRing, wcagMinText, "focused button"},
			{p.FocusRing, p.Surface, wcagMinGraphic, "focus/surface"},
			{p.SuccessFg, p.Surface, wcagMinText, "success/surface"},
			{p.WarningFg, p.Surface, wcagMinText, "warning/surface"},
			{p.DangerFg, p.Surface, wcagMinText, "danger/surface"},
			{p.InfoFg, p.Surface, wcagMinText, "info/surface"},
			{p.SelectionFg, p.SelectionBg, wcagMinText, "selection"},
			{p.DisabledFg, p.DisabledBg, wcagMinText, "disabled"},
			{p.ActionDangerFg, p.ActionDangerBg, wcagMinText, "action.danger"},
			{p.BorderControl, p.Surface, wcagMinGraphic, "border.control/surface"},
			{p.BorderControl, p.Canvas, wcagMinGraphic, "border.control/canvas"},
		}
		for _, pair := range pairs {
			if got := contrast(pair.fg, pair.bg); got < pair.minimum {
				t.Errorf("%s %s = %.2f:1, want at least %.1f:1", name, pair.label, got, pair.minimum)
			}
		}
	}
}

func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	linear := func(v uint32) float64 {
		s := float64(v>>8) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}
