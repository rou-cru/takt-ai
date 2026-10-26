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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// gitMutations are mutating Git command classes, beyond the PR-VFS-GIT-5
// enumeration: history rewrite and publication, work discard, ref, stash,
// worktree, config and remote changes, writing options on read-only forms,
// program-running overrides, global options in front of the subcommand, and
// an unknown subcommand.
var gitMutations = []string{
	"git commit -m wip", "git commit --amend --no-edit", "git push", "git push --force origin trunk",
	"git rebase trunk", "git reset --hard HEAD~1", "git reset HEAD~1", "git reset --soft HEAD~1",
	"git clean -fdx", "git restore a.txt", "git checkout trunk", "git checkout -- a.txt", "git switch -c x",
	"git merge x", "git revert HEAD", "git cherry-pick HEAD", "git branch -D x", "git branch x",
	"git tag -d v1", "git tag v1", "git stash", "git stash pop", "git worktree add ../w",
	"git config user.name x", "git remote add o https://example.invalid/r.git", "git add .", "git rm a.txt",
	"git mv a.txt b.txt", "git apply x.patch", "git gc", "git update-ref refs/heads/x HEAD", "git fetch", "git pull",
	"git diff --output=patch.txt", "git diff --output patch.txt", "git log -p --output=log.txt",
	"git show --output=x HEAD", "git grep -O todo", "git grep --open-files-in-pager=vi todo",
	"git -c core.pager=true log", "git -c alias.st=!true st", "git --config-env=core.pager=X log", "git --exec-path=/tmp log",
	"git -C sub commit -m x", "git --git-dir=.git --work-tree=. reset --hard", "git --no-pager push",
	"git frobnicate",
}

// gitMilestones are milestone commits: a label, message or environment never
// turns one into an exempt operation.
var gitMilestones = []string{
	"git commit -m 'feat: milestone'", "TAKT_MILESTONE=1 git commit -m x", "git commit --no-verify -m milestone",
	"env MILESTONE=verified git commit -am x",
}

// gitReadOnly are read-only inspections every role keeps.
var gitReadOnly = []string{
	"git status", "git log --oneline -5", "git diff --stat HEAD", "git show HEAD:a.txt", "git blame a.txt",
	"git grep -n todo", "git rev-parse HEAD", "git ls-files",
}

// gitSpecialists are every role class but the orchestrator, plus a caller
// with no role.
var gitSpecialists = []model.RoleClass{
	"", model.RoleDirectInterlocutor, model.RolePlanningAuthor, model.RoleExecution,
	model.RoleVerification, model.RoleQualityAssurance, model.RoleMaintenance,
}

// Every orchestrator Git mutation, a milestone commit included,
// is held for confirmation of that exact command before it runs.
func TestOrchestratorMutationIsConfirmedBeforeEffects(t *testing.T) {
	for _, command := range append(append([]string{}, gitMutations...), gitMilestones...) {
		decision, reason := vfs.GitShellDecision(model.RoleOrchestrator, command)
		if decision != vfs.ShellAsk || !strings.Contains(reason, command) {
			t.Errorf("%q: decision = %q (%s); want an ask naming the exact command", command, decision, reason)
		}
	}
	for _, command := range []string{"cd sub && git push", "sh -c 'git reset'", "echo $(git stash)"} {
		if decision, _ := vfs.GitShellDecision(model.RoleOrchestrator, command); decision != vfs.ShellAsk {
			t.Errorf("%q: decision = %q; want a wrapped mutation asked", command, decision)
		}
	}
}

// Specialists stay denied every Git mutation and keep read-only inspection.
func TestSpecialistsDeniedReadOnlyKept(t *testing.T) {
	for _, role := range gitSpecialists {
		for _, command := range append(append([]string{}, gitMutations...), gitMilestones...) {
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
	readOnly := append([]string{"git -C sub status", "git --no-pager log -1", "git diff -- --output"}, gitReadOnly...)
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
		for _, command := range append(append([]string{}, gitMutations...), gitMilestones...) {
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

// PR-VFS-GIT-5: the orchestrator's native rules ask only for commit, push,
// rebase, clean, restore and hard reset; every other Git command is conduct.
func TestOrchestratorNativeRulesGateOnlyPublishRewriteDiscard(t *testing.T) {
	rules := vfs.GitPermissionRules(model.RoleOrchestrator)
	want := map[string]string{
		"git add .": vfs.ShellAllow, "git fetch": vfs.ShellAllow, "git branch x": vfs.ShellAllow,
		"git stash": vfs.ShellAllow, "git switch -c x": vfs.ShellAllow, "git merge x": vfs.ShellAllow,
		"git reset HEAD~1": vfs.ShellAllow, "git reset --soft HEAD~1": vfs.ShellAllow,
		"git status": vfs.ShellAllow, "git log --oneline -5": vfs.ShellAllow,
		"git commit": vfs.ShellAsk, "git commit -m wip": vfs.ShellAsk, "git push": vfs.ShellAsk,
		"git push --force origin trunk": vfs.ShellAsk, "git rebase trunk": vfs.ShellAsk,
		"git reset --hard HEAD": vfs.ShellAsk, "git reset HEAD --hard": vfs.ShellAsk,
		"git clean -fdx": vfs.ShellAsk, "git restore a.txt": vfs.ShellAsk,
	}
	for command, effect := range want {
		if got := gitEffect(rules, command); got != effect {
			t.Errorf("orchestrator, %q: native effect = %q; want %q", command, got, effect)
		}
	}
}

// gitCommand runs git in dir and returns its output.
func gitCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// gitState is the repository state a mutation could change.
func gitState(t *testing.T, dir string) string {
	t.Helper()
	config, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	return gitCommand(t, dir, "for-each-ref") + gitCommand(t, dir, "status", "--porcelain=v1", "--untracked-files=all") +
		gitCommand(t, dir, "stash", "list") + gitCommand(t, dir, "worktree", "list", "--porcelain") + string(config)
}

// Deciding, denying or refusing a Git mutation leaves a disposable
// repository unchanged; only a confirmed command, once run, changes it.
func TestRefusalLeavesRepositoryUnchanged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, root, "add", "a.txt")
	gitCommand(t, root, "commit", "-q", "-m", "base")
	state := filepath.Join(t.TempDir(), "private")
	f, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	before := gitState(t, root)

	specialist := vfs.Caller{SessionID: "s", HostSession: "host/fix/elsewhere", Agent: "fix", WorkUnitID: "elsewhere"}
	for i, command := range append(append([]string{}, gitMutations...), gitMilestones...) {
		// The orchestrator's ask is refused: nothing runs on that basis.
		if decision, _ := vfs.GitShellDecision(model.RoleOrchestrator, command); decision != vfs.ShellAsk {
			t.Fatalf("%q: decision = %q; want ask", command, decision)
		}
		// A specialist's shell plan is denied before any sandbox exists, and a
		// repeated call is decided afresh.
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
	if after := gitState(t, root); after != before {
		t.Fatalf("repository changed without a confirmed mutation:\nbefore %s\nafter %s", before, after)
	}
	// The fixture is sensitive: the same mutation, once confirmed and run, shows.
	gitCommand(t, root, "tag", "confirmed")
	if after := gitState(t, root); after == before {
		t.Fatal("repository state does not reflect a confirmed mutation")
	}
}
