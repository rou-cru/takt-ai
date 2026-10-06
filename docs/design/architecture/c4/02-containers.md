# Containers (C4 L2)

Diagram: [containers.html](diagrams/containers.html)

Takt splits at runtime: the installer runs as a CLI, and the harness runs inside OpenCode through plugins that call back into the same binary.

| Container | Technology | Responsibility | Evidence |
| :--- | :--- | :--- | :--- |
| takt-ai binary | Go, Bubble Tea | Setup, doctor, TUI and the harness subcommands (`codegraph`, `dag`, `dispatch`, `gc`, `memory`, `obs`, `vfs`). Single entry point. Embeds the catalog (12 agents, 32 skills) as a component, not a separate unit. | `takt/cli/main.go`, `takt/catalog/packages.go` |
| OpenCode configuration | Files | Written by the binary, loaded by OpenCode: `opencode.json`, `AGENTS.md`, `plugins/` and the `takt/` package dir under `~/.config/opencode`, plus `.opencode/skills/`. | `takt/model/paths.go`, `takt/agents/opencode/layout.go`, `takt/skills/skills.go` |
| OpenCode plugins | TypeScript, Bun | VFS, DAG view and memory plugins; the VFS plugin loads the sandbox adapter (`takt-sandbox.mjs`). They spawn `takt-ai` subcommands. | `takt/agents/opencode/assets/takt-vfs.ts`, `takt-memory.ts`, `takt-dag.tsx` |
| Local state | SQLite, files | Execution history, event store and VFS staging under the private `.takt-ai` state directory. | `takt/history/history.go`, `takt/obs/store.go` |
| Managed MCP servers | Engram binary, CodeGraph npm package | Acquired by Takt into `.takt-ai`, run by OpenCode. The binary also calls Engram over HTTP. | `takt/engram/acquire.go`, `takt/codegraph/acquire.go`, `takt/memory/client.go` |
| OpenCode v2 | External runtime | Hosts the crew; the binary writes its configuration files and calls its HTTP API through one adapter. | `takt/internal/opencodeapi/client.go` |
| Workspace image and Helm chart | Docker, Helm | Package opencode and takt-ai; the image build runs `takt-ai setup image` and exposes OpenCode on port 4096. | `deploy/images/Dockerfile` |

## Deployment variants

| Variant | How |
| :--- | :--- |
| Local | `install.sh`, Homebrew cask or `go install`. |
| Container | Workspace image from Docker Hub or GHCR (`workspace` target). |
| Cluster | Helm chart from GHCR, directly or through `deploy/workspace/helm.sh`. |
| Development | `make dev` runs the `compose.yml` dev service, built from the `dev` target of `deploy/images/Dockerfile`. |
| CI | Containerized targets: `make test-containerized` and the e2e container job. |
| Release | GoReleaser builds archives and the Homebrew cask; checksums, the image and the chart are signed with cosign. |

## Notes

- Plugins call into `takt-ai` subcommands for domain work. The VFS plugin itself holds the permission hooks, the GC lane tool sets and the orchestrator-only guards.
- Multi-bend routes in the diagram are layout choices, not extra hops.
- Policy files (`takt/dispatch/admission_policy.yaml`, `takt/gc/trigger_policy.yaml`) belong to the binary; [data and policy](../reference/data-and-policy.md) covers them.
