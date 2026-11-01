---
name: takt-sdd-recovery
description: "Committed SDD work fails twice or exposes a contract discrepancy. Route bounded diagnosis to its owner and resume only the corrected subtree."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
---

# Takt SDD Recovery

Use this route after a second failure or a code–contract discrepancy in committed SDD
work. A first localized failure within the unit's scope and acceptance retries in place.

1. Freeze the affected task and its dependents; keep independent lanes running. For
   conflicting staged deltas, freeze both and preserve them for diagnosis.
2. Send the task ID, evidence and contract segment to its owner: structure/interfaces
   → `architect`; behavior/acceptance → `spec`; experience/identity → `product-designer`.
   Use PM for scope analysis; reserve changes to user value or intent for the user.
   A discrepancy requires diagnosis, not an assumption that the contract is wrong.
3. Consume the owner's correction and revised decomposition where needed. Rebuild only
   the affected sub-DAG. Commit its revision with `dispatch_commit` and the standing
   `base_version`; check ownership and ready parallel work before redispatch.
4. Resume after correction and required approvals, within remaining bounds. Repair
   consolidated work forward. Exhausted scope requires a user-approved finite allowance;
   a plan revision or contract delta does not reset consumption.

After two nonconverging contract deltas, reassess the decomposition instead of issuing a
third. Resolve technical uncertainty within authority; consult the user for value decisions,
required authorization, irreversible effects, or no justifiable continuation. Follow any
explicit user policy requiring consultation.
