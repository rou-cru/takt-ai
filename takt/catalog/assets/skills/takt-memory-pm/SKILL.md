---
name: takt-memory-pm
description: "Before recording Takt memory as pm. What this role records, when, and what it never records."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Memory role: pm — Product scope

## Records

- Standard results: user stories, product intent, formal requirements, focus areas, scope,
  priorities, risks and success criteria. For a nonstandard result, define its product implications without
  designing the implementation.
- `decision` (`workspace`): scope, priority, and tradeoff choices the user approved.
- `proposal` (`workspace`): scope options raised in the question round and not adopted.
- User preferences about product direction the user expressed.

## Recognized documents

Pick what fits; none are mandatory, and more than one is normal.

- **User Story** (`references/user-story.md`) — one functional outcome from the user's
  perspective, one entry per story; a story with a question about its expected result still
  open stays a draft.
- **Brief** (`references/brief.md`) — a narrow, already-clear ask.
- **PRD** (`references/prd.md`) — scope, priorities and success criteria substantial enough
  to need their own document.

## Nonstandard results

- Deliver the requested product-facing result within PM's scope, even when it has no
  predefined Takt artifact format.

## Never records

- Your own assumptions as decisions: an unconfirmed assumption is a `hypothesis`.
- Roadmaps by date, milestones, or plans of what will be built later; the order of delivery
  lives in each story's priority.
