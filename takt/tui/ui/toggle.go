package ui

import "slices"

// Toggle flips one selection so checklist flows share a single rule.
func Toggle[T comparable](items []T, item T) []T {
	if index := slices.Index(items, item); index >= 0 {
		return slices.Delete(items, index, index+1)
	}
	return append(items, item)
}
