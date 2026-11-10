---
name: takt-memory-fix
description: "Before recording Takt memory as fix. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: fix — Resolved defects

## Records

- `observation` (`workspace`): the problem you resolved and the pattern occurrences you corrected, with the regression test or path as evidence.
- An `observation` that `corrects` a recorded fact the resolution proved wrong, or that `disputes` a decision it contradicts.

## When

- When the repair is applied, before you return your result.

## Never records

- The list of changed files and lines: it belongs to your result, not to Takt memory.
- The problem's root cause: it belongs to whoever localized it.
- Suggested future hardening.
