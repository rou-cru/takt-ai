#!/usr/bin/env bash
# Container-isolated test runner.
#
# Runs `go test` inside a disposable golang Docker container. The repo source
# is COPIED into the container via a tar pipe on stdin -- no bind mounts at
# all, not even read-only ones. The built binary and every file a test writes
# stay inside the container; nothing can touch the host filesystem.
#
# Module and build caches live in named Docker volumes (Docker-managed, not
# host paths), so repeat runs are fast without ever exposing host state.
#
# Usage:
#   scripts/test-containerized.sh [go test flags and packages...]
#   scripts/test-containerized.sh            # runs default FS-touching packages
#   scripts/test-containerized.sh --dry-run  # print the docker command, don't run

set -euo pipefail

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
IMAGE="golang:1.25"          # matches go directive in go.mod
SEPARATOR="=================================================="
SRC_DIR="/src"
MODCACHE_VOL="takt-test-gomodcache"
GOCACHE_VOL="takt-test-gocache"

DEFAULT_PKGS="./cmd/takt-ai/... ./takt/setup/..."

# Resolve the final `go test` argument list.
#
# Flags that accept a value (e.g. `-run`, `-bench`) consume the NEXT argument
# as their value, so that argument must NOT be mistaken for a package. The
# space-separated form `-run TestX` was previously misclassified: `TestX`
# started with no `-`, so it was treated as a package, the default packages
# were dropped, and `go test` ran with nothing to test in /src.
#
# - RESOLVED_PKGS: the package list (defaults when none are named).
# - RESOLVED_ARGS: flags verbatim, with defaults prepended only when no
#   package was named.
#
# ponytail: the value-taking flag list covers `go test`'s common flags; add a
# name here if a new value flag appears and starts eating the next argument.
compute_go_args() {
    pkgs=""
    skip_next=0
    for arg in "$@"; do
        if [ "$skip_next" -eq 1 ]; then
            skip_next=0
            continue
        fi
        case $arg in
            -run|-bench|-benchtime|-blockprofile|-cpuprofile|-memprofile|-coverprofile|-coverpkg|-cpu|-count|-timeout|-parallel|-p|-exec|-outputdir|-gcflags|-ldflags|-tags|-vet|-test.run|-test.bench|-test.benchtime|-test.timeout|-test.count|-test.parallel|-test.cpu|-test.vet)
                skip_next=1
                ;;
            -*) ;;
            *) pkgs="$pkgs $arg" ;;
        esac
    done
    if [ -z "$pkgs" ]; then
        # shellcheck disable=SC2086
        pkgs="$DEFAULT_PKGS"
    fi
    RESOLVED_PKGS="$pkgs"
    if [ "$pkgs" = "$DEFAULT_PKGS" ]; then
        # shellcheck disable=SC2086
        RESOLVED_ARGS="$DEFAULT_PKGS $*"
    else
        RESOLVED_ARGS="$*"
    fi
}

DRY_RUN=0
if [ "${1:-}" = "--dry-run" ]; then
    DRY_RUN=1
    shift
fi

# Self-check mode: prove both `-run TestX` and `-run=TestX` resolve to the
# same package list, without invoking Docker.
if [ "${1:-}" = "--self-check" ]; then
    compute_go_args -run TestX
    form_space="$RESOLVED_PKGS"
    compute_go_args -run=TestX
    form_eq="$RESOLVED_PKGS"
    if [ "$form_space" = "$form_eq" ]; then
        echo "SELF-CHECK PASS: '-run TestX' and '-run=TestX' produce identical package list"
        echo "  -> $form_space"
        exit 0
    fi
    echo "SELF-CHECK FAIL:" >&2
    echo "  '-run TestX'   -> $form_space" >&2
    echo "  '-run=TestX'   -> $form_eq" >&2
    exit 1
fi

# Default to the FS-touching set when no packages are named; flags-only
# invocations must keep the defaults (a bare `go test` in /src finds no Go files).
compute_go_args "$@"
# shellcheck disable=SC2086
set -- $RESOLVED_ARGS

echo "==> containerized test run (source copied in via tar pipe, zero host mounts)"
echo "==> packages/flags: $*"

if [ "$DRY_RUN" = "1" ]; then
    echo "DRY-RUN (not executing):"
    echo "  tar -C '$REPO_ROOT' --exclude ./.git --exclude ./.codegraph -cf - . |"
    echo "    docker run --rm -i -e HOME=/tmp/fake-home \\"
    echo "      -v $MODCACHE_VOL:/go/pkg/mod -v $GOCACHE_VOL:/go/.cache/go-build \\"
    echo "      $IMAGE /bin/sh -c 'tar -x -C $SRC_DIR && cd $SRC_DIR && go test' ... $*"
    exit 0
fi

set +e
# COPYFILE_DISABLE=1: skip macOS ._ resource forks; --no-xattrs: skip Apple xattrs
# that GNU tar in the container would warn about.
tar -C "$REPO_ROOT" --no-xattrs --exclude ./\.git --exclude ./\.codegraph -cf - . |
    docker run --rm -i \
        -e HOME=/tmp/fake-home \
        -v "$MODCACHE_VOL":/go/pkg/mod \
        -v "$GOCACHE_VOL":/go/.cache/go-build \
        "$IMAGE" /bin/sh -c '
set -e
mkdir -p '"$SRC_DIR"'
tar -x -C '"$SRC_DIR"' 2>/dev/null
cd '"$SRC_DIR"'
export GOFLAGS=-mod=mod TMPDIR=/tmp GOPATH=/go GOCACHE=/go/.cache/go-build GOMODCACHE=/go/pkg/mod
go test "$@"
' go "$@"
PIPE_STATUS=("${PIPESTATUS[@]}")
set -e

TAR_STATUS=${PIPE_STATUS[0]}
DOCKER_STATUS=${PIPE_STATUS[1]}
STATUS=$DOCKER_STATUS
if [ "$TAR_STATUS" -ne 0 ]; then
    STATUS=$TAR_STATUS
fi

if [ "$STATUS" -eq 0 ]; then
    echo "$SEPARATOR"
    echo " PASS: containerized tests succeeded (exit 0)"
    echo "$SEPARATOR"
else
    echo "$SEPARATOR"
    echo " FAIL: containerized tests failed (exit $STATUS)"
    echo "$SEPARATOR"
fi
exit "$STATUS"
