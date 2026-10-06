# Components (C4 L3)

One diagram per container area of the `takt-ai` binary. Each section lists its components, the evidence for them and the import table the edges come from.

## CLI and setup

Diagram: [components-cli-setup.html](diagrams/components-cli-setup.html)

Scope: the installer side of the `takt-ai` binary. Harness packages (vfs, dispatch, history, gc, obs, memory, session) are in 03b, the TUI in 03c, and the catalog and OpenCode renderer in 03d.

| Component | Responsibility | Evidence |
| :--- | :--- | :--- |
| cli | Routes the first argument to a subcommand or to setup; opens the TUI when run interactively. | `takt/cli/main.go` |
| lifecycle | Orchestrates install, sync and uninstall across targets. | `takt/lifecycle/lifecycle.go` |
| setup | Plans, authorizes and applies changes; owns ownership records, drift, risk acceptance and restore. | `takt/setup/deploy.go` |
| filemerge | Merges Takt-managed content into user-owned JSON and TOML. | `takt/internal/filemerge/read.go` |
| engram | Downloads, injects and verifies the Engram binary. | `takt/engram/acquire.go` |
| codegraph | Installs CodeGraph through npm and wires its MCP entry. | `takt/codegraph/acquire.go` |
| skills | Deploys catalog skills. | `takt/skills/skills.go` |
| agents/opencode | Renders the OpenCode projection (detail under Catalog and OpenCode adapter). | `takt/setup/deploy.go` |
| doctor | Health check of tools, configuration and the OpenCode connection. | `takt/doctor/doctor.go` |
| verify | Collects functional evidence without invoking agent tools or authentication. | `takt/verify/verify.go` |
| opencodeapi | Single adapter to the OpenCode v2 HTTP API. | `takt/internal/opencodeapi/client.go` |

### Import table

The diagram shows principal dependencies. The full set, taken from the packages' imports at the pinned revision:

| Package | Imports (within Takt) |
| :--- | :--- |
| cli | catalog, codegraph, dispatch, doctor, engram, gc, history, lifecycle, memory, model, obs, session, setup, tui, tui/runtime, verify, vfs |
| lifecycle | agents/opencode, catalog, codegraph, engram, model, setup, skills |
| setup | agents/opencode, agents/shared, catalog, dispatch, engram, internal/artifacts, internal/filemerge, model |
| skills | catalog, dispatch, setup |
| engram | agents/shared, catalog, internal/filemerge, model |
| codegraph | agents/shared, internal/filemerge, model |
| doctor | agents/opencode, codegraph, engram, internal/opencodeapi, memory, model, setup |
| verify | agents/shared, internal/opencodeapi, model, setup, skills |
| filemerge | model |

### Notes

- Command routing: `help`, `version`, `doctor`, `restore`, `vfs`, `obs`, `gc`, `dispatch`, `dag`, `memory` and `codegraph` are handled directly; anything else public goes to setup.
- `setup default-request` is handled before the general dispatch.

## Harness

Diagram: [components-harness.html](diagrams/components-harness.html)

Scope: the harness packages inside the `takt-ai` binary, plus the VFS plugin that reaches them from OpenCode. The cli also calls them as subcommands (see [containers](02-containers.md)) and is not drawn.

| Component | Responsibility | Evidence |
| :--- | :--- | :--- |
| dispatch | Generic admission and lifecycle protocol for crew work. | `takt/dispatch/dispatch.go` |
| vfs | Transactional execution layer for agent file operations. | `takt/vfs/vfs.go` |
| history | Append-only execution history, stored in SQLite in the private state directory. | `takt/history/history.go` |
| memory | Writes agent memories into Engram with harness metadata, links and session anchors. | `takt/memory/memory.go` |
| gc | Computes and coordinates the workspace GC cycle. | `takt/gc/trigger.go` |
| gc analysis | Preparation (`gc_prepare`), the manual verbs (`takt-ai gc plan`, `findings`, `refute`, `acceptance`), the dead-code and complexity analyzers, and the CodeGraph dependents closure. | `takt/gc/preparation.go`, `takt/gc/analyzers.go`, `takt/gc/deadcode.go`, `takt/gc/codegraph.go`, `takt/cli/gc.go` |
| VFS plugin | Tool surface inside OpenCode: the `vfs_*` and `gc_*` tools, permission hooks, GC lane tool sets and orchestrator-only guards. Spawns `takt-ai vfs` and `takt-ai gc coordinate`, and runs `takt-ai codegraph ensure-index` at load. | `takt/agents/opencode/assets/takt-vfs.ts` |
| CodeGraph | External code index, kept fresh by the plugin and queried by the GC closure. | `takt/cli/codegraph.go` |
| obs | One local event stream and control record, no network or memory coupling. | `takt/obs/obs.go` |
| session | Composition root of one crew execution session. | `takt/session/session.go` |

### Import table

Taken from the packages' imports at the pinned revision.

| Package | Imports (within Takt) |
| :--- | :--- |
| dispatch | catalog, history, memory, model, vfs |
| vfs | catalog, model, obs |
| gc | dispatch, history, internal/filemerge, obs, vfs |
| memory | agents/shared, catalog, history, internal/filemerge, model |
| session | obs, vfs |
| history | none |
| obs | none |

### Notes

- Not drawn for legibility: gc to vfs, and the catalog, model and filemerge edges.
- gc analysis lives in the `gc` package; it is drawn apart to show the analysis pipeline.
- history and obs are leaf packages; everything else depends toward them.
- Runtime behavior of these packages is in [runtime](../runtime/README.md).

## TUI

Diagram: [components-tui.html](diagrams/components-tui.html)

Scope: the interactive surface of the `takt-ai` binary (Bubble Tea v2). The cli opens it when run in a terminal (see CLI and setup). Screen contracts and state machines are normative in `product/app/tui/PRD_TUI.md` and `VDS_TUI.md`.

| Component | Responsibility | Evidence |
| :--- | :--- | :--- |
| tui | Composes the interactive lifecycle flows. | `takt/tui/tui.go` |
| install | Focused install selection flow. | `takt/tui/install/install.go` |
| uninstall | Target-scoped managed-file uninstall screen. | `takt/tui/uninstall/uninstall.go` |
| drift | Shows which managed files differ from their installed state. | `takt/tui/drift/drift.go` |
| models | Lets the user reassign specialists' models after install. | `takt/tui/models/models.go` |
| modelpicker | Edits one harness's specialist model drafts. | `takt/tui/modelpicker/model_picker.go` |
| diagnostics | Presents functional checks without reinstalling. | `takt/tui/diagnostics/diagnostics.go` |
| runtime | Bubble Tea action boundaries for lifecycle operations. | `takt/tui/runtime/runtime.go` |
| ui | Reusable terminal UI primitives. | `takt/tui/ui/shell.go` |
| keys | Global keyboard grammar. | `takt/tui/keys/keys.go` |
| theme | Single source of visual truth for renderers. | `takt/tui/theme/theme.go` |
| styles | Renders the brand logo from generated data. | `takt/tui/styles/logo.go` |

### Import table

| Package | Imports (within Takt) |
| :--- | :--- |
| tui | setup, tui/diagnostics, tui/drift, tui/install, tui/models, tui/runtime, tui/styles, tui/theme, tui/ui, tui/uninstall |
| install | catalog, model, setup, tui/keys, tui/runtime, tui/theme, tui/ui |
| uninstall | lifecycle, setup, tui/keys, tui/runtime, tui/theme, tui/ui |
| drift | setup, tui/keys, tui/runtime, tui/theme, tui/ui |
| models | internal/opencodeapi, setup, tui/keys, tui/modelpicker, tui/runtime, tui/theme, tui/ui |
| modelpicker | catalog, internal/opencodeapi, model, tui/keys, tui/theme, tui/ui |
| diagnostics | setup, tui/keys, tui/theme, tui/ui, verify |
| runtime | agents/opencode, catalog, engram, internal/opencodeapi, lifecycle, model, setup, tui/theme, tui/ui, verify |
| ui | tui/keys, tui/theme, verify |

### Notes

- `runtime` is the action boundary shared by install, uninstall, drift and models; those flows also import setup, catalog or the OpenCode adapter directly (table above).
- Not drawn: tui to styles, diagnostics to ui and verify, and every flow's use of keys, theme and ui.
- The TUI state machines do not define the crew's execution model (see `product/INDEX.md`).

## Catalog and OpenCode adapter

Diagram: [components-catalog-adapter.html](diagrams/components-catalog-adapter.html)

Scope: how declarative content becomes OpenCode configuration and plugins. The projection contract is normative in `product/meta-harness/platforms/PRD_PLATFORMS.md`.

| Component | Responsibility | Evidence |
| :--- | :--- | :--- |
| catalog assets | Agent definitions, skill packages and shared files, embedded in the binary. | `takt/catalog/packages.go` |
| catalog | Single declarative source of installable content; loaders validate before any deployment is planned. | `takt/catalog/capabilities.go` |
| agents/shared | Helpers shared by harness renderers. | `takt/agents/shared/shared.go` |
| agents/opencode | Renders native OpenCode configuration (`RenderConfig`) from catalog references. | `takt/agents/opencode/renderer.go` |
| plugin assets | The TypeScript plugins embedded for deployment (vfs, dag, memory), plus the sandbox adapter `takt-sandbox.mjs`, a module the VFS plugin loads to wrap shell commands. | `takt/agents/opencode/components.go` |
| runtime/sandbox | Canonical sandbox adapter and its probe test; the embedded `takt-sandbox.mjs` mirrors it by hand. | `takt/runtime/sandbox/adapter.mjs` |
| opencodeapi | Single adapter to the OpenCode v2 HTTP API. | `takt/internal/opencodeapi/client.go` |
| OpenCode v2 | Asked to reload loaded locations after install, and to expose models for the handshake. | `takt/agents/opencode/reload.go`, `models.go` |
| internal/artifacts | Artifact helpers used by agents/shared. | `takt/internal/artifacts/artifacts.go` |

### Import table

| Package | Imports (within Takt) |
| :--- | :--- |
| catalog | model |
| agents/shared | catalog, internal/artifacts, model |
| agents/opencode | agents/shared, catalog, engram, internal/opencodeapi, model, obs, vfs |

### Agents in the catalog

Twelve agent definitions: takt (orchestrator), analyst, architect, dev, fix, judge, pm, product-designer, simplify, spec, tpm and verify. Skills live beside them under `takt/catalog/assets/skills`; authoring rules are in `takt/catalog/CONVENTIONS.md` and `product/governance/DOCTRINE.md`.

### Notes

- agents/opencode imports obs and vfs to build permission rules (event store resource, git-mutation guard).
- The generated plugin files carry a managed-by-takt-ai header; edits are overwritten on sync.
