#!/usr/bin/env bash
# Combined coverage gate: merges the host and containerized coverage
# profiles (same two files SonarCloud consumes via sonar.go.coverage.reportPaths)
# and fails the build if the total statement coverage drops below the floor.
#
# Usage: scripts/check-coverage.sh <profile.out> [<profile.out> ...]

set -euo pipefail

if [[ "$#" -eq 0 ]]; then
    echo "usage: check-coverage.sh <profile.out> [<profile.out> ...]" >&2
    exit 2
fi

# ponytail: baseline measured at introduction was 63.4% combined; raise this
# as real coverage grows, never lower it without a documented reason.
MIN_COVERAGE=60

MERGED=$(mktemp)
trap 'rm -f "$MERGED"' EXIT

first=1
for profile in "$@"; do
    if [[ "$first" -eq 1 ]]; then
        cp "$profile" "$MERGED"
        first=0
    else
        tail -n +2 "$profile" >>"$MERGED"
    fi
done

TOTAL_LINE=$(go tool cover -func="$MERGED" | tail -1)
TOTAL_PCT=$(echo "$TOTAL_LINE" | awk '{print $NF}' | tr -d '%')

if awk -v pct="$TOTAL_PCT" -v floor="$MIN_COVERAGE" 'BEGIN { exit !(pct < floor) }'; then
    echo "FAIL: coverage ${TOTAL_PCT}% is below the ${MIN_COVERAGE}% floor" >&2
    exit 1
fi

echo "PASS: coverage ${TOTAL_PCT}% meets the ${MIN_COVERAGE}% floor"
