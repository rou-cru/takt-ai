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
                  │ C  contracts+owners │   ← Relevant owners: Spec, Architect, Designer; TPM scopes; Takt binds
                  └──────────┬──────────┘
        ┌─────────────┬──────┼──────┬─────────────┐
        ▼             ▼             ▼             ▼
   ┌─────────┐   ┌─────────┐   ┌─────────┐   ┌─────────┐    Wave 1 · width 4 · 4/4 in-flight
   │ T1 types│   │ T2 API  │   │ T3 queue│   │ T4 UI   │
   │ + store │   │ handler │   │ worker  │   │ view    │
   └────┬────┘   └────┬────┘   └────┬────┘   └────┬────┘
        │             │             │             │
        │             ▼             │             │
        │        ┌─────────┐        │             │
        │        │ T5 tests│        │             │    Wave 2 · width 1
        │        └────┬────┘        │             │    (T5 writes api/* → after T2 only)
        │             │             │             │
        └─────────────┴──────┬──────┘─────────────┘
                             ▼
                  ┌─────────────────┐
                  │ I   integration │    ← Fan-in · Single integration owner
                  └────────┬────────┘
                           ▼
                  ┌─────────────────┐
                  │ V  verify + e2e │    ← Acceptance · Full build + test + e2e
                  └─────────────────┘
```

Mean width: 5 implementation nodes ÷ 2 waves = 2.5. Passes the wave-width self-check.

Two topological readings that matter more than the labels:

- **The absent edge `T2 → T4` is what this skill buys.** The UI consumes the API contract,
  not the API implementation. The signature lives in `C`, so the consumer never waits on the
  producer. Every lane in wave 1 exists because some edge was dissolved this way.
- **`T5` hangs off `T2` alone**, not off wave 1. It writes into `api/*`, which `T2` owns —
  a named implementation edge. Placing `T5` after the full wave would surrender three lanes
  to buy nothing.
