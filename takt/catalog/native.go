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

package catalog

import (
	"github.com/rou-cru/takt-ai/takt/model"
)

// NativeSubAgentContent holds target-neutral prose, without model choices.
type NativeSubAgentContent struct {
	ID           string `json:"id" yaml:"id"`
	Description  string `json:"description" yaml:"description"`
	Instructions string `json:"instructions" yaml:"instructions"`
	// Role decides the permission profile and whether users may talk to this specialist directly.
	Role model.RoleClass `json:"role" yaml:"role"`
}
