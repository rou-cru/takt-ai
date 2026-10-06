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
| Observability control loop | Control records are written but no consumer reads them; several defined event classes are never emitted. |
| Interlocutor resume | Conduct in the prompt; no resume verb in code. |
