# syntax=docker/dockerfile:1
# Shared multi-stage build for takt-ai's Docker images:
#   - `workspace`: disposable OpenCode+takt-ai runtime for Kubernetes (and
#     plain `docker run`), onboarding baked in, published as roucru/takt-ai.
#   - `dev`: same toolchain, interactive shell instead of a server, meant to
#     `docker run -it -v $PWD:/workspace ...` and just start working;
#     published as roucru/takt-ai:dev.
# Both share the `toolchain` stage below, so a bake invocation that builds
# both targets together (`docker buildx bake workspace dev`) only pays for
# the OS/language toolchain once.
#
# The builder runs on the target platform on purpose: its Go toolchain is
# copied into later stages, so it must match the target architecture.
# Release CI builds each architecture on a native runner.

ARG GO_VERSION=1.27.1
FROM golang:${GO_VERSION}-bookworm AS builder
# Keep GO_VERSION in sync with the `go` directive in go.mod (CI builds with
# go-version-file: go.mod). Dev tool pins live in development/quality/go.mod.
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/takt-ai ./takt/cli
# Go quality/docs tools, built once here while network and toolchain exist;
# later stages only copy binaries (versions pinned by go.sum, can't float).
RUN GOBIN=/out/tools go install github.com/princjef/gomarkdoc/cmd/gomarkdoc
WORKDIR /src/development/quality
RUN GOBIN=/out/tools go install tool

FROM ubuntu:26.04 AS toolchain
# Shared OS/language toolchain for both final images below: Go (compiler +
# quality/docs tools), Python (uv + ruff + pytest + pdoc), Node (pnpm +
# vitest + eslint + prettier + typedoc). No OpenCode and no final user or
# entrypoint here — those are stage-specific, added by `workspace`/`dev`.
ARG PYTHON_VERSION=3.14
ARG NODE_MAJOR=24
ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    COLORTERM=truecolor \
    UV_TOOL_BIN_DIR=/usr/local/bin \
    UV_TOOL_DIR=/usr/local/share/uv/tools \
    UV_PYTHON_INSTALL_DIR=/usr/local/share/uv/python \
    PATH="/usr/local/go/bin:${PATH}"
# Pinned by digest so image builds are reproducible; bump deliberately.
COPY --from=ghcr.io/astral-sh/uv:0.12.20@sha256:100047e74f30778ab704942321a09750d6158739573ff58bf3924085cc6cd2d8 /uv /uvx /bin/
COPY --from=builder /usr/local/go /usr/local/go
COPY --from=builder /out/tools/ /usr/local/bin/
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      ca-certificates curl fd-find git gnupg jq less openssh-client ripgrep tar tzdata unzip; \
    # Register the NodeSource apt repo via NodeSource's own documented
    # manual steps (GPG-signed key + apt source) instead of piping their
    # setup script to bash: apt then verifies every nodejs package signature
    # against this key before installing anything, so no unverified remote
    # script ever runs as root (docker:S8482).
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
    # ruff covers lint+format; pytest+pytest-cov covers UT+coverage;
    # pdoc generates docs from docstrings.
    uv tool install ruff==0.16.9 --no-build; \
    uv tool install pytest==9.1.1 --with pytest-cov==7.1.0 --no-build; \
    uv tool install pdoc==16.0.0 --no-build; \
    # npm owns the globals so no PNPM_HOME perms to manage; pnpm stays for
    # projects. typedoc generates docs from TSDoc comments.
    npm install -g --no-fund --no-audit --ignore-scripts \
      pnpm@12.6.0 vitest@5.0.2 @vitest/coverage-v8@5.0.2 eslint@10.11.0 prettier@3.9.9 typedoc@0.28.20; \
    rm -rf /var/lib/apt/lists/* /root/.npm /root/.cache; \
    ln -s "$(command -v fdfind)" /usr/local/bin/fd; \
    python --version; \
    uv --version; \
    ruff --version; \
    pytest --version; \
    pdoc --version; \
    node --version; \
    pnpm --version; \
    vitest --version; \
    eslint --version; \
    prettier --version; \
    typedoc --version; \
    # gofmt/go vet/go test ship with Go; the rest are pinned above
    go version; \
    gocyclo -h 2>&1 | head -1; \
    deadcode -h 2>&1 | head -1; \
    gofumpt -h 2>&1 | head -1; \
    goimports -h 2>&1 | head -1; \
    golangci-lint version; \
    gomarkdoc -h 2>&1 | head -1
COPY --from=builder /out/takt-ai /usr/local/bin/takt-ai

# ---------------------------------------------------------------------------
# workspace: disposable Kubernetes/`docker run` runtime, OpenCode served as a
# daemon, onboarding (config/plugins/skills/MCP deps) baked in at build time.
# ---------------------------------------------------------------------------
FROM toolchain AS workspace
LABEL org.opencontainers.image.source="https://github.com/rou-cru/takt-ai" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.vendor="Takt AI Contributors" \
      org.opencontainers.image.title="takt-ai" \
      org.opencontainers.image.description="Prepared Takt AI + OpenCode v2 workspace runtime"
# Pinned to OpenCode 2.x on purpose; never "latest".
ARG OPENCODE_VERSION=2.0.20
# SHA-256 of https://opencode.ai/v2/install. The installer endpoint is shared
# across all CLI versions and carries no vendor-published checksum of its own,
# so this pin substitutes one and the build verifies it before ever executing
# the script (docker:S8482). When OpenCode legitimately updates the installer,
# this check fails closed: re-download, review the diff, refresh the pin.
ARG OPENCODE_INSTALL_SHA256=bc70dd317fd12ef09350cdb1667e55e19f9e7b3d2897d3a1b750b165ae579d87
ENV PATH="/usr/local/go/bin:/home/takt/.opencode/bin:/home/takt/.local/bin:${PATH}"
RUN useradd --create-home --uid 10001 --shell /bin/bash takt; \
    install -d -o takt -g takt \
      /workspace \
      /home/takt/.local/share /home/takt/.local/state \
      /home/takt/.cache /home/takt/.config /home/takt/.takt-ai
COPY --chmod=0755 deploy/images/workspace-entrypoint.sh /usr/local/bin/workspace-entrypoint
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

# ---------------------------------------------------------------------------
# dev: general-purpose interactive image. Same onboarding as `workspace`, but
# an interactive shell instead of a server — `docker run -it
# -v $PWD:/workspace roucru/takt-ai:dev`, log in or set a provider key, and
# work. Published builds leave DEV_UID/DEV_GID at their default (1000); a
# local `docker buildx bake dev` (e.g. via compose, for takt-ai contributors)
# can still override them at build time to match the host uid exactly. A
# pulled image can instead match a host uid at run time with `docker run --user $(id -u):$(id -g)`.
# ---------------------------------------------------------------------------
FROM toolchain AS dev
LABEL org.opencontainers.image.source="https://github.com/rou-cru/takt-ai" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.vendor="Takt AI Contributors" \
      org.opencontainers.image.title="takt-ai (dev)" \
      org.opencontainers.image.description="Takt AI + OpenCode v2, ready for interactive Go/TS/Python work"
ARG OPENCODE_VERSION=2.0.20
ARG OPENCODE_INSTALL_SHA256=bc70dd317fd12ef09350cdb1667e55e19f9e7b3d2897d3a1b750b165ae579d87
ARG DEV_UID=1000
ARG DEV_GID=1000
ENV PATH="/usr/local/go/bin:/home/dev/.opencode/bin:/home/dev/.local/bin:${PATH}"
# ubuntu:26.04 ships its own uid/gid 1000 `ubuntu` user. A duplicate (-o) uid
# leaves `getpwuid(1000)` ambiguous: $HOME resolves to whichever account
# comes first in /etc/passwd, which is `ubuntu`, not `dev` — silently
# breaking every ~-relative path (including where the OpenCode installer
# below writes its binary). Free the uid/gid first instead of duplicating it.
RUN if getent passwd "${DEV_UID}" >/dev/null; then \
      userdel -r "$(getent passwd "${DEV_UID}" | cut -d: -f1)" 2>/dev/null || true; \
    fi; \
    if getent group "${DEV_GID}" >/dev/null; then \
      groupdel "$(getent group "${DEV_GID}" | cut -d: -f1)" 2>/dev/null || true; \
    fi; \
    groupadd -g "${DEV_GID}" dev; \
    useradd -m -s /bin/bash -u "${DEV_UID}" -g "${DEV_GID}" dev; \
    install -d -o dev -g dev /workspace
USER dev
WORKDIR /home/dev

RUN set -eux; \
    case "${OPENCODE_VERSION}" in \
      2.[0-9]*.[0-9]*) ;; \
      *) echo "OPENCODE_VERSION must be 2.x.y, got '${OPENCODE_VERSION}'" >&2; exit 1 ;; \
    esac; \
    curl -fsSL --proto '=https' --tlsv1.2 -o /tmp/opencode-install.sh https://opencode.ai/v2/install; \
    echo "${OPENCODE_INSTALL_SHA256}  /tmp/opencode-install.sh" | sha256sum -c -; \
    bash /tmp/opencode-install.sh --no-modify-path --version "${OPENCODE_VERSION}"; \
    rm -f /tmp/opencode-install.sh; \
    opencode --version; \
    takt-ai version; \
    takt-ai setup default-request | takt-ai setup image --root /home/dev --yes --json; \
    install -d -o dev -g dev /home/dev/.config/opencode/skills; \
    cp -a /home/dev/.config/opencode/takt/skills/. /home/dev/.config/opencode/skills/

WORKDIR /workspace
ENTRYPOINT ["bash"]
