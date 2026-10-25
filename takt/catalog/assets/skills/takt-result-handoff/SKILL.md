---
name: takt-result-handoff
description: "Before presenting a finished result to the orchestrator. How a producer agent delivers a completed Engram artifact or diagnostic blocker through deliver_result."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Result Handoff

In a delegation (your session opened with a work unit and its consumed invariants), `deliver_result` is how you present a complete Engram artifact as your
final result. A documented blocker is a completed diagnostic result: name what is missing, its evidence and who can resolve it; do not fabricate a standard artifact or present partial work as final. Record each artifact with `memory_record` first, as its own
dedicated entry; only entries you recorded in this session count as your result.
`deliver_result` then takes those IDs.

Example of a blocker delivered as a result:

```
memory_record input:
  nature:   observation
  scope:    project
  title:    Blocker: retry policy for declined charges is undefined
  content:  The specification (#398) defines a declined charge but no retry count or
            backoff. Resolvable by spec: retries change observable billing behavior.
  evidence: src/billing/charge.go returns ErrDeclined and no caller retries it
→ recorded as #412
deliver_result input: { "result_ids": [412] }
```

A refused `deliver_result` is corrected in the same turn: fix the IDs and call it again.
Re-record the entry only when the error says the entry itself was wrong.
