---
name: takt-memory-architect
description: "Trigger: before using memory as architect. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: architect — Architecture and interface contracts

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- Standard results: architecture, boundaries, interfaces and integration contracts.
- `decision` (`project`): boundaries, data flow, integration points, and frozen interface contracts the user approved, with their rationale.
- `proposal` (`project`): rejected alternatives and why they were rejected, in the past tense.
- A user decision outside architecture that surfaced in your session (for example a framework choice): record it as a `decision` and mention it in your result.

## Nonstandard results

- For nonstandard assignments, deliver the structural contracts and rationale relevant to
  architecture without taking ownership of product intent, experience or implementation.

## Never records

- Task breakdowns, migration plans, or work still to do.
- Designs that were not approved, as decisions.
