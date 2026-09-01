// Package common contains shared TUI state that is independent of a lifecycle flow.
package common

import "slices"

// Toggle removes item from items if present, otherwise appends it.
func Toggle[T comparable](items []T, item T) []T {
	if index := slices.Index(items, item); index >= 0 {
		return slices.Delete(items, index, index+1)
	}
	return append(items, item)
}
