---
name: takt-result-handoff
description: "Before presenting a finished result to the orchestrator. How a producer agent delivers a completed Takt memory artifact or diagnostic blocker through deliver_result."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Result Handoff

In a delegation (your session opened with a work unit and its consumed invariants), `deliver_result` is how you present a complete Takt memory artifact as your
final result. A documented blocker is a completed diagnostic result: name what is missing and its evidence; do not fabricate a standard artifact or present partial work as final. Record each artifact with `takt_memory_record` first, as its own
dedicated entry; only entries you recorded in this session count as your result.
`deliver_result` then takes those IDs.

Example of a blocker delivered as a result:

```
takt_memory_record input:
  nature:   observation
  scope:    workspace
  title:    Blocker: retry policy for declined charges is undefined
  content:  The specification (#398) defines a declined charge but no retry count or
            backoff; retries change observable billing behavior.
  evidence: src/billing/charge.go returns ErrDeclined and no caller retries it
→ Recorded #412
deliver_result input: { "result_ids": [412] }
```

Re-record an entry after a refused `deliver_result` only when the error says the entry itself
was wrong.
