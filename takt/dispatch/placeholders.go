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

package dispatch

import (
	"bytes"
	"fmt"
	"strconv"
)

// policyPlaceholderPrefix opens every marker catalog prose uses to carry an
// admission value, so prose and policy never hold two copies of one number.
const policyPlaceholderPrefix = "{{policy."

// ResolvePolicyPlaceholders replaces each {{policy.<name>}} marker in catalog
// prose with the admission policy's value. A marker left unresolved is an
// error: unresolved prose would hand an agent a limit that does not exist.
func ResolvePolicyPlaceholders(content []byte) ([]byte, error) {
	if !bytes.Contains(content, []byte(policyPlaceholderPrefix)) {
		return content, nil
	}
	p, err := LoadAdmissionPolicy()
	if err != nil {
		return nil, err
	}
	values := map[string]int{
		"concurrent_specialists": p.Concurrency.Specialists,
		"unplanned_units":        p.Budgets.UnplannedUnits,
		"contests":               p.Budgets.Contests,
		"recovery_failures":      p.Budgets.RecoveryFailures,
		"recovery_max_actions":   p.Recovery.MaxActions,
		"recovery_max_attempts":  p.Recovery.MaxAttempts,
	}
	for name, value := range values {
		content = bytes.ReplaceAll(content, []byte(policyPlaceholderPrefix+name+"}}"), []byte(strconv.Itoa(value)))
	}
	if i := bytes.Index(content, []byte(policyPlaceholderPrefix)); i >= 0 {
		end := min(i+len(policyPlaceholderPrefix)+40, len(content))
		return nil, fmt.Errorf("unresolved policy placeholder near %q", content[i:end])
	}
	return content, nil
}
