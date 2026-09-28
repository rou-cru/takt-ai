#!/usr/bin/env bash
# Container-isolated test runner.
#
# Runs `go test` inside a disposable Go + Node Docker container. The repo source
# is COPIED into the container via a tar pipe on stdin -- no bind mounts at
# all, not even read-only ones. The built binary and every file a test writes
# stay inside the container; nothing can touch the host filesystem.
#
# No host or Docker-volume mounts are used. Module and build caches are
# disposable container state and are recreated on each run.
#
# Usage:
#   development/testing/test-containerized.sh [go test flags and packages...]
#   development/testing/test-containerized.sh            # runs default FS-touching packages
#
# Passing -coverprofile=<file> (or -coverprofile <file>) copies it out of the
# disposable container onto the host once tests pass, so CI can feed it to
# SonarCloud alongside the host-run coverage.out.

set -euo pipefail

REPO_ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
IMAGE="takt-test:go1.25-node24"
SEPARATOR="=================================================="
SRC_DIR="/src"
source "$REPO_ROOT/development/testing/test-packages.sh"

# Resolve the final `go test` argument list.
#
# Flags that accept a value (e.g. `-run`, `-bench`) consume the NEXT argument
# as their value, so that argument must NOT be mistaken for a package. The
# space-separated form `-run TestX` was previously misclassified: `TestX`
# started with no `-`, so it was treated as a package, the default packages
# were dropped, and `go test` ran with nothing to test in /src.
#
# - RESOLVED_ARGS: an array preserving every argument boundary, with defaults
#   prepended only when no package was named. -args ends go flag parsing.
#
# The value-taking flag list covers `go test`'s common flags; extend it when a
# new value flag appears and starts consuming the next argument.
compute_go_args() {
    local arg has_packages=0 skip_next=0
    for arg in "$@"; do
        if [[ "$skip_next" -eq 1 ]]; then
            skip_next=0
            continue
        fi
        case $arg in
            -args|--args|--) break ;;
            -run|-bench|-benchtime|-blockprofile|-blockprofilerate|-cpuprofile|-memprofile|-memprofilerate|-mutexprofile|-mutexprofilefraction|-trace|-coverprofile|-covermode|-coverpkg|-cpu|-count|-timeout|-parallel|-p|-exec|-outputdir|-gcflags|-ldflags|-asmflags|-gccgoflags|-tags|-vet|-shuffle|-fuzz|-fuzztime|-fuzzminimizetime|-list|-o|-mod|-modfile|-overlay|-pkgdir|-toolexec|-buildmode|-compiler|-pgo|-test.run|-test.bench|-test.benchtime|-test.timeout|-test.count|-test.parallel|-test.cpu|-test.vet)
                skip_next=1
                ;;
            -*) ;;
            *) has_packages=1 ;;
        esac
    done
    if [[ "$has_packages" -eq 0 ]]; then
        RESOLVED_ARGS=("${CONTAINER_TEST_PKGS[@]}" "$@")
    else
        RESOLVED_ARGS=("$@")
    fi
}

# Self-check mode: prove both `-run TestX` and `-run=TestX` resolve to the
# same package list, without invoking Docker.
if [[ "${1:-}" = "--self-check" ]]; then
    compute_go_args -run TestX
    form_space="${RESOLVED_ARGS[*]:0:${#CONTAINER_TEST_PKGS[@]}}"
    compute_go_args -run=TestX
    form_eq="${RESOLVED_ARGS[*]:0:${#CONTAINER_TEST_PKGS[@]}}"
    if [[ "$form_space" = "$form_eq" ]]; then
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
set -- "${RESOLVED_ARGS[@]}"

docker build -f "$REPO_ROOT/development/testing/Dockerfile" -t "$IMAGE" "$REPO_ROOT"

echo "==> containerized test run (source copied in via tar pipe, zero host mounts)"
echo "==> packages/flags: $*"

# Extract the -coverprofile=<file> target, if any, so the container can be
# kept (instead of --rm) long enough for `docker cp` to pull the file out.
COVER_FILE=""
cover_next=0
for arg in "$@"; do
    if [[ "$cover_next" -eq 1 ]]; then
        COVER_FILE="$arg"
        cover_next=0
        continue
    fi
    case "$arg" in
        -args|--args|--) break ;;
        -coverprofile) cover_next=1 ;;
        -coverprofile=*) COVER_FILE="${arg#-coverprofile=}" ;;
    esac
done
CONTAINER_NAME="takt-test-containerized-$$"

set +e
# COPYFILE_DISABLE=1: skip macOS ._ resource forks; --no-xattrs: skip Apple xattrs
# that GNU tar in the container would warn about.
#
# When a coverage file was requested, keep the container around (drop --rm)
# so `docker cp` can pull the file out below; otherwise remove it immediately.
DOCKER_KEEP_FLAGS=(--rm)
[[ -n "$COVER_FILE" ]] && DOCKER_KEEP_FLAGS=(--name "$CONTAINER_NAME")
COPYFILE_DISABLE=1 tar -C "$REPO_ROOT" --no-xattrs --exclude ./\.git --exclude ./\.codegraph -cf - . |
    docker run "${DOCKER_KEEP_FLAGS[@]}" -i \
        -e HOME=/tmp/fake-home \
        "$IMAGE" /bin/sh -c '
set -e
mkdir -p '"$SRC_DIR"'
tar -x -C '"$SRC_DIR"' 2>/dev/null
cd '"$SRC_DIR"'
export GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local TMPDIR=/tmp GOPATH=/go GOCACHE=/go/.cache/go-build GOMODCACHE=/go/pkg/mod
./development/testing/test-packages.sh check
go test "$@"
' go "$@"
PIPE_STATUS=("${PIPESTATUS[@]}")
set -e

TAR_STATUS=${PIPE_STATUS[0]}
DOCKER_STATUS=${PIPE_STATUS[1]}
STATUS=$DOCKER_STATUS
if [[ "$TAR_STATUS" -ne 0 ]]; then
    STATUS=$TAR_STATUS
fi

if [[ -n "$COVER_FILE" ]]; then
    if [[ "$STATUS" -eq 0 ]]; then
        docker cp "${CONTAINER_NAME}:${SRC_DIR}/${COVER_FILE}" "${REPO_ROOT}/${COVER_FILE}" || STATUS=1
    fi
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
fi

if [[ "$STATUS" -eq 0 ]]; then
    echo "$SEPARATOR"
    echo " PASS: containerized tests succeeded (exit 0)"
    echo "$SEPARATOR"
else
    echo "$SEPARATOR"
    echo " FAIL: containerized tests failed (exit $STATUS)"
    echo "$SEPARATOR"
fi
exit "$STATUS"
