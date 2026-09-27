# Theoretical Framework: Evolution and Degradation of Agentic Systems

A framework for reasoning about how an agent swarm and the codebase it works on degrade over time. It takes three sources: Lehman's laws (what evolution does), Agent Lifespan Engineering (how agents lose knowledge), and Liu's formalization (where disorder originates). It gives vocabulary and structure, not a quantitative model.

## 1. Lehman's laws (selected)

Lehman observed long-lived software evolving under real use. Five of the laws apply directly to a swarm evolving a codebase.

| Law | Statement | Reading for an agent swarm |
|---|---|---|
| **2. Increasing complexity** | Structure grows more complex as the system changes, unless extra resources go into preserving and simplifying it. | Degradation is the default. Instructions don't count as a resource, only dedicated work does (SlopCodeBench: quality prompts lower initial slop but not the degradation rate). |
| **3. Self-regulation** | Size, release interval and reported errors stay roughly invariant across releases. | A healthy harness keeps a stable baseline. Deviation from it signals a problem; a drop in consistency is a regulation failure, not noise. |
| **4. Organizational stability** | Development rate is roughly constant and independent of the resources applied. | Adding agents or tokens does not raise throughput. |
| **5. Conservation of familiarity** | Everyone involved must keep full understanding of the system; excessive growth per increment destroys that ability. | An agent starts every session with no familiarity; its only capacity is its reliable context. Increment size is bounded by what a fresh agent can understand inside that window. |
| **8. Feedback system** | Evolution is a multi-level, multi-loop, multi-agent feedback system and must be treated as one (FEAST). | The harness *is* the feedback system. Improving one agent locally does not imply improving the system. |

## 2. Agent Lifespan Engineering: aging mechanisms

Even with frozen weights, an agent degrades across sessions. ALE separates four mechanisms, each with its own repair:

| Mechanism | What fails | Typical symptom |
|---|---|---|
| **Compression** | Summarization discards details that are needed later. | Omissions: exact values, names, decisions are lost. |
| **Interference** | Similar accumulated entries crowd out the right one at retrieval time. | Confusion even though the information still exists. |
| **Revision** | Changed or derived state is not updated. | Stale facts, wrong accumulated values. |
| **Maintenance** | Lifecycle events (recompaction, flushing) cause abrupt drops. | Performance cliffs with no change in model or task. |

Diagnosis is counterfactual: substitute a perfect component (perfect recall, gold context) and see whether the failure disappears. That shows whether the fault is in writing, retrieval, use, or lifecycle handling.

Aging is multidimensional, so one symptom can have different causes. Behavioral compliance can stay intact while factual precision decays.

## 3. Liu: formalization of disorder

Liu's quantitative model is **not adopted**: the exponential law is not validated and its entropy measure is bounded. What is kept is the formalization. Disorder is intrinsic to agentic systems, it builds up silently below the detection threshold, and it originates in identifiable layers:

| Layer | Properties |
|---|---|
| **Foundation semantics** | Natural language is ambiguous; input ambiguity propagates without attenuation; sampling yields non-identical outputs. |
| **Inter-agent transmission** | Re-encoding loses information at each handoff; context compression drops content selectively; multi-hop propagation amplifies small deviations; there is no built-in fidelity check. |
| **Memory persistence** | Budget-driven pruning is unrecoverable; retrieval is probabilistic; cross-session memory lacks causal identity; compression changes meaning non-linearly. |
| **Task execution** | Long-range dependencies are limited by context; subtasks can be locally correct and globally inconsistent; error detection is limited by the cascade. |
| **Feedback correction** | Feedback is itself semantically encoded; delay decouples cause from correction; self-evaluation has confirmation bias; corrections from earlier sessions are not guaranteed to persist. |
| **Systemic evolution** | Sub-threshold deviations accumulate until they become detectable; open agent systems have no natural entropy sink; task diversity and system complexity accelerate disorder. |

Engineering consequence: rules kept in agent memory decay, while rules enforced deterministically outside the LLM path do not. Invariants belong in the harness, not in instructions.

## 4. How the sources fit together

- **Lehman** describes *what* happens to a system under evolution: it gets more complex, has a stable rhythm, a familiarity limit, and feedback loops.
- **Liu** locates *where* the disorder behind it is born: in handoffs, memory, execution, self-evaluation, and accumulation.
- **ALE** explains *how* an agent loses the familiarity that Law 5 requires, through four mechanisms that can be diagnosed separately.

Together they support one premise: **degradation is the default state, and preventing it takes dedicated, deterministic, measurable work.** When a flaw has not caused damage yet, that is luck, not quality.

## 5. Conclusion: regulate the rate, don't chase its level

Degradation is the default, so cleanup is not an optimization. It is a permanent component, as critical as a garbage collector. What the system must watch is not an individual agent's decay — that is local — but the system itself: code degradation and organizational stability, measured as the rate of problems appearing (rework, bugs, regressions).

**The controlled variable is the stability of that rate, not its level.** A stable rate means a stable system whatever its value. A rising rate means the organization is destabilizing, which calls for control action or, if the session can no longer contain it, a safe abort. Lowering the base rate — better models, a better harness — is welcome, but it is an optimization, not the design goal: the rate will rise again. The goal is to hold it at a stable point with little or no oscillation.

Consequences:

| Principle | Consequence |
|---|---|
| Flat beats low | Between two harnesses, a higher but flat rate beats a lower but erratic one. An erratic system supports no decision. Comparing means alone misses this. |
| Oscillation is the failure signature | Control systems oscillate from excessive gain or delayed feedback. Both appear here: delay decouples cause from correction (Liu), and recompaction produces abrupt cliffs (ALE maintenance aging). |
| Small and continuous, not big and rare | Late, heavy maintenance is itself a source of instability: debt builds up, a massive cleanup follows, churn brings new bugs. Incremental collection over stop-the-world. |
| A level change is a new baseline | If a better model lowers the rate, re-establish the baseline and watch stability against it. An optimization that lowers the mean while raising oscillation is a regression. |
| The harness measures, never the agent | If success is judged by the rate of problems and the agent reports it, the "pre-existing" excuse follows. The rate must come from deterministic signals: tests that start failing, re-edited files, discarded work. |
| Abort is a control outcome | If oscillation grows despite control, the session is out of control. That is detectable before the final result proves it. |

Two deterministic levels follow, because unlike a garbage collector's reachability, "what is garbage" here is not always decidable: what can be computed (dead code, duplication, complexity metrics, superseded entries) is collected by the harness without intervention; what takes judgment needs a dedicated role, and deletion needs the user.

## References

- M. M. Lehman, *Programs, Life Cycles, and Laws of Software Evolution*, Proc. IEEE, 1980.
- M. M. Lehman et al., *Metrics and Laws of Software Evolution — The Nineties View*, METRICS 1997 (FEAST).
- *Your Agents Are Aging Too: Agent Lifespan Engineering for Deployed Systems*, UT Austin, 2026 — arXiv:2605.26302
- D. Liu, *Silent Failure in LLM Agent Systems: The Entropy Principle and the Inevitable Disorder of Autonomous Agents*, 2026 — arXiv:2606.08162
- G. Orlanski et al., *SlopCodeBench*, 2026 — arXiv:2603.24755
