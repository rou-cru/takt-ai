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

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergePreexistingConfigPreservesUserJSONKeys(t *testing.T) {
	root := t.TempDir()
	path := ".config/opencode/opencode.json"
	existing := "{\n  \"model\": \"user/model\",\n  \"theme\": \"user-theme\"\n}\n"
	if err := os.MkdirAll(filepath.Join(root, ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".config", "opencode", "opencode.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := NewOwnershipManifest()
	overlay := "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"model\": \"takt/model\"\n}\n"

	merged, err := mergePreexistingConfig(root, Artifact{Path: path, Content: []byte(overlay)}, manifest)
	if err != nil {
		t.Fatal(err)
	}
	content := string(merged.Content)
	if !strings.Contains(content, "\"user-theme\"") {
		t.Fatalf("merged content dropped the user-only key: %s", content)
	}
	if !strings.Contains(content, "\"takt/model\"") {
		t.Fatalf("merged content lost the overlay value: %s", content)
	}
	if strings.Contains(content, "\"user/model\"") {
		t.Fatalf("overlay must win shared keys, got: %s", content)
	}
}

func TestMergePreexistingConfigMissingFileReturnsArtifact(t *testing.T) {
	merged, err := mergePreexistingConfig(t.TempDir(), Artifact{Path: ".config/opencode/opencode.json", Content: []byte("{}\n")}, NewOwnershipManifest())
	if err != nil {
		t.Fatal(err)
	}
	if string(merged.Content) != "{}\n" {
		t.Fatalf("content = %q, want unchanged", merged.Content)
	}
}
