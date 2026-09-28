// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

// dagStatus drives the public CLI entry once and decodes its stdout.
func dagStatus(t *testing.T, workspace, state string) (map[string]any, error) {
	t.Helper()
	var out, errout bytes.Buffer
	err := runDag([]string{"status", "--workspace", workspace, "--state", state, "--format", "json"}, &out, &errout)
	if err != nil {
		return nil, err
	}
	var got map[string]any
	if decodeErr := json.Unmarshal(out.Bytes(), &got); decodeErr != nil {
		t.Fatalf("decode %q: %v", out.String(), decodeErr)
	}
	return got, nil
}

// TestDagStatusMissingWorkspaceIsUnavailable proves a read failure never
// renders as an empty or completed DAG (PRD_DAG_TUI.md §11): a workspace or
// state directory that was never set up must not silently create one either
// (mirroring gc.go's/vfs.go's own "existing store required" guard).
func TestDagStatusMissingWorkspaceIsUnavailable(t *testing.T) {
	root := t.TempDir()
	got, err := dagStatus(t, filepath.Join(root, "nope"), filepath.Join(root, "state"))
	if err != nil {
		t.Fatalf("missing workspace returned a hard error, want a graceful unavailable JSON: %v", err)
	}
	if got["capture"] != history.CaptureUnavailable {
		t.Fatalf("capture = %v; want %q", got["capture"], history.CaptureUnavailable)
	}
	if _, hasNodes := got["nodes"]; hasNodes {
		t.Fatalf("unavailable response claims data: %+v", got)
	}
}

// TestDagStatusUnreadableWorkspaceIsUnavailable proves a corrupt store is
// reported the same way as a missing one, never as an empty or completed DAG.
func TestDagStatusUnreadableWorkspaceIsUnavailable(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "history.sqlite"), []byte("not a sqlite file"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := dagStatus(t, root, state)
	if err != nil {
		t.Fatalf("unreadable workspace returned a hard error, want a graceful unavailable JSON: %v", err)
	}
	if got["capture"] != history.CaptureUnavailable {
		t.Fatalf("capture = %v; want %q", got["capture"], history.CaptureUnavailable)
	}
}

// TestDagStatusPopulatedWorkspace proves a real history round-trips through
// the CLI into the same snapshot BuildSnapshot would produce directly.
func TestDagStatusPopulatedWorkspace(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	entries := []history.Entry{
		{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "root", WorkUnitID: "storage",
			AttemptID: history.FirstAttempt, Cause: history.CauseNone, PlanVersion: "v1", Contract: "storage contract"},
		{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "root", WorkUnitID: "api",
			AttemptID: history.FirstAttempt, Cause: history.CauseNone, PlanVersion: "v1", Contract: "api contract",
			Prerequisites: []string{"storage"}},
	}
	for _, e := range entries {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := dagStatus(t, root, state)
	if err != nil {
		t.Fatal(err)
	}
	if got["capture"] != history.CaptureCurrent {
		t.Fatalf("capture = %v; want %q", got["capture"], history.CaptureCurrent)
	}
	if got["session_id"] != "root" {
		t.Fatalf("session_id = %v; want %q", got["session_id"], "root")
	}
	if got["plan_version"] != "v1" {
		t.Fatalf("plan_version = %v; want %q", got["plan_version"], "v1")
	}
	if n, ok := got["projection_revision"].(float64); !ok || int(n) != len(entries) {
		t.Fatalf("projection_revision = %v; want %d", got["projection_revision"], len(entries))
	}
	nodes, ok := got["nodes"].([]any)
	if !ok || len(nodes) != 2 {
		t.Fatalf("nodes = %+v; want 2", got["nodes"])
	}
	edges, ok := got["edges"].([]any)
	if !ok || len(edges) != 1 {
		t.Fatalf("edges = %+v; want 1", got["edges"])
	}
}

// TestDagStatusSessionScopesTheGraph proves a workspace history shared by
// several sessions projects only the requested root session's plan: a fresh
// session must not inherit an earlier session's settled graph.
func TestDagStatusSessionScopesTheGraph(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"old", "new"} {
		e := history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: session,
			WorkUnitID: session + "-unit", AttemptID: history.FirstAttempt, Cause: history.CauseNone,
			PlanVersion: "v1", Contract: "contract"}
		if err := h.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if err := runDag([]string{"status", "--workspace", root, "--state", state, "--session", "new", "--format", "json"}, &out, &errout); err != nil {
		t.Fatal(err)
	}
	var got struct {
		SessionID string `json:"session_id"`
		Nodes     []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "new" || len(got.Nodes) != 1 || got.Nodes[0].ID != "new-unit" {
		t.Fatalf("snapshot = %+v; want only session new's unit", got)
	}
}
