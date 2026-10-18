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
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestLoadDeployedModelsReadsTheFileNotTheRecord(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(model.OpenCodeConfigRelativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{
  // edited by hand
  "agents": {
    "verify": {"model": "databricks-gateway/system.ai.grok-4-7"},
    "dev": {"model": "p/m#high"},
    "pm": {"description": "inherits"},
  }
}`
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := setup.LoadDeployedModels(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]model.ModelAssignment{
		"verify": {Model: "databricks-gateway/system.ai.grok-4-7"},
		"dev":    {Model: "p/m#high"},
	}
	if len(got) != len(want) || got["verify"] != want["verify"] || got["dev"] != want["dev"] {
		t.Fatalf("deployed models = %v, want %v", got, want)
	}
}

func TestLoadDeployedModelsMissingConfigHasNoAssignments(t *testing.T) {
	got, err := setup.LoadDeployedModels(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want no assignments", got, err)
	}
}
