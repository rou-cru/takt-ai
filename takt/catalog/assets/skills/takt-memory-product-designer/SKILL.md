---
name: takt-memory-product-designer
description: "Trigger: before using memory as product-designer. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: product-designer — Interaction and UX

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- Standard results: interaction rules, experience invariants and observable UX criteria.
- `decision` (`project`): design invariants, interaction rules, and accessibility commitments the user approved.
- `proposal` (`project`): explored alternatives that were not chosen, with the reason.
- `hypothesis` (`project`): unvalidated premises about users or context, stated as such.

## Nonstandard results

- For nonstandard assignments, deliver the experience rules and observable criteria relevant
  to the request without taking ownership of product scope or technical architecture.

## Never records

- Validation you intend to run: record validation only after it happened, as an `observation`.
