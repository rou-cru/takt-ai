package tui

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/tui/styles"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// Only real file streams are measured; the override decides here because a
// test has no terminal to answer.
func TestMeasureLogoModeUsesFileStreamsOnly(t *testing.T) {
	t.Cleanup(func() { styles.SetLogoMode(styles.ModeQuadrants); theme.SetMode(theme.DetectMode()) })
	theme.SetMode(theme.ModeColor)
	t.Setenv("TAKT_LOGO", "image")

	// A run that cannot probe drops what an earlier run measured.
	styles.SetLogoMode(styles.ModeImage)
	measureLogoMode(&bytes.Buffer{}, &bytes.Buffer{})
	if styles.ImageLogo() {
		t.Fatal("non-file streams kept the image logo")
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devNull.Close() })
	measureLogoMode(devNull, devNull)
	if !styles.ImageLogo() {
		t.Fatal("file streams with the image override did not pick the image logo")
	}
}

// imageHome is a home screen whose logo is drawn as an image.
func imageHome(t *testing.T) Model {
	t.Helper()
	t.Cleanup(func() { styles.SetLogoMode(styles.ModeQuadrants); theme.SetMode(theme.DetectMode()) })
	theme.SetMode(theme.ModeColor)
	styles.SetLogoMode(styles.ModeImage)
	return New(t.TempDir())
}

// rawOutput runs cmd and every command it batches and returns the raw
// sequences they send to the terminal, plus whether one scheduled a logo
// refresh.
func rawOutput(cmd tea.Cmd) (raw string, refresh bool) {
	if cmd == nil {
		return "", false
	}
	switch msg := cmd().(type) {
	case logoRefreshMsg:
		return "", true
	case tea.RawMsg:
		return msg.Msg.(string), false
	case tea.BatchMsg:
		for _, inner := range msg {
			r, tick := rawOutput(inner)
			raw, refresh = raw+r, refresh || tick
		}
	}
	return raw, refresh
}

func TestInitTransmitsTheImageOnlyWhenDrawingIt(t *testing.T) {
	app := imageHome(t)
	if raw, _ := rawOutput(app.Init()); raw != styles.TransmitLogo() {
		t.Error("Init() in image mode did not transmit the logo")
	}
	styles.SetLogoMode(styles.ModeQuadrants)
	if cmd := app.Init(); cmd != nil {
		t.Error("Init() with quadrants has startup work")
	}
}

// The home places the image over the cells it reserves, once, and again
// after a resize; leaving the home hides it.
func TestHomeKeepsTheImageOverItsReservedCells(t *testing.T) {
	app := imageHome(t)
	next, cmd := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = next.(Model)
	at := findLogo(app.View().Content)
	if at.cols == 0 || at.rows == 0 {
		t.Fatalf("home reserves no logo cells:\n%s", app.View().Content)
	}
	raw, timer := rawOutput(cmd)
	if want := styles.PlaceLogo(at.x, at.y, at.cols, at.rows); raw != want || !timer {
		t.Fatalf("resize sent %q (refresh scheduled %v), want %q and a refresh", raw, timer, want)
	}

	next, cmd = app.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	app = next.(Model)
	if raw, _ := rawOutput(cmd); raw != "" {
		t.Errorf("moving the cursor re-placed the unmoved logo: %q", raw)
	}
	if app.logoAt != at || findLogo(app.View().Content) != at {
		t.Errorf("moving the cursor moved the logo: kept %+v, rendered %+v, want %+v", app.logoAt, findLogo(app.View().Content), at)
	}

	next, cmd = app.Update(logoRefreshMsg{})
	app = next.(Model)
	if raw, _ := rawOutput(cmd); raw != styles.PlaceLogo(at.x, at.y, at.cols, at.rows) {
		t.Errorf("refresh sent %q, want the placement again", raw)
	}

	app.cursor = 0
	next, cmd = app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app = next.(Model)
	if app.active() == nil {
		t.Fatal("enter on the first item did not open a flow")
	}
	if raw, _ := rawOutput(cmd); !strings.Contains(raw, styles.HideLogo()) {
		t.Errorf("opening a flow sent %q, want the logo hidden", raw)
	}
}

func TestFindLogoLocatesTheReservedBlock(t *testing.T) {
	cell := string(styles.LogoCell)
	content := "header\n  \x1b[1m" + cell + cell + cell + "\x1b[0m  menu\n  " + cell + cell + cell + "\nfooter"
	if got, want := findLogo(content), (logoRect{x: 2, y: 1, cols: 3, rows: 2}); got != want {
		t.Errorf("findLogo() = %+v, want %+v", got, want)
	}
	if got := findLogo("no logo here"); got != (logoRect{}) {
		t.Errorf("findLogo(no logo) = %+v, want zero", got)
	}
}

// Quitting frees the image before the program ends.
func TestQuitForgetsTheImage(t *testing.T) {
	imageHome(t)
	steps := reflect.ValueOf(exitProgram()())
	if steps.Kind() != reflect.Slice || steps.Len() != 2 {
		t.Fatalf("exitProgram() = %v, want a raw sequence then the quit", steps)
	}
	if raw, _ := rawOutput(steps.Index(0).Interface().(tea.Cmd)); raw != styles.ForgetLogo() {
		t.Errorf("exitProgram() first sends %q, want the logo freed", raw)
	}
	if _, quits := steps.Index(1).Interface().(tea.Cmd)().(tea.QuitMsg); !quits {
		t.Error("exitProgram() does not quit")
	}
}
