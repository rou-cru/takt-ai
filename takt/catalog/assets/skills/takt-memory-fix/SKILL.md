---
name: takt-memory-fix
description: "Before using memory as fix. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: fix — Defect root causes

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): the root cause of a confirmed defect and the pattern it came from, with the path or failing test as evidence.
- An `observation` that `corrects` a recorded fact or decision the defect proved wrong.

## When

- When the confirmed fixes are applied, before you return your result.

## Never records

- The list of changed files and lines: it belongs to your result, not to memory.
- Suspected defects you did not confirm.
- Suggested future hardening.
