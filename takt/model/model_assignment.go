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

// ModelAssignment pairs one model with its effort so each sub-agent has a single clear runtime choice.
type ModelAssignment struct {
	// Model holds the target-specific model id, explicit so typos fail fast instead of silently defaulting.
	Model string // target-specific model identifier
	// Effort holds the effort level; empty means target default, so callers can omit what they do not tune.
	Effort string // "" = target default; "low" | "medium" | "high"
}
