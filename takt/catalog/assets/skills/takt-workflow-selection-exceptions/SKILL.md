---
name: takt-workflow-selection-exceptions
description: "The scenario satisfies more than one row of takt-workflow-selection's table at once, satisfies none of them clearly, or the user's explicit instruction conflicts with what the work actually needs once evaluated."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Workflow Selection Exceptions

Exception handling for choosing a route under `takt-workflow-selection`, when the scenario
does not resolve cleanly against its table.

**An ambiguous signal is not resolved by guessing.** Whether no row's criteria clearly fit,
or more than one does at once, treat both as the same case: do not force a fit and do not
default silently to either the bounded route or the orchestrated one. State what is known
and what is unclear about the scenario, and let the user decide the route before that phase
starts.

**A conflict between the user's instruction and the evaluated work is named, not overridden
or hidden.** When the user directly and explicitly asked for the bounded route but the
work's evaluated scope clearly exceeds what one agent can carry alone, do not silently honor
either side. Tell the user the concrete reason the bounded route does not fit, and proceed
only on their answer — relaxing the constraint, accepting a staged bounded execution, or
accepting the orchestrated route.

**This is a normal branch, not a stalled session.** Escalating here is the correct outcome
for a genuinely ambiguous or conflicting scenario, not a sign the orchestrator failed to
decide.
