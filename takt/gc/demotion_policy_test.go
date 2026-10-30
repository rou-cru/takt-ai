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
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
)

// TestTriggerPolicyDefinesDemotion checks that PR-MNT-32's demotion
// parameters come from the canonical policy data and are usable as a rate
// threshold and a sample floor.
func TestTriggerPolicyDefinesDemotion(t *testing.T) {
	p, err := gc.LoadTriggerPolicy()
	if err != nil {
		t.Fatalf("LoadTriggerPolicy() error = %v", err)
	}
	if r := p.Demotion.ReversionRate; r <= 0 || r >= 1 {
		t.Errorf("demotion reversion rate = %v, want a fraction in (0, 1)", r)
	}
	if p.Demotion.MinRegisteredWork <= 0 {
		t.Errorf("demotion min registered work = %d, want positive", p.Demotion.MinRegisteredWork)
	}
}
