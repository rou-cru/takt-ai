#!/usr/bin/env bash
# Focused, offline regressions. No Docker daemon, network, or host install used.
set -euo pipefail
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/repo/scripts" "$TMP/repo/docker" "$TMP/bin" "$TMP/home"
cp "$ROOT"/scripts/*.sh "$TMP/repo/scripts/"
cp "$ROOT/install.sh" "$TMP/repo/"
cp "$ROOT/Makefile" "$TMP/repo/"
# Let the containerized runner (test-containerized.sh) reach its argument
# handling even when its Dockerfile is absent. Fake Docker never builds or
# executes an image.
printf 'FROM scratch\n' > "$TMP/repo/docker/Dockerfile.test"
export FIXTURE="$TMP" HOME="$TMP/home"
export PATH="$TMP/bin:$PATH"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }
expect_failure() {
    local expected=$1 status=0
    shift
    "$@" > "$TMP/output" 2>&1 || status=$?
    [[ "$status" -ne 0 ]] || fail "expected failure: $*"
    grep -Fq -- "$expected" "$TMP/output" || { cat "$TMP/output"; fail "missing: $expected"; }
}

cat > "$TMP/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
command=$1; shift
case "$command" in
    build) cat >/dev/null ;;
    run)
        printf '%s\0' "$@" > "$FIXTURE/docker-args"
        while [[ "$1" != go ]]; do shift; done
        shift
        printf '%s\0' "$@" > "$FIXTURE/go-args"
        cat >/dev/null
        exit "${FAKE_DOCKER_STATUS:-0}"
        ;;
    cp) printf '%s\0' "$@" > "$FIXTURE/cp-args"; exit "${FAKE_CP_STATUS:-0}" ;;
    rm) printf '%s\0' "$@" > "$FIXTURE/rm-args" ;;
    *) exit 99 ;;
esac
SH
chmod +x "$TMP/bin/docker"

assert_args() {
    local file=$1
    shift
    printf '%s\0' "$@" > "$TMP/expected"
    cmp -s "$file" "$TMP/expected" || fail "argument boundaries differ in $file"
}
run_runner() { bash "$TMP/repo/scripts/test-containerized.sh" "$@" > "$TMP/output" 2>&1; }

test_runner() {
    local run_arg='Test spaced'
    run_runner ./takt/vfs -run 'Test spaced/.*' -ldflags '-X main.label=hello world' -args '' '*.go' 'a;b'
    assert_args "$TMP/go-args" ./takt/vfs -run 'Test spaced/.*' -ldflags '-X main.label=hello world' -args '' '*.go' 'a;b'
    pass 'runner preserves spaces, empty arguments, globs, and shell metacharacters'

    # Source only the classification data, not its CLI.
    # shellcheck source=scripts/test-packages.sh
    source "$TMP/repo/scripts/test-packages.sh"
    run_runner
    assert_args "$TMP/go-args" "${CONTAINER_TEST_PKGS[@]}"
    run_runner -run "$run_arg" -ldflags '-s -w'
    assert_args "$TMP/go-args" "${CONTAINER_TEST_PKGS[@]}" -run "$run_arg" -ldflags '-s -w'
    run_runner -run="$run_arg" -args 'test argument, not a package'
    assert_args "$TMP/go-args" "${CONTAINER_TEST_PKGS[@]}" -run="$run_arg" -args 'test argument, not a package'
    pass 'flags-only and -args invocations retain classified container defaults'

    run_runner -coverprofile 'coverage with spaces.out' ./takt/vfs
    assert_args "$TMP/go-args" -coverprofile 'coverage with spaces.out' ./takt/vfs
    local args=() arg
    while IFS= read -r -d '' arg; do args+=("$arg"); done < "$TMP/cp-args"
    [[ "${args[0]}" = takt-test-containerized-*:/src/'coverage with spaces.out' ]] || fail 'coverage source'
    [[ "${args[1]}" = "$TMP/repo/coverage with spaces.out" ]] || fail 'coverage destination'
    [[ -s "$TMP/rm-args" ]] || fail 'coverage container not removed'
    local i=0
    args=()
    while IFS= read -r -d '' arg; do args+=("$arg"); done < "$TMP/docker-args"
    while [[ "$i" -lt "${#args[@]}" ]]; do
        case "${args[$i]}" in
            --mount|--mount=*|--volume=*|--privileged|-v) fail 'volume or privileged mount added' ;;
            *) ;; # every other argument is allowed; only mount/privileged flags are forbidden
        esac
        i=$((i + 1))
    done
    pass 'spaced coverage path copied and cleaned up; no Docker volumes mounted'

    rm -f "$TMP/cp-args" "$TMP/rm-args"
    export FAKE_DOCKER_STATUS=7
    expect_failure 'exit 7' run_runner -coverprofile=coverage.out ./takt/vfs
    [[ ! -e "$TMP/cp-args" ]] || fail 'copied coverage after failed tests'
    [[ -e "$TMP/rm-args" ]] || fail 'failed container not removed'
    unset FAKE_DOCKER_STATUS
    export FAKE_CP_STATUS=1
    expect_failure 'exit 1' run_runner -coverprofile=coverage.out ./takt/vfs
    unset FAKE_CP_STATUS
    pass 'test and coverage-copy failures propagate and still clean up'
}

test_packages() {
    # shellcheck source=scripts/test-packages.sh
    source "$TMP/repo/scripts/test-packages.sh"
    # Fake only discovery. The real checker must compare its manifest to it.
    printf '%s\n' "${HOST_TEST_PKGS[@]}" "${CONTAINER_TEST_PKGS[@]}" > "$TMP/packages"
    cat > "$TMP/bin/go" <<'SH'
#!/usr/bin/env bash
case "$1" in
    list) sed "s|^\./|$(pwd -P)/|" "$FIXTURE/packages" ;;
    test) shift; printf '%s\0' "$@" > "$FIXTURE/host-args" ;;
    *) exit 99 ;;
esac
SH
    chmod +x "$TMP/bin/go"
    check_test_packages
    make -s -C "$TMP/repo" > "$TMP/output" 2>&1
    assert_args "$TMP/host-args" -race -coverprofile=coverage.out "${HOST_TEST_PKGS[@]}"
    rm "$TMP/host-args"
    printf './new/unclassified\n' >> "$TMP/packages"
    expect_failure 'unclassified' check_test_packages
    expect_failure 'unclassified' make -s -C "$TMP/repo" test-host
    [[ ! -e "$TMP/host-args" ]] || fail 'host tests ran despite incomplete classification'
    printf '%s\n' "${HOST_TEST_PKGS[@]}" "${CONTAINER_TEST_PKGS[@]}" > "$TMP/packages"
    # Mutations intentionally stay in separate subshells: each case starts clean.
    # shellcheck disable=SC2030
    ( HOST_TEST_PKGS+=(./deleted/package); expect_failure 'stale' check_test_packages )
    # shellcheck disable=SC2030,SC2031
    ( HOST_TEST_PKGS+=("${CONTAINER_TEST_PKGS[0]}"); expect_failure 'duplicate' check_test_packages )
    # shellcheck disable=SC2031
    ( HOST_TEST_PKGS+=("${HOST_TEST_PKGS[0]}"); expect_failure 'duplicate' check_test_packages )
    rm "$TMP/bin/go"
    pass 'classification rejects unclassified, stale, overlapping, and duplicate packages'
    pass 'make defaults to host tests using the manifest and gates them on completeness'
}

test_install() {
    mkdir -p "$TMP/release" "$TMP/payload" "$TMP/install bin" "$TMP/downloads"
    export TMPDIR="$TMP/downloads" PATH="$TMP/install bin:$PATH"
    local os arch archive
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)
    case "$arch" in
        arm64|aarch64) arch=arm64 ;;
        x86_64|amd64) arch=amd64 ;;
        *) fail "unsupported native fixture architecture: $arch" ;;
    esac
    archive="takt-ai_1.2.3_${os}_${arch}.tar.gz"
    printf '#!/bin/sh\nprintf "fixture v1.2.3\\n"\n' > "$TMP/payload/takt-ai"
    COPYFILE_DISABLE=1 tar -czf "$TMP/release/$archive" -C "$TMP/payload" takt-ai
    [[ "$(wc -c < "$TMP/release/$archive")" -lt 1000 ]] || fail 'fixture must reproduce the old size rejection'
    checksum() { (cd "$TMP/release" && shasum -a 256 "$archive" > checksums.txt); }
    checksum
    cat > "$TMP/bin/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
out=''
while [[ "$#" -gt 0 ]]; do
    case "$1" in -o) out=$2; shift ;; esac
    url=$1
    shift
done
case "$url" in
    */releases/latest) printf '{"tag_name":"v1.2.3"}\n200\n' ;;
    *) cp "$FIXTURE/release/${url##*/}" "$out" ;;
esac
SH
    chmod +x "$TMP/bin/curl"
    install_fixture() { bash "$TMP/repo/install.sh" --method binary --dir "$TMP/install bin"; }
    install_fixture > "$TMP/output" 2>&1 || { cat "$TMP/output"; fail 'small valid archive rejected'; }
    cmp "$TMP/payload/takt-ai" "$TMP/install bin/takt-ai"
    grep -Fq 'Checksum verified' "$TMP/output" || fail 'checksum not checked'
    pass 'valid sub-1000-byte archive installs into an isolated spaced directory'

    rm "$TMP/install bin/takt-ai"
    printf '%064d  %s\n' 0 "$archive" > "$TMP/release/checksums.txt"
    expect_failure 'Checksum mismatch' install_fixture
    [[ ! -e "$TMP/install bin/takt-ai" ]] || fail 'installed mismatched archive'
    printf 'not an archive\n' > "$TMP/release/$archive"
    checksum
    expect_failure 'Failed to extract archive' install_fixture
    printf 'readme\n' > "$TMP/payload/README"
    COPYFILE_DISABLE=1 tar -czf "$TMP/release/$archive" -C "$TMP/payload" README
    checksum
    expect_failure "Binary 'takt-ai' not found in archive" install_fixture
    rm "$TMP/release/checksums.txt"
    expect_failure 'Refusing to install without integrity verification' install_fixture
    [[ ! -e "$TMP/install bin/takt-ai" ]] || fail 'installed invalid archive'
    pass 'checksum mismatch, corrupt archive, missing binary, and missing checksum fail closed'
}

case "${1:-all}" in
    runner) test_runner ;;
    packages) test_packages ;;
    install) test_install ;;
    all) test_runner; test_packages; test_install ;;
    *) fail 'usage: test-shell.sh [runner|packages|install]' ;;
esac
