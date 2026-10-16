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

package dispatch_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
)

func TestResolvePolicyPlaceholders(t *testing.T) {
	p, err := dispatch.LoadAdmissionPolicy()
	if err != nil {
		t.Fatal(err)
	}
	got, err := dispatch.ResolvePolicyPlaceholders([]byte("at most {{policy.concurrent_specialists}} and {{policy.unplanned_units}} under {{policy.max_file_lines}}"))
	if err != nil {
		t.Fatal(err)
	}
	want := "at most " + strconv.Itoa(p.Concurrency.Specialists) + " and " + strconv.Itoa(p.Budgets.UnplannedUnits) + " under " + strconv.Itoa(p.Modules.MaxFileLines)
	if string(got) != want {
		t.Fatalf("resolved = %q; want %q", got, want)
	}
	plain, err := dispatch.ResolvePolicyPlaceholders([]byte("no markers"))
	if err != nil || string(plain) != "no markers" {
		t.Fatalf("plain = %q, %v", plain, err)
	}
	if _, err = dispatch.ResolvePolicyPlaceholders([]byte("{{policy.unknown}}")); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("unknown marker = %v; want unresolved error", err)
	}
}
