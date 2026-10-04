package styles

import (
	_ "embed"
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// logoPNG is the mark on a transparent background, generated from the
// canonical PNG and drawn as is by terminals that take kitty graphics.
//
//go:embed logo.png
var logoPNG []byte

// logoImageID names the transmitted logo in the terminal, so placing it
// again replaces the placement instead of adding one.
const logoImageID = 7316

// logoPlacementID is the single placement of the logo.
const logoPlacementID = 1

// quietReplies silences every terminal reply to a graphics command, so none
// reaches the TUI's input.
const quietReplies = 2

// TransmitLogo is the sequence storing the logo in the terminal, sent once
// before it is placed.
func TransmitLogo() string {
	payload := base64.StdEncoding.EncodeToString(logoPNG)
	var b strings.Builder
	for start := 0; start < len(payload); start += kitty.MaxChunkSize {
		end := min(start+kitty.MaxChunkSize, len(payload))
		more := "m=0"
		if end < len(payload) {
			more = "m=1"
		}
		options := []string{more}
		if start == 0 {
			options = []string{"a=t", "f=100", "i=" + strconv.Itoa(logoImageID), "q=" + strconv.Itoa(quietReplies), more}
		}
		b.WriteString(ansi.KittyGraphics([]byte(payload[start:end]), options...))
	}
	return b.String()
}

// PlaceLogo is the sequence drawing the stored logo over cols×rows cells
// whose top-left cell is (x, y), zero-based. The cursor is saved and
// restored around it, so the renderer's idea of where it is stays true.
func PlaceLogo(x, y, cols, rows int) string {
	return ansi.SaveCursor + ansi.CursorPosition(x+1, y+1) + ansi.KittyGraphics(nil,
		"a=p",
		"i="+strconv.Itoa(logoImageID),
		"p="+strconv.Itoa(logoPlacementID),
		"c="+strconv.Itoa(cols),
		"r="+strconv.Itoa(rows),
		"C=1",
		"q="+strconv.Itoa(quietReplies),
	) + ansi.RestoreCursor
}

// HideLogo is the sequence removing the logo from the screen; the terminal
// keeps it stored for the next placement.
func HideLogo() string {
	return ansi.KittyGraphics(nil, "a=d", "d=i", "i="+strconv.Itoa(logoImageID), "q="+strconv.Itoa(quietReplies))
}

// ForgetLogo is the sequence removing the logo from the screen and freeing
// it from the terminal's memory, for when the TUI exits.
func ForgetLogo() string {
	return ansi.KittyGraphics(nil, "a=d", "d=I", "i="+strconv.Itoa(logoImageID), "q="+strconv.Itoa(quietReplies))
}
