#!/usr/bin/env bash
# Single source of truth for test isolation. List exact packages, not ./...
# patterns: new subpackages must be reviewed rather than silently run on host.
# Host tests must not mutate real user state or execute external binaries.
# OpenCode's Node integration tests belong in the container, even though its
# other agent tests are pure. generate-logo's current tests are pure transforms;
# opencodeapi's tests inject a runner (they do not invoke an installed OpenCode).
HOST_TEST_PKGS=(
    ./cmd/generate-logo
    ./takt/agents/shared
    ./takt/catalog
    ./takt/dispatch
    ./takt/history
    ./takt/internal/artifacts
    ./takt/internal/opencodeapi
    ./takt/model
    ./takt/obs
    ./takt/skills
    ./takt/skills/testutil
    ./takt/tui
    ./takt/tui/diagnostics
    ./takt/tui/drift
    ./takt/tui/install
    ./takt/tui/keys
    ./takt/tui/modelpicker
    ./takt/tui/models
    ./takt/tui/runtime
    ./takt/tui/styles
    ./takt/tui/testutil
    ./takt/tui/theme
    ./takt/tui/ui
    ./takt/tui/uninstall
)
CONTAINER_TEST_PKGS=(
    ./cmd/takt-ai
    ./takt/agents/opencode
    ./takt/codegraph
    ./takt/doctor
    ./takt/engram
    ./takt/gc
    ./takt/internal/filemerge
    ./takt/lifecycle
    ./takt/memory
    ./takt/session
    ./takt/setup
    ./takt/setup/testutil
    ./takt/verify
    ./takt/vfs
)

# Discovery compiles/runs no tests. Every package must occur exactly once and
# every manifest entry must still exist. Run from the repository root.
check_test_packages() {
    local discovered
    discovered=$(go list -f '{{.Dir}}' ./...) || return
    {
        printf '%s\n' "$discovered" | awk -v root="$(pwd -P)" '{
            if ($0 == root) print "actual ./"
            else print "actual ./" substr($0, length(root) + 2)
        }'
        printf 'host %s\n' "${HOST_TEST_PKGS[@]}"
        printf 'container %s\n' "${CONTAINER_TEST_PKGS[@]}"
    } | awk '
        $1 == "actual" { actual[$2] = 1; next }
        { count[$2]++; group[$2] = group[$2] " " $1 }
        END {
            for (pkg in actual) if (!(pkg in count)) {
                print "unclassified test package: " pkg; failed = 1
            }
            for (pkg in count) {
                if (!(pkg in actual)) { print "stale test package: " pkg; failed = 1 }
                if (count[pkg] != 1) { print "duplicate test package: " pkg " (" group[pkg] ")"; failed = 1 }
            }
            if (!failed) print "PASS: all Go packages classified exactly once"
            exit failed
        }'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    set -euo pipefail
    cd -- "$(dirname -- "$0")/.."
    case "${1:-check}" in
        host) printf '%s\n' "${HOST_TEST_PKGS[@]}" ;;
        container) printf '%s\n' "${CONTAINER_TEST_PKGS[@]}" ;;
        check) check_test_packages ;;
        *) echo 'usage: test-packages.sh [check|host|container]' >&2; exit 2 ;;
    esac
fi
