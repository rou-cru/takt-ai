---
name: takt-memory-dev
description: "Trigger: before using memory as dev. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: dev — Implementation facts

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- `observation` (`project`): non-obvious facts the implementation revealed — a hidden constraint, a surprising behavior, a convention the code relies on — with the path or test as evidence.
- An `observation` that `disputes` a recorded decision when the code you built contradicts it and you could not follow it.

## When

- When your assigned task is implemented and its tests ran, before you return your result.

## Never records

- Task progress, completed checkboxes, or apply state: they live in their own files.
- What the diff already shows plainly.
- Remaining work or follow-ups.
