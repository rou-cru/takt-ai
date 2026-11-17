---
name: takt-memory-tpm
description: "Before recording Takt memory as tpm. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: tpm — Final work breakdown

## Records

- Standard result: the complete final assignment list produced by the breakdown, in one dedicated
  `workspace` entry. Include each assignment's scope, acceptance, consumed contracts, exact writable
  files, and justified dependencies.
- When a traceability matrix exists: one column entry holding each row's owning assignment, titled
  with the matrix's exact title followed by `: Owning node`. Its first version `supplements`
  the matrix; each later one is the complete column and `supersedes` the column's current
  version, whoever wrote it; the column is yours to revise, so this needs no approval.
- `observation` (`workspace`): a dependency fact established by the breakdown, with the shared
  path or entry `#id` as evidence.

## Recognized documents

- **WBS** (`references/wbs.md`) — the work breakdown itself: scope, acceptance, contracts,
  files and dependencies per assignment.

## When

- When the work breakdown is accepted, before you return your result.

## Never records

- A checklist or the execution DAG: it belongs to Takt, not to Takt memory.
- Assignment status, progress, or commentary about work outside this breakdown.
- Your reasoning process. The final assignment list is the deliverable and is never optional.
