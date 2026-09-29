package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/tui/keys"
)

// pageScrollDivisor makes paging move by half the available body height.
const pageScrollDivisor = 2

// MoveCursor wraps the cursor within count.
func MoveCursor(cursor, count, delta int) int {
	if count <= 0 {
		return cursor
	}
	cursor += delta
	if cursor < 0 {
		return count - 1
	}
	if cursor >= count {
		return 0
	}
	return cursor
}

// Nudge moves *cursor within n rows on Up or Down.
func Nudge(cursor *int, n int, km keys.KeyMap, msg tea.Msg) bool {
	switch {
	case km.Up.Matches(msg):
		*cursor = MoveCursor(*cursor, n, -1)
	case km.Down.Matches(msg):
		*cursor = MoveCursor(*cursor, n, 1)
	default:
		return false
	}
	return true
}

// NudgeHorizontal moves a cursor across options rendered in one row.
func NudgeHorizontal(cursor *int, n int, km keys.KeyMap, msg tea.Msg) bool {
	switch {
	case km.Left.Matches(msg):
		*cursor = MoveCursor(*cursor, n, -1)
	case km.Right.Matches(msg):
		*cursor = MoveCursor(*cursor, n, 1)
	default:
		return false
	}
	return true
}

// Scroll applies pgup and pgdown for a screen of the given height.
func Scroll(scroll, height int, key string) (int, bool) {
	page := max(1, BodyHeight(height)/pageScrollDivisor)
	switch key {
	case "pgdown":
		return scroll + page, true
	case "pgup":
		return max(0, scroll-page), true
	}
	return scroll, false
}

// BackMsg asks the controller to pop the active flow.
type BackMsg struct{}

// Back requests navigation to the previous screen.
func Back() tea.Msg { return BackMsg{} }

// Dirtier marks flows with unapplied drafts.
type Dirtier interface{ Dirty() bool }
