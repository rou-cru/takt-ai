---
name: takt-sdd-workflow
description: "Several implementation units must deliver user stories as integrated vertical slices from formal specifications: compiling and running the dependency DAG through frozen contracts, with up to {{policy.concurrent_specialists}} concurrent specialists in one shared workspace."
license: AGPL-3.0
metadata:
  author: takt
  version: "4.0"
---

# Takt SDD & DAG Workflow

Orchestrator operational guide: convert an approved plan into a dependency DAG,
guarantee that every parallel lane is unlocked by a frozen contract, and
sustain execution with bounded concurrent specialists in a single shared workspace.

**When this skill applies.** It governs several implementation units producing an integrated
result. A simple change, a short investigation, or one obvious action
does not need it, and manufacturing a DAG for them is cost with no return. If it was loaded
and the work turns out not to need it, return control to Takt with the reason and evidence
for reevaluation: never build the graph to justify having opened it. Likewise return control,
preserving user constraints, whenever the evidence shows this route does not fit. This exit
does not select a replacement or authorize direct execution.

**The objective outranks the graph.** The DAG is the orchestrator's map — it exists to keep
the work oriented, to surface concurrency the domain actually allows, and to stop a plan
from serializing out of habit. It is never the thing being delivered. Where process
completeness conflicts with what the user asked for, the objective wins: name the trade-off,
record it, and keep moving within the permissions and approvals you were given.

**Input premise.** Start from formal, broken-down specifications with their acceptance, held
in Engram or in a repository artifact of equivalent rigor, and the contracts that exist.
Without them this route does not fit: return control to Takt with that reason. Resolve other
material gaps through the needed owners; do not require a PRD or a ceremonial document chain.
Before implementation dispatch, the relevant acceptance and consumed interfaces must be
settled. Takt compiles the execution DAG from these inputs; planning specialists do not own it.

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
| **Depth** | Number of waves from root to terminal node, inclusive. |
| **In-flight** | A dispatched task whose result has not yet been received. |
| **READY** | Dependencies delivered (consolidated to disk for nodes that read workspace files; author's delivery complete in VFS for verifier nodes) + contract valid + `writable_set` disjoint from every in-flight task. |
| **Information edge** | The consumer needs to *know* a signature, type, schema, or error shape that the other node produces. |
| **Implementation edge** | The consumer needs the other node's file content written to disk, or shares mutable state with it. |
| **Critical path** | The longest remaining downstream chain from a node to the terminal node, measured in nodes. |
| **Vertical slice** | Everything one user story, or one split of it, needs across the layers, ending in a functional result its independent test demonstrates. |
| **Sub-objective** | The fragment of one layer a slice needs; one node, delegated to one agent. |

---

## 1. Lane Discipline

Architecture, experience, specification, proposal, and verdict go to the lane that owns them
in the DAG, whatever dispatching that lane is estimated to cost. A DAG that grows or is
revised mid-execution is not itself drift; only a changed objective or a blocked path is.

Direct execution never skips the DAG's collision discipline: a directly executed change
still declares and respects `writable_set` like any node would. A unit may read any file it
does not own, provided no in-flight task writes it — this section constrains judgment and
writes, not shared access to settled output.

**Plan for safe concurrency.** Governed staging does not replace disjoint scopes and frozen
contracts. Resolve shared access before dispatch rather than relying on reverts after a collision.

**Value over ceremony.** Intermediate artifacts exist strictly to make parallel delegation
safe. If process completeness conflicts with user value, surface the trade-off, recommend a
path, and keep moving.

---

## 2. Protocol Rules

1. **Shared Workspace (No Worktrees):** All tasks execute in the root workspace. Isolation
   comes from disjoint file ownership and frozen interface contracts, never from Git
   branches.
2. **Concurrency Ceiling (Max {{policy.concurrent_specialists}}):** At most {{policy.concurrent_specialists}} specialists run at once, whatever their lane:
   appliers, verifiers and planning lanes share the same slots. Excess READY tasks queue.
   There is no minimum width or occupancy. Do not lower the ceiling out of caution or leave
   ready independent work waiting when capacity is available; do not invent work to fill slots.
3. **Exclusive Writable Ownership:** Every task declares an explicit `writable_set`. Two
   in-flight tasks never have overlapping writable paths.
4. **Frozen Contracts Before Dispatch:** A task may run in parallel only if every type,
   signature, schema, and public interface it consumes is frozen in its contract.
5. **One Integration Gate per Slice:** Appliers run no checks against staged work and never
   invoke global test runners, full workspace builds, or whole-repo linters, as this causes
   tool, cache, and artifact collisions in a shared workspace. Full build, lint, and
   end-to-end tests run exclusively in one Verify unit per vertical slice, on the materialized
   workspace after that slice's writers finish, and it runs the slice's independent test, plus the story's own on a split story's last slice.
   Never parallelize global verification, run it per wave or node, or run it unplanned. A
   milestone commit is a separate decision and follows only after an acceptance passes.
6. **Zero Silent Drops:** A first failure that OPERATIONS routes to retry in place retries once;
   a second failure, any other failure or a flagged contract defect loads `takt-sdd-recovery` and follows it. Never drop or skip a task silently.

---

## 3. Contracts — The Substitute For The Worktree

**This is the mechanism that makes everything else possible.** A worktree isolates *after*
the conflict exists. A contract makes the conflict *nonexistent beforehand*. Rule 4 is not
one rule beside "no worktrees" — it is its counterpart. {{policy.concurrent_specialists}} agents can write into one
workspace without colliding for exactly one reason: none of them needs to read what another
is currently writing. That property is bought by freezing interfaces up front.

### 3.1 The Edge Question

Apply this to every candidate dependency. It is the central operation of planning.

> **Can a settled contract remove this wait?**
> - **Yes:** supply the same contract to producer and consumer and remove the edge.
> - **No:** name the required output or shared resource under §6.

A pending planning or diagnostic result is not a settled interface. Keep that dependency;
never invent the result to manufacture concurrency.

### 3.2 Symmetric Delivery

A frozen signature goes to the agent that **implements** it *and* to every agent that
**consumes** it for unrelated work. Both sides receive the identical declaration.

Resolve consumed interface decisions before parallel dispatch; agents do not independently
redefine the contract they share.

### 3.3 What Gets Frozen

Exact types, exact signatures with their return, schemas, public interfaces, and error
shapes. Required granularity: `int compute_offset(float ratio)` with the return value
declared and its failure behavior stated — not "a function that computes the offset."

### 3.4 Contract Schema

Behavior and interface contracts come from the relevant owners (`spec`,
`architect`, and `product-designer` for experience rules). TPM assembles traceable
task scopes from them; Takt validates and places them into its execution DAG. Every task
contract must declare (TPM's WBS names them Writable files, Consumed contracts, Allowed reads and Acceptance):

- **`writable_set`**: Exact files the task may modify, existing or to be created.
- **`consumed_interfaces`**: Fully declared types, signatures, and schemas the task needs.
- **`allowed_reads`**: Files the task may read. Must exclude every path written by a
  concurrently in-flight task.
- **`acceptance_check`**: A deterministic, bounded command that the Verify unit runs on the
  materialized workspace to prove task completion (e.g., targeting a specific unit test
  file/function); never a global suite, whole-workspace test command or full build (Rule 5).

Example of a complete contract:

```
writable_set:        internal/config/parse.go, internal/config/parse_test.go
consumed_interfaces: func Parse(r io.Reader) (Config, error)  // ErrInvalid on malformed input
                     type Config struct { Port int; Hosts []string }
allowed_reads:       internal/config/types.go
acceptance_check:    go test ./internal/config -run TestParse
```

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
2. **Remove avoidable waits.** Ask the contract owner to settle an interface when that
   enables independent work.

---

## 4. Decomposition — Cutting By Story

This lever operates before deciding which edges must serialize. A wrong cut delays value and,
when it is by layer, makes the DAG serial by construction; no serialization matrix will save it.

- **Cut into vertical slices, one per user story.** A story needing more than one slice gets
  more than one, each ending in something functional; a story is never forced into one
  overloaded slice, and no slice is called complete while its result does not work.
- **Cut each slice into sub-objectives, one layer fragment per agent.** A large slice gains
  agents, never larger sub-objectives. Disjoint fragments run concurrently; a strong
  dependency between them stays an edge (§6).
- **Never cut a layer across stories.** "All the types", "the whole API layer", "all the
  storage" delivers nothing until every layer is done; cut it into each story's fragment.

**Orchestrator action:** return a cut to TPM when it bundles independently deliverable
functional results that admit disjoint scopes and settled contracts, naming those results.
Sharing a specialty, directory or agent, or avoiding coordination, does not justify bundling.
Keep genuinely coupled work coherent; do not create shared files or dependencies to justify
serializing otherwise independent work.

---

## 5. Topology — The Shape Of A Correct DAG

1. **Contracts precede their consumers.** The graph follows actual dependencies, not a
   prescribed diamond, depth or width.
2. **Every edge is justified against a named node, never against "what came before".** In
   the moment waves are treated as phases, implicit barriers appear and cost whole lanes for
   nothing. A test task depends on the one node whose files it reads, not on the wave; a blind
   test-author node consumes only frozen contracts and the traceability matrix, never in-flight
   implementation, so it shares the wave of the code it tests. The integration tail depends
   on all implementation nodes it validates.
3. **One global integration fan-in per vertical slice, at its tail.** Global serialization is paid once
   per slice, after the nodes it validates, whether the slice took one phase or several. A
   per-wave integration checkpoint is forbidden by Rule 5; a planned phase verification of
   staged deltas is not one.

### 5.1 Self-Check Before Dispatch

- Every node traces to one story's slice; no node builds a layer for several stories (§4).
- Independent functional results are not hidden inside one delegation (§4).
- Siblings in a wave share no edge; a horizontal edge means a layer cut (§4).
- Every edge names a required output or resource; settled contracts remove avoidable waits.
- Ready independent nodes use available slots; excess work queues without artificial edges.
- Planned gates are clearly fewer than implementation nodes; slice acceptance Verify nodes
  are not counted.
- **Width never triggers a rebuild.** A narrow or fully serialized graph whose every edge names
  an admissible §6 reason is valid as recorded; only an edge without such a reason is removed.

### 5.2 Reference material

`references/topology-reference.md` holds the named anti-pattern list for diagnosing a DAG
that came out wrong, and worked examples with a diagram. Read it when a
self-check fails or a new case needs a concrete comparison — not before every dispatch.

---

## 6. Serialization — The Burden Of Proof

**Parallel is the default. Serializing is the exception, and it requires naming a
condition from the closed list below.** An edge that cannot name one does not exist.

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
| A consumes a planning or diagnostic result that B has not established | Serialize A after B | An unknown result cannot be replaced with an invented contract. |
| A reads a file that in-flight B writes | Serialize A after B | Eliminates read-write races. |
| A and B write the same file | Chain A → B | Prevents overwrites. |
| Shared package/lock manifest (`package.json`, `go.mod`, `Cargo.toml`) | Serialize behind the writer | Prevents manifest corruption. |
| Shared mutable state (database, ports, global singletons) | Serialize | Avoids runtime collisions. |
| A verification node judges the staged deltas of its authors | Serialize the verification after those authors, and an author's consolidation after its verdict | A verdict covers exactly the revision it judged. |

### Dispatch Logic

1. Collect all READY tasks.
2. Dispatch all READY tasks that fit the available slots under the {{policy.concurrent_specialists}}-specialist ceiling; queue the rest.
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
[formal specs + contracts (invariant planning or existing)]
                  │
       [TPM: WBS, first node]
                  │
       [Takt: compile + validate execution DAG]
                  │
       [ready work] ──▶ [integration] ──▶ [acceptance]
```

1. **Planning:** consume the specifications and contracts the route starts from. For gaps the
   DAG exposes, dispatch contract owners concurrently only where their inputs are settled and
   scopes independent. Compare their results and return
   conflicts to the owners before consuming them; escalate changes to intent or authority.
2. **Validation:** Takt runs the dependency and decomposition self-check before `dispatch_commit`, without manufacturing work just to
   fill concurrency slots.
3. **Compilation:** TPM is the first node: it turns the specifications into vertical tasks
   with their hard dependencies. Takt compiles the DAG from its WBS, commits it with
   `dispatch_commit` (each node a unit with its contract and prerequisites), and assigns each node's contract; no specialist silently changes another's
   judgment.
4. **Execution:** dispatch READY nodes as dependencies clear, each delegation named with its
   node's committed unit identity. A returned delivery is complete in VFS and nothing more:
   take it where its contract sends it (a verifier, consolidation, or straight to disk). Work left staged has not been delivered. Ordinary handoffs reuse approvals; new final designs or scope
   decisions obtain the required user approval.
5. **Integration and acceptance:** preserve one tail fan-in per slice. When a phase calls for a gate,
   it assesses the phase's staged deltas together before consolidation: one verifier node, preassigned
   to all its author keys, and Takt consolidates a passing author's work with
   `vfs_consolidate`. The integration node reads for loose ends, subtle misalignments and code
   left dead; it runs no checks. Executable acceptance runs in a Verify unit on the
   materialized workspace after writers finish; with QA planned, that acceptance and the
   tail after it follow `takt-qa-workflow` from its Accept state.
   Check the integrated outcome, not merely completed nodes.
6. **Slice control point:** once a slice is accepted, evaluate what happened and where the
   work goes next before the next slice. When the user participates, tell them how to try the
   result and ask whether to refine or change it or to continue. Working autonomously, act on
   the evaluation: drop work the result shows unnecessary, add a prerequisite it reveals, or
   route a refactor it calls for (duplication, a missing single source of truth, speculative
   generality). A change alters the story first, the invariants derived from it realign, and
   then you commit the plan revision.

Deliver one story's slice before starting another's, unless each story is simple, its work
mechanical, their scopes disjoint and repairing both at once unlikely; then their slices may run
together. A user instruction on story concurrency prevails.

---

## 9. DAG Presentation

When presenting DAG state, render clean ASCII only: top-to-bottom flow, boxed
`[Task ID: 2-5 word label]` nodes, horizontal wave bands, and per-wave width plus a short
edge justification beside the diagram. Never substitute raw tables, JSON, or exhaustive
text dumps for the diagram.
