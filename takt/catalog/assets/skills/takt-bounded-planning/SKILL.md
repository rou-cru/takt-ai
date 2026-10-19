---
name: takt-bounded-planning
description: "Selected for planning when the user directly and explicitly asked for no specialist involvement in planning, or when Takt decides it because the remaining planning is demonstrably trivial. Governs gathering what's left directly and recognizing when it has outgrown this route."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Bounded Planning

Governs how the orchestrator settles the invariants a phase still needs by itself,
after the workflow-selection tree has selected this route for the planning phase; it never decides
that choice itself. It applies only to what is missing — whatever is already settled,
existing, or approved needs no gathering at all.

Gather only missing facts and decisions. If Takt chose this route, keep OPERATIONS' three
triviality tests; specialist judgment or larger scope returns to the selection tree.

An explicit user instruction permits nontrivial, multi-pass planning within its scope.
Complexity alone is not a mismatch. Name the specialties covered and evidence used;
OPERATIONS' permissions, ownership, budgets and assurance limits still apply.

Material uncertainty, new scope or a blocked path returns to the selection tree with evidence
and user constraints. Do not shrink the objective or unilaterally abandon the user's instruction.

**Stay inside planning.** This skill carries planning only. Implementation belongs to
whichever route the selection tree picks for that phase — never this skill's
concern.
