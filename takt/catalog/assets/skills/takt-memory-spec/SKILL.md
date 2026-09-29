---
name: takt-memory-spec
description: "Trigger: before using memory as spec. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: spec — Requirements

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- Standard results: behavioral requirements, scenarios and acceptance criteria.
- `observation` (`project`): requirements the user confirmed or discarded, with the approving entry `#id` or the specification file as evidence.
- An `observation` that `corrects` an earlier recorded assumption a clarification proved wrong.
- `proposal` (`project`): requirement options raised and not adopted.

## Nonstandard results

- For nonstandard assignments, deliver observable requirements and acceptance relevant to
  the request without taking ownership of implementation design.

## Never records

- Implementation design: it belongs to `architect`.
