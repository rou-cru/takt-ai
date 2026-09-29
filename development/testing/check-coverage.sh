#!/usr/bin/env bash
# Combined coverage gate: merges the host and containerized coverage
# profiles (same two files SonarCloud consumes via sonar.go.coverage.reportPaths)
# and fails the build if the total statement coverage drops below the floor.
#
# Usage: development/testing/check-coverage.sh <profile.out> [<profile.out> ...]

set -euo pipefail

if [[ "$#" -eq 0 ]]; then
    echo "usage: check-coverage.sh <profile.out> [<profile.out> ...]" >&2
    exit 2
fi

# Current minimum combined statement coverage. Raise it after establishing
# a new measured baseline; never lower it without a documented reason.
MIN_COVERAGE=80

MERGED=$(mktemp)
trap 'rm -f "$MERGED"' EXIT

first=1
for profile in "$@"; do
    if [[ ! -s "$profile" ]] || ! head -n 1 "$profile" | grep -Eq '^mode: (set|count|atomic)$' || [[ $(wc -l < "$profile") -lt 2 ]]; then
        echo "FAIL: missing or invalid coverage profile: $profile" >&2
        exit 1
    fi
    if [[ "$first" -eq 1 ]]; then
        mode=$(head -n 1 "$profile")
        cp "$profile" "$MERGED"
        first=0
    else
        if [[ $(head -n 1 "$profile") != "$mode" ]]; then
            echo "FAIL: incompatible coverage mode in $profile" >&2
            exit 1
        fi
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
