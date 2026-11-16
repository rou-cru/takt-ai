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

package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

// dagStatus runs `dag status` once and decodes its stdout.
func dagStatus(t *testing.T, s *session, workspace, state string) map[string]any {
	t.Helper()
	res := s.run("dag", "status", "--workspace", workspace, "--state", state, "--format", "json")
	if res.code != 0 {
		t.Fatalf("dag status exited %d, want a graceful JSON answer: %s", res.code, res.stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &got); err != nil {
		t.Fatalf("decode %q: %v", res.stdout, err)
	}
	return got
}

// TestDagStatusMissingWorkspaceIsUnavailable proves a read failure never
// renders as an empty or completed DAG (PRD_DAG_TUI.md §11): a workspace or
// state directory that was never set up must not silently create one either.
func TestDagStatusMissingWorkspaceIsUnavailable(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	got := dagStatus(t, s, filepath.Join(s.root, "nope"), s.state)
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
	s := newSession(t, buildTaktAI(t))
	if err := os.MkdirAll(s.state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.state, "history.sqlite"), []byte("not a sqlite file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := dagStatus(t, s, s.root, s.state); got["capture"] != history.CaptureUnavailable {
		t.Fatalf("capture = %v; want %q", got["capture"], history.CaptureUnavailable)
	}
}

// TestDagStatusPopulatedWorkspace proves a real history round-trips through
// the CLI into its snapshot, naming admitted agents by their catalog label.
func TestDagStatusPopulatedWorkspace(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	h, err := history.Open(s.state)
	if err != nil {
		t.Fatal(err)
	}
	entries := []history.Entry{
		{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "root", WorkUnitID: "storage",
			AttemptID: history.FirstAttempt, Cause: history.CauseNone, PlanVersion: "v1", Contract: "storage contract"},
		{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "root", WorkUnitID: "api",
			AttemptID: history.FirstAttempt, Cause: history.CauseNone, PlanVersion: "v1", Contract: "api contract",
			Prerequisites: []string{"storage"}},
		{Author: history.AuthorHarness, Kind: history.KindAdmitted, SessionID: "root", WorkUnitID: "storage",
			AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured, Agent: "product-designer"},
	}
	for _, e := range entries {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	got := dagStatus(t, s, s.root, s.state)
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
	// Nodes are sorted by id: api (planned, no agent), then storage.
	if agent, has := nodes[0].(map[string]any)["agent"]; has {
		t.Fatalf("planned api agent = %v; want none", agent)
	}
	if agent := nodes[1].(map[string]any)["agent"]; agent != "design" {
		t.Fatalf("admitted storage agent = %v; want the catalog label %q", agent, "design")
	}
}
