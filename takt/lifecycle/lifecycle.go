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

// Package lifecycle dispatches the install, sync, and uninstall orchestration
// shared by the CLI (cmd/takt-ai) and the TUI runtime. It lives outside
// package setup because skills.BuildSkillPlan depends on setup, so hosting
// this dispatch in setup would create an import cycle; lifecycle sits above
// both and composes them.
package lifecycle

import (
	"fmt"

	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/skills"
)

// LifecycleResult is the union of deploy and uninstall outcomes.
type LifecycleResult struct {
	Changed   []string
	Unchanged []string
	Removed   []string
	Preserved []string
}

// RunLifecycle executes one lifecycle action ("install", "sync", "uninstall")
// for the targets in request. Install and sync build target plans from the
// request and always deploy the embedded skills alongside them; uninstall
// converts request.Targets to ownership targets and always removes skills
// alongside them. Callers without an explicit request build one with
// setup.DefaultPlanRequest(targets).
func RunLifecycle(action string, rootDir string, request setup.PlanRequest) (LifecycleResult, error) {
	switch action {
	case "install", "sync":
		plans, err := setup.BuildTargetPlans(request)
		if err != nil {
			return LifecycleResult{}, err
		}
		skillPlan, err := skills.BuildSkillPlan()
		if err != nil {
			return LifecycleResult{}, err
		}
		plans = append(plans, skillPlan)
		if action == "install" {
			result, err := setup.Apply(rootDir, plans)
			return LifecycleResult{Changed: result.Changed, Unchanged: result.Unchanged}, err
		}
		result, err := setup.Sync(rootDir, plans)
		return LifecycleResult{Changed: result.Changed, Unchanged: result.Unchanged}, err
	case "uninstall":
		targets := make([]setup.OwnershipTarget, 0, len(request.Targets))
		for _, id := range request.Targets {
			ownershipTarget, err := setup.OwnershipTargetFor(string(id))
			if err != nil {
				return LifecycleResult{}, err
			}
			targets = append(targets, ownershipTarget)
		}
		// Skills are always removed alongside any target uninstall.
		targets = append(targets, setup.TargetSkills)
		result, err := setup.Uninstall(rootDir, targets...)
		return LifecycleResult{Removed: result.Removed, Preserved: result.Preserved}, err
	default:
		return LifecycleResult{}, fmt.Errorf("unsupported action %q", action)
	}
}
