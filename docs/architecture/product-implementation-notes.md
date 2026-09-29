# Product implementation notes

Non-normative implementation decisions extracted from the product corpus. These notes do not define acceptance, waive requirements, or establish that an implementation has been verified.

## Memory provider representation

For the authorship and evolution contracts in [ARCH_MEMORY](../../product/meta-harness/context/ARCH_MEMORY.md) (`MEM-AUT-2`, `MEM-AUT-6`, `MEM-AUT-7`, `MEM-OPS-2`, `MEM-OPS-5`), the selected representation assigns the author through the provider's `tool_name` field and uses provider relations for evolution links and session anchors. The harness maintains a per-session ledger of created entries.

The recorded provider limitation is that entry relations are not exposed for query. This is an implementation assumption to revalidate against the provider version, not a product guarantee or evidence that retrieval satisfies the memory contract.

## VFS storage

The selected storage design uses separate local SQLite records for staging and the content-free Action Journal, in private storage outside the workspace. SQLite durability alone does not make materialized files transactional. The required behavior remains defined by [PRD_VFS](../../product/meta-harness/harness/PRD_VFS.md), especially `PR-VFS-CSL-1` and `PR-VFS-JRN-1`–`3`.
