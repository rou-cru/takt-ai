package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// Frame is everything required to draw one screen.
type Frame struct {
	Body       string
	Keys       keys.KeyMap
	Extra      []keys.Binding
	Width      int
	Height     int
	Covered    bool
	HasFooter  bool
	EnterLabel string
}

// Shell composes a body and its keybar inside one frame.
func Shell(frame Frame) string {
	inner := InnerWidth(frame.Width)
	body := strings.TrimRight(frame.Body, "\n")
	if height := BodyHeight(frame.Height); height > 0 {
		body = fitLines(body, height)
	}
	if inner > 0 {
		body = lipgloss.PlaceHorizontal(inner, lipgloss.Center, body)
	}
	keybar := KeyBar(frame.Keys, frame.Extra, KeyBarContext{EnterLabel: frame.EnterLabel, ShowTab: frame.HasFooter}, inner)
	if inner > 0 && strings.TrimSpace(keybar) != "" {
		keybar = lipgloss.PlaceHorizontal(inner, lipgloss.Center, keybar)
	}
	border := theme.BorderFocus
	if frame.Covered {
		border = theme.Border
	}
	style := theme.Frame.BorderForeground(border)
	if frame.Width > 0 {
		style = style.Width(frame.Width)
	}
	out := style.Render(lipgloss.JoinVertical(lipgloss.Left, body, "", keybar))
	if frame.Height > 0 {
		return lipgloss.NewStyle().MaxHeight(frame.Height).Render(out)
	}
	return out
}

const frameOverhead = 2 + 2*theme.SpaceSM
const chromeHeight = 2 + 2*theme.SpaceLG + 2

// BodyHeight returns the usable content height.
func BodyHeight(height int) int {
	if height <= 0 {
		return 0
	}
	if height -= chromeHeight; height > 1 {
		return height
	}
	return 1
}

// InnerWidth returns the width inside the frame's border and padding.
func InnerWidth(width int) int {
	if width <= 0 {
		return 0
	}
	return width - frameOverhead
}

func fitLines(block string, count int) string {
	lines := strings.Split(block, "\n")
	if len(lines) > count {
		return strings.Join(lines[:count], "\n")
	}
	for len(lines) < count {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
