---
name: takt-sdd-recovery
description: "An SDD implementation task failed twice, or an applier flagged a contract defect, and the standard takt-sdd-workflow dispatch loop can't carry it further. Explains freezing the affected subtree, escalating to the right contract owner, and resuming without collapsing the rest of the plan."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Takt SDD Recovery

Failure and replanning protocol for an implementation DAG already built under
takt-sdd-workflow. It applies once a dispatched task fails a second time, or an applier
flags a contract defect — never for a first failure within authorized recovery bounds,
which retries in place without this protocol.

```
[Minor Failure in Bounds] ──▶ [Retry Once with Error Context]
                                   │
                    ┌──────────────┴──────────────┐
                    ▼ (Success)                   ▼ (Fails 2nd time or Defective Contract)
              [Resume DAG]               [Trigger Replanning Circuit]
                                                  │
                                                  ├── 1. FREEZE downstream dependent subtree
                                                  ├── 2. KEEP independent parallel lanes running
                                                  ├── 3. ESCALATE to relevant contract owner
                                                  ├── 4. OWNER emits delta; TAKT re-indexes DAG
                                                  └── 5. UNFREEZE & DISPATCH updated nodes
```

1. **Initial retry:** for a minor failure within authorized recovery bounds, retry once —
   reassign its scope with its `author_key`, keeping its staged work, and delegate the same node name again — with
   the exact error output and stack trace. A contract defect goes directly to its owner.
   Do not retry an objective-blocking discovery without direction: trigger the
   Replanning Circuit instead.
2. **Freeze subtree** (the DAG's backtracking step): on a second failure, or when an
   applier flags a contract defect, mark the task `BLOCKED` and freeze its downstream
   dependents. **Do NOT stop independent parallel lanes** — tasks with disjoint write sets
   and unrelated dependencies keep running. When two in-flight tasks produced conflicting
   deltas rather than one simply failing, freeze both and escalate the conflict to the
   owner of the contract they share, keeping both deltas staged so neither is silently
   dropped.
3. **Escalate:** structural/interface failure → `architect`; behavioral/acceptance
   failure → `spec`; experience/identity failure → `product-designer`; scope
   tradeoff → PM and the user. Provide the failed task ID, evidence, and contract segment.
4. **Contract delta & re-index:** the owner emits a contract delta, naming the exact prior
   version it replaces and what it adds or retires; TPM revises affected task decomposition
   if needed. Takt alone restructures the sub-DAG, commits the revised plan with
   `dispatch_commit` naming the standing version as its base, checks file ownership
   against in-flight work, and re-validates the affected region's wave width and
   first-wave count before dispatching it again.
5. **Resume:** re-queue only after correction and any required approval. Budget
   exhaustion abandons the recovery scope until the user decides (see the escalation
   rules). Consolidated work requires forward repair.

**Convergence criterion.** If one node has consumed two contract deltas without
converging, reassess the decomposition cut instead of emitting a third delta. This
diagnostic rule never overrides recovery bounds or authorizes continuation after failure.
