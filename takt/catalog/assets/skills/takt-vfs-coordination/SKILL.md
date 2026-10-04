---
name: takt-vfs-coordination
description: "Use when reserving file scope, resolving an ownership collision, releasing a claim, or consolidating staged work."
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
---

# Takt VFS Coordination

Reserve exact implementation paths with `claim_assign`; use `claim_list` to inspect a
collision. Release a prior-session claim by its listed key. For a current-session claim,
ask for explicit confirmation and pass `confirmed: true` after the user agrees. Release
preserves staged content; choose `vfs_discard` when it should be discarded.

Consolidate authorized staged work with `vfs_consolidate` after the applicable delivery
checks. A staged gate uses `claim_assign_verifier` and a verifier's empty-scope binding.
VFS ownership protects against collisions; the delivery contract determines when an
independent verdict is needed. On a physical-base change or unresolved collision, keep
the affected path frozen and report the competing state.
