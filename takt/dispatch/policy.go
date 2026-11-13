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
	"errors"
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
// (PR-HAR-16, PR-HAR-19..22).
type AdmissionPolicy struct {
	Version     int `yaml:"version"`
	Concurrency struct {
		Specialists int `yaml:"specialists"`
	} `yaml:"concurrency"`
	Budgets struct {
		UnplannedUnits   int `yaml:"unplanned_units"`
		Contests         int `yaml:"contests"`
		RecoveryFailures int `yaml:"recovery_failures"`
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

// parseAdmissionPolicy rejects unknown fields and a non-positive ceiling so a
// broken policy fails early instead of silently admitting without a limit.
func parseAdmissionPolicy(data []byte) (AdmissionPolicy, error) {
	var p AdmissionPolicy
	if err := yaml.UnmarshalStrict(data, &p); err != nil {
		return AdmissionPolicy{}, fmt.Errorf("admission policy YAML: %w", err)
	}
	if p.Version != 1 {
		return AdmissionPolicy{}, fmt.Errorf("admission policy: unsupported version %d", p.Version)
	}
	if p.Recovery.MaxActions <= 0 || p.Recovery.MaxAttempts <= 0 {
		return AdmissionPolicy{}, errors.New("admission policy: every declared recovery budget needs a positive ceiling")
	}
	if p.Concurrency.Specialists <= 0 || p.Budgets.UnplannedUnits <= 0 || p.Budgets.Contests <= 0 || p.Budgets.RecoveryFailures <= 0 || p.Modules.MaxFileLines <= 0 {
		return AdmissionPolicy{}, errors.New("admission policy: every ceiling, budget and size limit must be positive")
	}
	return p, nil
}
