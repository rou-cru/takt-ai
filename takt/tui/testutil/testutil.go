// Package testutil provides shared test helpers for the TUI test suites.
package testutil

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

// ActionRequest extracts a runtime request from a command so tests share one unwrapping rule.
func ActionRequest(t *testing.T, cmd tea.Cmd) runtime.ActionRequest {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil command")
	}
	if request, ok := cmd().(runtime.ActionRequest); ok {
		return request
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, sub := range batch {
			if request, ok := sub().(runtime.ActionRequest); ok {
				return request
			}
		}
	}
	t.Fatalf("command did not emit runtime.ActionRequest")
	return runtime.ActionRequest{}
}

// Shows reports whether a rendered view shows want: verbatim, or across the
// line breaks a panel's stable width wraps it into.
func Shows(view, want string) bool {
	if strings.Contains(view, want) {
		return true
	}
	flat := unwrap(view)
	if strings.Contains(flat, strings.Join(strings.Fields(want), " ")) {
		return true
	}
	// Words longer than the panel (paths) wrap mid-word, so compare spaceless.
	spaceless := func(s string) string { return strings.Join(strings.Fields(s), "") }
	return strings.Contains(spaceless(flat), spaceless(want))
}

// unwrap joins a view's text as one line: ANSI, panel borders and the
// whitespace the shell adds around wrapped lines are dropped.
func unwrap(view string) string {
	var words []string
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		words = append(words, strings.Fields(strings.Trim(line, " │┌┐└┘─"))...)
	}
	return strings.Join(words, " ")
}
