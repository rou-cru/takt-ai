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

package verify_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// Collect contacts real MCP servers and shells out to `opencode api get /api/agent`;
// neither is injectable, so these tests only exercise the deterministic
// branches reachable without network access or external processes: a missing
// or unreadable ownership manifest, and a manifest whose targets don't
// include OpenCode (so the orchestrator check short-circuits) and whose
// managed MCP config files are absent (so managedMCPChecks resolves purely
// from missing-file state).

func TestCollectNoManifest(t *testing.T) {
	root := t.TempDir()

	report := verify.Collect(context.Background(), root)

	if len(report.Checks) != 1 {
		t.Fatalf("len(Checks) = %d, want 1: %+v", len(report.Checks), report.Checks)
	}
	check := report.Checks[0]
	if check.ID != "installation" {
		t.Fatalf("ID = %q, want %q", check.ID, "installation")
	}
	if check.State != verify.NotVerifiable {
		t.Fatalf("State = %q, want %q", check.State, verify.NotVerifiable)
	}
	if report.Ready {
		t.Fatal("Ready = true, want false")
	}
}

func TestCollectNoTargets(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{"version": 1, "entries": {}}`)

	report := verify.Collect(context.Background(), root)

	if len(report.Checks) != 1 {
		t.Fatalf("len(Checks) = %d, want 1: %+v", len(report.Checks), report.Checks)
	}
	check := report.Checks[0]
	if check.ID != "orchestrator:opencode:takt" {
		t.Fatalf("ID = %q, want %q", check.ID, "orchestrator:opencode:takt")
	}
	if check.State != verify.NotVerifiable {
		t.Fatalf("State = %q, want %q", check.State, verify.NotVerifiable)
	}
}

func writeManifest(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, setup.OwnershipManifestFilename), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
