package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
)

// gcEntries reads the recorded execution history of a harness state directory,
// the way an independent consumer would.
func gcEntries(t *testing.T, state string) []history.Entry {
	t.Helper()
	h, e := history.Open(state)
	if e != nil {
		t.Fatalf("open history: %v", e)
	}
	defer func() {
		if e := h.Close(); e != nil {
			t.Fatalf("close history: %v", e)
		}
	}()
	return h.Entries()
}

// gcProjection replays that record without a clock or external state.
func gcProjection(t *testing.T, state string) history.Projection {
	t.Helper()
	return history.Project(gcEntries(t, state))
}

// dispatchHarness runs `dispatch` requests in process against a fresh
// workspace and state directory, returning the coordinator they leave behind.
func dispatchHarness(t *testing.T) (string, string, func(coordinationRequest) (gc.Coordinator, error)) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	call := func(r coordinationRequest) (gc.Coordinator, error) {
		var out, errout bytes.Buffer
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		e := runDispatch([]string{"--workspace", root, "--state", state, "--request", string(b)}, &out, &errout)
		c, load := gc.LoadCoordinator(state)
		if load != nil {
			return gc.Coordinator{}, load
		}
		return *c, e
	}
	return root, state, call
}
