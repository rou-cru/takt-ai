# Containers (C4 L2)

Diagram: [containers.html](diagrams/containers.html)

Takt splits at runtime: the installer runs as a CLI, and the harness runs inside OpenCode through plugins that call back into the same binary.

| Container | Technology | Responsibility | Evidence |
| :--- | :--- | :--- | :--- |
| takt-ai binary | Go, Bubble Tea | Setup, doctor, TUI and the harness subcommands (`codegraph`, `dag`, `dispatch`, `gc`, `memory`, `obs`, `vfs`). Single entry point. Embeds the catalog as a component, not a separate unit. | `takt/cli/main.go`, `takt/catalog/packages.go` |
| OpenCode configuration | Files | Written by the binary, loaded by OpenCode: `opencode.json`, `AGENTS.md`, `plugins/` and the `takt/` package dir under `~/.config/opencode`, plus `.opencode/skills/`. | `takt/model/paths.go`, `takt/agents/opencode/layout.go`, `takt/skills/skills.go` |
| OpenCode plugins | TypeScript, Bun | VFS, DAG view and memory plugins; the VFS plugin loads the sandbox adapter (`takt-sandbox.mjs`). VFS and DAG spawn `takt-ai` subcommands. The memory plugin vets MCP calls and binds crew attribution through the service admin endpoint. | `takt/agents/opencode/assets/takt-vfs.ts`, `takt-memory.ts`, `takt-dag.tsx` |
| Local state | SQLite, files | Execution history, event store and VFS staging under the private `.takt-ai` state directory. | `takt/history/history.go`, `takt/obs/store.go` |
| Takt Memory service | Go, MCP over Unix socket | Independent `takt-ai memory serve` process; sole reader and writer of `memory.db`. `/crew` and `/external` expose the same six tools; `/admin` handles binding, readiness, backup and shutdown. Packaged with the installation; the interface is MCP, so it also runs as a separate process. | `takt/memory/serve.go`, `takt/memory/server.go` |
| Memory store | SQLite, FTS5, derived embeddings | `$HOME/.takt-ai/memory/memory.db`, relocated by `TAKT_MEMORY_DIR`. Only the service opens it; clients use MCP. | `takt/memory/store.go`, `takt/memory/schema.go` |
| Memory stdio adapters | Go | `takt-ai memory mcp --crew` relays crew requests unchanged. Without `--crew`, the adapter supplies an external session identity and optional workspace. | `takt/memory/adapter.go` |
| CodeGraph MCP server | Node package | Resolved from PATH or acquired under `.takt-ai/codegraph`; OpenCode runs its MCP command. | `takt/codegraph/acquire.go` |
| OpenCode v2 | External runtime | Hosts the crew; the binary writes its configuration files and calls its HTTP API through one adapter. | `takt/internal/opencodeapi/client.go` |
| Workspace image and Helm chart | Docker, Helm | Package opencode and takt-ai; the image build runs `takt-ai setup image` and exposes OpenCode on port 4096. | `deploy/images/Dockerfile` |

## Deployment variants

| Variant | How |
| :--- | :--- |
| Local | `install.sh`, Homebrew cask or `go install`. |
| Container | Workspace image from Docker Hub or GHCR (`workspace` target). |
| Cluster | Helm chart from GHCR, directly or through `deploy/workspace/helm.sh`. It runs the `workspace` image, configured at runtime by the settings Kubernetes injects (environment, secrets, repository). |
| Development | `make dev` runs the `compose.yml` dev service, built from the `dev` target of `deploy/images/Dockerfile`. |
| CI | Containerized targets: `make test-containerized` and the e2e container job. |
| Release | GoReleaser builds archives and the Homebrew cask; checksums, the image and the chart are signed with cosign. |

## Notes

- Plugins call into `takt-ai` subcommands for domain work. The VFS plugin itself holds the permission hooks and the orchestrator-only guards.
- Multi-bend routes in the diagram are layout choices, not extra hops.
- Policy files (`takt/dispatch/admission_policy.yaml`, `takt/gc/trigger_policy.yaml`) belong to the binary; [data and policy](../reference/data-and-policy.md) covers them.
