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
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

// TestDagStatusEmptySessionNamesItself proves a requested root session with
// no recorded entries yet is answered as that session's empty DAG, never as
// an unscoped snapshot (PR-DTUI-17, PR-DTUI-18).
func TestDagStatusEmptySessionNamesItself(t *testing.T) {
	bin := buildTaktAI(t)
	root := t.TempDir()
	state := filepath.Join(root, "state")
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "old",
		WorkUnitID: "old-unit", AttemptID: history.FirstAttempt, Cause: history.CauseNone,
		PlanVersion: "v1", Contract: "contract"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "dag", "status", "--workspace", root, "--state", state, "--session", "fresh", "--format", "json")
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+dir)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("dag status: %v", err)
	}
	var got struct {
		SessionID string `json:"session_id"`
		Nodes     []any  `json:"nodes"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if got.SessionID != "fresh" || len(got.Nodes) != 0 {
		t.Fatalf("snapshot = %s; want session fresh with no nodes", out)
	}
}
