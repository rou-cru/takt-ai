#!/usr/bin/env bash
# Offline fixture for the Coder bootstrap diagnostic
# (deploy/coder/scripts/check-workspace.sh, work unit N2).
#
# The production diagnostic resolves its own locations through its existing
# interface:
#
#     workspace_dir=${TAKT_WORKSPACE_DIR:-$PWD}
#     home_dir=${HOME:-/home/takt}
#
# so this fixture drives it entirely inside an isolated temporary tree. It
# supplies a temporary HOME and TAKT_WORKSPACE_DIR, and it also exercises the
# PWD fallback and an explicitly empty TAKT_WORKSPACE_DIR. The literal
# production defaults are never reached, and neither /workspace nor /home/takt
# is created, read or written on the host. The production script is executed
# exactly as it is: this fixture rewrites no source file and introduces no
# script-specific variable, flag or configuration knob.
#
# Path selection exercised here (Engram #15074):
#   * an explicit nonempty TAKT_WORKSPACE_DIR takes precedence over PWD, so the
#     success run starts in a deliberately unwritable directory and still
#     passes;
#   * an empty TAKT_WORKSPACE_DIR falls back to PWD, as the :- expansion does;
#   * an unset TAKT_WORKSPACE_DIR falls back to PWD;
#   * a selected workspace that is missing or unwritable fails visibly, and the
#     selected path is never silently replaced by another location.
#
# The remaining cases cover each missing prerequisite, a failing and a hanging
# version command, a literal repository hint, the no-repository-configured
# message, and the absence of installation, network, Git or token output.
#
# Every executable the diagnostic may invoke is replaced by a fixture binary on
# a restricted PATH (no Homebrew, no /usr/local/bin). "timeout" is shimmed
# because macOS hosts ship no coreutils timeout.
#
# No Docker, network, image, cluster or credential is used.
#
# Bound: the diagnostic's normal bound is 60 seconds; that bound is asserted for
# the success run, and because the contract places the outer bound on the
# exerciser, this fixture also imposes a 90-second outer timeout on every run.
# The fixture adds one 15-second per-command timeout case.
#
# The fake binaries are generated through an unquoted heredoc, so every shell
# construct that must survive into the generated fake is escaped; only the tool
# name and the fixture root are substituted at generation time.
set -euo pipefail

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
PRODUCTION="$ROOT/deploy/coder/scripts/check-workspace.sh"

TMP=$(mktemp -d)
cleanup() {
    # chmod can make fixture paths unwritable; restore owner write permission before cleanup.
    chmod -R u+rwX "$TMP" 2>/dev/null || true
    rm -rf "$TMP"
}
trap cleanup EXIT
FIX="$TMP/fixture"
FIX_HOME="$FIX/home"
FIX_WORKSPACE="$FIX/workspace"
ABSENT_WORKSPACE="$FIX/absent-workspace"
READONLY_WORKSPACE="$FIX/read-only-workspace"
DECOY_PWD="$FIX/decoy-pwd"
OUTER_BOUND_SECONDS=90
OUTPUT="$TMP/output"
export FIXTURE="$FIX"
mkdir -p "$FIX/bin"

# A restricted PATH keeps a real opencode/takt-ai/engram/codegraph outside the
# fixture from satisfying a case whose fixture binary was removed.
PATH="$FIX/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

# --------------------------------------------------------------------------
# Fixture binaries
# --------------------------------------------------------------------------

# Only the four prerequisites named by the contract are runnable here; anything
# else the diagnostic tries to run is absent from this PATH.
make_fake() {
    local name=$1
    cat > "$FIX/bin/$name" <<SH
#!/usr/bin/env bash
printf '%s %s\\n' '$name' "\$*" >> "\$FIXTURE/calls"
if [[ "\${FAKE_FAIL:-}" == '$name' ]]; then
    printf '%s\\n' '$name: fixture failure' >&2
    exit 1
fi
if [[ "\${FAKE_SLEEP:-}" == '$name' ]]; then
    exec sleep "\${FAKE_SLEEP_SECONDS:-25}"
fi
printf '%s\\n' '$name fixture 0.0.1'
SH
    chmod +x "$FIX/bin/$name"
}

# Stand-in for GNU coreutils timeout, which macOS does not ship. It runs the
# command, kills it at the deadline and reports 124 exactly as GNU timeout
# does; a command that finishes first returns its own status.
cat > "$FIX/bin/timeout" <<'SH'
#!/usr/bin/env bash
d=15
if [[ "$#" -gt 0 ]]; then d=$1; shift; fi
case "$d" in *s) d=${d%s} ;; esac
"$@" &
pid=$!
( sleep "$d"; kill -TERM "$pid" 2>/dev/null ) &
watchdog=$!
status=0
wait "$pid" || status=$?
kill "$watchdog" 2>/dev/null || true
if [[ "$status" -eq 143 ]]; then exit 124; fi
exit "$status"
SH
chmod +x "$FIX/bin/timeout"

# Tripwires: installation, network and Git clients must never be invoked by the
# diagnostic. Each records the call and fails, so any attempt is both visible
# and unsuccessful.
for tripwire in git curl wget npm pnpm yarn uv pip pip3 apt-get apt apk dnf yum sudo; do
    cat > "$FIX/bin/$tripwire" <<'SH'
#!/usr/bin/env bash
printf '%s %s\n' "${0##*/}" "$*" >> "$FIXTURE/forbidden"
exit 127
SH
    chmod +x "$FIX/bin/$tripwire"
done

if [[ ! -f "$PRODUCTION" ]]; then
    fail "production diagnostic not found: $PRODUCTION"
fi

# --------------------------------------------------------------------------
# Fixture state
# --------------------------------------------------------------------------

# ready rebuilds the full, satisfiable prerequisite set: writable workspace and
# runtime paths beneath a temporary HOME, the four executables, the
# installed-config file and the OpenCode configuration directory.
ready() {
    rm -rf "$FIX_HOME" "$FIX_WORKSPACE" "$ABSENT_WORKSPACE" "$READONLY_WORKSPACE" "$DECOY_PWD"
    mkdir -p "$FIX_WORKSPACE" \
        "$FIX_HOME/.config/opencode" \
        "$FIX_HOME/.local/share" "$FIX_HOME/.local/state" \
        "$FIX_HOME/.local/bin" "$FIX_HOME/.cache"
    : > "$FIX_HOME/.takt-installed-config.json"
    rm -f "$FIX/forbidden"
    : > "$FIX/calls"
    local name
    for name in opencode takt-ai engram codegraph; do
        make_fake "$name"
    done
    unset FAKE_FAIL FAKE_SLEEP FAKE_SLEEP_SECONDS TAKT_REPOSITORY_URL CODER_AGENT_TOKEN OPENCODE_PASSWORD || true
}

# run_subject <cwd> <workspace-selection>
#   The workspace selection is a literal path, the word "empty", or the word
#   "unset". "unset" removes TAKT_WORKSPACE_DIR from the environment so the
#   script must fall back to PWD, exactly as the production expansion does.
run_subject() {
    local cwd=$1 selection=$2
    local status=0
    (
        cd "$cwd" || exit 1
        export HOME="$FIX_HOME"
        export TAKT_REPOSITORY_URL="${TAKT_REPOSITORY_URL-}"
        export CODER_AGENT_TOKEN="${CODER_AGENT_TOKEN-}"
        export OPENCODE_PASSWORD="${OPENCODE_PASSWORD-}"
        case "$selection" in
            unset) unset TAKT_WORKSPACE_DIR ;;
            empty) export TAKT_WORKSPACE_DIR="" ;;
            *) export TAKT_WORKSPACE_DIR="$selection" ;;
        esac
        timeout "$OUTER_BOUND_SECONDS" bash "$PRODUCTION"
    ) > "$OUTPUT" 2>&1 || status=$?
    return "$status"
}

expect_ok() {
    local label=$1 cwd=$2 selection=$3
    if ! run_subject "$cwd" "$selection"; then
        cat "$OUTPUT" >&2
        fail "$label: expected exit 0"
    fi
    pass "$label"
}

expect_bad() {
    local label=$1 cwd=$2 selection=$3
    if run_subject "$cwd" "$selection"; then
        cat "$OUTPUT" >&2
        fail "$label: expected a nonzero exit"
    fi
    if [[ ! -s "$OUTPUT" ]]; then
        fail "$label: the diagnostic failed without printing a stage or failure"
    fi
    pass "$label"
}

# --------------------------------------------------------------------------
# Cases
# --------------------------------------------------------------------------

# Success, with the explicit variable taking precedence over PWD.
ready
TAKT_REPOSITORY_URL="https://git.example.invalid/team/takt-ai.git"
export TAKT_REPOSITORY_URL
SUCCESS_CWD="$FIX_WORKSPACE"
if [[ "$(id -u)" -ne 0 ]]; then
    mkdir -p "$DECOY_PWD"
    chmod 0555 "$DECOY_PWD"
    SUCCESS_CWD="$DECOY_PWD"
fi
SUCCESS_START=$SECONDS
expect_ok 'all prerequisites present exits 0' "$SUCCESS_CWD" "$FIX_WORKSPACE"
SUCCESS_ELAPSED=$((SECONDS - SUCCESS_START))
[[ "$SUCCESS_ELAPSED" -lt 60 ]] || fail "the success run exceeded its 60 second bound (${SUCCESS_ELAPSED}s)"
grep -Fq -- "https://git.example.invalid/team/takt-ai.git" "$OUTPUT" || {
    cat "$OUTPUT"
    fail 'the configured repository hint must be printed verbatim'
}
[[ -s "$FIX/calls" ]] || fail 'no version command ran: exit 0 must be backed by the tool check'
[[ ! -e "$FIX/forbidden" ]] || { cat "$FIX/forbidden"; fail 'the diagnostic invoked an installer, network or Git client'; }
pass 'repository hint printed verbatim and the success run stays inside its 60 second bound'
if [[ "$(id -u)" -eq 0 ]]; then
    echo 'SKIP: the TAKT_WORKSPACE_DIR-over-PWD precedence proof needs a non-root host user'
else
    pass 'an explicit TAKT_WORKSPACE_DIR was used even though PWD was unwritable'
    chmod 0755 "$DECOY_PWD" 2>/dev/null || true
fi

# PWD fallback: an empty value and an unset value both fall back to PWD.
ready
expect_ok 'an empty TAKT_WORKSPACE_DIR falls back to the PWD workspace' "$FIX_WORKSPACE" empty
ready
expect_ok 'an unset TAKT_WORKSPACE_DIR falls back to the PWD workspace' "$FIX_WORKSPACE" unset

ready
expect_ok 'no repository configured still exits 0' "$FIX_WORKSPACE" "$FIX_WORKSPACE"
grep -Eiq 'repo' "$OUTPUT" || {
    cat "$OUTPUT"
    fail 'without a repository the diagnostic must report that none is configured'
}

# Environment values must never become shell program text.
ready
TAKT_REPOSITORY_URL='$(marker_injected)'
export TAKT_REPOSITORY_URL
expect_ok 'a repository hint containing a shell substitution is not executed' "$FIX_WORKSPACE" "$FIX_WORKSPACE"
grep -Fq -- '$(marker_injected)' "$OUTPUT" || {
    cat "$OUTPUT"
    fail 'the repository hint must be printed as a literal, not interpolated into shell source'
}

# Tokens and passwords must never be printed.
ready
CODER_AGENT_TOKEN='fixture-coder-agent-token'
OPENCODE_PASSWORD='fixture-opencode-password'
export CODER_AGENT_TOKEN OPENCODE_PASSWORD
expect_ok 'the diagnostic exits 0 with token and password present' "$FIX_WORKSPACE" "$FIX_WORKSPACE"
if grep -Fq -- 'fixture-coder-agent-token' "$OUTPUT" || grep -Fq -- 'fixture-opencode-password' "$OUTPUT"; then
    cat "$OUTPUT"
    fail 'the diagnostic must never print environment values or tokens'
fi
pass 'tokens and passwords are never printed'

# Each required executable is an independent prerequisite.
for missing in opencode takt-ai engram codegraph; do
    ready
    rm -f "$FIX/bin/$missing"
    expect_bad "missing executable '$missing' fails visibly" "$FIX_WORKSPACE" "$FIX_WORKSPACE"
done

ready
rm -f "$FIX_HOME/.takt-installed-config.json"
expect_bad 'missing readable .takt-installed-config.json fails visibly' "$FIX_WORKSPACE" "$FIX_WORKSPACE"

ready
rm -rf "$FIX_HOME/.config/opencode"
expect_bad 'missing readable .config/opencode directory fails visibly' "$FIX_WORKSPACE" "$FIX_WORKSPACE"

# A selected workspace path must be writable; a bad selection fails instead of
# being replaced by another location.
ready
expect_bad 'an explicit TAKT_WORKSPACE_DIR that does not exist fails instead of being replaced' "$FIX_WORKSPACE" "$ABSENT_WORKSPACE"

if [[ "$(id -u)" -eq 0 ]]; then
    echo 'SKIP: the unwritable-path cases need a non-root host user'
else
    ready
    mkdir -p "$READONLY_WORKSPACE"
    chmod 0555 "$READONLY_WORKSPACE"
    expect_bad 'an unwritable selected workspace fails visibly' "$FIX_WORKSPACE" "$READONLY_WORKSPACE"
    chmod 0755 "$READONLY_WORKSPACE" 2>/dev/null || true

    ready
    mkdir -p "$DECOY_PWD"
    chmod 0555 "$DECOY_PWD"
    expect_bad 'an unwritable PWD workspace fails visibly when TAKT_WORKSPACE_DIR is unset' "$DECOY_PWD" unset
    chmod 0755 "$DECOY_PWD" 2>/dev/null || true

    ready
    chmod 0555 "$FIX_HOME/.config" "$FIX_HOME/.config/opencode" "$FIX_HOME/.local" "$FIX_HOME/.local/share" "$FIX_HOME/.local/state" "$FIX_HOME/.local/bin" "$FIX_HOME/.cache"
    expect_bad 'unwritable runtime paths beneath HOME fail visibly' "$FIX_WORKSPACE" "$FIX_WORKSPACE"
    chmod -R u+rwX "$FIX_HOME" 2>/dev/null || true
fi

# A failing version command must not be reported as success.
ready
FAKE_FAIL=opencode
export FAKE_FAIL
expect_bad 'a failing "opencode --version" fails visibly' "$FIX_WORKSPACE" "$FIX_WORKSPACE"
ready
FAKE_FAIL=takt-ai
export FAKE_FAIL
expect_bad 'a failing "takt-ai version" fails visibly' "$FIX_WORKSPACE" "$FIX_WORKSPACE"

# A hung version command must hit the per-command timeout and be reported with a
# nonzero status instead of hanging the bootstrap.
ready
FAKE_SLEEP=opencode
FAKE_SLEEP_SECONDS=25
export FAKE_SLEEP FAKE_SLEEP_SECONDS
TIMEOUT_START=$SECONDS
expect_bad 'a hanging version command fails visibly instead of blocking' "$FIX_WORKSPACE" "$FIX_WORKSPACE"
TIMEOUT_ELAPSED=$((SECONDS - TIMEOUT_START))
[[ "$TIMEOUT_ELAPSED" -lt 45 ]] || fail "the per-command timeout did not bound the hanging version command (${TIMEOUT_ELAPSED}s)"
pass "the hanging version command returned within ${TIMEOUT_ELAPSED}s (15s per-command timeout)"

# Structural honesty: no installation, clone or Git configuration happened, and
# no client beyond the four prerequisites was invoked.
[[ ! -e "$FIX/forbidden" ]] || { cat "$FIX/forbidden"; fail 'an installer, network or Git client was invoked'; }
if [[ -e "$FIX_HOME/.gitconfig" || -e "$FIX_HOME/.git" ]]; then
    fail 'the diagnostic must not configure Git'
fi
EXTRA=''
while IFS=' ' read -r name _; do
    case "$name" in
        opencode|takt-ai|engram|codegraph|'') ;;
        *) EXTRA="$EXTRA $name" ;;
    esac
done < "$FIX/calls"
[[ -z "$EXTRA" ]] || fail "unexpected client invoked:$EXTRA"
pass 'no installation, clone, Git configuration or extra client invocation occurred'

pass 'bootstrap diagnostic fixture complete'
