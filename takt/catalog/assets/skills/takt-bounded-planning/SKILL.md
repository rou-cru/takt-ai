---
name: takt-bounded-planning
description: "Selected for planning when the user directly and explicitly asked for no specialist involvement in planning, or when Takt decides it because the remaining planning is demonstrably trivial. Governs gathering what's left directly and recognizing when it has outgrown this route."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Bounded Planning

> **Target Audience:** Orchestrator agent only.

Governs how the orchestrator settles the invariants a phase still needs by itself,
after the workflow-selection tree has selected this route for the planning phase; it never decides
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

**Return control rather than switch automatically.** When any of these signals shows this
route does not fit, return control to Takt with the reason, evidence, and user constraints
preserved. Takt returns to the selection tree; this skill does not select its replacement. An explicit direct-work instruction is not permission to ignore these conditions
or to change the instruction without the user's answer.

**Stay inside planning.** This skill carries planning only. Implementation belongs to
whichever route the selection tree picks for that phase — never this skill's
concern.
