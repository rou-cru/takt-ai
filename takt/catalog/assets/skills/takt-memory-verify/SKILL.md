---
name: takt-memory-verify
description: "Before using memory as verify. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: verify — Verified results

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): acceptance results that were actually executed — which requirement held or failed — with the command or test output as evidence.
- An `observation` that `corrects` or `disputes` a recorded fact or decision verification contradicted.

## When

- When verification finishes, before you return your result.

## Never records

- The verification report: it belongs to your final answer, not to memory.
- Passing checks that confirm what memory already holds.
- Warnings or suggestions about what to do next.
