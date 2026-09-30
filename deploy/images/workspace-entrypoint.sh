#!/usr/bin/env bash
set -Eeuo pipefail

workspace_dir=${TAKT_WORKSPACE_DIR:-/workspace}
server_port=${TAKT_OPENCODE_PORT:-4096}
server_url="http://127.0.0.1:${server_port}"

cd "$workspace_dir"

# OpenCode v2 always requires Basic Auth as the fixed user "opencode". Own the
# password here instead of letting the server generate it, so the server and
# every PTY it spawns inherit it: `opencode --server` inside the web terminal
# reads OPENCODE_PASSWORD. Without a Secret, print it the way the server would.
if [[ -z "${OPENCODE_PASSWORD:-}" ]]; then
	OPENCODE_PASSWORD=$(head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n')
	printf 'server password %s\n' "$OPENCODE_PASSWORD"
fi
export OPENCODE_PASSWORD

opencode serve --hostname 0.0.0.0 --port "$server_port" &
server_pid=$!

cleanup() {
	status=$?
	trap - EXIT INT TERM
	kill "$server_pid" 2>/dev/null || true
	wait "$server_pid" 2>/dev/null || true
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

api() {
	curl --fail --silent --show-error --user "opencode:$OPENCODE_PASSWORD" "$@"
}

ready=false
for _ in $(seq 1 60); do
	if ! kill -0 "$server_pid" 2>/dev/null; then
		printf 'workspace bootstrap: OpenCode exited before becoming ready\n' >&2
		exit 1
	fi
	if api "$server_url/api/info" >/dev/null 2>&1; then
		ready=true
		break
	fi
	sleep 1
done

if [[ "$ready" != true ]]; then
	printf 'workspace bootstrap: OpenCode API did not become ready\n' >&2
	exit 1
fi

sessions=$(api --get --data-urlencode "directory=$workspace_dir" "$server_url/api/session")
session_id=$(jq -r '[.data[] | select(.time.archived == null)][0].id // empty' <<<"$sessions")
if [[ -n "$session_id" ]]; then
	printf 'workspace bootstrap: existing session %s found for %s\n' "$session_id" "$workspace_dir"
else
	request=$(jq -cn --arg directory "$workspace_dir" '{title:"Workspace",location:{directory:$directory}}')
	created=$(api --request POST --header 'Content-Type: application/json' --data "$request" "$server_url/api/session")
	session_id=$(jq -er '.data.id' <<<"$created")
	printf 'workspace bootstrap: created session %s for %s\n' "$session_id" "$workspace_dir"
fi

wait "$server_pid"
