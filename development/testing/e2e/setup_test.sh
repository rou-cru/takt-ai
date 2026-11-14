#!/usr/bin/env bash
set -euo pipefail

tmp=$(mktemp -d)
# The memory store this test creates and discards lives in its own directory, never the user's.
export TAKT_MEMORY_DIR="$tmp/memory"
memory_dir="$TAKT_MEMORY_DIR"
stop_memory() {
    [[ -e "$memory_dir/serve.lock" ]] || return 0
    kill "$(cat "$memory_dir/serve.lock")" 2>/dev/null || true
    for _ in $(seq 100); do [[ -S "$memory_dir/memory.sock" ]] || break; sleep 0.1; done
    [[ ! -S "$memory_dir/memory.sock" ]]
}
# OpenCode v2 serves its API from a background service that `opencode api`
# resolves (and starts) on demand; this is the same path Takt's adapter uses,
# so the readiness probe exercises it instead of a hand-started `serve`.
opencode service start
trap 'stop_memory || true; opencode service stop >/dev/null 2>&1 || true; rm -rf "$tmp"' EXIT
opencode api GET /api/info >/dev/null

config="$HOME/.config/opencode/opencode.json"
mkdir -p "$HOME/.config/opencode"
printf '{"model":"e2e/pre-existing","e2e_marker":"keep"}\n' >"$config"
cp "$config" "$tmp/original-config"
takt-ai setup default-request >"$tmp/request.json"

takt-ai setup install --yes --json <"$tmp/request.json" >"$tmp/install.json"
[[ -s "$HOME/.takt-backups/.config/opencode/opencode.json" ]]
# Install leaves the memory service ready; start reuses it and names its socket.
takt-ai memory start >"$tmp/memory.json"
jq -e '.workspace and .socket' "$tmp/memory.json" >/dev/null
takt-ai doctor >"$tmp/doctor.txt" || true
grep -Eq '\[ok\] +memory:service' "$tmp/doctor.txt"
# Post-install verification covers the memory service with semantic vectors.
memory_verified='.verify.Checks | any(.[]; .ID == "memory:service" and .State == "verified" and (.Explanation | test("[0-9]+ of [0-9]+ entries have semantic vectors")))'
jq -e "$memory_verified" "$tmp/install.json" >/dev/null
takt-ai setup sync --yes --json <"$tmp/request.json" >"$tmp/sync.json"

artifact=$(jq -r '[.entries | keys[] | select(endswith(".md"))][0] // empty' "$HOME/.takt-manifest.json")
[[ -n "$artifact" ]]
cp "$HOME/$artifact" "$tmp/original-artifact"
printf 'user edit\n' >"$HOME/$artifact"
takt-ai setup sync --plan-only --json <"$tmp/request.json" >"$tmp/conflict.json"
jq -e --arg path "$artifact" \
    '.Conflicts | any(.[]; .Path == $path and .Reason == "user-edited")' \
    "$tmp/conflict.json" >/dev/null
takt-ai setup sync --yes --json <"$tmp/request.json" >"$tmp/sync-conflict.json"
[[ "$(cat "$HOME/$artifact")" = 'user edit' ]]
cp "$tmp/original-artifact" "$HOME/$artifact"

takt-ai restore >/dev/null
cmp "$config" "$tmp/original-config"
# Whether to retain the memory is the user's decision, which belongs to the TUI:
# the CLI refuses before changing anything while a store exists.
if takt-ai setup uninstall --yes --json <"$tmp/request.json" >"$tmp/uninstall.json" 2>"$tmp/uninstall.err"; then
    echo 'uninstall must refuse while a memory store exists' >&2
    exit 1
fi
grep -qi memory "$tmp/uninstall.err"
[[ -e "$HOME/.takt-manifest.json" ]]
# Discard the memory as the TUI would, then the CLI uninstall completes.
stop_memory
rm -rf "$memory_dir"
takt-ai setup uninstall --yes --json <"$tmp/request.json" >"$tmp/uninstall.json"
[[ ! -e "$HOME/.takt-manifest.json" ]]
cmp "$config" "$tmp/original-config"

echo 'PASS: install, memory ready and verified, doctor, sync, conflict, backup, restore, uninstall refused with memory, uninstall'
