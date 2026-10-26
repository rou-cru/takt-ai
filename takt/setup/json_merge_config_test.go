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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
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

// TestMergeJSONObjectsRejectsMalformedBase covers a malformed base: it is
// an error with its position in the original bytes, never an empty object.
func TestMergeJSONObjectsRejectsMalformedBase(t *testing.T) {
	overlay := []byte(`{"servers": {"engram": {"command": "engram"}}}`)
	for _, tc := range []struct {
		name         string
		base         string
		offset       int64
		line, column int
	}{
		{name: "invalid key", base: "{\n  \"a\": 1,\n  x\n}", offset: 14, line: 3, column: 3},
		{name: "invalid key after comment", base: "{\n// note\n  \"a\": 1,\n  x\n}", offset: 22, line: 4, column: 3},
		{name: "invalid key after block comment", base: "{ /* a\nb */ \"a\": 1,\n  x\n}", offset: 22, line: 3, column: 3},
		{name: "starts with letter", base: "allow: all", offset: 0, line: 1, column: 1},
		{name: "unclosed object", base: `{"ok": true`, offset: 10, line: 1, column: 11},
		{name: "trailing value", base: "{}\n x", offset: 4, line: 2, column: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			merged, err := filemerge.MergeJSONObjects([]byte(tc.base), overlay)
			if err == nil {
				t.Fatalf("MergeJSONObjects() = %s, nil; want an error for a malformed base", merged)
			}
			if merged != nil {
				t.Errorf("MergeJSONObjects() returned content %s alongside the error", merged)
			}
			var invalid *filemerge.InvalidJSONError
			if !errors.As(err, &invalid) {
				t.Fatalf("error %v is not *filemerge.InvalidJSONError", err)
			}
			if invalid.ByteOffset != tc.offset || invalid.Line != tc.line || invalid.Column != tc.column {
				t.Errorf("position = offset %d line %d column %d; want offset %d line %d column %d", invalid.ByteOffset, invalid.Line, invalid.Column, tc.offset, tc.line, tc.column)
			}
		})
	}
}

// TestMergeJSONObjectsRejectsNonObjectRoots covers non-object roots: they
// are not coerced, on either side of the merge.
func TestMergeJSONObjectsRejectsNonObjectRoots(t *testing.T) {
	for _, root := range []string{"[1, 2]", "null", "  null", `"text"`, "3", "true", "// c\n[]"} {
		_, err := filemerge.MergeJSONObjects([]byte(root), []byte(`{"a": 1}`))
		var invalid *filemerge.InvalidJSONError
		if !errors.As(err, &invalid) {
			t.Errorf("base %q: error = %v; want *filemerge.InvalidJSONError", root, err)
			continue
		}
		if !strings.Contains(invalid.Reason, "not an object") {
			t.Errorf("base %q: reason = %q; want it to name a non-object root", root, invalid.Reason)
		}
		if want := int64(len(root) - len(strings.TrimLeft(root, " "))); !strings.HasPrefix(root, "//") && invalid.ByteOffset != want {
			t.Errorf("base %q: offset = %d; want %d", root, invalid.ByteOffset, want)
		}
		if _, err := filemerge.MergeJSONObjects([]byte(`{"a": 1}`), []byte(root)); err == nil {
			t.Errorf("overlay %q: merge succeeded; want a non-object overlay rejected", root)
		}
	}
	if _, err := filemerge.MergeJSONObjects([]byte("// c\n[]"), []byte("{}")); err != nil {
		var invalid *filemerge.InvalidJSONError
		if errors.As(err, &invalid) && (invalid.Line != 2 || invalid.Column != 1) {
			t.Errorf("commented array root position = line %d column %d; want line 2 column 1", invalid.Line, invalid.Column)
		}
	}
}

// TestMergeJSONObjectsKeepsValidObjectSemantics is the control case: valid
// objects, JSONC and empty input keep the established merge.
func TestMergeJSONObjectsKeepsValidObjectSemantics(t *testing.T) {
	for name, base := range map[string]string{
		"object": `{"user": 1, "plugins": ["mine"]}`,
		"jsonc":  "{\n  // mine\n  \"user\": 1, /* x */ \"plugins\": [\"mine\",],\n}",
	} {
		merged, err := filemerge.MergeJSONObjects([]byte(base), []byte(`{"takt": 2, "plugins": ["takt"]}`))
		if err != nil {
			t.Fatalf("%s: MergeJSONObjects() error = %v", name, err)
		}
		var got map[string]any
		if err := json.Unmarshal(merged, &got); err != nil {
			t.Fatalf("%s: merged output is not JSON: %v", name, err)
		}
		plugins, _ := got["plugins"].([]any)
		if got["user"] != float64(1) || got["takt"] != float64(2) || len(plugins) != 2 || plugins[0] != "mine" || plugins[1] != "takt" {
			t.Errorf("%s: merged = %v; want user and takt keys and the plugin union", name, got)
		}
	}
	for _, empty := range []string{"", "  \n"} {
		merged, err := filemerge.MergeJSONObjects([]byte(empty), []byte(`{"takt": 2}`))
		if err != nil || !bytes.Contains(merged, []byte(`"takt": 2`)) {
			t.Errorf("empty base %q: merged = %s, %v; want the overlay", empty, merged, err)
		}
	}
}

// TestSetupReportsAndRetainsMalformedUnmanagedJSON covers both routes:
// setup discloses the parse position without content, keeps the bytes when
// preserved, and replaces the file behind a backup otherwise.
func TestSetupReportsAndRetainsMalformedUnmanagedJSON(t *testing.T) {
	for _, tc := range []struct {
		name         string
		content      string
		offset       int64
		line, column int
	}{
		{name: "syntax error", content: "{\n  \"key\": \"" + jsonFixtureMarker + "\",\n  x\n}\n", offset: 34, line: 3, column: 3},
		{name: "array root", content: "\n[\"" + jsonFixtureMarker + "\"]\n", offset: 1, line: 2, column: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			original := []byte(tc.content)
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
			if conflict.ByteOffset != tc.offset || conflict.Line != tc.line || conflict.Column != tc.column {
				t.Errorf("conflict position = offset %d line %d column %d; want offset %d line %d column %d", conflict.ByteOffset, conflict.Line, conflict.Column, tc.offset, tc.line, tc.column)
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
		})
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
