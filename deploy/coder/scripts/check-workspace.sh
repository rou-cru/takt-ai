#!/usr/bin/env bash
#
# check-workspace.sh - Coder agent blocking startup diagnostic.
#
# The phase-one Coder template injects this file with file() as the agent
# startup_script (startup_script_behavior = "blocking"); it is never copied into
# the image. It verifies that the reused workspace image still offers the
# installed prerequisites that a non-root (UID/GID 10001) terminal session needs
# in /workspace.
#
# Fixed by the template contract:
#   * non-mutating: the script inspects and writes nothing, not even a probe
#     file, so a failed run leaves the workspace untouched;
#   * bounded: each version probe is capped at 15 seconds by coreutils timeout,
#     keeping the whole run well inside the 60-second normal bound;
#   * quiet about the environment: it prints stage names, failures and the
#     non-secret repository hint only, never environment values or tokens;
#   * no installation, clone, Git configuration, secret probing or doctor
#     repair, and no environment value becomes shell program text.
#
# Exit 0 states that the bootstrap prerequisites below are present. It is not a
# claim that MCP servers, tools or models are ready.
set -Eeuo pipefail

version_timeout_seconds=15
# The Pod contract sets HOME=/home/takt and working_dir=/workspace, and the
# image's workspace-entrypoint.sh already reads TAKT_WORKSPACE_DIR defaulted to
# /workspace. Resolving both paths this way keeps the production paths exact
# while letting an exercising fixture point them at an isolated temporary tree.
home_dir=${HOME:-/home/takt}
workspace_dir=${TAKT_WORKSPACE_DIR:-$PWD}

stage_ok() {
	printf 'check-workspace: stage %s: ok\n' "$1"
}

fail() {
	printf 'check-workspace: stage %s: FAILED: %s\n' "$1" "$2"
	exit 1
}

require_writable_dir() {
	local stage=$1 path=$2 label=$3
	if [[ ! -d $path || ! -w $path ]]; then
		fail "$stage" "$label is not a writable directory: $path"
	fi
}

require_readable() {
	local stage=$1 path=$2 label=$3
	if [[ ! -r $path ]]; then
		fail "$stage" "$label is missing or not readable: $path"
	fi
}

require_executable() {
	local stage=$1 name=$2
	if ! command -v "$name" >/dev/null 2>&1; then
		fail "$stage" "required executable $name is not on PATH"
	fi
}

check_version_command() {
	local stage=$1 command=$2
	shift 2
	local status=0
	# Child output is discarded, both streams: this script publishes only its
	# own stage lines, so no child output can leak an environment value or a
	# token into the blocking startup log. The exit status is the check result.
	timeout "$version_timeout_seconds" "$command" "$@" >/dev/null 2>&1 || status=$?
	if ((status == 0)); then
		return 0
	fi
	if ((status == 124)); then
		fail "$stage" "$command $* timed out after ${version_timeout_seconds}s"
	fi
	fail "$stage" "$command $* failed with status $status"
}

# Writable /workspace.
require_writable_dir workspace "$workspace_dir" workspace
stage_ok workspace

# Writable runtime mount points of the Coder Pod.
require_writable_dir "runtime paths" "$home_dir/.local/share" "runtime path"
require_writable_dir "runtime paths" "$home_dir/.local/state" "runtime path"
require_writable_dir "runtime paths" "$home_dir/.cache" "runtime path"
require_writable_dir "runtime paths" /tmp "runtime path"
stage_ok "runtime paths"

# Installed Takt/OpenCode toolchain baked into the image.
require_executable executables opencode
require_executable executables takt-ai
require_executable executables engram
require_executable executables codegraph
stage_ok executables

# Baked onboarding artifacts.
require_readable "installed configuration" "$home_dir/.takt-installed-config.json" "installed configuration file"
require_readable "installed configuration" "$home_dir/.config/opencode" "OpenCode configuration directory"
stage_ok "installed configuration"

# Bounded version probes: a failing or hanging binary is a failed prerequisite.
check_version_command versions opencode --version
check_version_command versions takt-ai version
stage_ok versions

# Nonsensitive hint only. The value goes through a literal format string as an
# argument, so it reaches the log verbatim and is never interpreted as shell
# program text; provisioning itself never clones or fetches it.
if [[ -n ${TAKT_REPOSITORY_URL:-} ]]; then
	printf 'check-workspace: repository hint: %s\n' "${TAKT_REPOSITORY_URL}"
else
	printf 'check-workspace: repository hint: no repository configured\n'
fi

printf 'check-workspace: bootstrap prerequisites verified; MCP, tool and model readiness are not asserted\n'
