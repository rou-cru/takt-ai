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
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
)

// seedPlannedUnit records one planned unit of session in the session's execution history.
func seedPlannedUnit(t *testing.T, s *session, session, unit string) {
	t.Helper()
	h, err := history.Open(s.state)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: session,
		WorkUnitID: unit, AttemptID: history.FirstAttempt, Cause: history.CauseNone,
		PlanVersion: "v1", Contract: "contract"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
}

// dagNodes runs `dag status` for the root session and returns the answered session and its node names.
func dagNodes(t *testing.T, s *session, root string) (string, []string) {
	t.Helper()
	res := s.run("dag", "status", "--workspace", s.root, "--state", s.state, "--session", root, "--format", "json")
	if res.code != 0 {
		t.Fatalf("dag status: %s", res.stderr)
	}
	var got struct {
		SessionID string `json:"session_id"`
		Nodes     []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &got); err != nil {
		t.Fatalf("decode %q: %v", res.stdout, err)
	}
	names := []string{}
	for _, n := range got.Nodes {
		names = append(names, n.ID)
	}
	return got.SessionID, names
}

// TestDagStatusEmptySessionNamesItself proves a requested root session with
// no recorded entries yet, and no continuity, is answered as that session's
// empty DAG, never as an unscoped snapshot (PR-DTUI-17, PR-DTUI-18).
func TestDagStatusEmptySessionNamesItself(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	seedPlannedUnit(t, s, "old", "old-unit")
	if session, names := dagNodes(t, s, "fresh"); session != "fresh" || len(names) != 0 {
		t.Fatalf("snapshot = %s %v; want session fresh with no nodes", session, names)
	}
}

// TestDagStatusFollowsDeclaredContinuity proves a root that declared continuity
// with a closed crew session shows that session's DAG with its own, from the
// declaration the memory service owns, before and after its first entry
// (PR-DAG-USE-1); a session that declared none shows only its own.
func TestDagStatusFollowsDeclaredContinuity(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	for _, root := range []string{"old", "new", "alone"} {
		s.bindRoot(root)
	}
	s.crewCall("old", memory.ToolRecord, map[string]any{"nature": "observation", "scope": "workspace", "title": "earlier work", "content": "body", "evidence": "seen"})
	s.crewCall("old", memory.ToolCloseSession, map[string]any{"objective": "build it", "state": "built"})
	s.crewCall("new", memory.ToolContinueSession, map[string]any{"previous_session_id": "crew:old"})
	seedPlannedUnit(t, s, "old", "design")
	seedPlannedUnit(t, s, "new", "build")
	seedPlannedUnit(t, s, "alone", "solo")

	// Before the new session records anything the declaration is still pending.
	if session, names := dagNodes(t, s, "new"); session != "new" || !slices.Equal(names, []string{"build", "design"}) {
		t.Fatalf("pending continuity: snapshot = %s %v; want new with design and build", session, names)
	}
	s.crewCall("new", memory.ToolRecord, map[string]any{"nature": "observation", "scope": "workspace", "title": "first", "content": "body", "evidence": "seen"})
	if _, names := dagNodes(t, s, "new"); !slices.Equal(names, []string{"build", "design"}) {
		t.Fatalf("fixed continuity: nodes = %v; want design and build", names)
	}
	if _, names := dagNodes(t, s, "alone"); !slices.Equal(names, []string{"solo"}) {
		t.Fatalf("a session without continuity: nodes = %v; want its own only", names)
	}
}
