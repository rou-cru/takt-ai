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

package gc_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
)

// TestReadTypeScriptSymbolsKeepsExportsAndFilenames checks that exported
// variables and locally re-exported names keep their export protection, and
// that a filename starting with a dash stays a filename, never a Node option.
func TestReadTypeScriptSymbolsKeepsExportsAndFilenames(t *testing.T) {
	typescript, err := filepath.Abs("../agents/opencode/assets/node_modules/typescript")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	if _, err := os.Stat(typescript); err != nil {
		t.Skip("plugin dependencies are not installed")
	}
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(typescript, filepath.Join(workspace, "node_modules", "typescript")); err != nil {
		t.Fatal(err)
	}
	source := "export const a = () => 1\nconst b = 2\nlet c = 3\nexport { c }\n"
	if err := os.WriteFile(filepath.Join(workspace, "-x.ts"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	symbols, err := gc.ReadSymbols(context.Background(), workspace, "-x.ts", "typescript")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"a": true, "b": false, "c": true}
	for _, s := range symbols {
		if exported, ok := want[s.Name]; !ok || exported != s.Exported {
			t.Errorf("symbol %s exported = %v; want %v", s.Name, s.Exported, exported)
		}
		delete(want, s.Name)
	}
	if len(want) != 0 {
		t.Errorf("missing symbols %v in %+v", want, symbols)
	}
}
