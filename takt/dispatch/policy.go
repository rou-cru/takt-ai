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
	// Required blank import: the embed directive below depends on this package.
	_ "embed"
	"fmt"
	"sync"

	"gopkg.in/yaml.v2"
)

//go:embed admission_policy.yaml
var admissionPolicyYAML []byte

// loadAdmissionPolicy shares one parsed policy because the embedded content never changes at runtime.
var loadAdmissionPolicy = sync.OnceValues(func() (AdmissionPolicy, error) {
	return parseAdmissionPolicy(admissionPolicyYAML)
})

// AdmissionPolicyRef names the admission rule on every record.
const AdmissionPolicyRef = "harness.admission"

// AdmissionPolicy holds the ceiling and the session budgets so every fixed
// value is canonical policy data rather than a literal in an execution path
// (PR-HAR-16, PR-HAR-19..22, PR-HAR-29).
type AdmissionPolicy struct {
	Version     int `yaml:"version"`
	Concurrency struct {
		Specialists int `yaml:"specialists"`
	} `yaml:"concurrency"`
	Budgets struct {
		UnplannedUnits   int `yaml:"unplanned_units"`
		Contests         int `yaml:"contests"`
		RecoveryFailures int `yaml:"recovery_failures"`
		QARounds         int `yaml:"qa_rounds"`
	} `yaml:"budgets"`
	// Modules holds the size limit catalog prose cites for module layout.
	Modules struct {
		MaxFileLines int `yaml:"max_file_lines"`
	} `yaml:"modules"`
	// Recovery holds the admissible ceilings of the two budgets the orchestrator
	// declares per recovery (PR-ORQ-13).
	Recovery struct {
		MaxActions  int `yaml:"max_actions"`
		MaxAttempts int `yaml:"max_attempts"`
	} `yaml:"recovery"`
}

// LoadAdmissionPolicy returns the shared embedded policy; callers must not mutate the result.
func LoadAdmissionPolicy() (AdmissionPolicy, error) { return loadAdmissionPolicy() }

// parseAdmissionPolicy rejects unknown fields and a non-positive limit so a
// broken policy fails early instead of silently admitting without a limit. The
// error names the limit and the policy entry to correct (PR-HAR-29).
func parseAdmissionPolicy(data []byte) (AdmissionPolicy, error) {
	var p AdmissionPolicy
	if err := yaml.UnmarshalStrict(data, &p); err != nil {
		return AdmissionPolicy{}, fmt.Errorf("admission policy YAML: %w", err)
	}
	if p.Version != 1 {
		return AdmissionPolicy{}, fmt.Errorf("admission policy: unsupported version %d", p.Version)
	}
	// A missing entry decodes to zero, so it is refused like an explicit zero.
	for _, limit := range []struct {
		entry string
		value int
	}{
		{"concurrency.specialists", p.Concurrency.Specialists},
		{"budgets.unplanned_units", p.Budgets.UnplannedUnits},
		{"budgets.contests", p.Budgets.Contests},
		{"budgets.recovery_failures", p.Budgets.RecoveryFailures},
		{"budgets.qa_rounds", p.Budgets.QARounds},
		{"modules.max_file_lines", p.Modules.MaxFileLines},
		{"recovery.max_actions", p.Recovery.MaxActions},
		{"recovery.max_attempts", p.Recovery.MaxAttempts},
	} {
		if limit.value <= 0 {
			return AdmissionPolicy{}, fmt.Errorf("admission policy: %s is %d, a limit must be positive; set a positive value for %s in admission_policy.yaml", limit.entry, limit.value, limit.entry)
		}
	}
	return p, nil
}
