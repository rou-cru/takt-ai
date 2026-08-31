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

package shared

// NewManagedPaths builds a target adapter's list of Takt-owned paths relative
// to the user's home directory: the adapter's native paths plus its
// renderer-generated paths, normalized and sorted. Every path not listed is
// outside the adapter's ownership contract and is preserved by future
// lifecycle consumers. It returns an error if a path is invalid or duplicated.
func NewManagedPaths(label string, nativePaths, generatedPaths []string) ([]string, error) {
	return NormalizeManagedPaths(label, nativePaths, generatedPaths)
}
