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
    printf '%b\n' '\033[0m        \033[0;38;2;40;45;85m▄▄\033[0;38;2;40;45;85;48;2;14;14;16m▀▀▀▀▀▀▀▀\033[0;38;2;40;45;85m▄▄\033[0m        \033[0m'
    printf '%b\n' '\033[0m     \033[0;38;2;40;45;85m▄\033[0;38;2;40;45;85;48;2;14;14;16m▀\033[0;38;2;14;14;16m█\033[0;38;2;14;14;16;48;2;53;66;118m▀▀\033[0;38;2;53;66;118m█\033[0;38;2;53;66;118;48;2;14;14;16m▀▀▀▀▀▀\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16;48;2;53;66;118m▀▀\033[0;38;2;14;14;16m█\033[0;38;2;40;45;85;48;2;14;14;16m▀\033[0;38;2;40;45;85m▄\033[0m     \033[0m'
    printf '%b\n' '\033[0m   \033[0;38;2;40;45;85m▄\033[0;38;2;40;45;85;48;2;14;14;16m▀\033[0;38;2;14;14;16;48;2;53;66;118m▀\033[0;38;2;53;66;118m█\033[0;38;2;53;66;118;48;2;14;14;16m▀▀\033[0;38;2;14;14;16m██████████\033[0;38;2;53;66;118;48;2;14;14;16m▀▀\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16;48;2;53;66;118m▀\033[0;38;2;40;45;85;48;2;14;14;16m▀\033[0;38;2;40;45;85m▄\033[0m   \033[0m'
    printf '%b\n' '\033[0m  \033[0;38;2;40;45;85;48;2;14;14;16m▀\033[0;38;2;14;14;16m█\033[0;38;2;53;66;118m█\033[0;38;2;53;66;118;48;2;14;14;16m▀\033[0;38;2;14;14;16m████\033[0;38;2;14;14;16;48;2;243;239;238m▀▀▀▀▀▀▀\033[0;38;2;14;14;16m█████\033[0;38;2;53;66;118;48;2;14;14;16m▀\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m█\033[0;38;2;40;45;85;48;2;14;14;16m▀\033[0m  \033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;40;45;85m█\033[0;38;2;14;14;16m█\033[0;38;2;53;66;118m█\033[0;38;2;53;66;118;48;2;14;14;16m▀\033[0;38;2;14;14;16m█████\033[0;38;2;14;14;16;48;2;243;239;238m▀\033[0;38;2;14;14;16m█\033[0;38;2;14;14;16;48;2;243;239;238m▀\033[0;38;2;243;239;238m███\033[0;38;2;243;239;238;48;2;14;14;16m▀▀\033[0;38;2;243;239;238m█\033[0;38;2;14;14;16;48;2;243;239;238m▀\033[0;38;2;14;14;16m███\033[0;38;2;53;66;118;48;2;14;14;16m▀\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m█\033[0;38;2;40;45;85m█\033[0m \033[0m'
    printf '%b\n' '\033[0;38;2;40;45;85m█\033[0;38;2;14;14;16m█\033[0;38;2;53;66;118m█\033[0;38;2;53;66;118;48;2;14;14;16m▀\033[0;38;2;14;14;16m█████\033[0;38;2;243;239;238m███████\033[0;38;2;14;14;16;48;2;243;239;238m▀▀\033[0;38;2;243;239;238m███\033[0;38;2;14;14;16m███\033[0;38;2;53;66;118;48;2;14;14;16m▀\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m█\033[0;38;2;40;45;85m█\033[0m'
    printf '%b\n' '\033[0;38;2;40;45;85m█\033[0;38;2;14;14;16m█\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m█████\033[0;38;2;243;239;238m████████████\033[0;38;2;243;239;238;48;2;14;14;16m▀\033[0;38;2;14;14;16m████\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m█\033[0;38;2;40;45;85m█\033[0m'
    printf '%b\n' '\033[0;38;2;40;45;85m█\033[0;38;2;14;14;16m█\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m████\033[0;38;2;243;239;238m████████\033[0;38;2;243;239;238;48;2;14;14;16m▀\033[0;38;2;14;14;16m█████████\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m█\033[0;38;2;40;45;85m█\033[0m'
    printf '%b\n' '\033[0;38;2;40;45;85m█\033[0;38;2;14;14;16m█\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16;48;2;53;66;118m▀\033[0;38;2;14;14;16m███\033[0;38;2;243;239;238m██████\033[0;38;2;243;239;238;48;2;14;14;16m▀\033[0;38;2;14;14;16m█\033[0;38;2;184;196;208m█\033[0;38;2;14;14;16m████████\033[0;38;2;14;14;16;48;2;53;66;118m▀\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m█\033[0;38;2;40;45;85m█\033[0m'
    printf '%b\n' '\033[0m \033[0;38;2;40;45;85m█\033[0;38;2;14;14;16m█\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16;48;2;53;66;118m▀\033[0;38;2;14;14;16m██\033[0;38;2;243;239;238m██████\033[0;38;2;14;14;16m█\033[0;38;2;184;196;208m██\033[0;38;2;14;14;16m███████\033[0;38;2;14;14;16;48;2;53;66;118m▀\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m█\033[0;38;2;40;45;85m█\033[0m \033[0m'
    printf '%b\n' '\033[0m  \033[0;38;2;14;14;16;48;2;40;45;85m▀\033[0;38;2;14;14;16m█\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16;48;2;53;66;118m▀\033[0;38;2;14;14;16m█\033[0;38;2;243;239;238m██████\033[0;38;2;14;14;16;48;2;243;239;238m▀\033[0;38;2;184;196;208m██\033[0;38;2;14;14;16m██████\033[0;38;2;14;14;16;48;2;53;66;118m▀\033[0;38;2;53;66;118m█\033[0;38;2;14;14;16m█\033[0;38;2;14;14;16;48;2;40;45;85m▀\033[0m  \033[0m'
    printf '%b\n' '\033[0m   \033[0;38;2;40;45;85m▀\033[0;38;2;14;14;16;48;2;40;45;85m▀\033[0;38;2;53;66;118;48;2;14;14;16m▀\033[0;38;2;53;66;118m██\033[0;38;2;243;239;238;48;2;53;66;118m▀\033[0;38;2;243;239;238m██████\033[0;38;2;184;196;208m██\033[0;38;2;14;14;16;48;2;184;196;208m▀\033[0;38;2;14;14;16m█\033[0;38;2;14;14;16;48;2;53;66;118m▀▀\033[0;38;2;53;66;118m█\033[0;38;2;53;66;118;48;2;14;14;16m▀\033[0;38;2;14;14;16;48;2;40;45;85m▀\033[0;38;2;40;45;85m▀\033[0m   \033[0m'
    printf '%b\n' '\033[0m     \033[0;38;2;40;45;85m▀\033[0;38;2;14;14;16;48;2;40;45;85m▀\033[0;38;2;14;14;16m█\033[0;38;2;53;66;118;48;2;14;14;16m▀▀\033[0;38;2;53;66;118m██\033[0;38;2;243;239;238;48;2;53;66;118m▀▀▀▀\033[0;38;2;53;66;118m██\033[0;38;2;53;66;118;48;2;14;14;16m▀▀\033[0;38;2;14;14;16m█\033[0;38;2;14;14;16;48;2;40;45;85m▀\033[0;38;2;40;45;85m▀\033[0m     \033[0m'
    printf '%b\n' '\033[0m        \033[0;38;2;40;45;85m▀▀\033[0;38;2;14;14;16;48;2;40;45;85m▀▀▀▀▀▀▀▀\033[0;38;2;40;45;85m▀▀\033[0m        \033[0m'
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
