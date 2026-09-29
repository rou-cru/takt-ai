package ui_test

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func TestToggleAddsMissingItem(t *testing.T) {
	got := ui.Toggle([]string{"a", "b"}, "c")
	want := []string{"a", "b", "c"}
	if !slices.Equal(got, want) {
		t.Errorf("Toggle add = %v, want %v", got, want)
	}
}

func TestToggleRemovesPresentItem(t *testing.T) {
	got := ui.Toggle([]string{"a", "b", "c"}, "b")
	want := []string{"a", "c"}
	if !slices.Equal(got, want) {
		t.Errorf("Toggle remove = %v, want %v", got, want)
	}
}

func TestToggleOnEmptySlice(t *testing.T) {
	got := ui.Toggle([]string(nil), "a")
	want := []string{"a"}
	if !slices.Equal(got, want) {
		t.Errorf("Toggle on empty = %v, want %v", got, want)
	}
}

func TestToggleIntItems(t *testing.T) {
	got := ui.Toggle([]int{1, 2, 3}, 2)
	want := []int{1, 3}
	if !slices.Equal(got, want) {
		t.Errorf("Toggle int remove = %v, want %v", got, want)
	}
}
