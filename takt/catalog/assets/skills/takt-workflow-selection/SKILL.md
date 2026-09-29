---
name: takt-workflow-selection
description: "Before any planning lane is dispatched, or before implementation work is delegated, decide which route governs that phase of work: an orchestrated framework or the bounded route the orchestrator runs itself. Evaluated once per phase, independently of the other phase's choice."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Workflow Selection

Chooses which route governs a phase of work, before that phase starts. It only chooses;
how the chosen route runs belongs entirely to that route's own skill, and this skill says
nothing about it.

If the scenario satisfies more than one row below, none of them, or the user's explicit
instruction conflicts with what the scenario needs, this table does not resolve it — load
`takt-workflow-selection-exceptions`.

| Phase | Route | Choose it when |
| --- | --- | --- |
| Planning | `takt-bounded-planning` | The invariants still needed are few and low-risk enough for the orchestrator to settle directly, or the user directly and explicitly asked for no specialist involvement in planning. |
| Planning | `takt-invariant-planning` | Two or more planning lanes are genuinely needed, or what's missing needs a specialist's judgment rather than a fact the orchestrator can establish itself. |
| Implementation | `takt-bounded-workflow` | The work is one bounded, independently deliverable unit with no cross-file integration risk, or the user directly and explicitly asked for no delegation. |
| Implementation | `takt-sdd-workflow` | Implementation units must integrate into one delivered result, or delegation would exceed four independently deliverable units in one concurrent round. |

**A new route extends this table, never its logic.** A future workflow framework, for
either phase, adds one row here with its own criteria; it never requires rewriting the
criteria already governing the routes above it.

**Evaluate each phase on its own evidence.** The route chosen for planning does not decide
the route for implementation, or the reverse. Re-evaluate at the start of each phase against
what is actually known then, not against the other phase's choice.

**A user's direct, explicit request is not the orchestrator's to override.** When the user
asked for this route directly, recognizing it no longer fits does not license switching
away from it alone. State plainly why it no longer fits and the resulting risk, and
continue only on the user's answer.

**A route recognizing it no longer fits does not return here.** Once a bounded route is
running, it names its own orchestrated counterpart directly if it outgrows itself — that
belongs to its own skill, not to a fresh pass through this table.
