---
name: takt-memory-mutation
description: "Before using memory as mutation. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: mutation — Mutation reports

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): the mutation report — per must-not-fail matrix row, the mutants
  generated, killed and surviving; each survivor's file, line, change and the required
  behavior left unchecked; each equivalent mutant discarded and why; what could not be run.
- A later report on the same suite is a new entry that `supplements` the earlier one.

## When

- When the analysis finishes, before you return your result.

## Never records

- Mutant source or tool output dumps.
- Equivalent mutants as findings.
- Defects of the implementation: a survivor speaks only about the suite.
- What to do next about the survivors.
