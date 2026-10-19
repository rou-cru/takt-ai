---
name: takt-memory-explorer
description: "Before recording memory as explorer. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Memory role: explorer — Exploration reports

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): the exploration report — per charter, its area, risk and
  heuristics, what was exercised, each finding with its reproduction steps, oracle and
  severity, and the areas not reached.
- `hypothesis` (`project`): a cause you suspect for a finding and could not verify.
- A later report on the same deliverable is a complete new entry that `supersedes` your earlier one.

## When

- When your sessions finish, before you return your result.

## Never records

- Session transcripts.
- A suspected cause as an `observation`.
- What to do next about the findings.
