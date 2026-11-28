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

// AdmissionPolicyRef names the admission rule on every record.
const AdmissionPolicyRef = catalog.AdmissionPolicyRef

// AdmissionPolicy holds the ceiling and the session budgets so every fixed
// value is canonical policy data rather than a literal in an execution path
// (PR-HAR-16, PR-HAR-19..22, PR-HAR-29).
type AdmissionPolicy = catalog.AdmissionPolicy

// LoadAdmissionPolicy returns the shared embedded policy; callers must not mutate the result.
func LoadAdmissionPolicy() (AdmissionPolicy, error) { return catalog.LoadAdmissionPolicy() }
