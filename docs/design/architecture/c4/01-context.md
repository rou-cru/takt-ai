# System context (C4 L1)

Diagram: [context.html](diagrams/context.html)

Takt AI sits between the developer's workspace and OpenCode v2. It installs and configures its capabilities into OpenCode, which then runs the crew.

| Element | Role | Evidence |
| :--- | :--- | :--- |
| Developer | Runs setup and doctor through Takt, then works with the crew in OpenCode. | `takt/cli/main.go` |
| Takt AI | Installer CLI/TUI plus the meta-harness it installs. Single binary. | `takt/cli/main.go` |
| OpenCode v2 | Host agent runtime. Takt reaches it through one HTTP adapter. | `takt/internal/opencodeapi/client.go` |
| LLM providers | Reached by OpenCode only; Takt reads provider and model references from OpenCode. | `takt/internal/opencodeapi/wire.go` |
| GitHub Releases | Source of the `takt-ai` release archives (`install.sh`) and of the Engram binary that Takt downloads. | `install.sh`, `takt/engram/acquire.go` |
| Homebrew tap | Ships the `takt-ai` cask. | `.goreleaser.yaml` |
| GHCR / Docker Hub | Ship the workspace image and, on GHCR, the Helm chart. | `.github/workflows/release.yml` |
| npm registry | Source of the CodeGraph package and the sandbox runtime (`@anthropic-ai/sandbox-runtime`) that Takt installs. | `takt/codegraph/acquire.go`, `takt/agents/opencode/components.go` |
| MCP servers | Engram and CodeGraph are always installed and run by OpenCode. Takt also calls Engram over HTTP for memory writes and doctor checks. | `takt/verify/config.go`, `takt/memory/client.go`, `takt/doctor/doctor.go` |
| Context7 | Optional remote MCP endpoint, reached by OpenCode; nothing runs locally. | `takt/agents/shared/types.go` |

## Notes

- Takt does not call LLM providers itself.
- Release pipeline and deployment variants are detailed in [containers](02-containers.md).
