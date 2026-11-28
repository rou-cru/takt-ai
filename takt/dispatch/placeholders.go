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

package dispatch

import "github.com/rou-cru/takt-ai/takt/catalog"

// ResolvePolicyPlaceholders replaces each {{policy.<name>}} marker in catalog
// prose with the admission policy's value. A marker left unresolved is an
// error: unresolved prose would hand an agent a limit that does not exist.
func ResolvePolicyPlaceholders(content []byte) ([]byte, error) {
	return catalog.ResolvePolicyPlaceholders(content)
}
