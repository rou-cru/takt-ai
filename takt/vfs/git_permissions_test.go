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

package vfs_test

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// gitMutations hold one mutating command per classification branch: a
// mutating subcommand, one behind a global option, a program-running
// override, a writing option on a read-only subcommand, a wrapper prefix and
// an unknown subcommand.
var gitMutations = []string{
	"git commit -m wip",
	"git -C sub commit -m x",
	"git -c core.pager=true log",
	"git diff --output=patch.txt",
	"TAKT_MILESTONE=1 git commit -m x",
	"git frobnicate",
}

// gitReadOnly are read-only inspections every role keeps.
var gitReadOnly = []string{"git status", "git diff --stat HEAD"}

// gitSpecialists are every role class but the orchestrator, plus a caller
// with no role.
var gitSpecialists = []model.RoleClass{
	"", model.RoleDirectInterlocutor, model.RolePlanningAuthor, model.RoleExecution,
	model.RoleVerification, model.RoleQualityAssurance, model.RoleMaintenance,
}

// Every orchestrator Git mutation is held for confirmation of that exact
// command before it runs.
func TestOrchestratorMutationIsConfirmedBeforeEffects(t *testing.T) {
	for _, command := range gitMutations {
		decision, reason := vfs.GitShellDecision(model.RoleOrchestrator, command)
		if decision != vfs.ShellAsk || !strings.Contains(reason, command) {
			t.Errorf("%q: decision = %q (%s); want an ask naming the exact command", command, decision, reason)
		}
	}
}

// Specialists stay denied every Git mutation and keep read-only inspection.
func TestSpecialistsDeniedReadOnlyKept(t *testing.T) {
	for _, role := range gitSpecialists {
		for _, command := range gitMutations {
			if decision, reason := vfs.GitShellDecision(role, command); decision != vfs.ShellDeny || !strings.Contains(reason, command) {
				t.Errorf("role %q, %q: decision = %q (%s); want a denial naming the command", role, command, decision, reason)
			}
		}
	}
	for _, command := range gitMutations {
		if err := vfs.GuardShellGit(command); !errors.Is(err, vfs.ErrGitMutationDenied) {
			t.Errorf("GuardShellGit(%q) = %v; want ErrGitMutationDenied", command, err)
		}
	}
	// A global option before a read-only subcommand, and a writing option's
	// name after "--", keep it read-only.
	readOnly := append([]string{"git -C sub status", "git diff -- --output"}, gitReadOnly...)
	for _, command := range readOnly {
		for _, role := range append([]model.RoleClass{model.RoleOrchestrator}, gitSpecialists...) {
			if decision, reason := vfs.GitShellDecision(role, command); decision != vfs.ShellAllow || reason != "" {
				t.Errorf("role %q, %q: decision = %q (%s); want read-only inspection allowed", role, command, decision, reason)
			}
		}
		if err := vfs.GuardShellGit(command); err != nil {
			t.Errorf("GuardShellGit(%q) = %v; want read-only inspection allowed", command, err)
		}
	}
}

// gitEffect evaluates rules with last-match-wins wildcard matching, as the
// host's native permission does.
func gitEffect(rules []vfs.GitPermissionRule, command string) string {
	effect := ""
	for _, rule := range rules {
		pattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(rule.Pattern), regexp.QuoteMeta("*"), ".*") + "$"
		if regexp.MustCompile(pattern).MatchString(command) {
			effect = rule.Effect
		}
	}
	return effect
}

// Specialists' native rules deny every Git mutation and allow
// read-only inspection.
func TestNativeRulesProjectTheDecision(t *testing.T) {
	for _, role := range gitSpecialists {
		rules := vfs.GitPermissionRules(role)
		for _, command := range gitMutations {
			// Native patterns match the command text; an environment prefix is
			// the guard's to see through, not the pattern's.
			if !strings.HasPrefix(command, "git ") {
				continue
			}
			if got := gitEffect(rules, command); got != vfs.ShellDeny {
				t.Errorf("role %q, %q: native effect = %q; want deny", role, command, got)
			}
		}
		for _, command := range gitReadOnly {
			if got := gitEffect(rules, command); got != vfs.ShellAllow {
				t.Errorf("role %q, %q: native effect = %q; want allow", role, command, got)
			}
		}
	}
}

// PR-VFS-GIT-5: the orchestrator's native rules ask only for publishing,
// rewriting or discarding git, a hard reset included; every other Git
// command is conduct.
func TestOrchestratorNativeRulesGateOnlyPublishRewriteDiscard(t *testing.T) {
	rules := vfs.GitPermissionRules(model.RoleOrchestrator)
	want := map[string]string{
		"git add .": vfs.ShellAllow, "git reset HEAD~1": vfs.ShellAllow, "git status": vfs.ShellAllow,
		"git commit": vfs.ShellAsk, "git push --force origin trunk": vfs.ShellAsk,
		"git reset --hard HEAD": vfs.ShellAsk, "git reset HEAD --hard": vfs.ShellAsk,
	}
	for command, effect := range want {
		if got := gitEffect(rules, command); got != effect {
			t.Errorf("orchestrator, %q: native effect = %q; want %q", command, got, effect)
		}
	}
}

// A specialist's shell plan for a Git mutation is denied before any sandbox
// exists, and a repeated call is decided afresh.
func TestRefusalLeavesRepositoryUnchanged(t *testing.T) {
	f, _, state := newStore(t)
	specialist := vfs.Caller{SessionID: "s", HostSession: "host/fix/elsewhere", Agent: "fix", WorkUnitID: "elsewhere"}
	for i, command := range gitMutations {
		for attempt := range 2 {
			plan, err := f.PrepareShell(specialist, fmt.Sprintf("rq20-%d-%d", i, attempt), command, state)
			if err != nil {
				t.Fatalf("%q: %v", command, err)
			}
			if plan.Decision != vfs.ShellDeny || plan.Cwd != "" || plan.Writable != nil {
				t.Fatalf("%q: plan = %+v; want a denial with no sandbox", command, plan)
			}
		}
	}
}
