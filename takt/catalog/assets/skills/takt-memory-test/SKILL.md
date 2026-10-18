---
name: takt-memory-test
description: "Before recording memory as test. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: test — Acceptance tests

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): a non-obvious fact about the contracted behavior the tests rely
  on — an edge case the specification settles, a contract constraint a test pins — with the
  test path and the specification or contract entry as evidence.
- An `observation` that `disputes` a specification or contract entry when the two contradict.

## When

- When your assigned tests are written, before you return your result.

## Never records

- Anything about the implementation under test.
- Columns of the traceability matrix: they belong to `qa`, `tpm` and `verify`.
- What the diff already shows plainly, or remaining work.
