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

package engram

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCommand = "/opt/takt/bin/engram"

// readTest loads a file or fails fast so assertions always run on real content.
func readTest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

// writeTest creates parent dirs and writes content so each test starts from a known state.
func writeTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestInjectOpenCodeMergesIntoExistingConfig verifies OpenCode inject adds its key while keeping user keys and model intact.
func TestInjectOpenCodeMergesIntoExistingConfig(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	writeTest(t, configPath, "{\n  \"model\": \"user/model\",\n  \"custom\": true\n}\n")

	if _, err := Inject(home, testCommand); err != nil {
		t.Fatal(err)
	}
	config := readTest(t, configPath)
	if !strings.Contains(config, "\"engram\"") {
		t.Fatalf("config missing engram server: %s", config)
	}
	if !strings.Contains(config, "\"custom\": true") {
		t.Fatalf("user key lost: %s", config)
	}
	if !strings.Contains(config, "\"user/model\"") {
		t.Fatalf("user model lost: %s", config)
	}
	prompt := readTest(t, filepath.Join(home, ".config", "opencode", "AGENTS.md"))
	if !strings.Contains(prompt, "engram-protocol") {
		t.Fatalf("prompt missing engram section: %s", prompt)
	}
}

// TestRemoveOpenCodeDeletesKeyAndSection verifies OpenCode remove drops its key while keeping user JSON intact.
func TestRemoveOpenCodeDeletesKeyAndSection(t *testing.T) {
	home := t.TempDir()
	writeTest(t, filepath.Join(home, ".config", "opencode", "opencode.json"), "{\n  \"custom\": true\n}\n")
	if _, err := Inject(home, testCommand); err != nil {
		t.Fatal(err)
	}

	if _, err := Remove(home); err != nil {
		t.Fatal(err)
	}
	config := readTest(t, filepath.Join(home, ".config", "opencode", "opencode.json"))
	if strings.Contains(config, "engram") {
		t.Fatalf("engram key survived: %s", config)
	}
	if !strings.Contains(config, "\"custom\": true") {
		t.Fatalf("user content lost: %s", config)
	}
}
