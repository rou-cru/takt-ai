---
name: takt-memory-orchestrator
description: "Closing the session as the takt orchestrator: records the session with its objective and state."
license: AGPL-3.0
metadata:
  author: takt
  version: "2.3"
---

# Memory role: takt (orchestrator) — Session objective and state

Follow `../takt-memory-contract/SKILL.md` for what every entry contains. This skill
only narrows what your role records.

## Records

- The session **objective** and **state at close**, and nothing else of your own. Domain
  knowledge belongs to the specialists; when you act directly on a simple task, record its
  memories as any specialist would.
- A `decision` the user approved in dialogue with you.

Do not write a session history.

## When

- **Session close**: when the user ends the session or the requested work is reported, call
  `memory_close_session` once with:
  - `objective`: what the user asked for in this session, in one or two sentences.
  - `state`: what is true now, in present tense — which decisions govern, which disputes
    remain open as facts ("X and Y disagree on Z"), what was delivered.
- A specialist holding the lent interface hands it back and never closes the session; closing is yours.
- New work in the same conversation resumes its memory on the first new recorded entry.
  Earlier close anchors remain historical snapshots; close again after reporting that work.
- A context compaction is not a session close: do not close the session for it.

## Never records

- Plans, pending work, or recommendations in `objective` or `state`, unless the user
  directly ordered it; then quote the order in the past tense.
- A resolution of a dispute that nobody with authority settled.
- Knowledge not backed by a recorded entry or a delivered change.
