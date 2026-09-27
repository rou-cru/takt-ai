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

package model_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestVFSCapabilityValuesAreExplicitAndClosed(t *testing.T) {
	for _, capability := range []model.VFSCapability{
		model.VFSCapabilityClaimList,
		model.VFSCapabilityClaimAssign,
		model.VFSCapabilityClaimRelease,
		model.VFSCapabilityBind,
		model.VFSCapabilityWrite,
		model.VFSCapabilityRead,
		model.VFSCapabilityDelete,
		model.VFSCapabilityDiscard,
		model.VFSCapabilityVerify,
		model.VFSCapabilityConsolidate,
	} {
		if !capability.Valid() {
			t.Errorf("%q should be valid", capability)
		}
	}
	for _, capability := range []model.VFSCapability{"", "inherit", "none", "all", "extend"} {
		if capability.Valid() {
			t.Errorf("%q should not be valid", capability)
		}
	}
}
