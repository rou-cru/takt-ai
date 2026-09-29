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

package gc

import (
	"fmt"
	"strings"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// AcceptancePolicyRef names the rule closing a cycle.
const AcceptancePolicyRef = "gc.acceptance"

// ReasonAcceptanceRegression explains a cycle undone after it consolidated.
const ReasonAcceptanceRegression = "acceptance_regression"

// AcceptanceResult is how the acceptance checks stood.
type AcceptanceResult string

// Acceptance results are how the closing checks stood.
const (
	// AcceptancePass means the checks held, so the cycle stands.
	AcceptancePass AcceptanceResult = "pass"
	// AcceptanceRegress means previously passing checks now fail.
	AcceptanceRegress AcceptanceResult = "regress"
)

// acceptanceResults is the closed set, in stable order for errors.
var acceptanceResults = []AcceptanceResult{AcceptancePass, AcceptanceRegress}

// ParseAcceptanceResult rejects anything outside the closed set, so a typo
// never reads as a passing cycle.
func ParseAcceptanceResult(value string) (AcceptanceResult, error) {
	for _, r := range acceptanceResults {
		if AcceptanceResult(value) == r {
			return r, nil
		}
	}
	return "", fmt.Errorf("gc: unknown acceptance result %q (valid: %s)", value, joinResults())
}

// joinResults renders the closed set for an error message.
func joinResults() string {
	names := make([]string, 0, len(acceptanceResults))
	for _, r := range acceptanceResults {
		names = append(names, string(r))
	}
	return strings.Join(names, ", ")
}

// Closure is how a cycle ended once acceptance ran.
type Closure struct {
	CycleID string       `json:"cycle_id"`
	Mandate MandateClass `json:"mandate_class,omitempty"`
	// Result is how the closing checks stood.
	Result AcceptanceResult `json:"acceptance"`
	// Restored names the paths put back, empty when the cycle stands.
	Restored []string `json:"restored,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// Discarded reports whether the cycle has to be undone in full.
func (c Closure) Discarded() bool { return c.Result == AcceptanceRegress }

// ControlRecord renders the end of a cycle for the control bus.
func (c Closure) ControlRecord(agent string) obs.ControlRecord {
	class, outcome := obs.ActionObserve, "completed"
	if c.Discarded() {
		class, outcome = obs.ActionEscalate, "discarded"
	}
	cond := fmt.Sprintf("gc cycle %s (cycle=%s mandate=%s acceptance=%s)", outcome, c.CycleID, c.Mandate, c.Result)
	if c.Reason != "" {
		cond += " reason=" + c.Reason
	}
	if n := len(c.Restored); n > 0 {
		cond += fmt.Sprintf(" restored=%d", n)
	}
	return obs.ControlRecord{ActionClass: class, TriggeringCondition: cond, PolicyRef: AcceptancePolicyRef, ActingAgent: agent}
}
