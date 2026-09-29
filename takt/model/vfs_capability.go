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

package model

// VFSCapability names one VFS operation that an agent instance may use.
// Instances with no VFS access declare an explicit empty capability list.
type VFSCapability string

const (
	// VFSCapabilityClaimList lets the orchestrator list claims.
	VFSCapabilityClaimList VFSCapability = "claim_list"
	// VFSCapabilityClaimAssign lets the orchestrator assign claims.
	VFSCapabilityClaimAssign VFSCapability = "claim_assign"
	// VFSCapabilityClaimRelease lets the orchestrator release claims.
	VFSCapabilityClaimRelease VFSCapability = "claim_release"
	// VFSCapabilityBind lets an instance establish a VFS binding.
	VFSCapabilityBind VFSCapability = "bind"
	// VFSCapabilityWrite lets an instance stage file writes.
	VFSCapabilityWrite VFSCapability = "write"
	// VFSCapabilityRead lets an instance read workspace or staged content.
	VFSCapabilityRead VFSCapability = "read"
	// VFSCapabilityDelete lets an instance stage file deletions.
	VFSCapabilityDelete VFSCapability = "delete"
	// VFSCapabilityDiscard lets the orchestrator discard an author's staged changes.
	VFSCapabilityDiscard VFSCapability = "discard"
	// VFSCapabilityVerify lets an instance verify another instance's delta.
	VFSCapabilityVerify VFSCapability = "verify"
	// VFSCapabilityConsolidate lets the orchestrator consolidate verified work.
	VFSCapabilityConsolidate VFSCapability = "consolidate"
)

// Valid reports whether c names one of the closed set of VFS operations.
func (c VFSCapability) Valid() bool {
	switch c {
	case VFSCapabilityClaimList, VFSCapabilityClaimAssign, VFSCapabilityClaimRelease,
		VFSCapabilityBind, VFSCapabilityWrite,
		VFSCapabilityRead, VFSCapabilityDelete, VFSCapabilityDiscard,
		VFSCapabilityVerify, VFSCapabilityConsolidate:
		return true
	default:
		return false
	}
}
