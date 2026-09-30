# Dedicated test runtime: Node executes the rendered OpenCode TypeScript plugin.
FROM node:24-bookworm-slim AS node
# Real codegraph for the E2E-C tests (dead-code findings over fixture workspaces).
# 1.6.1 is the release the gc adapter's JSON surface was verified against; the
# product itself resolves any release at or above takt/codegraph.CodegraphVersion.
ARG CODEGRAPH_VERSION=1.6.1
RUN npm install -g --prefix /opt/codegraph --no-fund --no-audit --loglevel=error --ignore-scripts \
      "@colbymchenry/codegraph@${CODEGRAPH_VERSION}"

FROM golang:1.27.1
# deadcode's version is pinned by the module's own `tool` directive
# (go.mod) and its checksum by go.sum — no separate ARG to drift from it.
WORKDIR /tmp/takt-module
COPY go.mod go.sum ./
RUN go mod download && \
    GOBIN=/opt/tools go install golang.org/x/tools/cmd/deadcode
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /opt/codegraph /opt/codegraph
ENV PATH="/opt/codegraph/bin:/opt/tools:$PATH" TAKT_E2E_CODEGRAPH=/opt/codegraph/bin/codegraph TAKT_E2E_DEADCODE=/opt/tools/deadcode CODEGRAPH_TELEMETRY=0
# development/testing/test-containerized.sh always runs this image as `runner`
# account below (no --user override), so ownership can be scoped to it
# instead of opened up to everyone.
RUN go version && node --version && codegraph --version && \
    useradd -m -s /bin/bash runner && \
    mkdir -p /src /go && chown -R runner:runner /src /go /opt/tools /opt/codegraph
USER runner
