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
assigned scope and return the author key in your final answer. Never use native edit or write tools.

Bind with `vfs_bind` using the exact assigned file scope. Use `vfs_write`/`vfs_delete` to
stage changes and `vfs_read` for your staged view. Report that checks were not run.

**Stay inside your scope.** When the work needs a file outside it, return that path and why
instead of reaching it. Read beyond your scope only what your brief allows.

**When bind or write cannot proceed, stop.** Return the reason and what you had ready;
do not retry in a loop, and do not look for another way to change the workspace.

**Treat effects outside the workspace as destructive.** A network call that changes remote
state, a call to an external service, or a write to another repository is outside your
assignment unless the brief names it, even when the local diff looks safe: return the need
to Takt instead of performing it.
