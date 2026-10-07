# syntax=docker/dockerfile:1
FROM golang:1.27.1-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /usr/local/bin/takt-ai ./takt/cli

# Node stays in the runtime: `takt-ai setup install` provisions CodeGraph via npm.
FROM node:25-bookworm-slim
# Takt requires the OpenCode v2 API (`opencode api`), which is not published to
# npm; install the same pinned release, through the same checksum-verified
# installer, as deploy/images/Dockerfile. Keep both pins in sync.
ARG OPENCODE_VERSION=2.0.20
ARG OPENCODE_INSTALL_SHA256=bc70dd317fd12ef09350cdb1667e55e19f9e7b3d2897d3a1b750b165ae579d87
RUN apt-get update && apt-get install -y --no-install-recommends bash ca-certificates curl jq && \
    rm -rf /var/lib/apt/lists/* && \
    useradd -m -s /bin/bash runner
COPY --from=builder /usr/local/bin/takt-ai /usr/local/bin/takt-ai
COPY development/testing/e2e/setup_test.sh /usr/local/bin/setup-test
USER runner
WORKDIR /home/runner
ENV PATH="/home/runner/.opencode/bin:${PATH}"
RUN set -eux; \
    case "${OPENCODE_VERSION}" in \
      2.[0-9]*.[0-9]*) ;; \
      *) echo "OPENCODE_VERSION must be 2.x.y, got '${OPENCODE_VERSION}'" >&2; exit 1 ;; \
    esac; \
    curl -fsSL --proto '=https' --tlsv1.2 -o /tmp/opencode-install.sh https://opencode.ai/v2/install; \
    echo "${OPENCODE_INSTALL_SHA256}  /tmp/opencode-install.sh" | sha256sum -c -; \
    bash /tmp/opencode-install.sh --no-modify-path --version "${OPENCODE_VERSION}"; \
    rm -f /tmp/opencode-install.sh; \
    opencode --version
ENTRYPOINT ["/bin/bash", "/usr/local/bin/setup-test"]
