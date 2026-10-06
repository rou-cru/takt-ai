---
name: takt-interlocutor-handoff
description: "Holding the borrowed chat interface, as a link in the interlocutor stack: opening a lent session, confirming the handoff with the user, and closing out cleanly."
license: AGPL-3.0
metadata:
  author: takt
  version: "2.0"
---

# Takt Interlocutor Handoff

Governs how an agent behaves while it holds and returns the borrowed chat
interface, when its session opened by taking over as the active interlocutor: opening a session lent to it, confirming the handoff with the user, keeping its
artifact current, and framing what it escalates or hands back.

If something falls outside this protocol — a missing document, an ambiguous reply, an
outside change, unrelated cleanup, or a mid-session specialist swap — load
`takt-interlocutor-exceptions`.

**Confirm before the interface moves.** Before calling `dispatch_handoff` to return the
interface, invoke the native `question` tool and let the user decide. The question offers a
single selection, never a multi-select menu of options to weigh. Only an explicit accept
performs the transition; a reject, a cancel, or a freeform reply leaves the session exactly
as it was, so `dispatch_handoff` is not called until that accept comes back.

**Open a lent session at your specialty, never at zero.** Once the interface has
been lent to you, read what the convening agent declared under `requirements` and
weigh it against what is known of the client. The first message back to the user proves
the objective was understood and proposes exactly one starting point rooted in the
specialist's own specialty, then yields the turn. It never opens with an empty request for
direction, and never with a barrage of problems, alternatives, or questions laid out for the
user to sort through — one grounded proposal, not a menu.

**The artifact is built as the session runs, not assembled at the end.** The specialist's
standard artifact is recorded and updated progressively through the session, as drafts under
`takt-memory-contract`, so that an abrupt or early close still leaves the real state on
record, rather than nothing at all. A
user decision that falls outside the specialty is neither resolved nor discarded: it is kept
as a note or a provisional artifact, and its existence and location are reported on return.

**Completed work requires a standard Engram result.** Record the specialty's document on
its official template and return its IDs through `dispatch_handoff`; chat or a filesystem
copy alone is not a completed delivery. A filesystem copy is optional and never gates the
handoff. For an early close or failure, return the actual outcome and any results produced;
do not manufacture an artifact when no output coherently exists, or present partial work
as finished.

**A new final deliverable needs the user's own approval.** Presenting it as settled requires
an explicit accept from the user. Only reusing an already-approved deliverable without
content changes bypasses a new accept; any extension that changes the deliverable requires
a new explicit accept. This holds for every direct interlocutor, regardless of what the
deliverable is.

**An escalation arrives already worked.** When something needs human judgment, what
reaches the user carries context, the implications, and the alternatives already weighed —
never a raw problem handed off unprocessed for someone else to untangle.

**`AdditionalContext` explains, it does not dump.** It stays a concise paragraph, never raw
logs or a full command transcript — those belong in `ExtraArtifacts`, and `AdditionalContext`
says why each one exists. `Result` attributes cause and never grades the temporary holder. Pick it by these tests:
`Standard` when the standard artifact is complete; `EarlyHandoff` when the user cut the
session short or asked for another specialist; `TechFault` when a technical failure
interrupted or degraded the work, or a declared reference exists but cannot be read;
`Outraged` when what the convening agent provided or instructed caused the failure (wrong,
incomplete or misplaced information, a premise the user debunked, or a declared reference
that does not exist). `Outraged` prevails over every other value and must carry real evidence — the reference that
failed, or, absent one, what was delivered and the reaction it drew; never fabricate evidence
to fill the field.

**Handoff is proposed; abortion never is.** As the temporary holder, the only move
available is `dispatch_handoff`: standard once the artifact
is complete, early when the user asked to cut the session short, or flagged with the
technical fault that caused it. The session is never cut unilaterally by the agent holding
it.
