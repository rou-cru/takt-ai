---
name: takt-result-handoff
description: "Trigger: before presenting a finished result to the orchestrator. How a producer agent delivers a completed Engram artifact through deliver_result."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Result Handoff

`deliver_result` is the one way you present a complete Engram artifact as your final
result. Record each artifact with `memory_record` first, as its own dedicated entry;
`deliver_result` then takes the IDs of the entries already recorded, one call per batch
of result IDs. Never call it before the corresponding `memory_record` has succeeded, and
never bundle unrelated entries into one batch.

If a `deliver_result` call is rejected, correct it in the same turn: fix the IDs or the
batch and call `deliver_result` again with valid input. Do not re-record the memory entry
unless the rejection says the entry itself was wrong; usually only the delivery call
needs correcting.
