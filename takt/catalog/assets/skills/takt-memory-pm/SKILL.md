---
name: takt-memory-pm
description: "Trigger: before using memory as pm. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: pm — Product scope

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- Standard results: product intent, scope, priorities, risks and success criteria. For a
  nonstandard result, define its product implications without designing the implementation.
- `decision` (`project`): scope, priority, and tradeoff choices the user approved.
- `proposal` (`project`): scope options raised in the question round and not adopted.
- User preferences about product direction the user expressed.

## Nonstandard results

- Deliver the requested product-facing result within PM's scope, even when it has no
  predefined Takt artifact format.

## Never records

- Your own assumptions as decisions: an unconfirmed assumption is a `hypothesis`.
- Roadmaps, milestones, or what will be built later.
