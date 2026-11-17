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

package history_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

func TestStateDirsRegistryListsEachDirectoryOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if global, err := history.GlobalStateDir(); err != nil || global != filepath.Join(home, ".takt-ai") {
		t.Fatalf("GlobalStateDir = %q, %v; want ~/.takt-ai", global, err)
	}
	if dirs, err := history.StateDirs(); err != nil || dirs != nil {
		t.Fatalf("StateDirs before any registration = %v, %v; want none", dirs, err)
	}
	a, b := filepath.Join(home, "a", ".takt-ai"), filepath.Join(home, "b", ".takt-ai")
	for _, dirs := range [][]string{{a, b}, {b, a}, {a}} {
		if err := history.RegisterStateDirs(dirs...); err != nil {
			t.Fatal(err)
		}
	}
	if dirs, err := history.StateDirs(); err != nil || !slices.Equal(dirs, []string{a, b}) {
		t.Fatalf("StateDirs = %v, %v; want %v once each", dirs, err, []string{a, b})
	}
	for _, bad := range []string{"relative/.takt-ai", filepath.Join(home, "x\ny")} {
		if err := history.RegisterStateDirs(bad); err == nil {
			t.Errorf("RegisterStateDirs(%q) = nil; want a refusal", bad)
		}
	}
}
