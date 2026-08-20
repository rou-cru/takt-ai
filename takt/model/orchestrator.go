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

import "errors"

// OrchestratorProjection contains harness-only runtime mechanics. Canonical policy lives in the catalog YAML.
type OrchestratorProjection struct {
	Platform, Delegate, Wait, Close, Question, Background, Models, Effort, Isolation, SkillRoot string
}

// Validate reports whether all projection fields are set.
func (p OrchestratorProjection) Validate() error {
	if p.Platform == "" || p.Delegate == "" || p.Wait == "" || p.Close == "" || p.Question == "" || p.Background == "" || p.Models == "" || p.Effort == "" || p.Isolation == "" || p.SkillRoot == "" {
		return errors.New("invalid orchestrator projection")
	}
	return nil
}
