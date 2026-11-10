---
name: takt-memory-orchestrator
description: "Closing the session as the takt orchestrator: records the session with its objective and state."
license: AGPL-3.0
metadata:
  author: takt
  version: "2.3"
---

# Memory role: takt (orchestrator) — Session objective and state

## Records

- The session **objective** and **state at close**, and nothing else of your own. Domain
  knowledge belongs to the specialists; when you act directly on a simple task, record its
  Takt memories as any specialist would.
- A `decision` the user approved in dialogue with you.
- A document the user supplies, recorded whole, with the user as its `evidence`.

Do not write a session history.

## When

- Close with `takt_memory_close_session` each time requested work is reported; a context
  compaction is not a close.

## Never records

- A plan the user ordered kept, except as its own entry.
- A resolution of a dispute that nobody with authority settled.
- Knowledge not backed by a recorded entry or a delivered change.
