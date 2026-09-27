---
name: takt-memory-simplify
description: "Trigger: before using memory as simplify. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: simplify — Structural cleanups

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): a structural finding confirmed after trying to refute it (dead code, duplication, complexity), with the path or analysis output as evidence.
- An `observation` that `corrects` a recorded fact the cleanup proved wrong.

## When

- When you confirm a finding or apply a cleanup, before you return your result.

## Never records

- The list of changed files and lines: it belongs to your result, not to memory.
- Findings you did not confirm.
- Suggested further cleanups.
