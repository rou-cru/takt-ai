// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package setup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// jsonFixtureConfigPath is an unmanaged mergeable JSON config the plan deploys.
const jsonFixtureConfigPath = ".config/opencode/cli.json"

// jsonFixtureMarker is user content that diagnostics must never echo.
const jsonFixtureMarker = "rq4-private-value"

func jsonFixturePlan() []setup.TargetPlan {
	return []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{jsonFixtureConfigPath, ".config/opencode/agents/takt-dev.md"},
		Artifacts: []setup.Artifact{
			{Path: jsonFixtureConfigPath, Content: []byte("{\"takt\": true}\n")},
			{Path: ".config/opencode/agents/takt-dev.md", Content: []byte("dev\n")},
		},
	}}
}

func jsonFixtureWrite(t *testing.T, root, rel string, content []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonFixtureRead(t *testing.T, root, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestSetupReportsAndRetainsMalformedUnmanagedJSON covers both routes:
// setup discloses the parse position without content, keeps the bytes when
// preserved, and replaces the file behind a backup otherwise.
func TestSetupReportsAndRetainsMalformedUnmanagedJSON(t *testing.T) {
	// The invalid token sits on the third line, after two spaces.
	const wantLine, wantColumn = 3, 3
	root := t.TempDir()
	original := []byte("{\n  \"key\": \"" + jsonFixtureMarker + "\",\n  x\n}\n")
	jsonFixtureWrite(t, root, jsonFixtureConfigPath, original)

	conflicts, err := setup.DetectConflicts(root, jsonFixturePlan())
	if err != nil {
		t.Fatalf("DetectConflicts() error = %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].Path != jsonFixtureConfigPath {
		t.Fatalf("DetectConflicts() = %+v; want exactly one conflict for %s", conflicts, jsonFixtureConfigPath)
	}
	conflict := conflicts[0]
	if conflict.Reason != "pre-existing" || conflict.SHA256 == "" || conflict.Impact == setup.ImpactUnrelated {
		t.Errorf("conflict = %+v; want a pre-existing conflict that requires a decision", conflict)
	}
	if conflict.Line != wantLine || conflict.Column != wantColumn {
		t.Errorf("conflict position = line %d column %d; want line %d column %d", conflict.Line, conflict.Column, wantLine, wantColumn)
	}
	for _, text := range []string{conflict.Consequence, conflict.Alternative, conflict.Affects} {
		if strings.Contains(text, jsonFixtureMarker) {
			t.Errorf("conflict text %q discloses file content", text)
		}
	}

	// Keep: the file stays as the user wrote it.
	if _, err := setup.ApplyContext(context.Background(), root, jsonFixturePlan(), setup.ProviderRuntime{}, jsonFixtureConfigPath); err != nil {
		t.Fatalf("ApplyContext() preserving %s error = %v", jsonFixtureConfigPath, err)
	}
	if got := jsonFixtureRead(t, root, jsonFixtureConfigPath); !bytes.Equal(got, original) {
		t.Errorf("preserved config = %q; want original bytes %q", got, original)
	}
	if got := jsonFixtureRead(t, root, ".config/opencode/agents/takt-dev.md"); string(got) != "dev\n" {
		t.Errorf("unrelated artifact = %q; want it deployed", got)
	}

	// Replace: Takt's config lands and the backup keeps the original.
	if _, err := setup.ApplyContext(context.Background(), root, jsonFixturePlan(), setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() replacing %s error = %v", jsonFixtureConfigPath, err)
	}
	if got := jsonFixtureRead(t, root, jsonFixtureConfigPath); string(got) != "{\"takt\": true}\n" {
		t.Errorf("replaced config = %q; want Takt's config", got)
	}
	if got := jsonFixtureRead(t, root, setup.BackupDir+"/"+jsonFixtureConfigPath); !bytes.Equal(got, original) {
		t.Errorf("backup = %q; want original bytes %q", got, original)
	}
}

// TestSetupMergesValidUnmanagedJSON is the control case: a valid
// unmanaged object raises no conflict and keeps the user's keys on apply.
func TestSetupMergesValidUnmanagedJSON(t *testing.T) {
	root := t.TempDir()
	jsonFixtureWrite(t, root, jsonFixtureConfigPath, []byte("{\n  // mine\n  \"user\": 1,\n}\n"))
	conflicts, err := setup.DetectConflicts(root, jsonFixturePlan())
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("DetectConflicts() = %+v, %v; want no conflict for a valid unmanaged object", conflicts, err)
	}
	if _, err := setup.ApplyContext(context.Background(), root, jsonFixturePlan(), setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(jsonFixtureRead(t, root, jsonFixtureConfigPath), &got); err != nil {
		t.Fatalf("merged config is not JSON: %v", err)
	}
	if got["user"] != float64(1) || got["takt"] != true {
		t.Errorf("merged config = %v; want the user key kept and the Takt key added", got)
	}
}
