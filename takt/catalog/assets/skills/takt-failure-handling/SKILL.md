---
name: takt-failure-handling
description: "A failure exceeds an in-place retry, or a recovery signal appears. Bound technical diagnosis and reserve user escalation for value and authority decisions."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt Failure Handling

A failure that touches a contract, another unit's paths, or the objective is not minor; unless
it is a contract defect, it goes to a declared recovery: declare with
`dispatch_declare_recovery` a binary result, scope, an action budget (at
most {{policy.recovery_max_actions}}) and an attempt budget (at most {{policy.recovery_max_attempts}}) before acting, and close it
with `dispatch_close_recovery`.
Reversibility is confined to unconsolidated governed state, never promised for external effects.

## Recovery and escalation

On a recovery or diagnosis signal:
1. Freeze the affected path.
2. Confirm termination of the active work (a delegation that will never return, because its session is gone or will not end, is ended with `dispatch_abandon` on its dispatch); restore (`dispatch_restore` with the objective) only the scope a declared recovery abandoned, and freeze and report any other staged work.
3. Preserve unrelated progress.
4. Route technical uncertainty to its competent specialist, or investigate directly when the user's instruction covers it. Keep existing authority, ownership and effective budgets.
5. Consult the user with evidence and alternatives for value decisions, required authorization, irreversible effects, or no justifiable continuation; await direction only on that decision. Follow any explicit user policy requiring consultation.

For an active VFS claim, releasing and discarding follow takt-vfs-coordination.

## Exhausted recoveries

After {{policy.recovery_failures}} consecutive failed recoveries of the same objective, escalate (a second failing verdict for the same author key counts as one failed recovery); no further recovery
is authorized merely by sending the escalation. An exhausted recovery means the real state is unknown: the escalation offers the blind review pair over that objective before any allowance, and an allowance granted afterward is spent on a strategy rebuilt from the pair's findings. Continuing an abandoned recovery scope is proposed with `dispatch_exception` on that objective's attempts or actions, which asks the user; the decision to drop it, with `dispatch_restore`. After consolidation, repair forward.
