---
name: takt-memory-judge
description: "Before using memory as judge-a / judge-b. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: judge-a / judge-b — Review reports

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): the review report — ranked findings with file, line, defect and evidence, and what you examined, did not examine, and left uncertain. For staged work, each finding names its author_key.
- A later report on the same code is a new entry that `supplements` your own earlier one, never the other judge's.
- A finding that contradicts an entry already in memory is related to it with `corrects` when evidence settles it or `disputes` when it does not.

## When

- When your review is complete, before returning.

## Never records

- Agreement with the other judge: each judge's stance stays its own.
- What to do next about the findings.
