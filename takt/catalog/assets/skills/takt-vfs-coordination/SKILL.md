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
the plan sends it — a phase verification, consolidation, or a correction.

Reserve exact implementation paths with `claim_assign`; use `claim_list` to inspect a
collision. To retry or correct work that already has staged changes, or to give it more
paths, call `claim_assign` again with that work's `author_key` and the new exact scope: the
staged work stays; naming only part of it hands that part over under a new author key.

Release a claim by its listed key. A claim with no staged work releases directly; one an
agent is active under in the current session needs the user's explicit yes first, then
`confirmed: true`. A claim holding staged work is not released: reassign it, consolidate it,
or discard it.

Discarding partial work with `vfs_discard` is your decision and never a default: not because
the work is imperfect, and not because a verifier failed it. A failing verdict is corrected;
discard only when correcting the staged work cannot reach it.

Consolidate authorized staged work with `vfs_consolidate`; its label is optional. Whether a
phase is verified is a planned DAG decision. Declare the phase's `author_keys` in one verifier
delegation; every gate is ready when the verifier starts, retries included; each judge holds one verdict per
author, and delegating it again replaces its earlier one; verdicts inform you; consolidating over a
current failing one needs the user's acceptance, which the harness asks for, and the staged work of an author whose verdict fails is normally
corrected: whoever corrects it continues that work through `claim_assign` with its author_key. On a physical-base change, keep the affected path frozen and
report the competing state.
