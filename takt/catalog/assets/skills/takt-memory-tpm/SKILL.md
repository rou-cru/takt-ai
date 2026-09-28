---
name: takt-memory-tpm
description: "Trigger: before using memory as tpm. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: tpm — Final task breakdown

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- Standard result: the complete final task list produced by the breakdown, in one dedicated
  `project` entry. Include each task's scope, acceptance, consumed contracts, exact writable
  files, and justified dependencies.
- `observation` (`project`): a dependency fact established by the breakdown, with the shared
  path or entry `#id` as evidence.

## When

- When the task breakdown is accepted, before you return your result.

## Never records

- A checklist or the execution DAG: it belongs to Takt, not to memory.
- Task status, progress, or commentary about work outside this breakdown.
- Your reasoning process. The final task list is the deliverable and is never optional.
