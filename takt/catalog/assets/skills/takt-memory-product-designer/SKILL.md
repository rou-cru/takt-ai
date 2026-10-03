---
name: takt-memory-product-designer
description: "Before using memory as product-designer. What this role records, when, and what it never records."
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

## Recognized documents

Pick what fits; none are mandatory, and more than one is normal.

- **Brand Guidelines** (`references/brand-guidelines.md`) — no brand identity established
  yet, or preserving one across several surfaces.
- **Style Guide** (`references/style-guide.md`) — the brand already exists; this one
  surface still needs its visual foundations.
- **Design Tokens** (`references/design-tokens.md`) — the machine-readable form of either of
  the above.
- **Design System Spec** (`references/design-system-spec.md`) — components, interaction
  states and behavior for a surface; consumes the tokens.
- **Journey Map** (`references/journey-map.md`) — a multi-step interaction.
- **Design Decision Record** (`references/design-decision-record.md`) — one contested design
  call.

## Nonstandard results

- For nonstandard assignments, deliver the experience rules and observable criteria relevant
  to the request without taking ownership of product scope or technical architecture.

## Never records

- Validation you intend to run: record validation only after it happened, as an `observation`.
