// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package semver_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/internal/semver"
)

func TestAtLeastComparesReleaseTripletsAndRejectsMalformedVersions(t *testing.T) {
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
			if got := semver.AtLeast(test.have, test.want); got != test.pass {
				t.Fatalf("AtLeast(%q, %q) = %t, want %t", test.have, test.want, got, test.pass)
			}
		})
	}
}
