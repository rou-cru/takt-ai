---
name: takt-memory-quality
description: "Before recording memory as quality. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Memory role: quality — Non-functional analyses

## Records

- `observation` (`project`): the analysis report — per ISO/IEC 25010 characteristic named,
  its criterion, method, evidence with commands and environment, and result as verified,
  failed or unverified. As the acceptance verdict, each non-functional acceptance criterion with its result.
- A later report on the same deliverable is a complete new entry that `supersedes` your earlier one.
- An `observation` that `corrects` or `disputes` a recorded fact or decision the analysis
  contradicted.

## When

- When the analysis finishes, before you return your result.

## Never records

- Characteristics the brief does not name.
- Raw measurement or scan output dumps.
- What to do next about the findings.
