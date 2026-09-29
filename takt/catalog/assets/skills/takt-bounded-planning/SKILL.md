---
name: takt-bounded-planning
description: "Selected under takt-workflow-selection for planning: the invariants still needed are few and low-risk enough to settle directly, or the user directly and explicitly asked for no specialist involvement in planning. Governs gathering what's left directly and recognizing when it has outgrown this route."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Bounded Planning

> **Target Audience:** Orchestrator agent only.

Governs how the orchestrator settles the invariants a phase still needs by itself, once
`takt-workflow-selection` has picked this route for the planning phase; it never decides
that choice itself. It applies only to what is missing — whatever is already settled,
existing, or approved needs no gathering at all.

**Gather only what's missing, directly.** Establish the fact or confirm the decision the
work is waiting on, and move on. The moment settling it needs more than one bounded pass,
it has already stopped being bounded.

**Recognize when it no longer fits.** The signals are two or more genuinely different kinds
of judgment being needed at once (an architecture call and an experience call, for
instance), or what's missing turning out to need a specialist's own judgment rather than a
fact the orchestrator can establish itself. Either one means the route no longer matches
the gap, not that the gap needs to be narrowed to keep fitting it.

**Switching away is a normal step, not a failure.** When the orchestrator itself chose this
route and then finds it no longer fits, it loads `takt-invariant-planning` and continues
there — the next correct step, not a retry or an apology.

**A user's direct, explicit request is not the orchestrator's to override.** When the user
asked for this route directly, recognizing it no longer fits does not license switching
away from it alone. State plainly why it no longer fits and the resulting risk, and
continue only on the user's answer.

**Stay inside planning.** This skill carries planning only. Implementation belongs to
whichever route `takt-workflow-selection` picked for that phase — never this skill's
concern.
