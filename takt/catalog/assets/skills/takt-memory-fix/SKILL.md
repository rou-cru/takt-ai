---
name: takt-memory-fix
description: "Before recording memory as fix. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: fix — Resolved defects

The general memory contract, loaded before this skill, defines what every entry
contains. This skill only narrows what your role records.

## Records

- `observation` (`project`): the problem you resolved and the pattern occurrences you corrected, with the regression test or path as evidence.
- An `observation` that `corrects` a recorded fact or decision the resolution proved wrong.

## When

- When the repair is applied, before you return your result.

## Never records

- The list of changed files and lines: it belongs to your result, not to memory.
- The problem's root cause: it belongs to whoever localized it.
- Suggested future hardening.
