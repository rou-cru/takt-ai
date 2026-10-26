# Topology Reference

## Named Anti-Patterns

- **Wave barrier:** an edge drawn against a whole wave instead of a named node.
- **Intermediate integration checkpoint:** any global build/test before a slice's tail fan-in.
- **Unscoped applier verification:** an applier running global test suites, whole-repo linters, or broad builds instead of targeted checks, inducing tool, cache, or port collisions across concurrent appliers.
- **Horizontal cut:** decomposition by layer across stories, so nothing works until every
  layer is done and each layer waits on the one beneath it.
- **Overloaded slice:** a story forced into fewer, larger delegations to look like one slice,
  instead of more agents or more slices.
- **Convenience bundling:** independently deliverable functional results assigned to one
  agent merely to avoid planning or because they share a specialty or directory.
- **Defensive serialization:** an edge added because a conflict was *not ruled out*, rather
  than because a closed serialization reason names it. Rule it out with the Edge Question instead.
- **Post-failure collapse:** abandoning the plan and folding remaining work into one
  delegation after a failure, instead of freezing the affected subtree and loading `takt-sdd-recovery`.

## Integrated delivery example

Story S1: a user requests a processed value through the UI. Its slice needs one fragment of
storage, one of the API and one of the UI. This graph illustrates that slice once contracts
settle; it is not a required shape.

```
                       ┌─────────────────────┐
                       │ C  TPM WBS+contract │   ← TPM WBS over settled stories, specs and contracts; Takt binds
                       └──────────┬──────────┘
          ┌─────────────┬─────────┴───┬─────────────┐
          ▼             ▼             ▼             ▼
     ┌─────────┐   ┌─────────┐   ┌─────────┐   ┌─────────┐   Wave 1 · width 4 · 4 in flight
     │S1 store │   │ S1 API  │   │ S1 UI   │   │S1 Q2    │
     │fragment │   │fragment │   │fragment │   │ tests   │
     └────┬────┘   └────┬────┘   └────┬────┘   └────┬────┘
          └─────────────┴──────┬──────┴─────────────┘
                               ▼
                      ┌─────────────────┐
                      │ I   integration │    ← Fan-in · single integration owner for S1
                      └────────┬────────┘
                               ▼
                      ┌─────────────────┐
                      │ V  verify S1    │    ← Acceptance · full build + test + S1 independent test
                      └────────┬────────┘
                               ▼
                      ┌─────────────────┐
                      │ slice control   │    ← Evaluate, revise stories or plan, then S2
                      │ point           │
                      └─────────────────┘
```

Three topological readings that matter more than the labels:

- **Every node belongs to S1.** Each one builds only the fragment of its layer that S1 needs,
  so the slice ends working. No node builds storage, API or UI for S2 meanwhile.
- **The absent edge `S1 API → S1 UI` is what frozen contracts buy.** The UI fragment consumes
  the API contract, not the API implementation, so the consumer never waits on the producer.
- **`S1 Q2` has no edge to `S1 API`.** The test author writes its own test files from the frozen
  contracts and the traceability matrix, never reading `api/*` while the API fragment is
  written, so it shares the wave with the code it tests.

With QA planned, the acceptance tail is `takt-qa-workflow`'s: `V` is its Accept state, and
the critique and close states run for must-not-fail stories or at the objective's close.

## Contrasting cases

| Case | Correct treatment |
| --- | --- |
| Four agents build the whole database for every story in one phase | Wrong: one agent builds the storage fragment S1 needs, beside S1's API and UI fragments |
| One story needs more work than its agents can carry | Add agents with disjoint fragments, or split the story into more slices; never overload a delegation |
| Two independent functional units with settled contracts | Dispatch both; do not manufacture additional work. |
| A consumes B's actual generated file and cannot use a settled contract instead | Keep B → A, even if the resulting graph is entirely serial. |
| Several independent results share one specialty | Separate units; the specialty label does not justify one oversized delegation. |
| An API signature is settled and producer/consumer scopes are disjoint | Share the signature and remove the implementation wait. |
| A diagnosis determines what repair is needed | Wait for the diagnosis; do not invent its finding. |
