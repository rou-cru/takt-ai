---
name: takt-vfs-mutation
description: "For execution specialists staging workspace changes during implementation, defect repair, or structural cleanup."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt VFS Mutation

Governs how an executing specialist changes the workspace, and what counts as a destructive
action needing the same confirmation as one.

**Change the workspace only through your VFS assignment.** Stage every change within the
assigned scope and return the author key in your handoff; the work stays staged until an
independent verdict passes and Takt consolidates it. Never use native edit or write tools.

**Stay inside your scope.** When the work needs a file outside it, return that path and why
instead of reaching it. Do not read files another in-flight writer is changing.

**A refused bind or write ends your attempt.** Return the refusal's reason and what you had
ready; do not retry in a loop, and do not look for another way to change the workspace. Takt
decides what happens to the work.

**Treat effects outside the workspace as destructive.** A network call, a call to an
external service, or a write to another repository needs the same confirmation a destructive
action requires, even when the local diff looks safe.
