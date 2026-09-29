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

import (
	"fmt"
	"slices"
)

// RoleClass names one fixed permission profile so every specialist gets exactly the access it needs.
type RoleClass string

// Role classes are the closed set of permission profiles.
const (
	// RoleOrchestrator names the dispatch owner, the only role with git-write rights so history stays controlled.
	RoleOrchestrator RoleClass = "orchestrator"
	// RoleDirectInterlocutor names the user-facing role, the only voice allowed to hold the conversation.
	RoleDirectInterlocutor RoleClass = "direct_interlocutor"
	// RolePlanningAuthor names the spec and design author, separate so plans stay reviewable before execution.
	RolePlanningAuthor RoleClass = "planning_author"
	// RoleExecution names the contract implementer, fenced to file work so builds stay reproducible.
	RoleExecution RoleClass = "execution"
	// RoleVerification names the evidence judge, internal so verdicts stay independent of user chat.
	RoleVerification RoleClass = "verification"
	// RoleMaintenance names the harness-controlled cycle runner (GC, dreaming), never invocable as an interlocutor.
	RoleMaintenance RoleClass = "maintenance"
)

// roleClasses lists every allowed class in stable order so validation and errors stay consistent.
var roleClasses = []RoleClass{
	RoleOrchestrator,
	RoleDirectInterlocutor,
	RolePlanningAuthor,
	RoleExecution,
	RoleVerification,
	RoleMaintenance,
}

// Valid guards dispatch by rejecting unknown classes early instead of failing mid-run.
func (r RoleClass) Valid() bool { return slices.Contains(roleClasses, r) }

// HoldsInterface marks who may speak with the user so only one role can hold the conversation.
func (r RoleClass) HoldsInterface() bool { return r == RoleDirectInterlocutor }

// ValidateRoleClass fails fast on missing or unknown classes so bad configs surface before dispatch.
func ValidateRoleClass(subject string, r RoleClass) error {
	if r == "" {
		return fmt.Errorf("%s: missing role class; a specialist without a declared class cannot be dispatched", subject)
	}
	if !r.Valid() {
		return fmt.Errorf("%s: unknown role class %q; must be one of %v", subject, r, roleClasses)
	}
	return nil
}
