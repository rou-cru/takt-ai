---
name: takt-result-handoff
description: "Your delegated work is finished or blocked. How its result reaches the orchestrator: a Takt memory artifact by ID, staged work as it stands, or a blocker marked as one; and how a refused delivery is corrected."
license: AGPL-3.0
metadata:
  author: takt
  version: "2.0"
---

# Takt Result Handoff

In a delegation (your session opened with a work unit), the delivery is the one channel between
your session and the orchestrator's. What you deliver decides how:

| Your result | How it reaches the orchestrator |
| --- | --- |
| A Takt memory artifact: a plan, research, a design, a report | Record it with `takt_memory_record`, then `deliver_result` with its IDs |
| Staged work | It is complete in VFS as it stands. Your final reply states what only you know of it, as your OPERATIONS lists. No `deliver_result` |
| A blocker, whatever your work | Record it as its own entry, then `deliver_result` with its ID and `blocker` set |

A result travels as the IDs of the entries you recorded for it in this session, never as its
text: each artifact is its own dedicated entry, and only entries you recorded in this session
count as your result. Do not fabricate a standard artifact or present partial work as final.

A blocker is a completed diagnostic result: a question, a missing input, a decision or an
approval you need. Name what is missing and its evidence; never ask it in your reply. Work
already staged stays staged.

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
deliver_result input: { "result_ids": [412], "blocker": true }
```

When the user asked for a repository copy of a document you authored, write it with native
file tools before delivering; if that write failed, state the copy as unmet in the entry.

A refused `deliver_result` is corrected in the same turn: fix the IDs as the error states and
call it again. Re-record an entry only when the error says the entry itself was wrong. A refusal
saying no running delegation was credited is not cleared by any correction: end your turn with
the recorded IDs in your reply.

Otherwise, when your result is an artifact or a blocker, end your turn only after
`deliver_result` succeeds. What follows your delivery is the orchestrator's to decide: it
accepts the work or delegates you again with what to correct.
