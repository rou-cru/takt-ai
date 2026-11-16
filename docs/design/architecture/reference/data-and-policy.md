# Data and policy

Tables only; no diagram. Stores and files are those named in the current code.

## Stores written by the harness

Two `.takt-ai` directories: `<workspace>/.takt-ai` holds per-workspace state; `$HOME/.takt-ai` (the setup root) holds per-user state.

| Store | Format | Owner | Notes | Evidence |
| :--- | :--- | :--- | :--- | :--- |
| `<workspace>/.takt-ai/events.db` | SQLite, tables `events` and `control_actions` | obs | One per workspace, shared by every invocation. | `takt/obs/store.go` |
| `<workspace>/.takt-ai/vfs/history.sqlite` | SQLite | history | Append-only execution history; lives in the private state directory beside the VFS store. | `takt/history/history.go`, `takt/cli/dag.go` |
| `<workspace>/.takt-ai/vfs/vfs.sqlite` | SQLite | vfs | Staged deltas, claims, journal and recovery state; workspace-level file lock. Private mode. | `takt/vfs/storage.go` |
| GC state in `<workspace>/.takt-ai/vfs/`: maintenance record in `vfs.sqlite`, plus `gc-preparation.json` and `gc-refutations.json` | SQLite maintenance field, JSON files | vfs for maintenance; gc for preparation and refutations | Cycle state is stored with the VFS bindings, not in a separate coordinator file; reconstructed projections are stripped on save. Preparation and refutations are files. | `takt/vfs/gc_state.go`, `takt/gc/coordinator.go`, `takt/gc/preparation.go`, `takt/gc/refutation.go` |
| `$HOME/.takt-ai/memory/memory.db` | SQLite | memory service | Canonical entries, relations, sessions, closes, crew bindings, FTS indexes and derived vectors. Only the service opens it. `TAKT_MEMORY_DIR` relocates the directory. | `takt/memory/store.go`, `takt/memory/schema.go`, `takt/memory/serve.go` |
| `$HOME/.takt-ai/memory/memory.sock` and `serve.lock` | Unix socket, lock file | memory service | Private local MCP transport and single-owner process lock; not a second memory store. | `takt/memory/serve.go` |
| `$HOME/.takt-ai/models/` | Verified model assets | memory/embed | Local semantic model; derived vectors can be rebuilt without replacing canonical entries. | `takt/memory/embed/manifest.go`, `takt/memory/embedding.go` |
| `$HOME/.takt-ai/codegraph/` | npm prefix | codegraph | Managed CodeGraph copy acquired by Takt. | `takt/codegraph/acquire.go` |
| `~/.config/opencode/plugins/node_modules/` | npm prefix | agents/opencode | Sandbox runtime dependency of the sandbox adapter. | `takt/agents/opencode/components.go` |

## Stores Takt reads but does not own

| Store | Owner | Notes | Evidence |
| :--- | :--- | :--- | :--- |
| `<workspace>/.takt/gc.json` | Workspace | Reviewed GC project configuration; preparation requires it. | `takt/gc/preparation.go` |

The TUI asks whether to retain memory before uninstall. Retention requests a consistent backup through the service before retirement; unresolved retention or deletion failures leave the installation recorded as incomplete (`takt/lifecycle/lifecycle.go`).

## Records written by setup

Setup records live at the `--root` directory (default `$HOME`).

| File | Purpose | Evidence |
| :--- | :--- | :--- |
| `.takt-manifest.json` | Ownership manifest: which files Takt installed, with hashes and backup paths. | `takt/setup/ownership.go` |
| `.takt-installed-config.json` | Record of the installed configuration. | `takt/setup/installed_config.go` |
| `.takt-operation.json` | In-flight operation record; a leftover signals an interrupted run. | `takt/setup/operation_record.go` |
| `.takt-accepted-risks.json` | Conflict acceptances matched by path, hash and impact. | `takt/setup/risk_acceptance.go` |
| `.takt-backups/` | Prior content of overwritten files, used by rollback and restore. | `takt/setup/operations.go` |

Records are replaced atomically with the contents synced first (`takt/setup/record.go`).

## Declarative policy and content

| Source | Governs | Evidence |
| :--- | :--- | :--- |
| `takt/dispatch/admission_policy.yaml` | Dispatch budgets: specialists, unplanned units, contests, recovery limits. | `takt/dispatch/` |
| `takt/gc/trigger_policy.yaml` | GC trigger: units and mutations per cycle, maximum deferrals, finding and file limits. | `takt/gc/` |
| `takt/catalog/capabilities.yaml` | Install manifest of capabilities; embedded and validated before any deployment is planned. | `takt/catalog/capabilities.go` |
| `takt/catalog/assets/` | Agents, skills and shared files; embedded. | `takt/catalog/packages.go` |

## Ownership rules

- Takt-managed files carry a hash in the manifest; a file whose hash differs is treated as user-edited and preserved on sync and uninstall.
- User-owned JSON and TOML are merged, not replaced (`takt/internal/filemerge`).
- Plugin files carry a managed-by-takt-ai header; edits are overwritten on sync.
