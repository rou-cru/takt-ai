# Architecture documentation

Derived view of Takt AI using the C4 model. Diagrams are standalone HTML produced with archify; each topic pairs short notes with its diagrams. The normative source stays in `product/`; where this view and a PRD disagree, the PRD wins (see `product/INDEX.md`). The engineering design of the closed-loop control systems, ahead of implementation, is in [control](../control/README.md).

## Two systems

Takt is two systems under one Constitution:

- **Application**: the CLI/TUI that plans, authorizes, applies and reports installation and configuration into OpenCode v2.
- **Meta-harness**: the capabilities installed into OpenCode v2 (crew, memory, orchestration, VFS, observability, maintenance). Its plugins run inside OpenCode; Takt Memory runs as an independent service reached through MCP, not in the installation TUI.

They share patterns (events, state machines, declarative tables) but not a runtime, clock or event bus.

## Layout

| Path | Content | C4 level |
| :--- | :--- | :--- |
| [c4/01-context.md](c4/01-context.md) | Who and what Takt touches | L1 |
| [c4/02-containers.md](c4/02-containers.md) | Runtime and deployable units, deployment variants | L2 |
| [c4/03-components.md](c4/03-components.md) | CLI and setup, harness, TUI, catalog and OpenCode adapter | L3 |
| [runtime/](runtime/README.md) | Key flows over time: install, VFS, work units, GC, memory; observability and TUI state machines as notes only | Dynamic |
| [agents/](agents/README.md) | The crew as a deterministic system: topology, orchestrator decision trees and state machines, happy-path flows, harness touchpoints | Dynamic |
| [reference/data-and-policy.md](reference/data-and-policy.md) | Stores, records, policy files, ownership rules | Supplement |
| [reference/decisions.md](reference/decisions.md) | Norms behind the structure, and gaps between corpus and code | Supplement |

Each topic folder keeps its notes at the top, its standalone diagrams in `diagrams/` and the editable diagram sources in `sources/` (same file names).

## Where each PRD lands

| Area | Owning documents | Shown in |
| :--- | :--- | :--- |
| Setup and installation | `app/PRD_SETUP.md`, `app/installation/PRD_INSTALLATION.md` | containers, components, runtime |
| Release validation | `app/PRD_RELEASE_VALIDATION.md` | containers |
| TUI | `app/tui/PRD_TUI.md`, `app/tui/VDS_TUI.md` | components, runtime |
| Platform projection | `meta-harness/platforms/PRD_PLATFORMS.md` | containers, components |
| Crew and context | `meta-harness/crew/PRD_CREW.md`, `meta-harness/context/ARCH_CONTEXT.md`, `meta-harness/context/ARCH_MEMORY.md` | containers, components, runtime, agents |
| Independent memory service | `meta-harness/context/PRD_MEMORY_SERVICE.md` | context, containers, components, memory session |
| Orchestration and execution history | `meta-harness/orchestration/PRD_ORCHESTRATION.md`, `PRD_DAG.md`, `PIRS_ORCHESTRATION.md` | components, runtime, agents |
| Workflow selection | `meta-harness/orchestration/WORKFLOW_SELECTION.md` | agents |
| DAG TUI sidebar | `meta-harness/orchestration/PRD_DAG_TUI.md` | containers |
| Guardrails and VFS | `meta-harness/harness/PRD_HARNESS.md`, `meta-harness/harness/PRD_VFS.md` | components, runtime, agents |
| Observability | `meta-harness/observability/PRD_OBSERVABILITY.md` | components, runtime, agents |
| Maintenance | `meta-harness/maintenance/PRD_GC.md`, `meta-harness/maintenance/PRD_DREAMING.md` | components, runtime, agents |
| Quality and testing | `meta-harness/quality/PRD_QA.md` | agents |

Paths are relative to `product/`.

## Conventions

- One binary (`takt/cli/main.go`) serves both systems; plugins inside OpenCode call back into its subcommands. The same executable also runs the independent memory service and its stdio MCP adapters.
- Every box and edge is checked against code or a PRD before it is drawn; unresolved facts are stated as unknown, not guessed.
- API reference lives in `docs/api/` (generated); this set does not duplicate it.

## Regenerating a diagram

Diagrams are produced with the archify skill. Each file in a `sources/` folder is the archify input for the HTML of the same name; its `meta.output` holds the HTML path relative to the repository root. After editing a source, run archify's `finalize` for the source's `diagram_type` with the source, its output path, `--repo-root` at the repository root and `--quality showcase`. Sources pin a commit in `meta.repository`; update it when re-checking evidence.
