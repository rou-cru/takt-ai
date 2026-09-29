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

package obs_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
)

func TestUnappliedFindingRejectsUnlistedNestedAndIncomplete(t *testing.T) {
	_, clock := newBus(t)
	e := obs.NewEnvelope(clock, obs.PlaneLifecycle, "collector")
	e.EventClass = obs.EventGCFindingUnapplied
	e.Attributes = map[string]any{"cycle_id": "c", "mandate_class": "m", "finding_kind": "voluminous", "count": 1}
	if err := e.Validate(); err != nil {
		t.Errorf("listed attributes rejected: %v", err)
	}
	for _, attrs := range []map[string]any{
		{"snippet": "func x() {}"},
		{"mandate_class": []string{"x"}},
		{"cycle_id": map[string]any{"n": 1}},
		{"content": "code"},
	} {
		e.Attributes = attrs
		if err := e.Validate(); err == nil {
			t.Errorf("accepted %v", attrs)
		}
	}
}
