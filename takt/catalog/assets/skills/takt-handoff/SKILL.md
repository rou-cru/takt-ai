---
name: takt-handoff
description: "Trigger: before submitting any delivery to the orchestrator. What a handoff is, how submitted content is checked, and how to correct a rejected attempt."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Handoff

A handoff is the moment your work becomes visible to the orchestrator: a delivery of
final results, or a return of the conversational interface once your assignment is
complete. It is the one channel between your session and the orchestrator's; everything
else you do along the way stays inside your own work.

## What a submission must carry

A handoff checks the values the operation declares: an allowed outcome, and for a result,
the IDs of the entries you recorded for it in this session. Nobody grades your prose there;
write it plainly and completely, for the orchestrator that reads it next.

## A rejected attempt is simply corrected

If a handoff attempt is rejected, treat it as ordinary feedback: read why it was
rejected, correct the submission, and call the same operation again in the same session
with the corrected input. There is no separate escalation path and no need to explain
the earlier attempt. The orchestrator only ever sees the accepted handoff; it never
learns that an earlier attempt was malformed or incomplete.
