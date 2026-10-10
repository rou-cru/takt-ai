package testutil

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/rou-cru/takt-ai/takt/tui/styles"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// fixtureSizes are the VDS §6 validation sizes plus a wide, low terminal.
var fixtureSizes = [][2]int{{120, 32}, {80, 24}, {60, 20}, {160, 24}}

// fixtureModes are the declared color capabilities (VDS §3).
var fixtureModes = []struct {
	name string
	mode theme.Mode
}{
	{"truecolor", theme.ModeColor},
	{"ansi256", theme.ModeColor256},
	{"mono", theme.ModeMono},
}

// RequireFixtures compares render's output with the committed golden file for
// every size and color mode: testdata/<Test>/<mode>/<W>x<H>.golden. render must
// build its screen inside the call, after the mode is set, so styles resolve
// for that mode. Regenerate with `go test ./takt/tui/... -update` and review
// before committing.
func RequireFixtures(t *testing.T, render func(width, height int) string) {
	t.Helper()
	t.Cleanup(func() { theme.SetMode(theme.ModeColor); styles.SetLogoMode(styles.ModeQuadrants) })
	// Quadrants: the logo every terminal draws, independent of the runner's terminal.
	styles.SetLogoMode(styles.ModeQuadrants)
	for _, mode := range fixtureModes {
		for _, size := range fixtureSizes {
			t.Run(fmt.Sprintf("%s/%dx%d", mode.name, size[0], size[1]), func(t *testing.T) {
				theme.SetMode(mode.mode)
				golden.RequireEqual(t, render(size[0], size[1]))
			})
		}
	}
}
