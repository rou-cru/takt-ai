---
name: takt-vfs-mutation
description: "Use when staging workspace changes during implementation, defect repair, or structural cleanup."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt VFS Mutation

Governs how an executing specialist changes the workspace, and what counts as a destructive
action needing the same confirmation as one.

**Change the workspace only through your VFS assignment.** Stage every change within the
assigned scope; you arrive bound to it. Never use native edit or write tools.

Use `vfs_write`/`vfs_delete` to stage changes and `vfs_read` for your staged view.

**Stay inside your scope.** When the work needs a file outside it, leave that file untouched,
finish the work inside your scope, then end the unit with a blocker result naming each needed
path and why the work needs it. Read beyond your scope only what your brief allows.

**When bind or write cannot proceed, end the unit.** A refusal naming a corrected call is a
correction: make that call once. Any other refusal ends the unit with a blocker result stating
its reason and the paths it names, instead of a retry loop or another way to change the workspace.

**Treat effects outside the workspace as destructive.** A network call that changes remote
state, a call to an external service, or a write to another repository is outside your
assignment unless the brief names it, even when the local diff looks safe: leave it unperformed
and end the unit with a blocker result naming the effect and why the work needs it.
