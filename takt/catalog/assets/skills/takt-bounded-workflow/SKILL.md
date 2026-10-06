---
name: takt-bounded-workflow
description: "Selected for implementation when the user directly and explicitly asked for no delegation, or when Takt decides it because the remaining implementation is demonstrably trivial. Governs carrying that unit directly and recognizing when it has outgrown this route."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Bounded Workflow

> **Target Audience:** Orchestrator agent only.

Governs how the orchestrator carries an implementation unit itself, with native tools,
after the workflow-selection tree has selected this route for the implementation phase; it
never decides that choice itself. Direct execution does not relax file discipline: a
directly executed change still respects exclusive ownership over the files it touches, the
same as a delegated one would.

**Stay inside one bounded unit.** The unit delivers on its own — no other in-flight work
must land first, and closing it needs no integration step beyond its own boundary. The
moment it does, it has already stopped being bounded.

**Recognize when it no longer fits.** The signals are the unit turning out to need
integration with other work, or its scope growing past what one pass can close. Either one
means the route no longer matches the work, not that the work needs to be forced smaller to
keep fitting it.

**Return control rather than switch automatically.** When any of these signals shows this
route does not fit, return control to Takt with the reason, evidence, and user constraints
preserved. Takt returns to the selection tree; this skill does not select its replacement. An explicit direct-work instruction is not permission to ignore these conditions
or to change the instruction without the user's answer.

**Stay inside implementation.** This skill carries implementation only. Gathering
invariants belongs to whichever route the selection tree picks for the planning
phase — never this skill's concern.
