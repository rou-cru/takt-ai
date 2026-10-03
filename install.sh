#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# Takt AI — install script
# Meta harness for agent teams, on top of OpenCode v2.
#
# Usage:
#   curl -sL https://raw.githubusercontent.com/rou-cru/takt-ai/main/install.sh | bash
#
# Or download and run:
#   curl -sLO https://raw.githubusercontent.com/rou-cru/takt-ai/main/install.sh
#   chmod +x install.sh
#   ./install.sh
# ============================================================================

GITHUB_OWNER="rou-cru"
GITHUB_REPO="takt-ai"
BINARY_NAME="takt-ai"
BREW_TAP="rou-cru/homebrew-takt-ai"
BREW_CASK="rou-cru/takt-ai/takt-ai"  # fully-qualified form `brew trust --cask` requires

# Every curl call is restricted to HTTPS only, via `curl --proto "=https"`.
CURL_PROTO_HTTPS="=https"
# Preferred system-wide install directory, also probed when verifying an install.
USR_LOCAL_BIN="/usr/local/bin"

# ============================================================================
# Color support
# ============================================================================

setup_colors() {
    RED='' GREEN='' YELLOW='' BRAND='' FOCUS='' BOLD='' DIM='' NC=''
    if [[ ! -t 1 ]] || [[ -n "${NO_COLOR:-}" ]] || [[ "${TERM:-}" == "dumb" ]]; then
        return
    fi
    BOLD='\033[1m'
    DIM='\033[2m'
    NC='\033[0m'
    if [[ "${COLORTERM:-}" == "truecolor" || "${COLORTERM:-}" == "24bit" ]]; then
        # Brand tokens (product/brand.md §5): signature P400, focus P300, and
        # the success, warning and danger foregrounds of the dark theme.
        BRAND='\033[38;2;75;163;176m'
        FOCUS='\033[38;2;128;195;203m'
        GREEN='\033[38;2;110;231;183m'
        YELLOW='\033[38;2;252;211;77m'
        RED='\033[38;2;252;165;165m'
    else
        BRAND='\033[36m'
        FOCUS='\033[36m'
        GREEN='\033[32m'
        YELLOW='\033[33m'
        RED='\033[31m'
    fi
}

# Status marks read without color; UTF-8 terminals get the glyphs.
setup_marks() {
    MARK_OK='ok' MARK_WARN='!' MARK_ERR='x'
    if [[ "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" == *UTF-8* || "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" == *utf8* ]]; then
        MARK_OK='✓' MARK_ERR='✗'
    fi
}

# ============================================================================
# Logging helpers
# ============================================================================

info()    { echo -e "  ${DIM}$*${NC}"; }
success() { echo -e "${GREEN}${MARK_OK}${NC} $*"; }
warn()    { echo -e "${YELLOW}${MARK_WARN}${NC} $*"; }
error()   { echo -e "${RED}${MARK_ERR}${NC} $*" >&2; }
fatal()   { error "$@"; exit 1; }
step()    { echo -e "\n${BOLD}$*${NC}"; }

# ============================================================================
# Help
# ============================================================================

show_help() {
    cat <<EOF
${BOLD}Takt AI installer${NC}

Usage: install.sh [OPTIONS]

Options:
  --method METHOD   Force install method: brew, go, binary (default: auto-detect)
  --dir DIR         Custom install directory for binary method
  --insecure        Skip checksum verification (not recommended)
  -h, --help        Show this help

Install methods (auto-detected in priority order):
  1. brew    — Homebrew tap (recommended)
  2. binary  — Pre-built binary from GitHub Releases

  go — go install from source; not auto-detected, select with --method go

Examples:
  curl -sL https://raw.githubusercontent.com/${GITHUB_OWNER}/${GITHUB_REPO}/main/install.sh | bash
  ./install.sh --method binary
  ./install.sh --method binary --dir \$HOME/.local/bin
  ./install.sh --method binary --insecure   # skip checksum (not recommended)

EOF
}

# ============================================================================
# Platform detection
# ============================================================================

detect_platform() {
    local uname_os uname_arch

    uname_os="$(uname -s)"
    uname_arch="$(uname -m)"

    case "$uname_os" in
        Darwin) OS="darwin"; OS_LABEL="macOS"; GORELEASER_OS="darwin" ;;
        Linux)  OS="linux";  OS_LABEL="Linux"; GORELEASER_OS="linux" ;;
        *)      fatal "Unsupported OS: $uname_os. Only macOS and Linux are supported." ;;
    esac

    case "$uname_arch" in
        x86_64|amd64)   ARCH="amd64" ;;
        arm64|aarch64)  ARCH="arm64" ;;
        *)              fatal "Unsupported architecture: $uname_arch. Only amd64 and arm64 are supported." ;;
    esac

    success "Platform: ${OS_LABEL} (${OS}/${ARCH})"
}

# ============================================================================
# GoReleaser archive naming
#
# From .goreleaser.yaml:
#   name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
#
# GoReleaser v2 {{ .Os }} produces GOOS values (lowercase: darwin, linux)
# GoReleaser {{ .Arch }} produces GOARCH values (amd64, arm64)
# Examples:
#   takt-ai_1.0.0_darwin_arm64.tar.gz
#   takt-ai_1.0.0_linux_amd64.tar.gz
# ============================================================================

get_archive_name() {
    local version="$1"
    echo "${BINARY_NAME}_${version}_${GORELEASER_OS}_${ARCH}.tar.gz"
}

# ============================================================================
# Prerequisites
# ============================================================================

check_prerequisites() {
    step "Checking prerequisites"

    local missing=()

    if ! command -v curl &>/dev/null; then
        missing+=("curl")
    fi

    if ! command -v git &>/dev/null; then
        missing+=("git")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        fatal "Missing required tools: ${missing[*]}. Please install them and try again."
    fi

    success "curl and git are available"
}

# ============================================================================
# Install method detection
# ============================================================================

detect_install_method() {
    if [[ -n "${FORCE_METHOD:-}" ]]; then
        case "$FORCE_METHOD" in
            brew|go|binary) INSTALL_METHOD="$FORCE_METHOD" ;;
            *) fatal "Unknown install method: $FORCE_METHOD. Use: brew, go, or binary" ;;
        esac
        info "Using forced method: $INSTALL_METHOD"
        return
    fi

    step "Detecting best install method"

    # Priority: brew > binary > go
    # Brew handles upgrades natively and is instant.
    # Binary download from GitHub Releases is always up-to-date.
    # go install is last resort because the Go module proxy can lag
    # behind new tags for up to 30 minutes, causing @latest to install
    # a stale version.
    if command -v brew &>/dev/null; then
        INSTALL_METHOD="brew"
        success "Homebrew found — will install via brew tap"
    else
        INSTALL_METHOD="binary"
        info "Will download pre-built binary from GitHub Releases"
    fi
}

# ============================================================================
# Install via Homebrew
# ============================================================================

install_brew() {
    step "Installing via Homebrew"

    # Homebrew 7+ requires non-official taps/casks to be explicitly trusted
    # before it will load them; skip on older Homebrew where the command
    # doesn't exist.
    if brew trust --help &>/dev/null; then
        brew trust --tap "$BREW_TAP" 2>/dev/null || true
        brew trust --cask "$BREW_CASK" 2>/dev/null || true
    fi

    # Always refresh the tap to pick up new releases. Homebrew's own hints
    # (outdated formulae, env tips) are noise here; output shows on failure.
    export HOMEBREW_NO_ENV_HINTS=1
    info "Refreshing ${BREW_TAP}..."
    brew untap "$BREW_TAP" >/dev/null 2>&1 || true
    local tap_output
    if ! tap_output="$(brew tap "$BREW_TAP" 2>&1)"; then
        printf '%s\n' "$tap_output" >&2
        # The tap is optional for releases; auto-detection falls back to the binary.
        if [[ -z "${FORCE_METHOD:-}" ]]; then
            warn "Failed to tap $BREW_TAP — falling back to the pre-built binary"
            install_binary
            return
        fi
        fatal "Failed to tap $BREW_TAP"
    fi

    # Releases publish a cask (goreleaser homebrew_casks), not a formula.
    if brew list --cask "$BINARY_NAME" &>/dev/null; then
        info "Already installed, upgrading ${BINARY_NAME}..."
        local output
        if output="$(brew upgrade --cask "$BINARY_NAME" 2>&1)"; then
            success "Upgraded ${BINARY_NAME} via Homebrew"
        elif printf '%s' "$output" | grep -Eiq 'already.*(up-to-date|installed)|not outdated'; then
            success "${BINARY_NAME} is already at the latest version"
        else
            printf '%s\n' "$output" >&2
            fatal "Failed to upgrade ${BINARY_NAME} via Homebrew"
        fi
    else
        info "Installing ${BINARY_NAME}..."
        local output
        if output="$(brew install --cask "$BINARY_NAME" 2>&1)"; then
            success "Installed ${BINARY_NAME} via Homebrew"
        else
            printf '%s\n' "$output" >&2
            fatal "Failed to install ${BINARY_NAME} via Homebrew"
        fi
    fi
}

# ============================================================================
# Install via go install
# ============================================================================

install_go() {
    step "Installing via go install"

    # Lowercase the owner portably: ${var,,} needs bash 4+, but macOS ships
    # bash 3.2, so piping `| bash` would fail with "bad substitution".
    local owner_lc
    owner_lc="$(printf '%s' "$GITHUB_OWNER" | tr '[:upper:]' '[:lower:]')"
    local go_package="github.com/${owner_lc}/${GITHUB_REPO}/takt/cli@latest"

    local gobin
    gobin="$(go env GOBIN)"
    if [[ -z "$gobin" ]]; then
        gobin="$(go env GOPATH)/bin"
    fi

    # The main package lives in takt/cli, so go install names the binary
    # "cli"; build into a scratch GOBIN and install it under its real name.
    local tmpbin
    tmpbin="$(mktemp -d)"
    trap '[[ -n "${tmpbin:-}" ]] && rm -rf "$tmpbin"' EXIT

    info "Running: go install ${go_package}"
    if ! GOBIN="$tmpbin" go install "$go_package"; then
        fatal "Failed to install via go install. Make sure Go is properly configured."
    fi
    mkdir -p "$gobin"
    mv -f "${tmpbin}/cli" "${gobin}/${BINARY_NAME}" || fatal "Failed to install ${BINARY_NAME} into ${gobin}"

    # Verify GOBIN / GOPATH/bin is in PATH

    if [[ ":$PATH:" != *":$gobin:"* ]]; then
        warn "${gobin} is not in your PATH"
        warn "Add this to your shell profile: export PATH=\"\$PATH:${gobin}\""
    fi

    success "Installed ${BINARY_NAME} via go install"
}

# ============================================================================
# Install via binary download
# ============================================================================

get_latest_version() {
    local url="https://api.github.com/repos/${GITHUB_OWNER}/${GITHUB_REPO}/releases/latest"

    info "Fetching latest release from GitHub..."

    local response
    response="$(curl -sL --proto "$CURL_PROTO_HTTPS" -w "\n%{http_code}" "$url")" || fatal "Failed to fetch latest release"

    local http_code body
    http_code="$(echo "$response" | tail -n1)"
    body="$(echo "$response" | sed '$d')"

    if [[ "$http_code" != "200" ]]; then
        fatal "GitHub API returned HTTP $http_code. Rate limited? Try again later or use --method brew/go"
    fi

    # Extract tag_name — works without jq
    LATEST_VERSION="$(echo "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"

    if [[ -z "$LATEST_VERSION" ]]; then
        fatal "Could not determine latest version from GitHub API response"
    fi

    # Strip leading 'v' for archive naming (goreleaser uses version without v prefix)
    VERSION_NUMBER="${LATEST_VERSION#v}"

    success "Latest version: ${LATEST_VERSION}"
}

# Verifies the keyless Sigstore signature of checksums.txt when cosign is
# available. It proves the checksums (and so every archive) were produced by
# this repository's release workflow. Without cosign, sha256 still applies.
verify_signature() {
    local dir="$1"
    if ! command -v cosign &>/dev/null; then
        info "cosign not found — skipping signature verification (checksums still verified)"
        return
    fi
    local bundle_url="https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/checksums.txt.sigstore.json"
    if ! curl -sfL --proto "$CURL_PROTO_HTTPS" -o "${dir}/checksums.txt.sigstore.json" "$bundle_url"; then
        if [[ "$INSECURE" = "true" ]]; then
            warn "Could not download the signature bundle — signature verification skipped (--insecure)"
            return
        fi
        fatal "Could not download ${bundle_url}\nRefusing to install with cosign present but no signature.\nUse --insecure to skip (not recommended)."
    fi
    if ! cosign verify-blob \
        --bundle "${dir}/checksums.txt.sigstore.json" \
        --certificate-identity-regexp "^https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/.github/workflows/release.yml@" \
        --certificate-oidc-issuer https://token.actions.githubusercontent.com \
        "${dir}/checksums.txt" >/dev/null 2>&1; then
        fatal "Signature verification of checksums.txt failed"
    fi
    success "Signature verified (Sigstore, ${GITHUB_OWNER}/${GITHUB_REPO} release workflow)"
}

install_binary() {
    step "Installing pre-built binary"

    get_latest_version

    local archive_name
    archive_name="$(get_archive_name "$VERSION_NUMBER")"
    local download_url="https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/${archive_name}"
    local checksums_url="https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/checksums.txt"

    # Create temp directory — clean up on exit
    local tmpdir
    tmpdir="$(mktemp -d)"
    trap '[[ -n "${tmpdir:-}" ]] && rm -rf "$tmpdir"' EXIT

    # Download archive
    info "Downloading ${archive_name}..."
    if ! curl -sfL --proto "$CURL_PROTO_HTTPS" -o "${tmpdir}/${archive_name}" "$download_url"; then
        fatal "Failed to download ${download_url}"
    fi

    # Size is informational; integrity and format are checked below. A small
    # valid archive is not an error (and a large HTML response is not proof).
    local file_size
    file_size="$(wc -c < "${tmpdir}/${archive_name}" | tr -d '[:space:]')"
    success "Downloaded ${archive_name} (${file_size} bytes)"

    # Download and verify checksum — fail closed unless --insecure is set
    info "Verifying checksum..."
    if curl -sfL --proto "$CURL_PROTO_HTTPS" -o "${tmpdir}/checksums.txt" "$checksums_url"; then
        verify_signature "$tmpdir"
        local expected_checksum
        expected_checksum="$(grep "${archive_name}" "${tmpdir}/checksums.txt" 2>/dev/null | awk '{print $1}' || true)"

        if [[ -n "$expected_checksum" ]]; then
            local actual_checksum
            if command -v sha256sum &>/dev/null; then
                actual_checksum="$(sha256sum "${tmpdir}/${archive_name}" | awk '{print $1}')"
            elif command -v shasum &>/dev/null; then
                actual_checksum="$(shasum -a 256 "${tmpdir}/${archive_name}" | awk '{print $1}')"
            else
                if [[ "$INSECURE" = "true" ]]; then
                    warn "No sha256sum or shasum found — checksum verification skipped (--insecure)"
                    actual_checksum="$expected_checksum"
                else
                    fatal "No sha256sum or shasum tool found. Cannot verify checksum.\nInstall coreutils (sha256sum) or use --insecure to skip (not recommended)."
                fi
            fi

            if [[ "$actual_checksum" != "$expected_checksum" ]]; then
                fatal "Checksum mismatch!\n  Expected: ${expected_checksum}\n  Got:      ${actual_checksum}"
            fi
            success "Checksum verified"
        else
            if [[ "$INSECURE" = "true" ]]; then
                warn "Archive '${archive_name}' not found in checksums.txt — checksum verification skipped (--insecure)"
            else
                fatal "Archive '${archive_name}' not found in checksums.txt. Refusing to install unverified binary.\nUse --insecure to skip (not recommended)."
            fi
        fi
    else
        if [[ "$INSECURE" = "true" ]]; then
            warn "Could not download checksums.txt — checksum verification skipped (--insecure)"
        else
            fatal "Could not download checksums.txt from:\n  ${checksums_url}\nRefusing to install without integrity verification.\nUse --insecure to skip (not recommended)."
        fi
    fi

    # Extract binary
    info "Extracting ${BINARY_NAME}..."
    if ! tar -xzf "${tmpdir}/${archive_name}" -C "$tmpdir"; then
        fatal "Failed to extract archive"
    fi

    if [[ ! -f "${tmpdir}/${BINARY_NAME}" ]]; then
        fatal "Binary '${BINARY_NAME}' not found in archive"
    fi

    # Determine install directory
    local install_dir="${INSTALL_DIR:-}"

    if [[ -z "$install_dir" ]]; then
        if [[ -d "$USR_LOCAL_BIN" ]] && [[ -w "$USR_LOCAL_BIN" ]]; then
            install_dir="$USR_LOCAL_BIN"
        elif [[ "$(id -u)" = "0" ]]; then
            install_dir="$USR_LOCAL_BIN"
        else
            install_dir="${HOME}/.local/bin"
        fi
    fi

    # Create install dir if needed
    mkdir -p "$install_dir"

    # Install binary
    info "Installing to ${install_dir}/${BINARY_NAME}..."
    if cp "${tmpdir}/${BINARY_NAME}" "${install_dir}/${BINARY_NAME}" 2>/dev/null; then
        chmod +x "${install_dir}/${BINARY_NAME}"
    elif command -v sudo &>/dev/null; then
        warn "Permission denied. Trying with sudo..."
        sudo cp "${tmpdir}/${BINARY_NAME}" "${install_dir}/${BINARY_NAME}"
        sudo chmod +x "${install_dir}/${BINARY_NAME}"
    else
        fatal "Cannot write to ${install_dir}. Run with sudo or use --dir to specify a writable directory."
    fi

    success "Installed ${BINARY_NAME} to ${install_dir}/${BINARY_NAME}"

    # Check if install dir is in PATH
    if [[ ":$PATH:" != *":${install_dir}:"* ]]; then
        warn "${install_dir} is not in your PATH"
        echo ""
        warn "Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
        echo -e "  ${DIM}export PATH=\"\$PATH:${install_dir}\"${NC}"
        echo ""
    fi
}

# ============================================================================
# Verify installation
# ============================================================================

verify_installation() {
    step "Verifying installation"

    # Allow PATH changes to take effect
    hash -r 2>/dev/null || true

    if command -v "$BINARY_NAME" &>/dev/null; then
        local version_output
        version_output="$("$BINARY_NAME" version 2>&1 || true)"
        success "${BINARY_NAME} is installed: ${version_output}"
        return 0
    fi

    # Check common locations even if not in PATH
    local locations=(
        "${USR_LOCAL_BIN}/${BINARY_NAME}"
        "${HOME}/.local/bin/${BINARY_NAME}"
        "$(go env GOPATH 2>/dev/null || echo "")/bin/${BINARY_NAME}"
    )

    for loc in "${locations[@]}"; do
        if [[ -n "$loc" ]] && [[ -x "$loc" ]]; then
            local version_output
            version_output="$("$loc" version 2>&1 || true)"
            success "Found ${BINARY_NAME} at ${loc}: ${version_output}"
            warn "Binary location is not in your PATH. Add it to use '${BINARY_NAME}' directly."
            return 0
        fi
    done

    warn "Could not verify installation. You may need to restart your shell."
    return 0
}

# ============================================================================
# Print next steps
# ============================================================================

# BEGIN GENERATED LOGO (development/generate-logo; DO NOT EDIT)
print_logo() {
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m      \033[0m \033[0;38;2;66;93;99m▂\033[0;38;2;72;122;132m▃\033[0;38;2;70;118;128m▅\033[0;38;2;70;116;126m▆\033[0;38;2;66;105;115m▔\033[0;38;2;65;103;113m▔\033[0;38;2;64;102;112m▔\033[0;38;2;64;103;113m▔\033[0;38;2;68;114;124m▆\033[0;38;2;70;118;128m▅\033[0;38;2;66;107;115m▄\033[0;38;2;68;96;102m▂\033[0m \033[0;38;2;3;3;3m       \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m    \033[0m \033[0;38;2;63;109;118m▄\033[0;38;2;63;108;118m▔\033[0;38;2;26;30;34;48;2;63;112;123m▃\033[0;38;2;17;18;21;48;2;64;107;118m▅\033[0;38;2;11;12;14;48;2;59;80;90m▆\033[0;38;2;45;49;56;48;2;11;12;13m▔\033[0;38;2;13;14;16;48;2;9;10;12m▄\033[0;38;2;14;14;16;48;2;10;10;12m▄\033[0;38;2;14;14;16;48;2;10;11;12m▄\033[0;38;2;14;15;17;48;2;9;10;12m▄\033[0;38;2;44;47;54;48;2;12;13;14m▔\033[0;38;2;12;12;14;48;2;60;81;91m▆\033[0;38;2;17;18;21;48;2;65;108;119m▅\033[0;38;2;25;29;33;48;2;63;111;122m▃\033[0;38;2;64;109;119m▔\033[0;38;2;64;111;120m▄\033[0m \033[0;38;2;34;47;50m     \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m   \033[0;38;2;54;87;94m▄\033[0;38;2;54;85;92;48;2;62;110;121m▔\033[0;38;2;25;29;32;48;2;60;101;111m▄\033[0;38;2;44;54;62;48;2;14;14;16m▔\033[0;38;2;13;13;15;48;2;10;11;12m▆\033[0;38;2;23;24;25;48;2;12;13;14m▕\033[0;38;2;203;201;201;48;2;24;24;26m▃\033[0;38;2;239;237;237;48;2;15;15;17m▄\033[0;38;2;208;207;206;48;2;24;25;27m▃\033[0;38;2;233;231;232;48;2;26;26;28m▂\033[0;38;2;241;239;239;48;2;37;37;38m▃\033[0;38;2;223;219;218;48;2;14;15;17m▅\033[0;38;2;205;201;200;48;2;24;25;27m▃\033[0;38;2;166;165;164;48;2;17;18;20m▁\033[0;38;2;15;16;18;48;2;14;14;15m▆\033[0;38;2;14;15;17;48;2;12;12;14m▅\033[0;38;2;43;53;60;48;2;14;14;16m▔\033[0;38;2;23;26;30;48;2;59;99;110m▄\033[0;38;2;56;89;96;48;2;62;110;120m▔\033[0;38;2;55;91;98m▄\033[0m \033[0;38;2;1;2;2m   \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m  \033[0;38;2;52;82;89m▘\033[0;38;2;51;78;85;48;2;60;108;118m▕\033[0;38;2;60;83;92;48;2;14;15;17m▘\033[0;38;2;11;11;13;48;2;13;13;15m▘\033[0;38;2;11;11;13;48;2;14;15;17m \033[0;38;2;13;17;18;48;2;13;14;15m▂\033[0;38;2;21;21;22;48;2;214;210;209m▊\033[0;38;2;238;234;233;48;2;134;132;131m▊\033[0;38;2;187;184;182;48;2;20;21;22m▔\033[0;38;2;116;116;116;48;2;227;225;225m▍\033[0;38;2;245;243;243;48;2;244;242;242m▘\033[0;38;2;244;242;242;48;2;243;240;239m▄\033[0;38;2;189;188;188;48;2;242;238;237m▁\033[0;38;2;67;67;67;48;2;241;237;236m▃\033[0;38;2;103;102;102;48;2;235;231;230m▂\033[0;38;2;71;70;72;48;2;225;222;220m▝\033[0;38;2;191;188;187;48;2;22;23;24m▂\033[0;38;2;35;35;36;48;2;14;15;16m▁\033[0;38;2;12;12;13;48;2;14;14;15m▝\033[0;38;2;58;79;88;48;2;14;15;16m▝\033[0;38;2;58;74;83;48;2;59;112;122m▖\033[0;38;2;56;89;96m▝\033[0;38;2;1;2;2m   \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m \033[0;38;2;46;66;72m▋\033[0;38;2;67;111;121;48;2;57;109;119m▏\033[0;38;2;63;93;102;48;2;21;24;27m▘\033[0;38;2;12;12;14;48;2;14;14;15m▘\033[0;38;2;14;15;16;48;2;14;15;17m▊\033[0;38;2;14;15;17;48;2;14;15;16m▌\033[0;38;2;114;113;114;48;2;14;14;16m▗\033[0;38;2;227;225;226;48;2;74;74;75m▅\033[0;38;2;243;241;241;48;2;242;238;239m▘\033[0;38;2;187;185;185;48;2;242;238;238m▔\033[0;38;2;241;238;238;48;2;242;240;240m▔\033[0;38;2;243;241;241;48;2;242;240;240m▔\033[0;38;2;242;239;239;48;2;244;242;242m▅\033[0;38;2;163;162;163;48;2;238;235;235m▔\033[0;38;2;244;242;241;48;2;56;56;57m▄\033[0;38;2;233;231;229;48;2;50;51;51m▅\033[0;38;2;220;217;215;48;2;240;236;235m▏\033[0;38;2;242;238;237;48;2;243;240;239m▍\033[0;38;2;113;112;112;48;2;223;221;220m▗\033[0;38;2;120;118;118;48;2;53;52;53m▘\033[0;38;2;14;14;15;48;2;13;13;14m▁\033[0;38;2;61;83;93;48;2;20;21;24m▝\033[0;38;2;63;104;113;48;2;59;110;119m▏\033[0;38;2;54;79;85m▍  \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m \033[0;38;2;55;94;104m▍\033[0;38;2;48;53;60;48;2;61;108;117m▕\033[0;38;2;12;14;16;48;2;12;13;14m▔\033[0;38;2;14;15;17;48;2;14;14;16m▗\033[0;38;2;14;15;17;48;2;14;15;17m \033[0;38;2;121;120;121;48;2;14;14;15m▗\033[0;38;2;153;151;152;48;2;228;226;226m▏\033[0;38;2;242;239;240;48;2;242;239;238m▔\033[0;38;2;242;238;239;48;2;242;239;238m▕\033[0;38;2;242;238;237;48;2;242;238;239m▆\033[0;38;2;242;238;237;48;2;242;238;238m ▝\033[0;38;2;227;223;223;48;2;237;234;233m▕\033[0;38;2;223;219;218;48;2;218;214;213m▏\033[0;38;2;179;177;176;48;2;194;192;192m▁\033[0;38;2;225;223;222;48;2;210;208;207m▕\033[0;38;2;201;199;198;48;2;244;242;241m▁\033[0;38;2;38;38;38;48;2;230;227;226m▁\033[0;38;2;38;39;39;48;2;209;205;204m▄\033[0;38;2;44;43;44;48;2;12;12;13m▏\033[0;38;2;44;43;44;48;2;14;14;15m \033[0;38;2;13;13;14;48;2;10;11;12m▋\033[0;38;2;39;42;48;48;2;61;107;117m▏\033[0;38;2;62;103;113m▋  \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m \033[0;38;2;50;87;95m▍\033[0;38;2;64;73;77;48;2;59;106;116m▕\033[0;38;2;19;20;21;48;2;13;13;14m▁\033[0;38;2;14;14;15;48;2;15;15;16m▂\033[0;38;2;15;15;16;48;2;14;14;15m▘\033[0;38;2;76;75;76;48;2;227;225;225m▍\033[0;38;2;239;237;238;48;2;241;238;238m▃▄\033[0;38;2;240;238;238;48;2;242;238;237m▆\033[0;38;2;239;237;238;48;2;242;238;238m▅\033[0;38;2;239;237;238;48;2;241;238;238m▃\033[0;38;2;240;238;238;48;2;242;239;238m▄\033[0;38;2;103;102;101;48;2;235;233;232m▗\033[0;38;2;226;223;222;48;2;70;69;70m▘\033[0;38;2;80;79;79;48;2;173;171;170m▆\033[0;38;2;155;152;151;48;2;20;20;20m▔\033[0;38;2;16;17;17;48;2;11;12;12m▔\033[0;38;2;13;13;14;48;2;11;11;12m▅\033[0;38;2;11;12;12;48;2;13;13;14m▔\033[0;38;2;14;14;14;48;2;13;13;14m▕\033[0;38;2;14;14;14;48;2;14;14;15m \033[0;38;2;12;13;15;48;2;12;13;14m▕\033[0;38;2;50;57;61;48;2;60;106;116m▏\033[0;38;2;57;98;108m▋  \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m \033[0;38;2;36;60;65m▝\033[0;38;2;46;87;96;48;2;48;97;107m▁\033[0;38;2;71;104;112;48;2;30;37;39m▎\033[0;38;2;13;13;15;48;2;14;14;15m▖\033[0;38;2;139;136;135;48;2;15;15;16m▕\033[0;38;2;224;221;220;48;2;238;235;236m▏\033[0;38;2;239;235;235;48;2;238;235;235m▁\033[0;38;2;238;235;236;48;2;238;237;237m▅\033[0;38;2;238;235;235;48;2;239;237;237m▃\033[0;38;2;238;236;236;48;2;238;237;237m▂\033[0;38;2;238;235;236;48;2;238;237;237m▂\033[0;38;2;40;40;39;48;2;221;220;219m▗\033[0;38;2;178;191;194;48;2;55;54;55m▗\033[0;38;2;63;61;62;48;2;174;183;186m▗\033[0;38;2;54;52;53;48;2;13;13;14m▔\033[0;38;2;13;14;14;48;2;13;13;13m▅\033[0;38;2;14;14;16;48;2;14;14;15m▄▏\033[0;38;2;14;14;16;48;2;14;14;16m \033[0;38;2;14;14;16;48;2;14;14;15m▆\033[0;38;2;14;14;15;48;2;13;13;14m▍\033[0;38;2;70;106;114;48;2;26;30;32m▗\033[0;38;2;57;105;115;48;2;48;98;108m▔\033[0;38;2;48;76;84m▘  \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m  \033[0;38;2;44;90;99m▝\033[0;38;2;75;107;114;48;2;44;93;103m▝\033[0;38;2;68;106;114;48;2;26;30;32m▖\033[0;38;2;226;220;220;48;2;23;22;23m▕\033[0;38;2;237;233;233;48;2;237;233;235m▏\033[0;38;2;236;234;234;48;2;238;235;235m▅\033[0;38;2;237;234;234;48;2;238;235;236m▅\033[0;38;2;238;235;234;48;2;238;235;235m▌▘\033[0;38;2;238;235;235;48;2;238;235;236m▃\033[0;38;2;237;233;233;48;2;35;33;33m▍\033[0;38;2;151;143;144;48;2;186;202;206m▏\033[0;38;2;160;175;179;48;2;27;26;27m▋\033[0;38;2;11;11;12;48;2;13;13;13m▎\033[0;38;2;13;13;14;48;2;14;14;15m▌\033[0;38;2;14;14;16;48;2;14;14;15m▕\033[0;38;2;13;13;15;48;2;14;14;16m▃\033[0;38;2;14;14;16;48;2;14;14;15m▕\033[0;38;2;13;13;14;48;2;13;14;15m▗\033[0;38;2;70;105;113;48;2;22;25;27m▗\033[0;38;2;54;87;93;48;2;50;98;107m▔\033[0;38;2;37;63;70m▗\033[0;38;2;0;0;0m   \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m   \033[0;38;2;41;78;87m▝\033[0;38;2;27;48;52;48;2;48;96;106m▂\033[0;38;2;82;97;101;48;2;46;77;83m▕\033[0;38;2;135;160;166;48;2;227;226;228m▁\033[0;38;2;235;232;233;48;2;235;233;234m▅\033[0;38;2;235;232;232;48;2;235;234;235m▄\033[0;38;2;235;232;232;48;2;236;234;235m▂▂\033[0;38;2;236;232;233;48;2;236;234;235m▂\033[0;38;2;75;73;73;48;2;231;228;229m▝\033[0;38;2;139;137;139;48;2;176;194;198m▖\033[0;38;2;145;159;162;48;2;174;199;204m▕\033[0;38;2;153;169;174;48;2;42;41;43m▖\033[0;38;2;22;22;22;48;2;13;13;13m▏\033[0;38;2;13;13;13;48;2;13;13;14m▖\033[0;38;2;13;14;15;48;2;13;13;14m▕\033[0;38;2;65;87;92;48;2;16;17;18m▂\033[0;38;2;54;93;101;48;2;46;72;78m▕\033[0;38;2;30;48;53;48;2;48;91;100m▁\033[0;38;2;44;83;92m▘    \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m    \033[0m \033[0;38;2;39;76;84m▅\033[0;38;2;47;87;95m▂\033[0;38;2;46;85;94;48;2;187;199;202m▅\033[0;38;2;48;87;95;48;2;216;219;220m▃\033[0;38;2;81;115;122;48;2;232;230;231m▂\033[0;38;2;149;170;175;48;2;234;232;232m▁\033[0;38;2;229;230;231;48;2;235;232;232m▁\033[0;38;2;236;234;233;48;2;235;232;231m▁\033[0;38;2;200;200;202;48;2;232;229;229m▔\033[0;38;2;228;229;231;48;2;187;208;213m▃\033[0;38;2;148;171;176;48;2;182;205;210m▁\033[0;38;2;142;166;171;48;2;79;79;81m▆\033[0;38;2;93;127;135;48;2;46;61;65m▖\033[0;38;2;54;69;73;48;2;50;88;95m▘\033[0;38;2;46;89;98m▂\033[0;38;2;39;71;78m▄\033[0m \033[0;38;2;20;26;29m     \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;0;0;0m       \033[0;38;2;29;43;47m▔\033[0;38;2;35;65;72m▅\033[0;38;2;37;74;82m▄\033[0;38;2;39;78;86m▃\033[0;38;2;52;86;94m▂\033[0;38;2;62;97;104m▂\033[0;38;2;61;96;103m▂\033[0;38;2;52;84;92m▂\033[0;38;2;39;78;86m▃\033[0;38;2;38;74;82m▄\033[0;38;2;38;67;75m▅\033[0;38;2;33;48;54m▔        \033[0m'
}
# END GENERATED LOGO

print_banner() {
    # Decorative output only on an interactive terminal (brand.md §12.3); the
    # logo needs true color, so other terminals get the signature alone.
    [[ -t 1 ]] || return 0
    echo ""
    if [[ -n "$BRAND" && ( "${COLORTERM:-}" == "truecolor" || "${COLORTERM:-}" == "24bit" ) ]] && declare -F print_logo >/dev/null; then
        print_logo
        echo ""
    fi
    echo -e "${BRAND}${BOLD}Takt AI${NC}  ${DIM}Meta harness for agent teams${NC}"
}

print_next_steps() {
    local version
    version="$("$BINARY_NAME" version 2>/dev/null | awk '{print $2}' || true)"
    echo ""
    echo -e "${GREEN}${MARK_OK}${NC} ${BOLD}Takt AI${version:+ ${version}} is installed.${NC}"
    echo -e "  Run ${FOCUS}${BOLD}${BINARY_NAME}${NC} to set it up on OpenCode."
    echo -e "  ${DIM}https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}${NC}"
    echo ""
}

# ============================================================================
# Main
# ============================================================================

main() {
    setup_colors
    setup_marks

    # Parse arguments
    FORCE_METHOD=""
    INSTALL_DIR=""
    INSECURE="false"

    while [[ $# -gt 0 ]]; do
        local arg="$1"
        case "$arg" in
            --method)
                [[ $# -lt 2 ]] && fatal "--method requires an argument"
                FORCE_METHOD="$2"; shift 2
                ;;
            --dir)
                [[ $# -lt 2 ]] && fatal "--dir requires an argument"
                INSTALL_DIR="$2"; shift 2
                ;;
            --insecure)
                INSECURE="true"; shift
                ;;
            -h|--help)
                setup_colors
                show_help
                exit 0
                ;;
            *)
                fatal "Unknown option: $arg. Use --help for usage."
                ;;
        esac
    done

    print_banner

    step "Detecting platform"
    detect_platform

    check_prerequisites
    detect_install_method

    case "$INSTALL_METHOD" in
        brew)   install_brew ;;
        go)     install_go ;;
        binary) install_binary ;;
        *)      fatal "Unexpected install method: ${INSTALL_METHOD}. Expected: brew, go, or binary" ;;
    esac

    verify_installation
    print_next_steps
}

main "$@"
