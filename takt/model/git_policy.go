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

import "slices"

// Native shell permission effects projected from role-based Git policy.
const (
	GitEffectAllow = "allow"
	GitEffectAsk   = "ask"
	GitEffectDeny  = "deny"
)

// GitPermissionRule is one native shell permission over a Git command
// pattern, where "*" matches any text.
type GitPermissionRule struct {
	Pattern string
	Effect  string
}

// orchestratorGatedGit are the subcommands whose every use needs the user's
// approval when the orchestrator runs them: they publish or rewrite history
// or discard work (PR-VFS-GIT-5). A local commit needs none; an amended
// commit and a hard reset are gated by option, not here.
var orchestratorGatedGit = []string{"push", "rebase", "clean", "restore"}

// readOnlyGitSubcommands never write to the repository, whatever their
// arguments; subcommands that list or mutate by argument (branch, tag, stash,
// reflog, config, remote, worktree) stay out.
var readOnlyGitSubcommands = []string{
	"diff", "log", "show", "blame", "status", "ls-tree", "ls-files", "rev-parse", "cat-file", "grep",
	"shortlog", "describe", "merge-base", "rev-list", "for-each-ref", "check-ignore", "name-rev",
}

// gitWritingOptions are the options that make a read-only subcommand write a
// file or run a program, so they do not inherit its read-only exemption.
var gitWritingOptions = map[string][]string{
	"diff": {"--output"},
	"log":  {"--output"},
	"show": {"--output"},
	"grep": {"-O", "--open-files-in-pager"},
}

// GitPermissionRules is role's Git policy as native shell permission
// patterns, in evaluation order for a host where the last matching rule wins. Every role but the orchestrator denies every git command, then
// allows each read-only subcommand, and a read-only subcommand carrying one
// of its writing options returns to the deny; a form the patterns cannot tell
// apart from a mutation keeps the deny. The orchestrator allows git and asks
// only for the actions in orchestratorGatedGit, for an amended commit and
// for a hard reset (PR-VFS-GIT-5); every other Git mutation, a plain local
// commit included, is its conduct.
func GitPermissionRules(role RoleClass) []GitPermissionRule {
	if role == RoleOrchestrator {
		rules := []GitPermissionRule{{"git *", GitEffectAllow}}
		for _, sub := range orchestratorGatedGit {
			// "git * <sub>" also catches the subcommand behind leading options
			// such as -C or --no-pager.
			rules = append(rules, GitPermissionRule{"git " + sub, GitEffectAsk}, GitPermissionRule{"git " + sub + " *", GitEffectAsk},
				GitPermissionRule{"git * " + sub, GitEffectAsk}, GitPermissionRule{"git * " + sub + " *", GitEffectAsk})
		}
		return append(rules, GitPermissionRule{"git commit *--amend*", GitEffectAsk}, GitPermissionRule{"git * commit *--amend*", GitEffectAsk},
			GitPermissionRule{"git reset *--hard*", GitEffectAsk}, GitPermissionRule{"git *reset *--hard*", GitEffectAsk})
	}
	rules := []GitPermissionRule{{"git *", GitEffectDeny}}
	for _, sub := range readOnlyGitSubcommands {
		rules = append(rules, GitPermissionRule{"git " + sub, GitEffectAllow}, GitPermissionRule{"git " + sub + " *", GitEffectAllow})
	}
	for _, sub := range readOnlyGitSubcommands {
		for _, option := range gitWritingOptions[sub] {
			rules = append(rules, GitPermissionRule{"git " + sub + " *" + option + "*", GitEffectDeny})
		}
	}
	return rules
}

// ReadOnlyGitSubcommands returns the read-only git subcommands.
func ReadOnlyGitSubcommands() []string {
	return slices.Clone(readOnlyGitSubcommands)
}

// GitWritingOptions returns the writing options for a read-only git subcommand.
func GitWritingOptions(subcommand string) []string {
	return slices.Clone(gitWritingOptions[subcommand])
}
