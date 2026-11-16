# System context (C4 L1)

Diagram: [context.html](diagrams/context.html)

Takt AI sits between the developer's workspace and OpenCode v2. It installs and configures its capabilities into OpenCode, which then runs the crew.

| Element | Role | Evidence |
| :--- | :--- | :--- |
| Developer | Runs setup and doctor through Takt, then works with the crew in OpenCode. | `takt/cli/main.go` |
| Takt AI | Installer CLI/TUI plus the meta-harness it installs. Single binary. | `takt/cli/main.go` |
| OpenCode v2 | Host agent runtime. Takt reaches it through one HTTP adapter. | `takt/internal/opencodeapi/client.go` |
| LLM providers | Reached by OpenCode only; Takt reads provider and model references from OpenCode. | `takt/internal/opencodeapi/wire.go` |
| GitHub Releases | Source of the `takt-ai` release archives. | `install.sh` |
| Homebrew tap | Ships the `takt-ai` cask. | `.goreleaser.yaml` |
| GHCR / Docker Hub | Ship the workspace image and, on GHCR, the Helm chart. | `.github/workflows/release.yml` |
| npm registry | Source of the CodeGraph package and the sandbox runtime (`@anthropic-ai/sandbox-runtime`) that Takt installs. | `takt/codegraph/acquire.go`, `takt/agents/opencode/components.go` |
| Takt Memory | Independent `takt-ai memory serve` process; sole owner of the memory store. Crew and external clients reach the same tools through MCP over a private Unix socket. | `takt/memory/serve.go`, `takt/memory/server.go`, `takt/memory/adapter.go` |
| CodeGraph | MCP code index configured in OpenCode; Takt acquires the package when needed. | `takt/codegraph/acquire.go`, `takt/verify/config.go` |
| External MCP clients | Use `takt-ai memory mcp [--workspace <dir>]` without OpenCode; the adapter supplies an external session identity. | `takt/memory/adapter.go` |
| Context7 | Optional remote MCP endpoint, reached by OpenCode; nothing runs locally. | `takt/agents/shared/types.go` |

## Notes

- Takt does not call LLM providers itself.
- Release pipeline and deployment variants are detailed in [containers](02-containers.md).
