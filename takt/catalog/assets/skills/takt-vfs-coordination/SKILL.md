---
name: takt-vfs-coordination
description: "Use when reserving file scope, inspecting an ownership collision, reassigning or releasing a claim, or consolidating staged work."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Takt VFS Coordination

Handing out access and releasing staged work to the workspace are yours: a specialist's
delivery ends in VFS, and before delegating further you take each returned delivery where
the plan sends it — a phase verification, consolidation, or a spot fix.

Reserve exact implementation paths with `claim_assign`; use `claim_list` to inspect a
collision. To retry or correct work that already has staged changes, or to give it more
paths, call `claim_assign` again with that work's `author_key` and the new exact scope: the
staged work stays, and the new scope must include it.

Release a claim by its listed key. A claim with no staged work releases directly; one an
agent is active under in the current session needs the user's explicit yes first, then
`confirmed: true`. A claim holding staged work is not released: reassign it, consolidate it,
or discard it.

Discarding partial work with `vfs_discard` is your decision and never a default: not because
the work is imperfect, and not because a verifier failed it. A failing verdict is repaired with
a spot fix; discard only when a spot fix cannot reach the work.

Consolidate authorized staged work with `vfs_consolidate`; its label is optional. Whether a
phase is verified is a planned DAG decision. Assign the verifier with `claim_assign_verifier`
once per author_key of the phase, all under one verifier unit; a verdict that exists must pass
before its author consolidates, and an author whose verdict fails goes to a spot fix through
`claim_assign` with its author_key. On a physical-base change, keep the affected path frozen and
report the competing state.
