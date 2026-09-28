// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package shared

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeManagedPathsSortsAndRejectsUnsafeOrDuplicatePaths(t *testing.T) {
	got, err := NormalizeManagedPaths("OpenCode", []string{"z/plugin.ts"}, []string{"a/config.json", "m/cli.json"})
	if err != nil {
		t.Fatalf("NormalizeManagedPaths() error = %v", err)
	}
	want := []string{"a/config.json", "m/cli.json", "z/plugin.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeManagedPaths() = %v, want %v", got, want)
	}

	if _, err := NormalizeManagedPaths("OpenCode", []string{"same", "same"}); err == nil || !strings.Contains(err.Error(), "duplicate OpenCode managed path") {
		t.Fatalf("duplicate path error = %v, want duplicate-path error", err)
	}
	if _, err := NormalizeManagedPaths("OpenCode", []string{"../outside"}); err == nil || !strings.Contains(err.Error(), "invalid OpenCode managed path") {
		t.Fatalf("unsafe path error = %v, want invalid-path error", err)
	}
}

func TestNewManagedPathsIncludesCanonicalConfigAndSortedGeneratedPaths(t *testing.T) {
	got, err := NewManagedPaths([]string{"plugins/takt.ts", "AGENTS.md"})
	if err != nil {
		t.Fatalf("NewManagedPaths() error = %v", err)
	}
	if len(got) != 3 || got[0] != ".config/opencode/opencode.json" || got[1] != "AGENTS.md" || got[2] != "plugins/takt.ts" {
		t.Fatalf("NewManagedPaths() = %v, want sorted config and generated paths", got)
	}
}

func TestVersionAtLeastComparesReleaseTripletsAndRejectsMalformedVersions(t *testing.T) {
	tests := []struct {
		have string
		want string
		pass bool
	}{
		{have: "2.0.0", want: "2.0.0", pass: true},
		{have: "v2.1.0+build.7", want: "2.0.16", pass: true},
		{have: "1.99.99", want: "2.0.0", pass: false},
		{have: "2.0.15", want: "2.0.16", pass: false},
		{have: "2.0.0-rc.1", want: "2.0.0", pass: false},
		{have: "2.0", want: "2.0.0", pass: false},
		{have: "2.x.0", want: "2.0.0", pass: false},
	}
	for _, test := range tests {
		t.Run(test.have+"-at-least-"+test.want, func(t *testing.T) {
			if got := VersionAtLeast(test.have, test.want); got != test.pass {
				t.Fatalf("VersionAtLeast(%q, %q) = %t, want %t", test.have, test.want, got, test.pass)
			}
		})
	}
}
