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

package gc_test

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/obs"
)

func TestParseAcceptanceResultRejectsAnythingOutsideTheSet(t *testing.T) {
	for _, valid := range []string{"pass", "regress"} {
		got, err := gc.ParseAcceptanceResult(valid)
		if err != nil || string(got) != valid {
			t.Errorf("ParseAcceptanceResult(%q) = %q, %v", valid, got, err)
		}
	}
	for _, bad := range []string{"", "passed", "PASS", "ok", "fail"} {
		if _, err := gc.ParseAcceptanceResult(bad); err == nil {
			t.Errorf("ParseAcceptanceResult(%q) = nil error; a typo must never read as a passing cycle", bad)
		}
	}
}

func TestClosureDiscardsOnlyOnRegression(t *testing.T) {
	stands := gc.Closure{CycleID: "c1", Mandate: gc.MandateDeadCode, Result: gc.AcceptancePass}
	if stands.Discarded() {
		t.Error("a passing cycle must stand")
	}
	undone := gc.Closure{CycleID: "c1", Mandate: gc.MandateDeadCode, Result: gc.AcceptanceRegress}
	if !undone.Discarded() {
		t.Error("a regressed cycle must be discarded in full")
	}
}

func TestClosureRecordEscalatesOnlyWhenDiscarded(t *testing.T) {
	stands := gc.Closure{CycleID: "c1", Mandate: gc.MandateDeadCode, Result: gc.AcceptancePass}.ControlRecord("simplify")
	if stands.ActionClass != obs.ActionObserve {
		t.Errorf("completed cycle = %s, want OBSERVE", stands.ActionClass)
	}
	if !strings.Contains(stands.TriggeringCondition, "completed") || stands.PolicyRef != gc.AcceptancePolicyRef {
		t.Errorf("record = %+v", stands)
	}

	undone := gc.Closure{
		CycleID: "c1", Mandate: gc.MandateDeadCode, Result: gc.AcceptanceRegress,
		Reason: gc.ReasonAcceptanceRegression, Restored: []string{"a.go", "b.go"},
	}.ControlRecord("simplify")
	// A discarded cycle is a problem-rate signal, so it must be visible.
	if undone.ActionClass != obs.ActionEscalate {
		t.Errorf("discarded cycle = %s, want ESCALATE", undone.ActionClass)
	}
	for _, want := range []string{"discarded", "cycle=c1", "mandate=dead-code", gc.ReasonAcceptanceRegression, "restored=2"} {
		if !strings.Contains(undone.TriggeringCondition, want) {
			t.Errorf("condition %q missing %q", undone.TriggeringCondition, want)
		}
	}
	if err := undone.Validate(); err != nil {
		t.Errorf("Validate() = %v", err)
	}
}
