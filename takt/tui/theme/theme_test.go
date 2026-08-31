package theme_test

import (
	"image/color"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

func TestSemanticTokensUseRosePine(t *testing.T) {
	for name, token := range map[string]struct {
		got  color.Color
		want color.RGBA
	}{
		"primary": {theme.Primary, color.RGBA{196, 167, 231, 255}},
		"accent":  {theme.Accent, color.RGBA{235, 188, 186, 255}},
		"text":    {theme.Text, color.RGBA{224, 222, 244, 255}},
		"success": {theme.Success, color.RGBA{156, 207, 216, 255}},
		"warning": {theme.Warning, color.RGBA{246, 193, 119, 255}},
		"danger":  {theme.Danger, color.RGBA{235, 111, 146, 255}},
	} {
		red, green, blue, alpha := token.got.RGBA()
		got := color.RGBA{uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8), uint8(alpha >> 8)}
		if got != token.want {
			t.Errorf("%s = %#v, want %#v", name, got, token.want)
		}
	}
	if theme.Border != theme.Muted || theme.BorderFocus != theme.Primary {
		t.Fatal("border tokens must follow muted and primary roles")
	}
}
