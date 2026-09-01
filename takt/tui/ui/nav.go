package ui

// MoveCursor applies up/down wraparound.
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
