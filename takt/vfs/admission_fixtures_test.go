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

package vfs

// Fixtures the white-box tests of this package still share.
const (
	// escapingPath leaves the workspace, so no scope or invariant may name it.
	escapingPath = "../escape"
	// noGrantInstance is a catalog instance that declares an explicit empty grant list.
	noGrantInstance = "pm"
)

func id(agent, unit, specialist string) Identity {
	return Identity{SessionID: "s", WorkUnitID: unit, AgentID: AgentID(agent), Specialist: specialist}
}
