#!/bin/sh
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

set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
IMAGE="golang:1.25"          # matches go directive in go.mod
SRC_DIR="/src"
MODCACHE_VOL="takt-test-gomodcache"
GOCACHE_VOL="takt-test-gocache"

DEFAULT_PKGS="./cmd/takt-ai/... ./takt/setup/..."

DRY_RUN=0
if [ "${1:-}" = "--dry-run" ]; then
    DRY_RUN=1
    shift
fi

# Default to the FS-touching set when no packages are named; flags-only
# invocations must keep the defaults (a bare `go test` in /src finds no Go files).
has_pkg=0
for arg in "$@"; do
    case $arg in
        -*) ;;
        *) has_pkg=1 ;;
    esac
done
if [ "$has_pkg" -eq 0 ]; then
    # shellcheck disable=SC2086
    set -- $DEFAULT_PKGS "$@"
fi

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
STATUS=$?
set -e

if [ "$STATUS" -eq 0 ]; then
    echo "=================================================="
    echo " PASS: containerized tests succeeded (exit 0)"
    echo "=================================================="
else
    echo "=================================================="
    echo " FAIL: containerized tests failed (exit $STATUS)"
    echo "=================================================="
fi
exit "$STATUS"
