# syntax=docker/dockerfile:1
# ARM64 runtime image for a disposable, ready-to-use Takt/OpenCode workspace.
# This deliberately has its own build definition; Dockerfile.dev remains the
# unconfigured development example.

ARG GO_VERSION=1.25.0
FROM golang:${GO_VERSION}-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/takt-ai ./takt/cli

FROM ubuntu:26.04
ARG OPENCODE_VERSION=2.0.16
# SHA-256 of https://opencode.ai/v2/install. The installer endpoint is shared
# across all CLI versions and carries no vendor-published checksum of its own,
# so this pin substitutes one and the build verifies it before ever executing
# the script (docker:S8482). When OpenCode legitimately updates the installer,
# this check fails closed: re-download, review the diff, refresh the pin.
ARG OPENCODE_INSTALL_SHA256=bc70dd317fd12ef09350cdb1667e55e19f9e7b3d2897d3a1b750b165ae579d87
ARG PYTHON_VERSION=3.14
ARG NODE_MAJOR=24
ARG GO_VERSION=1.25.0

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    COLORTERM=truecolor \
    UV_TOOL_BIN_DIR=/usr/local/bin \
    UV_PYTHON_INSTALL_DIR=/usr/local/share/uv/python \
    PATH="/usr/local/go/bin:/home/takt/.opencode/bin:/home/takt/.local/bin:${PATH}"

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/
COPY --from=builder /usr/local/go /usr/local/go

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      ca-certificates curl fd-find git gnupg jq less openssh-client ripgrep tar tzdata unzip; \
    # Register the NodeSource apt repo via NodeSource's own
    # documented manual steps (GPG-signed key + apt source) instead of piping
    # their setup script to bash: apt then verifies every nodejs package
    # signature against this key before installing anything, so no
    # unverified remote script ever runs as root (docker:S8482).
    mkdir -p /usr/share/keyrings; \
    curl -fsSL --proto '=https' --tlsv1.2 https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
      | gpg --dearmor -o /usr/share/keyrings/nodesource.gpg; \
    chmod 644 /usr/share/keyrings/nodesource.gpg; \
    printf '%s\n' \
      'Types: deb' \
      "URIs: https://deb.nodesource.com/node_${NODE_MAJOR}.x" \
      'Suites: nodistro' \
      'Components: main' \
      "Architectures: $(dpkg --print-architecture)" \
      'Signed-By: /usr/share/keyrings/nodesource.gpg' \
      > /etc/apt/sources.list.d/nodesource.sources; \
    apt-get update; \
    apt-get install -y --no-install-recommends nodejs; \
    uv python install "${PYTHON_VERSION}"; \
    py="$(uv python find "${PYTHON_VERSION}")"; \
    ln -sf "${py}" /usr/local/bin/python"${PYTHON_VERSION}"; \
    ln -sf "${py}" /usr/local/bin/python3; \
    ln -sf "${py}" /usr/local/bin/python; \
    uv tool install ruff==0.16.9 --no-build; \
    uv tool install pytest==9.1.1 --with pytest-cov==7.1.0 --no-build; \
    npm install -g --no-fund --no-audit --ignore-scripts \
      pnpm@12.6.0 vitest@5.0.2 @vitest/coverage-v8@5.0.2 eslint@10.11.0 prettier@3.9.9; \
    rm -rf /var/lib/apt/lists/* /root/.npm /root/.cache; \
    node --version; \
    npm --version; \
    uv --version; \
    python --version; \
    ruff --version; \
    pytest --version; \
    useradd --create-home --uid 10001 --shell /bin/bash takt; \
    ln -s "$(command -v fdfind)" /usr/local/bin/fd; \
    install -d -o takt -g takt \
      /workspace \
      /home/takt/.local/share /home/takt/.local/state \
      /home/takt/.cache /home/takt/.config /home/takt/.takt-ai

COPY --from=builder /out/takt-ai /usr/local/bin/takt-ai
COPY --chmod=0755 deploy/workspace/entrypoint.sh /usr/local/bin/workspace-entrypoint
USER takt
WORKDIR /home/takt

# Install one explicitly pinned OpenCode v2 version at build time (no runtime
# installer/download is required when a workspace Pod starts), then bake the
# managed OpenCode config, plugins, skills, and MCP dependencies into the
# image directly (image mode uses the normal installer without requiring a
# running OpenCode server during the build). Takt keeps its managed skills
# grouped below `takt/skills` to avoid mixing ownership with user config;
# OpenCode scans the config-level `skills/` directory, so mirror those
# packages into its discovery location.
RUN set -eux; \
    case "${OPENCODE_VERSION}" in \
      2.[0-9]*.[0-9]*) ;; \
      *) echo "OPENCODE_VERSION must be 2.x.y, got '${OPENCODE_VERSION}'" >&2; exit 1 ;; \
    esac; \
    # Download the installer to a file and verify its checksum before ever
    # executing it, instead of piping curl straight into bash (docker:S8482).
    curl -fsSL --proto '=https' --tlsv1.2 -o /tmp/opencode-install.sh https://opencode.ai/v2/install; \
    echo "${OPENCODE_INSTALL_SHA256}  /tmp/opencode-install.sh" | sha256sum -c -; \
    bash /tmp/opencode-install.sh --no-modify-path --version "${OPENCODE_VERSION}"; \
    rm -f /tmp/opencode-install.sh; \
    opencode --version; \
    takt-ai version; \
    takt-ai setup default-request | takt-ai setup image --root /home/takt --yes --json; \
    install -d -o takt -g takt /home/takt/.config/opencode/skills; \
    cp -a /home/takt/.config/opencode/takt/skills/. /home/takt/.config/opencode/skills/

WORKDIR /workspace
EXPOSE 4096
ENTRYPOINT ["workspace-entrypoint"]
