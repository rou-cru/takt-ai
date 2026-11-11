---
name: takt-memory-verify
description: "Before recording Takt memory as verify. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Memory role: verify — Verification reports

## Records

- `observation` (`workspace`): the verification report — which requirements held, failed, or stayed unverified — each with the command, test output, or reading that shows it, and for staged work the author_key, file and line it concerns.
- A later report on the same code is a complete new entry that `supersedes` your earlier one.
- An `observation` that `corrects` a recorded fact verification proved wrong, or that `disputes` a decision it contradicts.
- The evidence for each traceability matrix row you verified, naming the evaluated scope, in one
  column entry titled with the matrix's exact title followed by `: Evidence`. Its first version
  `supplements` the matrix; each later one is the complete column and `supersedes` the column's
  current version, whoever wrote it; the column is yours to revise, so this needs no approval.

## When

- When verification finishes, before you return your result.

## Never records

- Passing checks that confirm what Takt memory already holds.
- What to do next about the findings.
