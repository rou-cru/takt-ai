# PRD: Garbage Collection Cycles

## 1. Functional Requirements

### 1.1 Cycle Control

| ID | Requirement |
| --- | --- |
| PR-MNT-33 | GC is the actuator of the loop whose controlled variable is crew capability (`PR-OBS-EST-1`). It does not hold the workspace to a target: it removes accumulated entropy so the crew keeps working on maintainable code. |
| PR-MNT-34 | A cycle starts from a control decision over the recorded estimate (`PR-OBS-EST-9`): entropy level, its rate of change, risk along the execution route (`PR-OBS-EST-7`), and capability alarms (`PR-MNT-9`, `PR-MNT-10`) over a region whose entropy is elevated. Activity volume or elapsed time alone never makes a cycle due. The decision is deterministic and records the estimate and uncertainty it read, the policy version, the selected region and mandate class, and the authority mode. |
| PR-MNT-35 | The control law rejects disturbances: it selects the minimum sufficient cycle, does not launch on variation within its deadband, applies hysteresis between launch and release, and does not launch again over a region until the previous cycle's effect on it has been measured. Successive disturbances, including those arriving during recovery, are absorbed without oscillation against crew work. While estimate uncertainty exceeds the policy bound, no cycle launches autonomously. |
| PR-MNT-36 | A cycle runs in its own maintenance execution, only while no crew session of the workspace is active, and never alongside orchestrated work. A decision reached while a session is active is held with its evidence and re-evaluated against the current estimate once none is. |
| PR-MNT-37 | Authority over launch follows the user's chosen mode: manual, where the user launches; assisted, where the harness presents the decision with its evidence and the user decides; or autonomous, where the harness launches. The mode changes only who authorizes; estimation and the control law are identical in every mode. |

### 1.2 Scope

| ID | Requirement |
| --- | --- |
| PR-MNT-4 | Each cycle carries a bounded scope declared before it starts: one mandate class — removal of dead code, complexity, duplication, documentation, or analyzer integrity — over the region the control decision of `PR-MNT-34` selected, followed along reachability outward, never merely the set of files opened. Work exceeding that bound is left to the next cycle, never batched into one intervention. |

### 1.3 Collector (Workspace)

| ID | Requirement |
| --- | --- |
| PR-MNT-13 | Within a GC cycle, the collector works over the workspace the crew is building. Its strict mandate is realigning the workspace to the structural doctrine and eliminating debris, without altering design or functionality: removing dead code, cleaning verbose generated comments, extracting magic values into constants, deduplicating logic, pruning defensive checks that compensate for a misplaced validation boundary, simplifying unjustified abstractions, and reducing cyclomatic complexity. |
| PR-MNT-14 | A cycle MUST be strictly behavior-preserving. The collector MUST NOT alter existing design, change functionality, add capability, or touch public contracts; if behavior changes, the delta is considered damage and rejected. Any finding requiring design or behavioral changes is reported, never applied. |
| PR-MNT-15 | The collector mutates the workspace through VFS. Before consolidation, a distinct verification instance evaluates its delta and runs the behavior-preservation checks against an isolated merged view. |
| PR-MNT-16 | A cycle whose delta fails verification or whose acceptance checks regress MUST be discarded in full, never partially applied. Effective termination is confirmed before discard. A discarded cycle is itself a problem-rate signal. |
| PR-MNT-17 | The collector acts on its own authority only where the finding rests on an already declared norm whose conformance a tool can check, or on a tool-computed fact the investigation of `PR-MNT-24` failed to refute. A norm declared only in prose grounds a proposal, never autonomous action. Where neither holds, or where the evidence is weak, the collector reports a proposal, does not apply it, and continues admissible work without waiting for confirmation. Any decision and subsequent application belong to the ordinary crew flow, outside this cycle. The collector never derives a norm of its own. |
| PR-MNT-24 | Dead-code, complexity, and duplication findings MUST originate from language-appropriate static analysis of the workspace — the code graph together with the language's own analyzer — never from agent inference alone. A specialist then investigates each finding to refute it, seeking evidence the analysis could not see: dynamic dispatch, reflection, configuration-driven wiring, framework entrypoints, serialization, and re-exports. The outcome of that refutation is recorded with the delta. Analyzer coverage per language is a declared capability recorded with the cycle: a workspace without coverage is a recorded gap, never a silent omission. |
| PR-MNT-25 | Code whose only callers are tests, and which is not part of a declared public contract or architectural export boundary, is either dead code the test masks or support code that serves only the test suite; each detection is classified by evidence, and only masked dead code is a dead-code finding. It is computed with the test tree excluded from the call graph. A test that the removal leaves failing is removed only because of it, never as an objective, and in a delta after the verdict on the removal: the collector never verifies a delta against a suite the same delta trimmed. Tests as an object of work fall outside the collector's mandate. |
| PR-MNT-26 | Newly introduced code that does not yet have a consumer is pending, not dead, and MUST NOT be removed under `PR-MNT-25`. |
| PR-MNT-27 | For complexity and duplication findings the specialist analyzes the code surrounding the finding and produces the smallest change that yields a cleaner equivalent, localized to what the analysis identified. Reach beyond the finding is design work, reported under `PR-MNT-14`. |
| PR-MNT-28 | The files the crew touched are left with complete docstrings of good quality: the collector completes the missing ones and corrects those of low quality: verbose, redundant against reading the function, stating what the code does rather than what it resolves, or narrating past events irrelevant to the implementation that stands. |
| PR-MNT-30 | A finding that requires extended investigation of the codebase, requires architectural reconsideration, or is simply too voluminous to apply as a routine action leaves the collector's responsibility: it is reported without application, and the cycle continues with admissible work without awaiting the user. Follow-up is ordinary crew work under an aligned contract, not a conversion of the cycle into planning. Accumulation and nature are distinct — many small findings over the declared bound are left to the next cycle, while a single finding that is not small leaves the collector's responsibility. |
| PR-MNT-31 | Re-editing of a file after a cycle consolidated it MUST be attributed to that cycle and to the mandate class it declared. |
| PR-MNT-29 | Newly introduced suppression, exclusion, or neutralization of the workspace analysis of `PR-MNT-24` is itself a collector finding. The suppression is visible in the delta and requires no separate baseline; removing it is a proposal under `PR-MNT-17`. |

### 1.4 Automatic Demotion

| ID | Requirement |
| --- | --- |
| PR-MNT-32 | Automatic demotion: a mandate class whose attributed reversions exceed a policy-defined fraction of its registered work, over at least a policy-defined minimum of registered work, loses autonomous action and becomes proposal-only. It is a control action driven by those recorded attributions. The demotion records the reversions, registered work, evaluated range, rate, threshold, and policy version, so replaying the same events under the same policy reproduces it; the demoted class stays persisted for later authorization. |

## 2. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| The investigation refutes an analyzer finding | The finding is discarded and the refutation recorded. It is not a problem-rate signal. |
