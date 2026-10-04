#!/usr/bin/env bash
# Creates the annotated release tag for a commit through the GitHub API.
#
# Usage: create-tag.sh VERSION SHA
#
# Prints two lines consumable as GitHub Actions outputs, for the version the
# tag was actually created with:
#   version=X.Y.Z
#   tag=vX.Y.Z
#
# A tag that already points at SHA is reused (a re-run). A tag on another
# commit is an error. GitHub refuses to create a tag whose release was
# published and then deleted (immutable releases answer 422), and git cannot
# see such a tag, so next-version.sh may propose it again: the patch number is
# then bumped and the creation retried, up to MAX_ATTEMPTS times.
#
# Requires gh with GH_TOKEN and GITHUB_REPOSITORY set.
set -euo pipefail

readonly MAX_ATTEMPTS=5
readonly TAGGER_NAME='github-actions[bot]'
readonly TAGGER_EMAIL='41898282+github-actions[bot]@users.noreply.github.com'

# die writes its arguments as an error message to stderr and exits with status 1.
die() { printf 'create-tag: %s\n' "$*" >&2; exit 1; }

[[ $# -eq 2 ]] || die 'usage: create-tag.sh VERSION SHA'
version=$1 sha=$2
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || die "invalid version '$version'"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

# existing_target prints the target SHA of the annotated tag named by $1 in
# GITHUB_REPOSITORY. Any ref lookup failure returns 0 without output; failures
# fetching the tag object propagate the gh exit status and stderr.
existing_target() {
	local ref_sha
	ref_sha=$(gh api "repos/$GITHUB_REPOSITORY/git/ref/tags/$1" --jq .object.sha 2>/dev/null) || return 0
	gh api "repos/$GITHUB_REPOSITORY/git/tags/$ref_sha" --jq .object.sha
}

# create_tag creates an annotated tag named by $1 at the global sha in
# GITHUB_REPOSITORY. Returns 0 on success or 2 if ref creation fails with
# HTTP 422 (including reserved tags); other API failures exit with status 1.
# A failed ref creation leaves the newly created tag object in the repository.
create_tag() {
	local tag_sha err
	tag_sha=$(gh api "repos/$GITHUB_REPOSITORY/git/tags" \
		-f tag="$1" \
		-f message="Release $1" \
		-f object="$sha" \
		-f type=commit \
		-f "tagger[name]=$TAGGER_NAME" \
		-f "tagger[email]=$TAGGER_EMAIL" \
		--jq .sha) || die "creating the $1 tag object failed"
	if err=$(gh api "repos/$GITHUB_REPOSITORY/git/refs" -f ref="refs/tags/$1" -f sha="$tag_sha" 2>&1 >/dev/null); then
		return 0
	fi
	[[ "$err" == *"HTTP 422"* ]] || die "creating refs/tags/$1 failed: $err"
	return 2
}

for ((attempt = 1; attempt <= MAX_ATTEMPTS; attempt++)); do
	tag="v$version"
	target=$(existing_target "$tag")
	if [[ "$target" == "$sha" ]]; then
		printf 'create-tag: %s already points at %s (re-run)\n' "$tag" "$sha" >&2
		printf 'version=%s\ntag=%s\n' "$version" "$tag"
		exit 0
	fi
	[[ -z "$target" ]] || die "$tag exists on $target, not $sha"
	status=0
	create_tag "$tag" || status=$?
	if ((status == 0)); then
		printf 'version=%s\ntag=%s\n' "$version" "$tag"
		exit 0
	fi
	printf '::warning::GitHub refused to create %s (a deleted release keeps its tag reserved); trying the next patch\n' "$tag" >&2
	IFS=. read -r major minor patch <<<"$version"
	version="$major.$minor.$((patch + 1))"
done
die "no tag could be created after $MAX_ATTEMPTS attempts; release with a Release-As trailer or workflow_dispatch"
