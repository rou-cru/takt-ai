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
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// E2E-C: `gc findings` with the real codegraph binary over a fixture workspace.
// It runs only in the disposable test container (scripts/test-containerized.sh),
// which installs codegraph and names its binary in TAKT_E2E_CODEGRAPH: indexing
// mutates the workspace.
func TestGCFindingsOverRealCodegraph(t *testing.T) {
	binary := os.Getenv("TAKT_E2E_CODEGRAPH")
	deadcode := os.Getenv("TAKT_E2E_DEADCODE")
	if binary == "" || deadcode == "" {
		t.Skip("E2E-C runs in the test container, which provides codegraph")
	}
	// TestMain puts a fake codegraph first on PATH; the CLI must resolve the real one.
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	files := map[string]string{
		"go.mod":     "module fx\n\ngo 1.25\n",
		"main.go":    "package main\n\nimport \"fx/pkg\"\n\nfunc main() { _ = pkg.Live() }\n",
		"pkg/lib.go": "package pkg\n\nfunc Live() int { return 1 }\nfunc Dead() int { return 2 }\n",
	}
	newFile := "package pkg\n\nfunc Unused() int { return 3 }\n"
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// gc findings consumes the private preparation produced by the coordinator;
	// prepare it explicitly instead of making the findings command acquire tools.
	if err := os.MkdirAll(filepath.Join(root, ".takt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".takt", "gc.json"), []byte(`{"version":1,"locks":["go.mod"],"checks":[["/bin/sh","-c","true"]],"analyzers":[{"language":"go","mandate":"dead-code","tool":"deadcode","version":"`+deadcodeVersion(t, deadcode)+`","command":["./deadcode","-json","./..."],"version_command":["go","version","./deadcode"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tool, err := os.ReadFile(deadcode)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deadcode"), tool, 0o755); err != nil {
		t.Fatal(err)
	}
	// The session's create of pkg/new.go is journaled; the file is already on disk
	// after the VFS close, before the real codegraph is initialized.
	state := filepath.Join(t.TempDir(), "private")
	fs, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	key, err := fs.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "u", AttemptID: "1", AgentID: "author", Specialist: "dev", InvariantsHash: "h"}, []string{"pkg/new.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gc.Prepare(context.Background(), root, state, "s"); err != nil {
		t.Fatalf("prepare GC state: %v", err)
	}
	if _, err = fs.Apply(vfs.Operation{Key: key, CallID: "c", Action: vfs.OpCreate, Path: "pkg/new.go", Content: []byte(newFile)}); err != nil {
		t.Fatal(err)
	}
	// Apply stages the VFS view; materialize the post-session workspace before
	// running the external index and analyzer, as a flushed session would.
	if err := os.WriteFile(filepath.Join(root, "pkg/new.go"), []byte(newFile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err = fs.Close(); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(binary, "init", "-y", root).CombinedOutput(); err != nil {
		t.Fatalf("index fixture: %v: %s", err, out)
	}
	args := []string{"gc", "findings", "--workspace", root, "--state", state, "--session", "s", "--cycle", "c1", "--mandate", "dead-code"}
	var out, stderr bytes.Buffer
	if err = run(args, strings.NewReader(""), &out, &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	var got struct {
		Plan   gc.Plan   `json:"plan"`
		Report gc.Report `json:"report"`
	}
	if err = json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// Only the session's own file is in scope: Dead in pkg/lib.go lies outside the closure.
	if got.Plan.Reachability != gc.ReachCodegraph || len(got.Report.Findings) != 1 ||
		got.Report.Findings[0].ID != "dead-code:pkg/new.go#Unused" || got.Report.Findings[0].Status != gc.StatusPending || len(got.Report.Gaps) != 0 {
		t.Fatalf("output = %s", out.String())
	}
}

func deadcodeVersion(t *testing.T, binary string) string {
	t.Helper()
	out, err := exec.Command("go", "version", binary).Output()
	if err != nil {
		t.Fatalf("resolve deadcode version: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		t.Fatalf("unexpected deadcode version output: %q", out)
	}
	return fields[len(fields)-1]
}
