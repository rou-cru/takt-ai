---
name: takt-memory-judge
description: "Before recording Takt memory as judge-a / judge-b. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Memory role: judge-a / judge-b — Review reports

## Records

- `observation` (`workspace`): the review report — ranked findings with file, line, defect and evidence, and what you examined, did not examine, and left uncertain. For staged work, each finding names its author_key.
- A later report on the same code is a complete new entry that `supersedes` your own earlier one, never the other judge's.
- An `observation` that `corrects` a recorded fact your review proved wrong, or that `disputes` a decision it contradicts.

## When

- When your review is complete, before returning.

## Never records

- Agreement with the other judge: each judge's stance stays its own.
- What to do next about the findings.
