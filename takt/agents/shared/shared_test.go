// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package shared_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
)

func TestNewManagedPathsSortsAndRejectsUnsafeOrDuplicatePaths(t *testing.T) {
	got, err := shared.NewManagedPaths([]string{"z/plugin.ts", "a/config.json", "m/cli.json"})
	if err != nil {
		t.Fatalf("NewManagedPaths() error = %v", err)
	}
	want := []string{".config/opencode/opencode.json", "a/config.json", "m/cli.json", "z/plugin.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NewManagedPaths() = %v, want %v", got, want)
	}

	if _, err := shared.NewManagedPaths([]string{"same", "same"}); err == nil || !strings.Contains(err.Error(), "duplicate OpenCode managed path") {
		t.Fatalf("duplicate path error = %v, want duplicate-path error", err)
	}
	if _, err := shared.NewManagedPaths([]string{"../outside"}); err == nil || !strings.Contains(err.Error(), "invalid OpenCode managed path") {
		t.Fatalf("unsafe path error = %v, want invalid-path error", err)
	}
}

func TestNewManagedPathsIncludesCanonicalConfigAndSortedGeneratedPaths(t *testing.T) {
	got, err := shared.NewManagedPaths([]string{"plugins/takt.ts", "AGENTS.md"})
	if err != nil {
		t.Fatalf("NewManagedPaths() error = %v", err)
	}
	if len(got) != 3 || got[0] != ".config/opencode/opencode.json" || got[1] != "AGENTS.md" || got[2] != "plugins/takt.ts" {
		t.Fatalf("NewManagedPaths() = %v, want sorted config and generated paths", got)
	}
}
