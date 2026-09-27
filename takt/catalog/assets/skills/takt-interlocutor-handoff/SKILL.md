---
name: takt-interlocutor-handoff
description: "Trigger: holding or requesting the borrowed chat interface as a link in the interlocutor stack — opening a lent session, confirming a switch or a handoff with the user, a requirement the convening agent declared but did not deliver, keeping the standard artifact current through the session, and framing an escalation or the session's close. Governs conduct only, not the harness mechanics that enforce it."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Interlocutor Handoff

Governs how an agent behaves while it holds, requests, or returns the borrowed chat
interface: opening a session lent to it, confirming the transitions that move the
interface, handling a document the convening agent declared but did not deliver, keeping
its artifact current, and framing what it escalates or hands back.

**Confirm before the interface moves.** Before calling `dispatch_switch` to lend the
interface to a specialist, or before proposing `dispatch_handoff` to return it, invoke the
native `question` tool and let the user decide. The question offers a single selection,
never a multi-select menu of options to weigh. Only an explicit accept performs the
transition; a reject, a cancel, or a freeform reply leaves the current holder and the
session exactly as they were, so `dispatch_switch` or `dispatch_handoff` is not called
until that accept comes back. A switch proposal that does not clear confirmation is
withdrawn with `dispatch_abort_switch` rather than left pending or repeated unchanged.

**Open a lent session at your specialty, never at zero.** Once `dispatch_switch` has
handed over the interface, read what the convening agent declared under `requirements` and
weigh it against what is known of the client. The first message back to the user proves
the objective was understood and proposes exactly one starting point rooted in the
specialist's own specialty, then yields the turn. It never opens with an empty request for
direction, and never with a barrage of problems, alternatives, or questions laid out for the
user to sort through — one grounded proposal, not a menu.

**A declared document that is missing is named, not filled in.** If a requirement the
convening agent said it delivered is not actually there, its content is never invented and
the session never proceeds as though it had been read. The user is told it is missing, and
is asked either to supply it or to say whether to postpone. Once the document arrives, or
the user accepts continuing without it, work returns straight to the specialty — the gap is
acknowledged once, not revisited.

**The artifact is built as the session runs, not assembled at the end.** The specialist's
standard artifact is produced and updated progressively through the session so that an
abrupt or early close still leaves the real state on record, rather than nothing at all. A
user decision that falls outside the specialty is neither resolved nor discarded: it is kept
as a note or a provisional artifact, and its existence and location are reported on return.

**The standard artifact gates the handoff.** The standard artifact is the document the
specialty produces on its own official template, for the reason the specialist was
convened. `dispatch_handoff` is never proposed before that artifact exists in some form.
When the user pushes for an early close, the template is filled with the actual state — thin
or incomplete is acceptable — but that state is never presented as finished work.

**An escalation arrives already worked.** When something needs human judgment, what
reaches the user carries context, the implications, and the alternatives already weighed —
never a raw problem handed off unprocessed for someone else to untangle.

**A freeform reply is not a rejection.** When the user's response to the confirmation question
is neither the offered accept nor an explicit reject or cancel, the interface does not move;
the freeform text returns to the agent that asked, for it to weigh and re-ask if still needed.

**`AdditionalContext` explains, it does not dump.** It stays a concise paragraph, never raw
logs or a full command transcript — those belong in `ExtraArtifacts`, and `AdditionalContext`
says why each one exists. `Result` attributes cause and never grades the temporary holder:
`Outraged` prevails over every other value and must carry real evidence — the reference that
failed, or, absent one, what was delivered and the reaction it drew; never fabricate evidence
to fill the field. `Aborted` assigns no fault to any agent.

**Requesting a different specialist mid-session reports as an early handoff.** If the user
asks to bring in someone else before the standard artifact is complete, that is `Early
Handoff`, not `Aborted` — the session still produced what it could and says so.

**A change from outside the session is named, not absorbed.** If something changes underneath
a lent session that the specialist did not cause, state the affected scope and the
preservation or restoration options before proceeding, rather than resolving or discarding the
uncertainty silently.

**Notice cleanup or maintenance work; relay it, do not run it.** A garbage-collection or
maintenance opportunity noticed while holding the interface goes back to Takt to schedule — it
is never started or performed directly.

**Handoff is proposed; abortion never is.** Ending the session outright — no artifact
produced, no negotiation held — belongs only to the user or to the harness. As the
temporary holder, the only move available is `dispatch_handoff`: standard once the artifact
is complete, early when the user asked to cut the session short, or flagged with the
technical fault that caused it. The session is never cut unilaterally by the agent holding
it.
