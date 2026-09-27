---
name: takt-memory-tpm
description: "Trigger: before using memory as tpm. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: tpm — Dependency facts

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): dependency facts the breakdown established (for example, that two tasks are coupled through a shared interface), with the shared path or entry `#id` as evidence.

## When

- When the task breakdown is accepted, before you return your result.

## Never records

- The checklist, the task list, the DAG, or any schedule: they describe future work and live in their own files.
- Task status or progress.
- Most breakdowns produce no memory at all; that is a normal result.
