# PRD: Garbage Collection Cycles

## 1. Functional Requirements

### 1.1 Scope

| ID | Requirement |
| --- | --- |
| PR-MNT-4 | Each cycle carries a bounded scope declared before it starts: one mandate class — removal of dead code, complexity, duplication, documentation, or analyzer integrity — over the causal closure of the delta — what the changes introduced or made stale, following reachability outward, never merely the set of files opened. Work exceeding that bound is left to the next cycle, never batched into one intervention. |

### 1.2 Collector (Workspace)

| ID | Requirement |
| --- | --- |
| PR-MNT-13 | Within a GC cycle, the collector works over the workspace the crew is building. Its strict mandate is realigning the workspace to the structural doctrine and eliminating debris, without altering design or functionality: removing dead code, cleaning verbose generated comments, extracting magic values into constants, deduplicating logic, pruning defensive checks that compensate for a misplaced validation boundary, simplifying unjustified abstractions, and reducing cyclomatic complexity. |
| PR-MNT-14 | A cycle MUST be strictly behavior-preserving. The collector MUST NOT alter existing design, change functionality, add capability, or touch public contracts; if behavior changes, the delta is considered damage and rejected. Any finding requiring design or behavioral changes is reported, never applied. |
| PR-MNT-15 | The collector mutates the workspace through VFS. Before consolidation, a distinct verification instance evaluates its delta and runs the behavior-preservation checks against an isolated merged view. |
| PR-MNT-16 | A cycle whose delta fails verification or whose acceptance checks regress MUST be discarded in full before consolidation, never partially applied. Effective termination is confirmed before discard. A discarded cycle is itself a problem-rate signal. |
| PR-MNT-17 | The collector acts on its own authority only where the finding rests on an already declared norm whose conformance a tool can check, or on a tool-computed fact the investigation of `PR-MNT-24` failed to refute. A norm declared only in prose grounds a proposal, never autonomous action. Where neither holds, or where the evidence is weak, the collector reports a proposal, does not apply it, and continues admissible work without waiting for confirmation. Any decision and subsequent application belong to the ordinary crew flow, outside this cycle. The collector never derives a norm of its own. |
| PR-MNT-24 | Dead-code, complexity, and duplication findings MUST originate from language-appropriate static analysis of the workspace — the code graph together with the language's own analyzer — never from agent inference alone. A specialist then investigates each finding to refute it, seeking evidence the analysis could not see: dynamic dispatch, reflection, configuration-driven wiring, framework entrypoints, serialization, and re-exports. The outcome of that refutation is recorded with the delta. Analyzer coverage per language is a declared capability recorded with the cycle: a workspace without coverage is a recorded gap, never a silent omission. |
| PR-MNT-25 | Code whose only callers are tests, and which is not part of a declared public contract or architectural export boundary, is either dead code the test masks or support code that serves only the test suite; each detection is classified by evidence, and only masked dead code is a dead-code finding. It is computed with the test tree excluded from the call graph. A test that the removal leaves failing is removed only because of it, never as an objective, and in a delta after the verdict on the removal: the collector never verifies a delta against a suite the same delta trimmed. Tests as an object of work fall outside the collector's mandate. |
| PR-MNT-26 | Newly introduced code that does not yet have a consumer is pending, not dead, and MUST NOT be removed under `PR-MNT-25`. |
| PR-MNT-27 | For complexity and duplication findings the specialist analyzes the code surrounding the finding and produces the smallest change that yields a cleaner equivalent, localized to what the analysis identified. Reach beyond the finding is design work, reported under `PR-MNT-14`. |
| PR-MNT-28 | The collector completes missing docstrings on newly introduced code, and corrects newly introduced ones of low quality: verbose, redundant against reading the function, stating what the code does rather than what it resolves, or narrating past events irrelevant to the implementation that stands. |
| PR-MNT-30 | A finding that requires extended investigation of the codebase, requires architectural reconsideration, or is simply too voluminous to apply as a routine action leaves the collector's responsibility: it is reported without application, and the cycle continues with admissible work without awaiting the user. Follow-up is ordinary crew work under an aligned contract, not a conversion of the cycle into planning. Accumulation and nature are distinct — many small findings over the declared bound are left to the next cycle, while a single finding that is not small leaves the collector's responsibility. |
| PR-MNT-31 | Re-editing of a file after a cycle consolidated it MUST be attributed to that cycle and to the mandate class it declared. |
| PR-MNT-29 | Newly introduced suppression, exclusion, or neutralization of the workspace analysis of `PR-MNT-24` is itself a collector finding. The suppression is visible in the delta and requires no separate baseline; removing it is a proposal under `PR-MNT-17`. |

### 1.3 Automatic Demotion

| ID | Requirement |
| --- | --- |
| PR-MNT-32 | Automatic demotion: a mandate class whose attributed reversions exceed a policy-defined fraction of its registered work, over at least a policy-defined minimum of registered work, loses autonomous action and becomes proposal-only. It is a control action driven by those recorded attributions. The demotion records the reversions, registered work, evaluated range, rate, threshold, and policy version, so replaying the same events under the same policy reproduces it; the demoted class stays persisted for later authorization. |

## 2. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| The investigation refutes an analyzer finding | The finding is discarded and the refutation recorded. It is not a problem-rate signal. |
