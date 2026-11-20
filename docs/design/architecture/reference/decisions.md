# Decisions and constraints

Structural norms and constitutional articles that shape the architecture. They are normative in `product/governance/`; this page only shows where they land in the structure.

## Structural norms (DOCTRINE)

| Norm | Effect on structure |
| :--- | :--- |
| STR-1 Single definition | One home per fact. The catalog is the single declarative source of installable content; the cli, setup and renderers read it instead of restating it. |
| STR-2 Fail fast by design | Invalid state is rejected where it first becomes representable: the history validates each entry on append, obs validates each envelope at ingest, and the catalog validates before deployment is planned. |
| STR-3, STR-4 Data contracts and validation at acquisition | Plugins send typed JSON to subcommands; the Go side validates at the boundary and consumers do not re-validate. |
| STR-5 Present requirement only | No abstraction without existing multiplicity; the OpenCode adapter is the one harness adapter because OpenCode is the one supported agent. |
| STR-6 Reuse before creation | Shared helpers sit in `agents/shared` and `internal/filemerge` rather than per-package copies. |
| STR-13 No orphan skill | Checked by a catalog test: every skill has a declared reader. |
| STR-15 Single authority for state | Each stored fact has one owner store, named in [data and policy](data-and-policy.md): the memory service alone opens `memory.db`. |
| STR-16 Concurrency by design | Concurrent access has a named serialization point: the VFS lock (`internal/flock`) and the memory service as sole opener of its store. |

## Constitutional articles

| Article | Effect on structure |
| :--- | :--- |
| 6. Takt does not appropriate the environment | Setup merges into user-owned files, preserves user edits, keeps backups and exposes restore. |

The remaining articles govern crew conduct and authority; they are carried by prompts and the crew contract, not by package structure.

## Boundaries that follow from the product index

- Application and meta-harness share patterns, not a runtime, clock or event bus (`product/INDEX.md`).
- The TUI state machines do not define crew execution.
- Where this view and a PRD disagree, the PRD wins.

## Known gaps between corpus and code

| Area | Gap |
| :--- | :--- |
| Dreaming | Specified in `PRD_DREAMING.md`; no dream cycles, diary or scheduler in code. |
| Observability control loop | Control records are written but no consumer reads them; several defined event classes are never emitted. Detectors, the stagnation counter, SMC and Kev have no code. |
| GC cycle control and workspace estimation | Specified in `PRD_GC.md` (cycle control) and `PRD_OBSERVABILITY.md` (workspace state estimation), designed in [control](../../control/README.md). No sensors, estimate or control law exist in code, and nothing launches coordinator cycles; `takt/gc/trigger_policy.yaml` holds the activity cadence. |
| First-use test image | Specified in `PRD_RELEASE_VALIDATION.md` (`PR-REL-1`, `PR-REL-2`, `PR-REL-13`): `takt-ai` is a command in the image and the installation is still to run from the TUI. The `dev` stage of `deploy/images/Dockerfile` runs `takt-ai setup image` with the default request, which applies the installation at build. |
| Memory service deployed apart | Specified in `PRD_MEMORY_SERVICE.md` (`PR-MSV-43` to `PR-MSV-45`). The service listens on a private Unix socket only, and setup starts it locally. |
| Interlocutor resume | Conduct in the prompt; no resume verb in code. |
