#!/usr/bin/env bash
# Computes the next release version for a commit on main.
#
# Usage: next-version.sh [REF]   (REF defaults to HEAD)
#
# Prints three lines consumable as GitHub Actions outputs:
#   version=X.Y.Z
#   tag=vX.Y.Z
#   skip=true|false
#
# Rules (the squash-merge subject is the PR title, a conventional commit),
# relative to the highest v* tag in the repo, reachable from REF or not:
#   - no v* tag yet                        -> v0.0.1
#   - "Release-As: vX.Y.Z" trailer         -> exactly that version
#   - "Release: skip" trailer, or only
#     docs/ci/test/chore commits           -> skip=true
#   - major == 0: breaking (! / BREAKING CHANGE) -> minor, anything else -> patch
#   - major >= 1: breaking -> major, feat -> minor, anything else -> patch
#
# RELEASE_AS (env) overrides everything; used by workflow_dispatch. Must be
# strictly newer than the last release, same as the Release-As trailer.
set -euo pipefail

ref=${1:-HEAD}
semver='^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

die() { printf 'next-version: %s\n' "$*" >&2; exit 1; }

emit() {
	local v=${1#v}
	[[ "v$v" =~ $semver ]] || die "invalid version '$1'"
	if git rev-parse -q --verify "refs/tags/v$v" >/dev/null; then
		die "tag v$v already exists"
	fi
	if [[ -n "$last" ]]; then
		local highest
		highest=$(printf '%s\n%s\n' "$last" "v$v" | sort -V | tail -n1)
		[[ "$highest" == "v$v" ]] || die "v$v is not newer than the last release $last"
	fi
	printf 'version=%s\ntag=v%s\nskip=%s\n' "$v" "$v" "${2:-false}"
}

git rev-parse -q --verify "$ref^{commit}" >/dev/null || die "unknown ref '$ref'"

# Tags are repo-global: a branch that diverged before the latest release
# (e.g. beta vs main) must still bump past it, never reuse its number.
last=$(git tag --list 'v*' --sort=-v:refname | grep -E "$semver" | head -n1 || true)

if [[ -n "${RELEASE_AS:-}" ]]; then
	emit "$RELEASE_AS"
	exit 0
fi

if [[ -z "$last" ]]; then
	emit 0.0.1
	exit 0
fi

if [[ -n "$(git tag --points-at "$ref" --list 'v*')" ]]; then
	printf 'version=\ntag=\nskip=true\n'
	exit 0
fi

log=$(git log --format='%B%x1e' "$last..$ref")
[[ -n "$log" ]] || { printf 'version=\ntag=\nskip=true\n'; exit 0; }

forced=$(grep -E '^Release-As:[[:space:]]*v?[0-9]+\.[0-9]+\.[0-9]+[[:space:]]*$' <<<"$log" | head -n1 | sed -E 's/^Release-As:[[:space:]]*//; s/[[:space:]]*$//' || true)
if [[ -n "$forced" ]]; then
	emit "$forced"
	exit 0
fi

if grep -qE '^Release:[[:space:]]*skip[[:space:]]*$' <<<"$log"; then
	printf 'version=\ntag=\nskip=true\n'
	exit 0
fi

subjects=$(git log --format='%s' "$last..$ref")
if ! grep -qvE '^(docs|ci|test|chore)(\([^)]*\))?:' <<<"$subjects"; then
	printf 'version=\ntag=\nskip=true\n'
	exit 0
fi

[[ "$last" =~ $semver ]]
major=${BASH_REMATCH[1]} minor=${BASH_REMATCH[2]} patch=${BASH_REMATCH[3]}

breaking=false feat=false
if grep -qE '^[a-z]+(\([^)]*\))?!:' <<<"$subjects" || grep -qE '^BREAKING[ -]CHANGE:' <<<"$log"; then
	breaking=true
fi
grep -qE '^feat(\([^)]*\))?:' <<<"$subjects" && feat=true

if ((major == 0)); then
	if $breaking; then minor=$((minor + 1)) patch=0; else patch=$((patch + 1)); fi
elif $breaking; then
	major=$((major + 1)) minor=0 patch=0
elif $feat; then
	minor=$((minor + 1)) patch=0
else
	patch=$((patch + 1))
fi

emit "$major.$minor.$patch"
