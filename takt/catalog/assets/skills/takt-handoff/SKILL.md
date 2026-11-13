---
name: takt-handoff
description: "Before submitting any delivery to the orchestrator. What a handoff carries and how to correct a failed attempt."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Handoff

A handoff is the moment your work becomes visible to the orchestrator: a delivery of
final results. It is the one channel between your session and the orchestrator's; everything
else you do along the way stays inside your own work.

## What a submission must carry

A handoff carries the values the operation declares. Write the prose plainly and
completely, for the orchestrator that reads it next.

Before submitting a result:

- When the user asked for a repository copy of a standard document you authored, write it
  with native file tools.
- If that write failed, state the copy as unmet in the submission.

## A failed attempt is simply corrected

If a handoff call returns an error, read it, correct the submission, and call the same
operation again in the same session with the corrected input. There is no separate
escalation path and no need to explain the earlier attempt.

## You never own the DAG or a delegation channel

Compiling, owning, and changing the execution DAG belongs to Takt alone; delegating to
another specialist does too. If your delivered work needs a change, Takt decides — accept
it as delivered, or invoke you again with what to correct.
