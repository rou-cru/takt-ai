---
name: takt-sdd-workflow
description: "Trigger: development workflow, full cycle, implement feature, DAG orchestration, plan the work, parallel tasks. Plans work as a dependency DAG biased toward maximum concurrency, converts information dependencies into frozen contracts, dispatches max 4 concurrent specialists, no worktrees. Orchestrator-only guide."
license: AGPL-3.0
metadata:
  author: takt
  version: "3.0"
---

# Takt SDD & DAG Workflow

Orchestrator operational guide: convert an approved plan into a maximally concurrent
dependency DAG, guarantee that every parallel lane is unlocked by a frozen contract, and
sustain execution with bounded concurrent specialists in a single shared workspace.

> **Target Audience:** Orchestrator agent only. Sub-agents consume individual task
> contracts, not this skill.

**When this skill applies.** It governs a delivery cycle spanning several lanes or
concurrent implementation. A simple change, a short investigation, or one obvious action
does not need it, and manufacturing a DAG for them is cost with no return. If it was loaded
and the work turns out not to need it, drop it: never build the graph to justify having
opened it.

**The objective outranks the graph.** The DAG is the orchestrator's map — it exists to keep
the work oriented, to surface concurrency the domain actually allows, and to stop a plan
from serializing out of habit. It is never the thing being delivered. Where process
completeness conflicts with what the user asked for, the objective wins: name the trade-off,
record it, and keep moving within the permissions and approvals you were given.

**Input premise.** Start from the approved objective or sufficient brief and the artifacts
that exist. Resolve material gaps through the needed owners; do not require a PRD or a
ceremonial document chain. Before implementation dispatch, the relevant acceptance and
consumed interfaces must be settled. Takt compiles the execution DAG from these inputs;
planning specialists do not own it.

**How to read this skill.** Every rule states its own applicability test. Where a decision is genuine judgment, the section says so and
supplies the deciding criterion. Judgment applies this skill to the case at hand; it never
decides what a rule means.

---

## 0. Operative Definitions

These terms appear inside rules. They carry exactly these meanings and no others.

| Term | Definition |
|---|---|
| **Node** | One task with one contract and one owner. |
| **Edge** | A directed dependency `A → B`: B may not start until A completes. |
| **Wave** | The set of nodes at equal topological depth from the root. A wave is a *result* of computing the order — never a planned phase, never a synchronization point. |
| **Width of a wave** | Node count in that wave. |
| **Mean width** | Implementation nodes ÷ number of waves containing implementation nodes. Excludes the contract root and the integration/verification tail. |
| **Depth** | Number of waves from root to terminal node, inclusive. |
| **In-flight** | A dispatched task whose result has not yet been received. |
| **READY** | Dependencies complete + contract valid + `writable_set` disjoint from every in-flight task. |
| **Information edge** | The consumer needs to *know* a signature, type, schema, or error shape that the other node produces. |
| **Implementation edge** | The consumer needs the other node's file content written to disk, or shares mutable state with it. |
| **Critical path** | The longest remaining downstream chain from a node to the terminal node, measured in nodes. |

---

## 1. Lane Discipline

**The orchestrator does not absorb a reachable lane's judgment.** Architecture, experience,
specification, proposal, and verdict go to the lane that owns them, whatever dispatching
that lane is estimated to cost. Everything else is a judgment call the orchestrator makes
normally: it acts directly when no unit could own the change with a sensible contract (for
example, a single-line fix with no delegable scope), in the mechanical work of its own
responsibilities, and when the user asks for it. Acting directly is not a failure of
orchestration; absorbing an available lane is. If the particular owner is absent, apply
the crew contract's declared fallback within existing permissions; self-checking cannot
replace a required independent verdict. A DAG that grows or is revised mid-execution is not
itself drift; only a changed objective or a blocked path is.

Direct execution never skips the DAG's collision discipline: a directly executed change
still declares and respects `writable_set` like any node would. A unit may read any file it
does not own, provided no in-flight task writes it — this section constrains judgment and
writes, not shared access to settled output. A file an in-flight task is writing is off limits in every mode, reading
included — see §3.4 `allowed_reads` and §6.

**Generating nothing does not make it a dispatcher.** A dispatcher wastes the agent: it
introduces leveraged errors, adds process friction, and guarantees no real progress. The
orchestrator is not a scheduler, but it must **think like one** most of the time. It does
so algorithmically — and the algorithm is not rigid, because it resolves against the
context and semantics of the moment. Understand the logic in this skill and apply it to
the session at hand. Do not follow fixed steps, and do not improvise from open-ended
judgment: that would make the outcome depend on which model happens to be reasoning and on
nothing going badly wrong along the way.

**No worktrees is a planning rule, not an isolation rule.** Governed staging does not replace correct planning. The orchestrator must face the concurrency risk directly, estimate
it, and plan with design guarantees — not with trial and error backed by reverts. Leaning
on a tool to resolve concurrency is abdicating the job.

**Value over ceremony.** Intermediate artifacts exist strictly to make parallel delegation
safe. If process completeness conflicts with user value, surface the trade-off, recommend a
path, and keep moving.

---

## 2. Invariant Rules

1. **Shared Workspace (No Worktrees):** All tasks execute in the root workspace. Isolation
   comes from disjoint file ownership and frozen interface contracts, never from Git
   branches. See §1.
2. **Concurrency Ceiling (Max 4):** At most 4 specialists run at once, whatever their lane:
   appliers, verifiers and planning lanes share the same slots. Excess READY tasks queue.
   **4 is an empirical maximum**, derived from running this
   method across distinct complex projects with distinct models: fewer forces the very
   linearity this skill exists to prevent, and more multiplies the cost of a single
   orchestration error into a random large token burn. Do not negotiate it downward for
   caution — a narrow DAG is the failure mode, not the safe choice.
3. **Exclusive Writable Ownership:** Every task declares an explicit `writable_set`. Two
   in-flight tasks must NEVER have overlapping writable paths.
4. **Frozen Contracts Before Dispatch:** A task may run in parallel ONLY IF every type,
   signature, schema, and public interface it consumes is frozen in its contract. See §3.
5. **Single Integration Gate (Targeted Applier Scope):** Appliers execute ONLY
   targeted, isolated tests associated directly with their `writable_set`. They must
   NEVER invoke global test runners, full workspace builds, or whole-repo linters during
   task implementation, as this causes tool, cache, and artifact collisions in a shared
   workspace. Full build, lint, and end-to-end tests run exclusively in the single
   downstream verification applier after all implementation tasks finish. Never parallelize
   global verification, and never insert an intermediate one.
6. **Zero Silent Drops:** Every task failure triggers explicit retry and escalation per §9.
   Never drop or skip a task silently.

---

## 3. Contracts — The Substitute For The Worktree

**This is the mechanism that makes everything else possible.** A worktree isolates *after*
the conflict exists. A contract makes the conflict *nonexistent beforehand*. Rule 4 is not
one rule beside "no worktrees" — it is its counterpart. Four agents can write into one
workspace without colliding for exactly one reason: none of them needs to read what another
is currently writing. That property is bought by freezing interfaces up front.

### 3.1 The Edge Question

Apply this to every candidate dependency. It is the central operation of planning.

> **Is this edge informational or implementational?**
> - **Informational → it is not an edge.** It is a signature missing from the contract
>   root. Move the signature to the root and delete the edge.
> - **Implementational → it survives** and must be named in §6.

Apparent linearity is, in the overwhelming majority of cases, uncongealed information. An
orchestrator that skips this question produces a chain and blames the problem domain.

### 3.2 Symmetric Delivery

A frozen signature goes to the agent that **implements** it *and* to every agent that
**consumes** it for unrelated work. Both sides receive the identical declaration.

Sending an agent off to "figure out how to do its part" is not delegation. It exports the
interface decision to four agents that cannot talk to each other, and it is precisely the
negligence that later requires a worktree to contain.

### 3.3 What Gets Frozen

Exact types, exact signatures with their return, schemas, public interfaces, and error
shapes. Required granularity: `int compute_offset(float ratio)` with the return value
declared and its failure behavior stated — not "a function that computes the offset."

### 3.4 Contract Schema

Behavior and interface contracts come from the relevant owners (`spec`,
`architect`, and `product-designer` for experience rules). TPM assembles traceable
task scopes from them; Takt validates and places them into its execution DAG. Every task
contract must declare:

- **`writable_set`**: Exact files the task may modify, existing or to be created.
- **`consumed_interfaces`**: Fully declared types, signatures, and schemas the task needs.
- **`allowed_reads`**: Files the task may read. Must exclude every path written by a
  concurrently in-flight task.
- **`acceptance_check`**: A deterministic, bounded command proving task completion
  (e.g., targeting a specific unit test file/function). Must NEVER run global suites,
  whole-workspace test commands, or full builds.

Extending an owner's `writable_set` after dispatch follows the same validation as the original
grant — it is never assumed from silence.

```
[Specialist Authors Contract] ──▶ [Orchestrator Validates] ──▶ [Dispatch to Applier]
                                          │
                                          └── (Incomplete/Unbounded) ──▶ [Reject to Specialist]
```

### 3.5 The Orchestrator's Two Jobs Here

1. **Validate completeness and execution bounds**: reject incomplete contracts or contracts
   specifying broad/unscoped acceptance commands back to their author.
2. **Demand more freezing when the DAG comes out narrow.** Width is negotiable upward:
   one additional frozen signature in the root converts a serialization into two lanes.
   This is the primary lever on concurrency, and it is the orchestrator's to pull.

---

## 4. Decomposition — Cutting For Concurrency

This lever operates *before* §6. If the decomposition is cut wrong, the DAG is serial by
construction and no serialization matrix will save it.

- **Cut vertically, by sub-objective.** Each task owns its own slice of files across
  layers.
- **Never cut horizontally, by layer.** "All the types", "the whole API layer", "all the
  storage" forces every downstream task to wait on the layer beneath it.

**Orchestrator action:** a decomposition that admits no concurrency is rejected back to the
specialist with the re-cut axis named. Validating contract completeness is not enough when
the cut itself makes parallelism impossible.

---

## 5. Topology — The Shape Of A Correct DAG

Five properties. Each is checkable by looking at the graph.

1. **Single planning root, and width is its consequence.** Everything descends from one
   contract node. The first wave does not measure 4 because four independent tasks were
   *found*; it measures 4 because the root froze the interfaces those four lanes consume. Concurrency is not discovered by
   inspecting tasks — it is **manufactured** by demanding more freezing at the root.
2. **Zero edges inside a wave.** Siblings never depend on each other. Any horizontal edge
   between siblings means the cut was by layer; return to §4.
3. **Every edge is justified against a named node, never against "what came before".** In
   the moment waves are treated as phases, implicit barriers appear and cost whole lanes for
   nothing. A test task depends on the one node whose files it reads, not on the wave. The
   tail fan-in is the sole exception: by Rule 5 it depends on all implementation nodes by
   definition. No other node may.
4. **Exactly one fan-in, at the tail.** Global serialization is paid once, at the end. A
   per-wave integration checkpoint is forbidden by Rule 5.
5. **The only admissible linear chain is the tail** (integration → verification), where it
   costs no concurrency.

**General shape: a diamond.** One neck at the top, maximum widening immediately, one neck
at the bottom. Reference proportions: depth 4–5 at width 4.

### 5.1 Self-Check Before Dispatch

Run both. They are arithmetic, not judgment.

- **First implementation wave must reach 4**, unless total implementation nodes are fewer
  than 4. If it does not, return to §3.5 and freeze more. If it still does not after
  freezing, every missing lane must be accounted for by a §6 condition named in writing.
- **Mean width must be ≥ 2.0.** Below that the plan is a chain: rebuild it. A DAG at depth
  8 and width 2 is wrong even when every single edge is individually defensible — that is
  exactly how one arrives there, one defensible edge at a time.

### 5.2 Named Anti-Patterns

- **Wave barrier:** an edge drawn against a whole wave instead of a named node.
- **Intermediate integration checkpoint:** any global build/test before the tail fan-in.
- **Unscoped applier verification:** an applier running global test suites, whole-repo linters, or broad builds instead of targeted checks, inducing tool, cache, or port collisions across concurrent appliers.
- **Horizontal cut:** decomposition by layer, producing a chain of layer dependencies.
- **Defensive serialization:** an edge added because a conflict was *not ruled out*, rather
  than because §6 names it. Rule it out via §3.1 instead.

### 5.3 Canonical DAG

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

Mean width: 5 implementation nodes ÷ 2 waves = 2.5. Passes §5.1.

Two topological readings that matter more than the labels:

- **The absent edge `T2 → T4` is what this skill buys.** The UI consumes the API contract,
  not the API implementation. The signature lives in `C`, so the consumer never waits on the
  producer. Every lane in wave 1 exists because some edge was dissolved this way.
- **`T5` hangs off `T2` alone**, not off wave 1. It writes into `api/*`, which `T2` owns —
  a named implementation edge. Placing `T5` after the full wave would surrender three lanes
  to buy nothing.

---

## 6. Serialization — The Burden Of Proof

**Parallel is the default. Serializing is the exception, and it requires naming a
condition from the closed list below.** An edge that cannot name one does not exist.

**A fully serialized DAG is a planning failure, not prudence.** It is admissible only with
specific, exceptionally strong evidence that this particular case genuinely demands it —
evidence written down, per edge. Absent that, a chain is a bad plan.

### Parallel — no edge

| Condition | Rationale |
|---|---|
| The contract fully declares every interface Task A consumes | Zero runtime reads from another task's output. |
| Tasks A and B read the same immutable file | Read-only access never conflicts. |
| Tasks A and B share a directory or domain tag without file overlap | Domain similarity is not a conflict. |

### Serialize — closed list of admissible reasons

No reason outside this list justifies an edge.

| Condition | Action | Rationale |
|---|---|---|
| A reads a file that in-flight B writes | Serialize A after B | Eliminates read-write races. |
| A and B write the same file | Chain A → B | Prevents overwrites. |
| Shared package/lock manifest (`package.json`, `go.mod`, `Cargo.toml`) | Serialize behind the writer | Prevents manifest corruption. |
| Shared mutable state (database, ports, global singletons) | Serialize | Avoids runtime collisions. |

### Dispatch Logic

1. Collect all READY tasks (per §0).
2. If fewer than 4 specialists are in flight (any lane), dispatch up to the available slots.
3. When `ready_count > available_slots`, prioritize by **critical path** — longest
   remaining downstream chain first.

---

## 7. Specialist Routing

Route by the need table in your OPERATIONS; this skill adds no second one. Every handoff
carries decisions, contracts, evidence, assumptions, and limits. When interfaces prevent
concurrency, return to Architect; behavioral gaps go to Spec and experience gaps to
Designer. TPM revises decomposition when needed; Takt revises the graph. If an owner is
absent, deliver permitted fallback work and name the missing assurance.

---

## 8. Planning & Execution Lifecycle

The following is an available route, not a procession required of every change:

```
[approved objective / brief + existing artifacts]
                  │
       [needed contracts: Spec / Architect / Designer]
                  │       (concurrent where inputs permit)
       [TPM: scoped tasks, if needed]
                  │
       [Takt: compile + validate execution DAG]
                  │
       [ready work] ──▶ [integration] ──▶ [acceptance]
```

1. **Planning:** use approved intent. Dispatch needed contract owners concurrently only
   where their inputs are settled and scopes independent. Compare their results and return
   conflicts to the owners before consuming them; escalate changes to intent or authority.
2. **Compilation:** TPM supplies tasks and justified dependencies when needed. Takt compiles
   the DAG, commits it with `dispatch_commit` (each node a unit with its contract and
   prerequisites), and assigns each node's contract; no specialist silently changes another's
   judgment.
3. **Validation:** Takt runs §5.1 before implementation dispatch, without manufacturing work
   just to fill concurrency slots.
4. **Execution:** dispatch READY nodes as dependencies clear, each delegation named with its
   node's committed unit identity. Ordinary handoffs reuse approvals; new final designs or scope
   decisions obtain the required user approval.
5. **Integration and acceptance:** preserve the single tail fan-in. The independent gate
   assesses staged deltas before consolidation: each verifier runs as its own node, preassigned
   to one author key, and Takt consolidates a passing author's work with `vfs_consolidate`. Executable acceptance runs on the
   materialized workspace after writers finish. Check the integrated outcome, not merely
   completed nodes.

---

## 9. Failure & Replanning Protocol

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
   discard its staged work, reassign its scope, and delegate the same node name again — with
   the exact error output and stack trace. A contract defect goes directly to its owner.
   Predeclare the binary result, failure condition, scope, recoverable point, attempt
   budget. Do not retry an objective-blocking discovery without direction.
2. **Freeze subtree** (this is the DAG's backtracking step): on a second failure, or when
   an applier flags a contract defect, mark the task `BLOCKED` and freeze its downstream
   dependents. **Do NOT stop independent parallel lanes** — tasks with disjoint write sets and
   unrelated dependencies keep running. When two in-flight tasks produced conflicting deltas
   rather than one simply failing, freeze both and escalate the conflict to the owner of the
   contract they share, keeping both deltas staged so neither is silently dropped.
3. **Escalate:** structural/interface failure → `architect`; behavioral/acceptance
   failure → `spec`; experience/identity failure → `product-designer`; scope
   tradeoff → PM and the user. Provide the failed task ID, evidence, and contract segment.
4. **Contract delta & re-index:** the owner emits a contract delta, naming the exact prior
   version it replaces and what it adds or retires; TPM revises affected
   task decomposition if needed. Takt alone restructures the sub-DAG, commits the revised plan
   with `dispatch_commit` naming the standing version as its base, checks file ownership
   against in-flight work, and re-runs §5.1 on the affected region.
5. **Resume:** re-queue only after correction and any required approval. Budget exhaustion
   ends the recovery scope; confirm termination and restoration of unconsolidated state before
   releasing ownership. Preserve unrelated progress. After two consecutive failed recoveries
   of the same objective, report evidence and alternatives and await the user's continuation
   decision; escalation itself authorizes no retry. Consolidated work requires forward repair.

**Convergence criterion.** If one node has consumed two contract deltas without
converging, reassess decomposition under §4 instead of emitting a third delta. This
diagnostic rule never overrides recovery bounds or authorizes continuation after failure.

---

## 10. DAG Presentation

When presenting DAG state, render clean ASCII only: top-to-bottom flow, boxed
`[Task ID: 2-5 word label]` nodes, horizontal wave bands, and per-wave width plus a short
edge justification beside the diagram. Never substitute raw tables, JSON, or exhaustive
text dumps for the diagram.
