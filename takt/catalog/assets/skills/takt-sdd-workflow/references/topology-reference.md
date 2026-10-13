# Topology Reference

## Named Anti-Patterns

- **Wave barrier:** an edge drawn against a whole wave instead of a named node.
- **Intermediate integration checkpoint:** any global build/test before the tail fan-in.
- **Unscoped applier verification:** an applier running global test suites, whole-repo linters, or broad builds instead of targeted checks, inducing tool, cache, or port collisions across concurrent appliers.
- **Horizontal cut:** decomposition by layer, producing a chain of layer dependencies.
- **Defensive serialization:** an edge added because a conflict was *not ruled out*, rather
  than because a closed serialization reason names it. Rule it out with the Edge Question instead.
- **Post-failure collapse:** abandoning the plan and folding remaining work into one
  delegation after a failure, instead of freezing the affected subtree and loading `takt-sdd-recovery`.

## Canonical DAG

```
                       ┌─────────────────────┐
                       │ C  TPM WBS+contract │   ← TPM WBS over settled specs and contracts; Takt binds
                       └──────────┬──────────┘
        ┌─────────────┬───────────┼─────────────┬─────────────┐
        ▼             ▼           ▼             ▼             ▼
   ┌─────────┐   ┌─────────┐ ┌─────────┐   ┌─────────┐   ┌─────────┐   Wave 1 · width 5 · 4 in flight, 1 queued
   │ T1 types│   │ T2 API  │ │ T3 queue│   │ T4 UI   │   │ T5 Q2   │
   │ + store │   │ handler │ │ worker  │   │ view    │   │ tests   │
   └────┬────┘   └────┬────┘ └────┬────┘   └────┬────┘   └────┬────┘
        └─────────────┴───────────┼─────────────┴─────────────┘
                                  ▼
                         ┌─────────────────┐
                         │ I   integration │    ← Fan-in · Single integration owner
                         └────────┬────────┘
                                  ▼
                         ┌─────────────────┐
                         │ V  verify + e2e │    ← Acceptance · Full build + test + e2e
                         └────────┬────────┘
                    ┌─────────────┴─────────────┐
                    ▼             ▼             ▼
               ┌─────────┐   ┌─────────┐   ┌─────────┐   ← Only what the test plan selects
               │ M mutate│   │X explore│   │Q quality│
               └────┬────┘   └────┬────┘   └────┬────┘
                    └─────────────┼─────────────┘
                                  ▼
                         ┌─────────────────┐
                         │ VC verify close │    ← Consumes their reports · matrix evidence · completion report
                         └─────────────────┘
```

Mean width: 5 implementation nodes ÷ 1 wave = 5. Passes the wave-width self-check.

Three topological readings that matter more than the labels:

- **The absent edge `T2 → T4` is what this skill buys.** The UI consumes the API contract,
  not the API implementation. The signature lives in `C`, so the consumer never waits on the
  producer. Every lane in wave 1 exists because some edge was dissolved this way.
- **`T5` has no edge to `T2`.** The test author writes its own test files from the frozen
  contracts and the traceability matrix, never reading `api/*` while `T2` writes it, so it
  shares the wave with the code it tests.
- **The tail is `takt-qa-workflow`'s.** `V`, `M`, `X`, `Q` and `VC` are its Accept, Critique
  and Close states drawn as nodes; only what the test plan selects appears. `VC` gathers
  reports, not implementation nodes, so it is no second integration fan-in.
