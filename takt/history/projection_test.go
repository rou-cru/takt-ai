// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

func TestRecoveryUnresolved(t *testing.T) {
	tests := []struct {
		name string
		r    history.Recovery
		want bool
	}{
		{"open recovery is unresolved", history.Recovery{Open: true}, true},
		{"backtracked and not restored is unresolved", history.Recovery{Backtracked: true, Restored: false}, true},
		{"backtracked and restored is resolved", history.Recovery{Backtracked: true, Restored: true}, false},
		{"closed and never backtracked is resolved", history.Recovery{Open: false, Backtracked: false}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Unresolved(); got != tc.want {
				t.Errorf("Recovery(%+v).Unresolved() = %v, want %v", tc.r, got, tc.want)
			}
		})
	}
}

func TestBudgetsScope(t *testing.T) {
	budgets := history.Budgets{
		Recoveries: map[string]history.Recovery{
			"objective-a": {Open: true, Scope: []string{"unit-1", "unit-2"}},
			"objective-b": {Open: false, Backtracked: false, Scope: []string{"unit-3"}},
		},
	}

	objective, recovery := budgets.Scope("unit-1")
	if objective != "objective-a" {
		t.Errorf("Scope(unit-1) objective = %q, want %q", objective, "objective-a")
	}
	if !recovery.Open {
		t.Errorf("Scope(unit-1) recovery = %+v, want the open objective-a recovery", recovery)
	}

	objective, _ = budgets.Scope("unit-3")
	if objective != "" {
		t.Errorf("Scope(unit-3) objective = %q, want empty since objective-b's recovery is resolved", objective)
	}

	objective, _ = budgets.Scope("unit-not-scoped")
	if objective != "" {
		t.Errorf("Scope(unrelated unit) objective = %q, want empty", objective)
	}
}

func TestProjectionInFlight(t *testing.T) {
	p := history.Projection{Units: map[string]history.Unit{
		"a": {State: history.StateInFlight},
		"b": {State: history.StateInFlight},
		"c": {State: history.StateSettled},
		"d": {State: history.StatePlanned},
	}}
	if got := p.InFlight(); got != 2 {
		t.Errorf("InFlight() = %d, want 2", got)
	}
}

func TestProjectionBudgetsDefaultsWhenAbsent(t *testing.T) {
	p := history.Projection{Sessions: map[string]*history.Budgets{}}
	got := p.Budgets("no-such-session")
	want := history.Budgets{}
	if got.Unplanned != want.Unplanned || got.Stopped != want.Stopped {
		t.Errorf("Budgets(absent session) = %+v, want the zero value", got)
	}
}

func TestAllowanceKey(t *testing.T) {
	if got := history.AllowanceKey(history.BoundConcurrency, ""); got != history.BoundConcurrency {
		t.Errorf("AllowanceKey with empty objective = %q, want the bound alone (%q)", got, history.BoundConcurrency)
	}
	want := history.BoundRecoveryActions + "/my-objective"
	if got := history.AllowanceKey(history.BoundRecoveryActions, "my-objective"); got != want {
		t.Errorf("AllowanceKey with an objective = %q, want %q", got, want)
	}
}
