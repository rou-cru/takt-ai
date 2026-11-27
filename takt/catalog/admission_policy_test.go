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

package catalog_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/catalog"
)

const validPolicy = `version: 1
concurrency:
  specialists: 4
budgets:
  unplanned_units: 4
  contests: 3
  recovery_failures: 2
  qa_rounds: 3
modules:
  max_file_lines: 1000
recovery:
  max_actions: 200
  max_attempts: 3
`

// PR-HAR-29: an invalid or missing limit names the limit and the policy entry
// to correct, whichever limit it is.
func TestAdmissionPolicyRejectionNamesTheEntry(t *testing.T) {
	if _, err := catalog.ParseAdmissionPolicy([]byte(validPolicy)); err != nil {
		t.Fatalf("valid policy refused: %v", err)
	}
	for _, entry := range []string{
		"concurrency.specialists", "budgets.unplanned_units", "budgets.contests", "budgets.recovery_failures",
		"budgets.qa_rounds", "modules.max_file_lines", "recovery.max_actions", "recovery.max_attempts",
	} {
		section, key, _ := strings.Cut(entry, ".")
		line := "  " + key + ": "
		for name, policy := range map[string]string{
			"zero":     replaceValue(t, validPolicy, section, line, "0"),
			"negative": replaceValue(t, validPolicy, section, line, "-1"),
			"missing":  dropEntry(t, validPolicy, line),
		} {
			_, err := catalog.ParseAdmissionPolicy([]byte(policy))
			if err == nil || !strings.Contains(err.Error(), entry) {
				t.Errorf("%s %s: error = %v, want it to name %s", entry, name, err, entry)
			}
		}
	}
}

func TestResolvePolicyPlaceholders(t *testing.T) {
	p, err := catalog.LoadAdmissionPolicy()
	if err != nil {
		t.Fatal(err)
	}
	got, err := catalog.ResolvePolicyPlaceholders([]byte("at most {{policy.concurrent_specialists}} and {{policy.unplanned_units}} under {{policy.max_file_lines}}"))
	if err != nil {
		t.Fatal(err)
	}
	want := "at most " + strconv.Itoa(p.Concurrency.Specialists) + " and " + strconv.Itoa(p.Budgets.UnplannedUnits) + " under " + strconv.Itoa(p.Modules.MaxFileLines)
	if string(got) != want {
		t.Fatalf("resolved = %q; want %q", got, want)
	}
	plain, err := catalog.ResolvePolicyPlaceholders([]byte("no markers"))
	if err != nil || string(plain) != "no markers" {
		t.Fatalf("plain = %q, %v", plain, err)
	}
	if _, err = catalog.ResolvePolicyPlaceholders([]byte("{{policy.unknown}}")); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("unknown marker = %v; want unresolved error", err)
	}
}

// replaceValue sets the value of the line starting with prefix inside section.
func replaceValue(t *testing.T, policy, section, prefix, value string) string {
	t.Helper()
	lines := strings.Split(policy, "\n")
	in := false
	for i, line := range lines {
		switch {
		case !strings.HasPrefix(line, " ") && line != "":
			in = line == section+":"
		case in && strings.HasPrefix(line, prefix):
			lines[i] = prefix + value
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("no %q line under %s", prefix, section)
	return ""
}

// dropEntry removes the only line starting with prefix.
func dropEntry(t *testing.T, policy, prefix string) string {
	t.Helper()
	var kept []string
	dropped := false
	for _, line := range strings.Split(policy, "\n") {
		if strings.HasPrefix(line, prefix) {
			dropped = true
			continue
		}
		kept = append(kept, line)
	}
	if !dropped {
		t.Fatalf("no %q line", prefix)
	}
	return strings.Join(kept, "\n")
}
