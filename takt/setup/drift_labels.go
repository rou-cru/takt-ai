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

package setup

// DriftLabel maps a conflict reason to one display label, severity, and explanation.
// CLI and TUI render from here so drift reasons always read the same.
func DriftLabel(reason string) (label, severity, explanation string) {
	switch reason {
	case "missing":
		return "missing/corrupt", "danger", "expected on disk but not found"
	case "takt-additions":
		return "pre-existing", "warning", "still contains settings Takt added"
	case "user-edited":
		return "modified", "warning", "content no longer matches what Takt deployed"
	default:
		return "pre-existing", "warning", "unmanaged file Takt would otherwise overwrite"
	}
}
