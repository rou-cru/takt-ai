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

package model_test

import (
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestOpenCodeConfigPath(t *testing.T) {
	got := model.OpenCodeConfigPath("/home/user")
	want := filepath.Join("/home/user", ".config/opencode", "opencode.json")
	if got != want {
		t.Errorf("OpenCodeConfigPath = %q, want %q", got, want)
	}
}

func TestOpenCodePromptPath(t *testing.T) {
	got := model.OpenCodePromptPath("/home/user")
	want := filepath.Join("/home/user", ".config/opencode", "AGENTS.md")
	if got != want {
		t.Errorf("OpenCodePromptPath = %q, want %q", got, want)
	}
}

func TestOpenCodePluginPath(t *testing.T) {
	got := model.OpenCodePluginPath("/home/user", "engram.ts")
	want := filepath.Join("/home/user", ".config/opencode", "plugins", "engram.ts")
	if got != want {
		t.Errorf("OpenCodePluginPath = %q, want %q", got, want)
	}
}
