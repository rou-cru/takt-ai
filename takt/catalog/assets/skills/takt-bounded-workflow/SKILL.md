---
name: takt-bounded-workflow
description: "Selected under takt-workflow-selection for implementation: the work is one bounded, independently deliverable unit, or the user directly and explicitly asked for no delegation. Governs carrying that unit directly and recognizing when it has outgrown this route."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Bounded Workflow

> **Target Audience:** Orchestrator agent only.

Governs how the orchestrator carries an implementation unit itself, with native tools,
once `takt-workflow-selection` has picked this route for the implementation phase; it
never decides that choice itself. Direct execution does not relax file discipline: a
directly executed change still respects exclusive ownership over the files it touches, the
same as a delegated one would.

**Stay inside one bounded unit.** The unit delivers on its own — no other in-flight work
must land first, and closing it needs no integration step beyond its own boundary. The
moment it does, it has already stopped being bounded.

**Recognize when it no longer fits.** The signals are the unit turning out to need
integration with other work, its scope growing past what one pass can close, or
delegation under it approaching the concurrent-unit ceiling. Any one of these means the
route no longer matches the work, not that the work needs to be forced smaller to keep
fitting it.

**Switching away is a normal step, not a failure.** When the orchestrator itself chose this
route and then finds it no longer fits, it loads `takt-sdd-workflow` and continues there —
this is the next correct step, not a retry, an apology, or grounds to collapse the
remaining work into one delegation.

**A user's direct, explicit request is not the orchestrator's to override.** When the user
asked for this route directly, recognizing it no longer fits does not license switching
away from it alone. State plainly why it no longer fits and the resulting risk, and
continue only on the user's answer.

**Stay inside implementation.** This skill carries implementation only. Gathering
invariants belongs to whichever route `takt-workflow-selection` picked for the planning
phase — never this skill's concern.
