package tui_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// renderOutEnv names the file the re-executed test binary writes its render to.
const renderOutEnv = "TAKT_GLYPH_RENDER_OUT"

// userFileName is user-provided content: it keeps its bytes in every glyph mode.
const userFileName = "配置.md"

// glyphInputs are the variables the theme reads at init to pick its glyphs.
var glyphInputs = []string{"TERM", "LC_ALL", "LC_CTYPE", "LANG", "NO_COLOR"}

// TestGlyphRenderHelper is the re-executed half of TestGlyphFallbackFollowsTheTerminal:
// the theme resolves glyphs once at init, so each environment needs its own process.
func TestGlyphRenderHelper(t *testing.T) {
	out := os.Getenv(renderOutEnv)
	if out == "" {
		t.Skip("runs only as the subprocess of TestGlyphFallbackFollowsTheTerminal")
	}
	body := strings.Join([]string{
		ui.Status(ui.StateFailed, "cannot reach OpenCode"),
		ui.Options([]string{"first", "second"}, 1, true),
		ui.CheckList([]ui.Item{{Label: "OpenCode", Checked: true}, {Label: "Skills"}}, 0, true),
		ui.Busy(ui.TextBusyInstallation, "*", ui.Progress{
			Done:      []string{"Checking OpenCode connection"},
			Message:   "Applying installation files",
			Current:   userFileName,
			Completed: 1,
			Total:     4,
		}),
	}, "\n")
	footer := ui.FooterActions(ui.Actions(ui.TextActionBack), 0, true)
	var screens []string
	for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}} {
		screens = append(screens, ui.Shell(ui.Frame{Header: "Install", Body: body, Footer: footer, Width: size[0], Height: size[1]}))
	}
	if err := os.WriteFile(out, []byte(strings.Join(screens, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}

// renderUnder re-executes the test binary with only env as glyph input and
// returns the composed screens it rendered.
func renderUnder(t *testing.T, env map[string]string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "render.txt")
	cmd := exec.Command(os.Args[0], "-test.run", "^TestGlyphRenderHelper$")
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(glyphInputs, name) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, renderOutEnv+"="+out)
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("render subprocess: %v\n%s", err, combined)
	}
	rendered, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(rendered)
}

// asciiOnly reports whether s holds no byte above ASCII.
func asciiOnly(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return r >= utf8.RuneSelf })
}

// TestGlyphFallbackFollowsTheTerminal pins PR-UX-6 end to end: TERM=dumb or
// a locale without the UTF-8 codeset renders every generated character in
// ASCII, a UTF-8 locale or none keeps the Unicode enhancement, and the
// user's file name stays verbatim in both.
func TestGlyphFallbackFollowsTheTerminal(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		ascii bool
	}{
		{"dumb terminal wins over a UTF-8 locale", map[string]string{"TERM": "dumb", "LANG": "en_US.UTF-8"}, true},
		{"C locale", map[string]string{"LANG": "C"}, true},
		{"UTF-8 locale", map[string]string{"LANG": "en_US.UTF-8"}, false},
		{"LC_ALL decides over LANG", map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, true},
		{"unset locale", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rendered := renderUnder(t, tc.env)
			if !strings.Contains(rendered, userFileName) || strings.Contains(rendered, "\uFFFD") {
				t.Fatalf("render must keep %q verbatim with no replacement character:\n%s", userFileName, rendered)
			}
			generated := ansi.Strip(strings.ReplaceAll(rendered, userFileName, ""))
			if got := asciiOnly(generated); got != tc.ascii {
				t.Fatalf("generated characters ASCII-only = %v, want %v:\n%s", got, tc.ascii, generated)
			}
		})
	}
}
