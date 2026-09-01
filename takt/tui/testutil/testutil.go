// Package testutil provides shared test helpers for the TUI test suites.
package testutil

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

// ActionRequest extracts the emitted ActionRequest from a command, following
// tea.Batch wrappers the flow attaches alongside the busy spinner tick.
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
