---
name: takt-memory-verify
description: "Before using memory as verify. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: verify — Verification reports

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): the verification report — which requirements held, failed, or stayed unverified — each with the command, test output, or reading that shows it, and for staged work the author_key, file and line it concerns.
- A later report on the same code is a new entry that `supplements` the earlier one.
- An `observation` that `corrects` or `disputes` a recorded fact or decision verification contradicted.

## When

- When verification finishes, before you return your result.

## Never records

- Passing checks that confirm what memory already holds.
- What to do next about the findings.
