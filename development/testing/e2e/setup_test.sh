#!/usr/bin/env bash
set -euo pipefail

tmp=$(mktemp -d)
# OpenCode v2 serves its API from a background service that `opencode api`
# resolves (and starts) on demand; this is the same path Takt's adapter uses,
# so the readiness probe exercises it instead of a hand-started `serve`.
opencode service start
trap 'opencode service stop >/dev/null 2>&1 || true; rm -rf "$tmp"' EXIT
opencode api GET /api/info >/dev/null

config="$HOME/.config/opencode/opencode.json"
mkdir -p "$HOME/.config/opencode"
printf '{"model":"e2e/pre-existing","e2e_marker":"keep"}\n' >"$config"
cp "$config" "$tmp/original-config"
takt-ai setup default-request >"$tmp/request.json"

takt-ai setup install --yes --json <"$tmp/request.json" >"$tmp/install.json"
test -s "$HOME/.takt-backups/.config/opencode/opencode.json"
takt-ai setup sync --yes --json <"$tmp/request.json" >"$tmp/sync.json"

artifact=$(jq -r '[.entries | keys[] | select(endswith(".md"))][0] // empty' "$HOME/.takt-manifest.json")
test -n "$artifact"
cp "$HOME/$artifact" "$tmp/original-artifact"
printf 'user edit\n' >"$HOME/$artifact"
takt-ai setup sync --plan-only --json <"$tmp/request.json" >"$tmp/conflict.json"
jq -e --arg path "$artifact" \
    '.Conflicts | any(.[]; .Path == $path and .Reason == "user-edited")' \
    "$tmp/conflict.json" >/dev/null
takt-ai setup sync --yes --json <"$tmp/request.json" >"$tmp/sync-conflict.json"
test "$(cat "$HOME/$artifact")" = 'user edit'
cp "$tmp/original-artifact" "$HOME/$artifact"

takt-ai restore >/dev/null
cmp "$config" "$tmp/original-config"
takt-ai setup uninstall --yes --json <"$tmp/request.json" >"$tmp/uninstall.json"
test ! -e "$HOME/.takt-manifest.json"
cmp "$config" "$tmp/original-config"

echo 'PASS: install, sync, conflict, backup, restore, uninstall'
