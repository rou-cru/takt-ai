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

// The staging/collision/consolidate/journal/isolation invariants this file
// used to test directly through FS's now-removed non-durable API (Write,
// Read, Delete, AttachVerdict, Consolidate, ...) are exercised through the
// production entry point (Bind + Apply + Verify + ConsolidateCheckpoint) in
// durable_test.go instead — see TestGateRevisionIdentityAndReplay,
// TestConcurrentUnitCorrelation and friends. Only the standalone git-guard
// helpers, which never depended on that API, remain here.
package vfs_test

import (
	"errors"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestGuardGitMutation_ExecutionDenied(t *testing.T) {
	// Execution specialists must not mutate git.
	if err := vfs.GuardGitMutation(model.RoleExecution); !errors.Is(err, vfs.ErrGitMutationDenied) {
		t.Errorf("GuardGitMutation(execution) = %v; want ErrGitMutationDenied", err)
	}
}

func TestGuardGitMutation_OrchestratorAllowed(t *testing.T) {
	// Only the orchestrator may mutate git, so history stays controlled.
	if err := vfs.GuardGitMutation(model.RoleOrchestrator); err != nil {
		t.Errorf("GuardGitMutation(orchestrator) = %v; want nil", err)
	}
}

func TestGuardGitMutation_AllNonOrchestratorRolesDenied(t *testing.T) {
	denied := []model.RoleClass{
		model.RoleDirectInterlocutor,
		model.RolePlanningAuthor,
		model.RoleExecution,
		model.RoleVerification,
		model.RoleMaintenance,
	}
	for _, role := range denied {
		if err := vfs.GuardGitMutation(role); !errors.Is(err, vfs.ErrGitMutationDenied) {
			t.Errorf("role %q: GuardGitMutation = %v; want ErrGitMutationDenied", role, err)
		}
	}
}

func TestGuardShellGit_IndirectionDenied(t *testing.T) {
	executed := []string{
		"git commit -m x", "command git commit -m x", "env GIT_DIR=x git reset --hard", "FOO=1 git push",
		`sh -c "git clean -fd"`, `bash -c 'git checkout x'`, "/usr/bin/git checkout x", "cd a && git push",
		"ls | git apply", "echo $(git stash)", `echo "$(git stash)"`, "eval git reset", "timeout 10 git push",
		"find . -name x -exec git rm {} ;", "sudo -n git commit",
	}
	for _, command := range executed {
		if err := vfs.GuardShellGit(command); !errors.Is(err, vfs.ErrGitMutationDenied) {
			t.Errorf("GuardShellGit(%q) = %v; want ErrGitMutationDenied", command, err)
		}
	}
	notExecuted := []string{
		"command git status", "git log -1", "git", "ls", "grep -r git .", `grep -rn "git commit" docs`,
		"echo git push", "rg 'git reset' && cat .gitignore", `sh -c "git diff"`, "cat git/commit.txt",
	}
	for _, command := range notExecuted {
		if err := vfs.GuardShellGit(command); err != nil {
			t.Errorf("GuardShellGit(%q) = %v; want nil", command, err)
		}
	}
}
