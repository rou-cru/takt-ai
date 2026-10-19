---
name: takt-bounded-workflow
description: "Selected for implementation when the user directly and explicitly asked for no delegation, or when Takt decides it because the remaining implementation is demonstrably trivial. Governs carrying that unit directly and recognizing when it has outgrown this route."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Bounded Workflow

Governs how the orchestrator carries an implementation unit itself, with native tools,
after the workflow-selection tree has selected this route for the implementation phase; it
never decides that choice itself. Direct execution does not relax file discipline: a
directly executed change still respects exclusive ownership over the files it touches, the
same as a delegated one would.

If Takt chose this route, keep OPERATIONS' three triviality tests; a larger unit, another
lane's judgment or integration with other work returns to the selection tree.

An explicit user instruction permits nontrivial, multi-pass implementation within its scope,
including integration. Complexity alone is not a mismatch. Name any specialist judgment covered;
OPERATIONS' permissions, ownership, budgets and assurance limits still apply.

Material uncertainty, new scope or a blocked path returns to the selection tree with evidence
and user constraints. Do not shrink the objective or unilaterally abandon the user's instruction.
When delegation is prohibited, report missing independent verification; do not delegate it silently.

**Stay inside implementation.** This skill carries implementation only. Gathering
invariants belongs to whichever route the selection tree picks for the planning
phase — never this skill's concern.
