#!/usr/bin/env bash
# Bounded, non-destructive Docker smoke runner for the phase-one Coder
# workspace-image path.
#
# It exercises the workspace image that Verify already built from the existing
# Dockerfile (--target workspace). It never pulls, never builds, never mounts
# host HOME, repositories, credentials or the Docker socket, keeps everything
# it writes inside the container or its own temporary directory, and removes
# no container, image or volume unless --cleanup names this run's container.
#
# What this proves (Tier 2 of Engram #15067 CA-08) and what it cannot:
#   * proves: the image runs as UID/GID 10001, the baked OpenCode/Takt
#     installation is visible to that user, the workspace is writable without
#     root, the production bootstrap diagnostic exits 0 inside the image, and
#     the unmodified workspace-entrypoint starts its HTTP server with the
#     synthetic test-only password and bootstraps a default session over a
#     loopback-only mapping, without printing authentication data.
#   * does not prove: a real Coder agent, agent init_script connectivity,
#     Kubernetes scheduling or storage, terminal transport, multi-human
#     isolation, or live memory-tool behaviour. Those rows stay separate and
#     unverified.
#
# Usage:
#   development/testing/test-coder-smoke.sh --image <local-tag> --name <unique-name> [--cleanup] [--tui]
#
#   --image    an existing local image tag; never pulled or built here.
#   --name     a unique container name for this run. The runner refuses a name
#              that already exists, so it can never touch another container.
#   --cleanup  the confirmation step: removes only this run's named container.
#              The image and its throwaway data are stated as untouched.
#   --tui      additionally attempt an interactive TUI check; without a
#              controlling terminal that item is reported not verifiable.
#
# Bound: 5 minutes total, enforced by an external process-group watchdog (not
# by elapsed-time checks after blocking calls); the authenticated HTTP readiness
# poll is capped at 90 seconds. On deadline, only local runner processes are
# terminated. The Docker container/image/volumes are not removed or pruned.
set -euo pipefail

TOTAL_BOUND_SECONDS=300

# Run the complete smoke in a fresh session/process group supervised by Perl.
# macOS does not provide coreutils timeout; Perl is available on supported host
# environments and POSIX::setsid isolates every descendant local blocking call
# (Docker CLI, curl, polling sleep, and optional TUI). At the deadline the
# supervisor sends TERM and then KILL to that process group. It never issues
# Docker cleanup commands and never prints environment values or auth data.
if [[ "${TAKT_CODER_SMOKE_WATCHDOG_PARENT:-}" != "$PPID" ]]; then
    command -v perl >/dev/null 2>&1 || {
        echo 'FAIL: Perl is required for the 300-second process-group watchdog' >&2
        exit 1
    }
    exec perl - "$0" "$@" <<'PERL'
use strict;
use warnings;
use POSIX qw(setsid);

my ($script, @args) = @ARGV;
my $supervisor = $$;
my $pid = fork();
defined $pid or die "FAIL: could not start bounded smoke runner: $!\n";
if ($pid == 0) {
    setsid() >= 0 or die "FAIL: could not isolate smoke process group: $!\n";
    $ENV{TAKT_CODER_SMOKE_WATCHDOG_PARENT} = $supervisor;
    exec { "bash" } "bash", $script, @args;
    die "FAIL: could not exec bounded smoke runner: $!\n";
}

my $deadline = $ENV{TOTAL_BOUND_SECONDS} || 300;
$SIG{ALRM} = sub {
    kill 'TERM', -$pid;
    select undef, undef, undef, 2;
    kill 'KILL', -$pid;
    waitpid($pid, 0);
    print STDERR "FAIL: smoke runner exceeded ${deadline}s; local process group stopped. Any created container is kept; no Docker cleanup or pruning was issued.\n";
    exit 124;
};
alarm($deadline);
waitpid($pid, 0);
alarm(0);
my $status = $?;
exit(($status & 127) ? 128 + ($status & 127) : ($status >> 8));
PERL
fi
unset TAKT_CODER_SMOKE_WATCHDOG_PARENT

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
DIAGNOSTIC="$ROOT/deploy/coder/scripts/check-workspace.sh"
ENTRYPOINT=/usr/local/bin/workspace-entrypoint
CONTAINER_HOME=/home/takt
WORKSPACE_DIR=/workspace
SERVER_PORT=4096
API_POLL_SECONDS=90
SESSION_POLL_SECONDS=60

DOCKER=docker

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

IMAGE=''
NAME=''
CLEANUP=0
TUI=0
CONTAINER_CREATED=0

usage() {
    cat >&2 <<'USAGE'
usage: development/testing/test-coder-smoke.sh --image <local-tag> --name <unique-name> [--cleanup] [--tui]

  --image    an existing local image tag; never pulled or built here
  --name     a unique container name for this run; an existing name is refused
  --cleanup  remove only this run's named container after the checks
  --tui      additionally attempt an interactive TUI check where a terminal exists
USAGE
}

fail() {
    echo "FAIL: $*" >&2
    if [[ "$CONTAINER_CREATED" -eq 1 ]]; then
        echo "The container '$NAME' is kept for inspection; remove it with:" >&2
        echo "  docker rm -f $NAME" >&2
    fi
    exit 1
}

pass() { echo "PASS: $*"; }

while [[ "$#" -gt 0 ]]; do
    case "$1" in
        --image) [[ "$#" -ge 2 ]] || { echo 'missing value for --image' >&2; exit 2; }; IMAGE=$2; shift 2 ;;
        --image=*) IMAGE="${1#*=}" ; shift ;;
        --name) [[ "$#" -ge 2 ]] || { echo 'missing value for --name' >&2; exit 2; }; NAME=$2; shift 2 ;;
        --name=*) NAME="${1#*=}" ; shift ;;
        --cleanup) CLEANUP=1; shift ;;
        --tui) TUI=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done

[[ -n "$IMAGE" ]] || { echo 'missing required --image <existing-local-tag>' >&2; usage >&2; exit 2; }
[[ -n "$NAME" ]] || { echo 'missing required --name <unique-container-name>' >&2; usage >&2; exit 2; }
case "$NAME" in
    [A-Za-z0-9]*) ;;
    *) echo "invalid container name: '$NAME'" >&2; exit 2 ;;
esac
case "$NAME" in
    *[!A-Za-z0-9_.-]*) echo "invalid container name: '$NAME'" >&2; exit 2 ;;
esac

command -v "$DOCKER" >/dev/null 2>&1 || fail 'the Docker CLI is required for the workspace smoke (prerequisite, not a failed check)'
"$DOCKER" info >/dev/null 2>&1 || fail 'the Docker daemon is not reachable (prerequisite, not a failed check)'
[[ -f "$DIAGNOSTIC" ]] || fail "the production diagnostic is missing: $DIAGNOSTIC"

if "$DOCKER" container inspect "$NAME" >/dev/null 2>&1; then
    fail "a container named '$NAME' already exists; choose a unique name or remove that container explicitly"
fi
if ! "$DOCKER" image inspect "$IMAGE" >/dev/null 2>&1; then
    fail "image '$IMAGE' is not present locally. Build it first from the existing Dockerfile --target workspace; this runner never pulls or builds."
fi

# The container runs an inert keep-alive process; the entrypoint is started
# separately so it can be observed rather than being PID 1.
RUN_ARGS=(run -d --name "$NAME"
    --entrypoint /bin/bash
    -e "HOME=$CONTAINER_HOME"
    -e "TAKT_OPENCODE_PORT=$SERVER_PORT"
    -e "TAKT_WORKSPACE_DIR=$WORKSPACE_DIR"
    -p "127.0.0.1::$SERVER_PORT"
    "$IMAGE" -c 'exec sleep infinity')

# Isolation self-check: nothing may mount host state, share a host namespace,
# add capabilities or expose the Docker socket. The HTTP mapping must be
# loopback-only.
i=0
while [[ "$i" -lt "${#RUN_ARGS[@]}" ]]; do
    arg="${RUN_ARGS[$i]}"
    case "$arg" in
        -v|--volume|--volume=*|--mount|--mount=*|--privileged|--network=host|--net=host|--pid=host|--ipc=host|--userns=host|--cap-add=*|--env-file*|*docker.sock*)
            fail "refusing an isolation-breaking argument: $arg" ;;
        -p|--publish)
            next="${RUN_ARGS[$((i + 1))]-}"
            case "$next" in
                127.0.0.1:*) ;;
                *) fail "the HTTP mapping must be loopback-only: $next" ;;
            esac
            i=$((i + 1)) ;;
        -p*|--publish=*)
            case "$arg" in
                *127.0.0.1:*) ;;
                *) fail "the HTTP mapping must be loopback-only: $arg" ;;
            esac ;;
    esac
    i=$((i + 1))
done

echo "==> starting isolated workspace container"
echo "    docker ${RUN_ARGS[*]}"
CONTAINER_ID=$("$DOCKER" "${RUN_ARGS[@]}")
CONTAINER_CREATED=1
pass "started the isolated container '$NAME' (${CONTAINER_ID:0:12}, entrypoint /bin/bash, no host mounts)"

# --------------------------------------------------------------------------
# Non-root runtime and baked installation visibility
# --------------------------------------------------------------------------

uid=$("$DOCKER" exec "$NAME" id -u || true)
gid=$("$DOCKER" exec "$NAME" id -g || true)
[[ "$uid" == "10001" ]] || fail "the runtime user must be UID 10001, got '$uid'"
[[ "$gid" == "10001" ]] || fail "the runtime group must be GID 10001, got '$gid'"
configured_user=$("$DOCKER" inspect --format '{{.Config.User}}' "$NAME")
case "$configured_user" in
    ''|0|root) fail "the image must not run as root (Config.User='$configured_user')" ;;
esac
pass 'the container runs as UID/GID 10001 with a non-root image user'

for tool in opencode takt-ai engram codegraph; do
    "$DOCKER" exec "$NAME" bash -c "command -v $tool >/dev/null" ||
        fail "the baked image must expose '$tool' on PATH for the runtime user"
done
"$DOCKER" exec "$NAME" test -r "$CONTAINER_HOME/.takt-installed-config.json" ||
    fail 'the baked installation must leave a readable .takt-installed-config.json'
"$DOCKER" exec "$NAME" test -d "$CONTAINER_HOME/.config/opencode" ||
    fail 'the baked installation must leave a readable .config/opencode directory'
"$DOCKER" exec "$NAME" test -d "$CONTAINER_HOME/.config/opencode/skills" ||
    fail 'the baked installation must leave the managed skills directory visible'
pass 'baked OpenCode/Takt binaries, installed config and skills are visible to UID 10001'

"$DOCKER" exec "$NAME" bash -c "echo takt-coder-smoke-marker > $WORKSPACE_DIR/coder-smoke-marker.txt" ||
    fail 'the runtime user must be able to write a marker into the workspace'
marker=$("$DOCKER" exec "$NAME" cat "$WORKSPACE_DIR/coder-smoke-marker.txt")
[[ "$marker" == "takt-coder-smoke-marker" ]] || fail "the workspace marker did not round-trip (got '$marker')"
marker_owner=$("$DOCKER" exec "$NAME" stat -c '%u' "$WORKSPACE_DIR/coder-smoke-marker.txt")
[[ "$marker_owner" == "10001" ]] || fail "the workspace marker must be owned by UID 10001, got '$marker_owner'"
pass 'the workspace is writable by the non-root runtime user'

# --------------------------------------------------------------------------
# The production bootstrap diagnostic, executed inside the image via stdin
# --------------------------------------------------------------------------
#
# TAKT_WORKSPACE_DIR is supplied explicitly, exactly as the production agent
# environment does, because the Coder command bypasses workspace-entrypoint and
# that entrypoint's own default is never executed. HOME and the working
# directory come from the container, so the diagnostic resolves the real
# /workspace and /home/takt that production uses. /bin/bash interprets the
# script because the script's contract uses bash expansions and set -o pipefail.

DIAGNOSTIC_OUT="$TMP/diagnostic.log"
if "$DOCKER" exec -i -e "TAKT_WORKSPACE_DIR=$WORKSPACE_DIR" "$NAME" /bin/bash -s < "$DIAGNOSTIC" > "$DIAGNOSTIC_OUT" 2>&1; then
    cat "$DIAGNOSTIC_OUT"
    pass 'the production bootstrap diagnostic exits 0 inside the image'
else
    cat "$DIAGNOSTIC_OUT" >&2
    fail 'the production bootstrap diagnostic failed inside the image; its stage output above names the missing prerequisite'
fi

# --------------------------------------------------------------------------
# Unmodified workspace entrypoint over a loopback-only HTTP mapping
# --------------------------------------------------------------------------

password=$(head -c 24 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n')
LOG_FILE="$WORKSPACE_DIR/smoke-entrypoint.log"

"$DOCKER" exec -d \
    -e "OPENCODE_PASSWORD=$password" \
    -e "TAKT_OPENCODE_PORT=$SERVER_PORT" \
    -e "TAKT_WORKSPACE_DIR=$WORKSPACE_DIR" \
    "$NAME" /bin/bash -c "exec $ENTRYPOINT > $LOG_FILE 2>&1"
pass 'started the unmodified workspace-entrypoint with a synthetic test-only password'

mapping_count=0
while read -r mapping; do
    [[ -n "$mapping" ]] || continue
    mapping_count=$((mapping_count + 1))
    case "$mapping" in
        127.0.0.1:*) ;;
        *) fail "the HTTP mapping must be loopback-only, got '$mapping'" ;;
    esac
done < <("$DOCKER" port "$NAME" "$SERVER_PORT")
[[ "$mapping_count" -ge 1 ]] || fail 'the HTTP port was not published'
HOST_PORT=$("$DOCKER" port "$NAME" "$SERVER_PORT" | head -n 1 | sed 's/.*://')
[[ -n "$HOST_PORT" ]] || fail 'could not resolve the published loopback port'
pass "the HTTP server is published on loopback only (127.0.0.1:$HOST_PORT)"

http_status=000
api_deadline=$((SECONDS + API_POLL_SECONDS))
while [[ "$SECONDS" -lt "$api_deadline" ]]; do
    http_status=$(curl -sS -o "$TMP/api-info.json" -w '%{http_code}' -u "opencode:$password" "http://127.0.0.1:$HOST_PORT/api/info" 2>/dev/null || true)
    [[ "$http_status" == "200" ]] && break
    sleep 2
done
[[ "$http_status" == "200" ]] || fail "the authenticated /api/info endpoint did not answer 200 within ${API_POLL_SECONDS}s (last status $http_status)"
[[ -s "$TMP/api-info.json" ]] || fail 'the authenticated /api/info endpoint returned an empty body'
pass "the authenticated /api/info endpoint answered 200 within ${API_POLL_SECONDS}s"

log_text=''
session_deadline=$((SECONDS + SESSION_POLL_SECONDS))
while [[ "$SECONDS" -lt "$session_deadline" ]]; do
    log_text=$("$DOCKER" exec "$NAME" cat "$LOG_FILE" 2>/dev/null || true)
    if grep -Fq 'workspace bootstrap:' <<<"$log_text"; then
        break
    fi
    sleep 2
done
if ! grep -Fq 'workspace bootstrap:' <<<"$log_text"; then
    printf '%s\n' "$log_text" >&2
    fail 'the workspace entrypoint did not bootstrap a default session'
fi
if grep -Fq -- "$password" <<<"$log_text"; then
    fail 'the workspace entrypoint printed authentication data: the synthetic password appears in its log'
fi
grep -F 'workspace bootstrap:' <<<"$log_text"
pass 'the entrypoint bootstrapped a default session and printed no authentication data'

api_sessions=$(curl -sS -G \
    --data-urlencode "directory=$WORKSPACE_DIR" \
    -u "opencode:$password" \
    "http://127.0.0.1:$HOST_PORT/api/session" 2>/dev/null || true)
grep -Fq '"id"' <<<"$api_sessions" ||
    fail 'the authenticated API must report the default session created for the workspace'
pass 'the authenticated API corroborates the default session for the workspace'

# --------------------------------------------------------------------------
# Result, remaining unverified rows and cleanup
# --------------------------------------------------------------------------

[[ "$SECONDS" -lt "$TOTAL_BOUND_SECONDS" ]] || fail "the runner exceeded its ${TOTAL_BOUND_SECONDS}s bound"

if [[ "$TUI" -eq 1 ]]; then
    if [[ -t 0 && -t 1 ]]; then
        echo '==> TUI check: launching "docker exec -it <name> opencode"; exit it when you have seen the interface'
        "$DOCKER" exec -it "$NAME" opencode || true
        pass 'the interactive TUI check ran in this terminal (rendering is observed by the operator, not asserted here)'
    else
        echo 'TUI rendering: NOT VERIFIABLE here (no controlling terminal). Run in a terminal:'
        echo "  docker exec -it $NAME opencode"
    fi
else
    echo 'TUI rendering: not checked by this run (needs a controlling terminal; use --tui there)'
fi

echo 'Not proven by this smoke and still unverified: real Coder agent, agent init_script'
echo 'connectivity, Kubernetes scheduling and storage, terminal transport, multi-human'
echo 'isolation, and live memory-tool behaviour.'

if [[ "$CLEANUP" -eq 1 ]]; then
    current=$("$DOCKER" inspect --format '{{.Name}}' "$NAME" || true)
    [[ "$current" == "/$NAME" ]] || fail "refusing cleanup: '$NAME' does not resolve to this run's container"
    "$DOCKER" rm -f "$NAME" >/dev/null
    pass "removed only the container '$NAME'; the image '$IMAGE' was not removed, pruned or rebuilt, and no volume was pruned"
else
    echo "The container '$NAME' is kept for inspection. Remove it with:"
    echo "  docker rm -f $NAME"
    echo "The image '$IMAGE' is untouched; it is never pruned by this runner."
fi

pass 'workspace image smoke complete'
