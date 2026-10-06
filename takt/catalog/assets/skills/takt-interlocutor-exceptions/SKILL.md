---
name: takt-interlocutor-exceptions
description: "Something unexpected happens while holding the borrowed chat interface: a document that never arrived, a user reply that is neither accept nor reject, a mid-session request to swap specialists, a change from outside the session, or unplanned cleanup work surfacing."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Interlocutor Exceptions

Exception handling for an agent holding the borrowed chat interface, when
something falls outside the normal handoff protocol.

**A declared document that is missing is named, not filled in.** If a requirement the
convening agent said it delivered is not actually there, its content is never invented and
the session never proceeds as though it had been read. The user is told it is missing, and
is asked either to supply it or to say whether to postpone. Once the document arrives, or
the user accepts continuing without it, work returns straight to the specialty — the gap is
acknowledged once, not revisited.

**A freeform reply is not a rejection.** When the user's response to a confirmation
question is neither the offered accept nor an explicit reject or cancel, the interface does
not move; the freeform text returns to the agent that asked, for it to weigh and re-ask if
still needed.

**Requesting a different specialist mid-session reports as an early handoff.** If the user
asks to bring in someone else before the standard artifact is complete, that is `Early
Handoff`, not `Aborted` — the session still produced what it could and says so.

**A change from outside the session is named, not absorbed.** If something changes
underneath a lent session that the specialist did not cause, state the affected scope and
the preservation or restoration options before proceeding, rather than resolving or
discarding the uncertainty silently.

**Notice cleanup or maintenance work; relay it, do not run it.** A garbage-collection or
maintenance opportunity noticed while holding the interface goes back to Takt to schedule —
it is never started or performed directly.
