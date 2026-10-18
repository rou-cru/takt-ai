---
name: takt-failure-handling
description: "A unit failed beyond a minor in-place retry, or an escalation trigger appeared. Explains declared recoveries, the escalation procedure, and what follows exhausted recoveries."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Failure Handling

A failure that touches a contract, another unit's paths, or the objective is not minor; unless
it is a contract defect, it goes to a declared recovery: declare with
`dispatch_declare_recovery` a binary result, scope, a recoverable point, an action budget (at
most {{policy.recovery_max_actions}}) and an attempt budget (at most {{policy.recovery_max_attempts}}) before acting, and close it
with `dispatch_close_recovery`.
Reversibility is confined to unconsolidated governed state, never promised for external effects.

## Escalation

On an escalation trigger:
1. Freeze the affected path.
2. Confirm termination of the active work (a delegation that will never return, because its session is gone or will not end, is ended with `dispatch_abandon` on its dispatch); restore (`dispatch_restore` with the objective) only the scope a declared recovery abandoned, and freeze and report any other staged work.
3. Preserve unrelated progress.
4. Report evidence and alternatives.
5. Await direction.

For an active VFS claim, name the exact active agent and claimed paths in the native user question;
releasing and discarding follow takt-vfs-coordination.

## Exhausted recoveries

After {{policy.recovery_failures}} consecutive failed recoveries of the same objective, escalate (a second failing verdict for the same author key counts as one failed recovery); no further recovery
is authorized merely by sending the escalation. An exhausted recovery means the real state is unknown: the escalation offers the blind review pair over that objective before any allowance, and an allowance granted afterwards is spent on a strategy rebuilt from the pair's findings. The user's decision to continue an abandoned recovery scope is recorded with `dispatch_exception` on that objective's attempts or actions; the decision to drop it, with `dispatch_restore`. After consolidation, repair forward.
