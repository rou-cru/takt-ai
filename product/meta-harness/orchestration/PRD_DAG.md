# PRD: Execution History and DAG Projection

**Actual implementation progress: 58%** — 30 own requirements: 11 complete, 13 partial, 6 not integrated. `takt/history/history.go`'s append-only store (`history_no_update`/`history_no_delete` triggers, history.go:42-43) and `projection.go`'s pure `Project` (projection.go:281-289) are real and deterministic, and `takt/dispatch/dispatch.go` fully wires admission, termination, contest, and bounded-recovery budgets against that projection (PR-DAG-AUT-1, PR-DAG-MUT-1, PR-DAG-MUT-9, PR-DAG-MUT-10), reachable in production through `dispatch_commit`/`dispatch_declare_recovery`/`dispatch_close_recovery`/`dispatch_restore`/`dispatch_contest` in `takt/agents/opencode/assets/takt-vfs.ts` (lines 1102-1233) and the automatic admit/launch/finish hooks on the `subagent` tool (takt-vfs.ts:860-946). Two structural gaps hold the score down: `history.AuthorVerification` (history.go:57) is declared but never assigned to any recorded `Kind` — no verification verdict or acceptance result ever enters the execution history, so PR-DAG-TMP-6's cross-author precedence cannot be read from this projection even though `takt/vfs/operation.go`'s `ConsolidateCheckpoint`/`admitVerifierLocked` enforce an equivalent precedence at the VFS layer; and plan revision is only partly wired — `dispatch.go` exposes `Commit` (dispatch.go:353) and a version-compared `Declare`/`Revise` (dispatch.go:480-545) that classifies each revision as tactical or strategic against the current plan (`classify`, dispatch.go:627; `KindRevised.Classification`), reachable through `dispatch_commit` with `base_version` (takt-vfs.ts:1102-1120), but no rationale is recorded (PR-DAG-MUT-5), an invalid first baseline is not recorded, and the orchestrator's `takt-sdd-workflow` skill does not mention the classification. Declared (voluntary) backtracking is unmodeled: only forced, budget-triggered backtracking (`forceBacktrackIfExhausted`, dispatch.go:810-838) is wired. A retry opens attempt N+1 of a settled unit (`nextAttempt`, dispatch.go:107-116), but the unit stays settled between attempts, and commits, recoveries, and contests still hardcode `AttemptID: history.FirstAttempt`.

## 1. Problem

The execution DAG represents the orchestrator's strategy during execution, including changes to planned work and the observed results.

### What the execution DAG is

Only the orchestrator creates the execution DAG, usually compiling it from specifications and frozen contracts (`PR-ORQ-15`, `PR-CRW-5`).

The DAG is a mutable strategy, not a reference invariant. Tactical and strategic mutations adjust the route under §2.3; substantial deviations are escalated under `PR-ORQ-5`.

The execution history records observed execution, supports later analysis, and provides the deterministic input for replay. Recorded facts and declared rationales remain distinct (`PR-DAG-AUT-2`).

### Operating model

The orchestrator owns the strategy and decides what to dispatch. The harness is the runtime host and the **only writer and sequencer of the execution history**: it captures actions, assigns their total order, and appends them. The verifier supplies independent verdicts through that same runtime. These are three semantic authors, not three concurrent writers. Concurrent specialist execution does not imply concurrent writes to the execution history.

```text
orchestrator action --\
verifier verdict -----+--> harness: sequence + append --> execution history --> replay
harness control ------/    (preserve semantic author)                           projection
```

The projection records both the planned route and the work actually performed. **Growth of the execution record is not necessarily a mutation of the plan.** A retry continues the same unit; a focused repair adds corrective work associated with a unit without changing the route; a mutation is the orchestrator consciously changing the planned route. Causal associations and planned prerequisites are distinct relations, not interchangeable edges.

The harness is not a workflow scheduler and does not enforce topological prerequisites (the orchestrator is the scheduler). Instead, the harness acts as an operational circuit breaker: its budget and limit enforcements are predeclared stop-losses against agent degradation (infinite loops, runaway concurrency, unguided delegation), not judgements of technical route merit. Ordinary execution decisions remain entirely with the orchestrator.

### Lifecycle

Takt therefore defines the execution DAG as a **projection derived from an append-only execution history**, never as a document edited in place. The lifecycle separates responsibilities:

1. **Plan Commitment**: The active workflow determines when commitment is required (`PR-ORQ-20`). A commitment establishes a baseline of `planned` work with explicit prerequisites. Outside that requirement, execution may begin without a baseline, subject to the unplanned-delegation bound in `PR-HAR-19`; delegated implementation does not unconditionally require a baseline.
2. **Strict DAG Topology & Concurrency**: The execution graph is a true Directed Acyclic Graph, never restricted to a single-root tree. It supports multiple root work units dispatched concurrently from the start and arbitrary fan-in/fan-out dependency joins.
3. **Execution & Observation**: Once committed, the orchestrator decides what to dispatch and when, including dependency satisfaction and recovery. The harness updates work-unit state through passive observation of dispatch and consolidation. Recorded prerequisites do not authorize or block dispatch; they describe the execution structure for observation and analysis.
4. **Execution changes**: Retries and focused repairs are recorded from observed actions without a separate bookkeeping declaration. A change to the planned route is a mutation: the orchestrator states the changed work and prerequisites, either with the dispatch carrying that change or as a plan revision for future work. The harness records this decision and classifies the complete revision against the previous valid plan (`PR-DAG-MUT-4`); it neither invents the change nor asks the orchestrator to label it tactical or strategic.

```text
  PLAN COMMIT (orchestrator declares its baseline DAG: all planned)
                          │
                          ▼
  EXECUTION HISTORY (append-only, totally ordered)      PROJECTION (derived, queried)

┌──────────────┐ ┌──────────┐ ┌──────────┐
│ DECLARE_PLAN │ │ dispatch │ │ consol.  │ ──replay──▶  current DAG shape
└──────────────┘ └──────────┘ └──────────┘

     Nobody edits the DAG. New entries are appended; the projection changes.
```

### Scope

This document governs the DAG's representation and mutation discipline, with enforcement owned by `PRD_HARNESS` and referenced through `PR-DAG-AUT-1`. It does not define the telemetry envelope, the internal control bus, or the Action Journal — it composes on top of them and does not duplicate their obligations.

Passive observation of dispatch and consolidation requires the OpenCode v2 runtime extension code required by `PR-PLT-8`.

## 2. Functional Requirements

The execution history, its total order, the projection, the temporal states, and the mutation discipline are first-release obligations, and so is their use as an input to baselines and detectors. Budget enforcement and forced backtracking apply from the first release.

### 2.1 Responsibility Boundary

| ID | Requirement |
| --- | --- |
| PR-DAG-AUT-1 | The orchestrator MUST remain responsible for deciding what work to dispatch and when, evaluating dependencies, choosing recovery, and deciding what to revise or abandon. The harness MUST NOT use the projection to select, schedule, authorize, delay, or reject work based on route merit. It enforces the admission, termination, budget, contest, and unplanned-delegation guarantees of `PR-HAR-6` and `PR-HAR-16`..`22`, reading this projection rather than maintaining a second in-flight record. Recording invalid plan declarations (`PR-DAG-MUT-6`) does not suppress observed execution. These guarantees grant no authority to choose work or assess the route. |
| PR-DAG-AUT-2 | The harness MUST record the orchestrator's observable actions and explicit declarations, including actions inconsistent with the recorded plan. It MUST NOT suppress an observed dispatch, rewrite history to make execution appear consistent, or record a motivation as an observed fact. Classification under `PR-DAG-MUT-4` records a structural fact about the recorded prerequisite change, never an inferred motive or causal relation; a declared rationale is recorded as a declaration. |
| PR-DAG-AUT-3 | The execution history's authority is authority over the recorded representation, not over execution decisions or their correctness. Independent analysis MAY use that record to establish what happened and of what kind, without relying on the orchestrator's own assessment. Any resulting intervention remains governed by the applicable harness and observability contracts; beyond the enforcements enumerated in `PR-DAG-AUT-1`, this document grants no intervention powers, and it removes no existing safety, approval, ownership, or budget enforcement. |

For example, if unit A fails and the orchestrator dispatches repair R, the harness records A's failure and R's dispatch, causally associated with A. That association is not a prerequisite requiring A to succeed before R runs, nor a reason to rewrite A's downstream dependencies. Whether R should run is the orchestrator's decision; whether that decision was sound is a separate reading of the evidence.

### 2.2 Execution History Representation

The replay model follows the patterns of [Temporal event history](https://docs.temporal.io/workflow-execution/event) and [Event Sourcing](https://learn.microsoft.com/en-us/azure/architecture/patterns/event-sourcing): durable observations support reconstructed state rather than an external mutable truth. These are pattern references, not requirements to adopt their technologies or infrastructure.

| ID | Requirement |
| --- | --- |
| PR-DAG-REP-1 | The harness MUST maintain the execution DAG solely as a projection derived from an append-only, totally ordered execution history, never as a mutable document edited in place. Any two consumers replaying the same recorded prefix MUST derive the same projection without external unrecorded state, wall-clock time, or knowledge that a stream later ended. The execution history MUST preserve the policy and semantic-version references and recorded inputs needed to interpret that prefix without silently applying newer rules. No consumer keeps a parallel record of the same facts. Capture completeness and route correctness are not implied; unmonitored intervals and inconsistencies remain visible evidence. |
| PR-DAG-REP-2 | The execution history begins with a committed baseline when one is declared, or with an empty work set otherwise. In either case it records observed admission, dispatch, consolidation, revision, verification, session lifecycle, reconciliation, and control actions. Session-, plan-, and global-control records identify their actual scope without inventing a work unit. The execution history correlates those actions with the telemetry envelope (`PR-OBS-ENV-4`) and Action Journal (`PR-VFS-JRN-1`..`3`) through references, duplicating neither. It does not enter memory tiers and is not hot telemetry under `PR-OBS-DAT-3` or `PR-OBS-DAT-5`; the content-free posture of `PR-OBS-DAT-1` applies unchanged. |
| PR-DAG-REP-3 | Every committed work unit MUST declare explicit prerequisite identities. Every entry MUST identify its scope, semantic actor, and causal status: a known referenced cause, explicitly declared absence of cause, or cause not captured. A known cause may be a work unit, verdict, acceptance check, collision, denial, exhausted budget, or user instruction. A baseline dispatch may explicitly declare no cause beyond the plan; a missing reference MUST NOT be interpreted as that declaration. References come from structured observed context or an explicit declaration, not prose interpretation. User authority is distinct from the acting agent (`MEM-AUT-2`). Work, attempt, plan-version, and evidence identities MUST be unambiguous throughout the replayable history, including declared session continuity; an identity MUST NOT be reused for different work, even after withdrawal or settlement. Unknown, duplicate, or ambiguous identities in plan declarations are invalid (`PR-DAG-MUT-6`). Without a committed plan, the projection records observed work and causal associations, not invented prerequisites. |
| PR-DAG-REP-4 | The harness MUST record causal associations separately from planned prerequisite dependencies. A focused repair is associated with the work unit it addresses; its insertion MUST NOT add, replace, or redirect that unit's downstream prerequisites. An event origin remains a reference to that event, with its recorded work-unit correlation where present; it does not become a prerequisite or license the harness to invent one. A retry adds attempt history to the same unit, not a node or dependency. For an actual mutation, the harness records the changed work and prerequisite structure supplied with the orchestrator's action or plan revision (`PR-DAG-MUT-6`), never derives a new plan from causal origin alone. Already executed history is not rewritten. These relations describe the record, not dispatch eligibility; they do not prove that the orchestrator followed the plan. |
| PR-DAG-REP-5 | Every entry MUST identify one of exactly three semantic authors. The **orchestrator** authors commitment, dispatch, retry, repair, consolidation (including its own completed/failed judgment), revision, and declared backtracking. The **harness** authors admission and denial dispositions, observed starts and effective terminations, forced backtracking, suspension, capture gaps, session closure and reconciliation, and references to its control actions (`PR-OBS-CTL-5`). It records terminal failure without a returned result only when execution termination is established; an unproven interruption remains capture uncertainty (`PR-OBS-BUS-6`). The **verification specialist** (`PR-CRW-9`) authors gate verdicts and acceptance results (`PR-VFS-CSL-3`, `PR-VFS-CSL-6`), including contest evidence. The harness alone sequences and physically appends all entries; neither other author writes directly. Denials remain visible even without a work-state change, and a harness record never relabels an orchestrator decision as its own. |

### 2.3 Plan Commitment and Mutation Discipline

```text
   retry same unit / focused repair        conscious change to planned route
                │                                         │
                ▼                                         ▼
   execution record grows,                 record changed work and prerequisites
   plan dependencies do not change         from the orchestrator's action/revision
                │                                         │
                ▼                                         ▼
   NOT a mutation                          compare the complete old and new valid plans:
   no tactical/strategic label              retained unit prerequisites change → STRATEGIC
                                            additions/withdrawals or only
                                            planned contracts change          → TACTICAL

   The harness derives the class, not the decision to change the plan.
   A rationale never changes the class.
```

The classification of `PR-DAG-MUT-4` is recorded because the route's evolution is itself worth reading later: the session narrative (`MEM-OPS-4`) and macro consolidation (`MEM-DRM-7`) consume it to see what the harness observed alongside what the orchestrator declared at the same moment. A mutating DAG is expected (`PR-DAG-MUT-7`); what the record makes legible is whether the route changed against the work or against the orchestrator's own conduct. The obligation over that memory content belongs to `ARCH_MEMORY`.

| ID | Requirement |
| --- | --- |
| PR-DAG-MUT-1 | Plan commitment requirements are determined by the active workflow under `PR-ORQ-20`, and enforced under `PR-HAR-19`. A valid commitment MUST record an identified baseline version, work identities, contracts, and explicit prerequisites, with every not-yet-admitted unit `planned`. Committing after execution has begun MUST preserve observed identities, contracts, attempts, and history; it MUST NOT retroactively portray unplanned work as having been planned. Outside a workflow requirement the unplanned-delegation bound applies; this representation introduces no universal commitment trigger. |
| PR-DAG-MUT-3 | The harness MUST NOT require an agent to declare a work unit's creation or status transition as a separate act. The harness MUST derive structure from observed admission, dispatch, consolidation, and effective termination associated with the actions the orchestrator already performs under `PR-ORQ-6` and `PR-ORQ-18`, without a separate bookkeeping act. |
| PR-DAG-MUT-4 | A **mutation** consciously changes the planned route: adding or withdrawing planned work, changing planned contracts, or changing prerequisites. Ordinary dispatch, consolidation, retry under the same contract, and **focused repair** under an unchanged contract are not mutations. Repair purpose is explicit in the dispatched contract or canonical specialist definition (`SSOT-1`), never guessed from code. Work that changes the route remains a mutation even if called a fix. Classification MUST compare the complete valid revision against the immediately preceding valid plan: it is **strategic** if the prerequisite set of any preexisting unit retained in the plan changes; otherwise additions or withdrawals without reconnecting retained units, or changes only to still-planned contracts, are **tactical**. Grouping units, naming a focal unit, declaring motives, or associating a repair MUST NOT change this classification. It measures structure, not risk, effort, or merit. New non-repair work outside a committed baseline is a plan addition, classified by this same comparison; without any baseline it is observed unplanned execution, not an invented plan mutation. An invalid revision is not an applied tactical or strategic mutation. |
| PR-DAG-MUT-5 | The orchestrator MAY attach a short rationale to a dispatch or to a plan revision. A rationale is recorded as a declaration and never changes the classification derived under `PR-DAG-MUT-4`; the harness MUST NOT require one for a tactical mutation. When a mutation originates in a user instruction — including a redirection after the user pauses execution — the entry MUST record the user as the authority behind it, distinct from the orchestrator as actor (`MEM-AUT-2`). |
| PR-DAG-MUT-6 | A **plan revision** is an orchestrator declaration identifying its expected prior valid plan version and the complete additions, changes, withdrawals, and resulting prerequisites, and any declared rationale. It MUST apply as a whole against that version, never partially. Baselines and revisions MUST reject cycles, invalid identities/references, and attempts to withdraw already-admitted work or alter its contracts/prerequisites or executed history; revisions MUST also reject stale base versions. Each invalid declaration and its reason remain recorded without replacing the last valid plan (or inventing one if none exists). Revision ordering does not silently rebase a stale declaration; the orchestrator must submit a new revision against the current version. Future work may depend on existing executed work without rewriting it. Withdrawal preserves identity and all history; it neither redirects nor silently removes retained units' references to that work. Such references remain visibly unsatisfied by the withdrawn unit unless explicitly revised; they do not become dangling unknown identities or authorize dispatch gating by the harness. A dispatch observed alongside an invalid declaration remains recorded as observed execution, visibly inconsistent with the valid plan (`PR-DAG-AUT-2`). |
| PR-DAG-MUT-7 | The DAG growing or mutating is not, by itself, a deviation, a blockage, or a fault. `PR-ORQ-3`'s supervision of goal consistency and `PR-ORQ-5`'s escalation of substantial deviations apply to what the new or altered work unit is trying to achieve, never to the act of the DAG acquiring it. |
| PR-DAG-MUT-8 | A **retry** continues the same work unit under the same contract. The harness MUST append the attempt's start and result against that unit's existing identity, without creating a work unit or changing dependencies. An unsuccessful attempt within continuing work is not by itself a terminal failure of the unit; the unit remains in flight until it terminates under `PR-DAG-TMP-1`. A retry MUST NOT erase a terminal failure, bypass verification or contest authority, or reactivate a superseded branch. Attempt history and both the attempts and the actions consumed within a bounded recovery (`PR-ORQ-13`, `PR-OBS-PRG-2`) MUST be recoverable from the execution history, not represented by artificial retry nodes. |
| PR-DAG-MUT-9 | For recovery declared under `PR-ORQ-13`, the execution history MUST record the binary result, both budget bounds — actions and attempts, each with its declared allowance — objective identity, recoverable-point reference, and explicit scope before uncertain work begins. Subsequent attempts and associated work MUST identify their scope membership, never inferred from code or chronological proximity alone. Budget consumption and enforcement reference `PR-HAR-6`; the recoverable boundary and restoration evidence reference `PR-VFS-STG-6`. The closing record MUST identify whether the result was demonstrated and link its evidence. Decision to abandon, effective termination, and restoration confirmation MUST be separate observable facts (`PR-DAG-TMP-5`). Missing declarations are recorded as missing, not invented defaults or proof that bounded recovery occurred. |
| PR-DAG-MUT-10 | The projection MUST expose the admitted unplanned-unit count, reached bound, outstanding restriction, and resolution history required by `PR-HAR-19` and `PR-HAR-22`. This volume is independent of the concurrent set and excludes repeat attempts of the same unit; it does not imply delegation depth. A commitment records which work it covers without erasing the earlier unplanned count. Stop declarations, escalation requests, and enabling decisions MUST be distinguishable, including the scope and authority of deterministic user acceptance after concurrent editing. The orchestrator's resolution responsibilities belong to `PR-ORQ-20`; enforcement belongs to `PR-HAR-19`, not this representation. |

#### Reading examples: execution growth versus plan mutation

In these examples, `-->` is a planned prerequisite and `..>` is a recorded causal association, not a prerequisite.

```text
Ordinary execution / retry
  plan before:   A --> B --> C
  plan after:    A --> B --> C
  A history:     attempt 1 unsuccessful; attempt 2 succeeds
  Same A, more recorded attempts; no A2 and no dependency rewrite.

Focused repair
  plan before:   A --> B --> C
  plan after:    A --> B --> C
  observed:      A ..> R (focused repair of A)
  Linked evidence shows R satisfies A's unchanged contract; A's failure stays visible.
  B still has prerequisite {A}, not {R} or {A, R}; satisfaction is not A's success.
  A second focused repair is another recorded association, not a new plan chain.

Conscious plan mutation
  plan before:   A --> B --> C
  plan after:    A --> M --> B --> C
  decision:      orchestrator adds M and changes B's prerequisites from {A} to {M}.
  The harness records that exact change, not a topology inferred from M's cause.

Tactical mutation (no retained prerequisite set changes)
  plan before:   A --> B --> C
  plan after:    A --> B --> C, plus new leaf D (no prerequisites, nothing depends on D)
  decision:      orchestrator adds D preemptively; no other unit's prerequisites change.
  Tactical regardless of rationale or effort: retained prerequisite sets are unchanged.

Strategic even when the changed unit is called focal
  plan before:   A --> B, independent C
  plan after:    C --> B, independent A
  B remains in the plan; prerequisites change from {A} to {C}: strategic.
  Grouping B with C in the same revision cannot make this tactical.

Withdrawal without reconnection
  plan before:   A --> B (both still planned)
  plan after:    A withdrawn; B still references {A}
  Tactical; A remains identifiable and cannot satisfy B by disappearance.
  Explicitly removing A from B's prerequisites instead would be strategic.

Contract-only change
  plan before/after: A --> B; B is still planned
  Only B's contract changes: tactical, regardless of the change's effort or risk.
```

A retry consumes effort within the same unit. A focused repair adds corrective effort without changing the strategy. The insertion of M changes the route: M is now planned work before B. None of these diagrams instructs the harness to block a dispatch. A dispatch inconsistent with the recorded plan remains observable evidence, not something the projection hides or rejects.

#### Bounded recovery: a predeclared stop-loss

```text
before uncertain work:
  orchestrator: declares binary result X + action/attempt budget
  harness:     records recoverable point S0 + recovery scope K; starts accounting

during recovery:
  K: uncertain work, its retries, and associated scoped work
  U: unrelated concurrent progress (not a member of K)

  X demonstrated within budget --> record evidence and close recovery
  last attempt admitted       --> no further attempt; it may finish while actions remain
  budget fails without X      --> record abandonment of K; terminate ability to act
                                  confirm VFS restoration to S0; preserve U
                                  orchestrator chooses another route or escalates
```

The harness has the recovery boundary before the risk is taken. Both bounds are counted in recorded facts — admitted attempts and recorded actions — never in readings of a clock, so the same recorded prefix exhausts the budget at the same point on replay (`PR-DAG-REP-1`). It does not negotiate another attempt at exhaustion, infer the abandoned branch from code, or rewind the entire session. A successful result remains subject to existing verification and consolidation contracts; an execution-history entry alone does not materialize or restore files. Declared backtracking remains available separately (`PR-DAG-TMP-5`), and repeated failed recoveries require escalation (`PR-ORQ-13`, `PR-ORQ-5`).

### 2.4 Temporal Partitioning of the Projection

```text
   SETTLED (terminal)           IN FLIGHT (concurrent set)        PLANNED (committed baseline)
┌────────────────────┐       ┌────────────────────────┐       ┌──────────────────────────┐
│ terminal entry:    │       │ admitted; pending or   │       │ declared in plan;        │
│ completed, failed, │◀──────│ executing; reserves    │◀──────│ revisable or withdrawable│
│ backtracked,       │       │ file ownership         │       │ until admission          │
│ interrupted        │       │                        │       │                          │
└────────────────────┘       └────────────────────────┘       └──────────────────────────┘
   history retained              active execution set              not yet admitted

   WITHDRAWN — revoked while still planned; never admitted, never settled,
               never removed from the execution history.
```

```text
   temporal state (PR-DAG-TMP-1):    planned | in flight | settled | withdrawn
   branch validity (orthogonal):  live    | superseded by backtracking

   a work unit can be, at once:
     settled + live           → ordinary completed work, still on the pursued line
     settled + superseded     → genuinely completed, but its line was abandoned
```

| ID | Requirement |
| --- | --- |
| PR-DAG-TMP-1 | The projection MUST assign every work unit exactly one temporal state: **planned** (declared, not admitted), **in flight** (admitted, not effectively terminated), **settled** (effective termination recorded, with prevailing outcome `completed`, `failed`, `backtracked`, or `interrupted`), or **withdrawn** (revoked while planned, never admitted). Within `in flight`, admitted-pending-launch, observed-running, pending-termination (suspension or cancellation request), and launch/liveness-uncertain conditions MUST remain distinguishable; reservation is not proof of execution. Capacity and ownership follow `PR-HAR-16`..`18`. A pending admission that terminates without launch retains that fact and MUST NOT appear executed; cancellation without a work result is `interrupted`, unless abandonment makes it `backtracked`. An attempt ending while the unit continues under `PR-DAG-MUT-8` is not effective termination of the unit. These are lifecycle facts, not topological eligibility. |
| PR-DAG-TMP-2 | What is frozen is the execution history, not the projection: no entry MAY be altered or removed once appended, and a later entry never rewrites an earlier one. A settled work unit's outcome is the one carried by the terminal entry that prevails under `PR-DAG-TMP-6`; every superseded terminal entry remains visible in the execution history, so a unit the orchestrator consolidated as `completed` and a verifier then failed shows both the claim and the verdict. |
| PR-DAG-TMP-3 | Planned work units MAY be revised or withdrawn through a valid plan revision (`PR-DAG-MUT-6`) until admission. Admission freezes the contract and prerequisites for the admitted work; pending launch is not permission to change them. |
| PR-DAG-TMP-4 | For any position in the execution history the harness MUST answer which units were planned, withdrawn, in flight (including pending or uncertain execution), or settled at that prefix. EOF, inactivity, and disconnection MUST NOT derive terminal state. Session closure MUST be a recorded fact under `PR-OBS-BUS-6`, following reconciliation of outstanding executions and effective termination under `PR-HAR-18`; remaining work ends as recorded `interrupted` unless a supported completion, failure, or backtracking outcome applies. A closure that cannot yet establish effective termination remains pending with capture uncertainty, not a falsely closed session. Abrupt loss without such records leaves the earlier prefix unchanged; later reconciliation appends evidence and outcomes, never rewrites the earlier prefix. A confirmed closed session has no in-flight work. |
| PR-DAG-TMP-5 | Whether a line is pursued MUST be independent of temporal state: **live** or **superseded by backtracking**. Declared backtracking names the orchestrator's abandoned scope; forced backtracking names the recovery scope already recorded under `PR-DAG-MUT-9`. The decision MUST name the superseded set and recoverable point and immediately mark the line abandoned, preserving unrelated progress. Planned units in that set are withdrawn. In-flight units remain in flight with abandonment and termination pending until `PR-HAR-18` is satisfied; then their outcome is `backtracked`. Already-settled units receive a superseding backtracked outcome without losing earlier outcomes. An admitted unit never launched retains that distinction. Restoration confirmation is a later fact governed by `PR-VFS-STG-6`, not implied by abandonment or termination. Neither repair, verification, nor contest reactivates the abandoned line. |
| PR-DAG-TMP-6 | For a live work unit, terminal outcome precedence applies only among entries evaluating the same identified attempt, artifact revision, and applicable invariant scope (`PR-VFS-CSL-7`). Verification prevails over the orchestrator's judgment; harness terminal failure also prevails over that judgment. Between entries of equal authority and the same evaluated scope, the later entry prevails. Verification supersedes a harness failure only through its targeted contest (`PR-DAG-TMP-7`). Evidence for another revision remains historical and does not automatically govern the current revision; pending verification is not a pass. Backtracking is abandonment rather than a competing quality verdict: once effectively terminated, a superseded unit remains backtracked regardless of later verdicts. All claims and evidence remain visible; evidence alone neither proves effective termination nor releases ownership. |
| PR-DAG-TMP-7 | The orchestrator MAY contest a specifically identified harness terminal failure by requesting verification against the preexisting invariants applicable to that failure and its evaluated artifact. It MUST NOT substitute more favorable invariants or a later repaired artifact as proof that the original failure was false. The harness invokes the verifier, never the orchestrator (`PR-CRW-12`). An upheld contest resolves only the targeted failure; rejection leaves it standing. Record the request identity, targeted failure, evaluated work/artifact, invariant references, verdict, and any denial. Admission and counting are owned by `PR-HAR-17`, `PR-HAR-20`, and `PR-HAR-22`; an upheld contest does not reactivate abandoned work. |
| PR-DAG-TMP-8 | Contract satisfaction MUST be distinguishable from a unit's terminal outcome. Evidence bound under `PR-VFS-CSL-7` may establish that repair R satisfies A's unchanged contract for the identified repaired artifact and applicable invariants. The projection MUST link R, A, that contract, and the evidence without erasing A's failure, changing its outcome to completed, or redirecting downstream prerequisites. This satisfaction does not automatically transfer to later artifacts or revised contracts and does not reopen a superseded line. The orchestrator evaluates what this evidence permits next (`PR-DAG-AUT-1`); the harness does not schedule successors. |

### 2.5 Uses of the Observational Execution History

| ID | Requirement |
| --- | --- |
| PR-DAG-USE-1 | Session continuity MAY reconstruct the projection only through an explicit continuity declaration by the agent holding the user-facing interface (`MEM-OPS-5`). That declaration MUST reference the exact prior execution-history prefix and preserve work identities, plan versions, history, unresolved capture uncertainty, and applicable carried controls (`PR-HAR-22`). The referenced prefix precedes new entries in the continued replay order; it MUST NOT fork into competing authoritative histories for the same ongoing work. Without declared continuity each session stands alone. Continuity does not reactivate terminal or withdrawn work; new work is separately identified and linked to its history, not a reset of the old unit. |
| PR-DAG-USE-2 | A recap of delivered work MAY be composed from the execution history's settled work units and their recorded classifications, distinguishing tactical from strategic mutation. This reports recorded classifications and declared rationales, not independently established motivations (`PR-DAG-AUT-2`). |
| PR-DAG-USE-3 | The execution history MAY serve as a source of comparable structural signals across sessions — the ratio of tactical to strategic mutation, or repair depth per work unit — for the baselining and degradation analysis contemplated by `PR-OBS-PRG-4`. The session's execution history itself is not hot telemetry (`PR-DAG-REP-2`); durable retention of this derived shape beyond the session, when it exists, is what falls under the protection class and bounded retention window of `PR-OBS-DAT-3`. |
| PR-DAG-USE-4 | Reuse under `PR-DAG-USE-1`..`2` MUST NOT introduce a second write path into the execution history. Every consumer reads the projection; the harness remains the sole writer, with the three semantic authors enumerated in `PR-DAG-REP-5`. |
| PR-DAG-USE-5 | The execution history's replayed record, its recorded classifications (`PR-DAG-MUT-4`), its declared rationales (`PR-DAG-MUT-5`, `PR-DAG-MUT-6`) and its recorded expected results and budgets (`PR-DAG-MUT-9`) MAY together be used to establish what kind of thing happened, and to attribute it: a signal the execution history should have captured but did not is a harness gap, recognizable through the unmonitored intervals of `PR-OBS-BUS-5`; a route that changed with no corresponding recorded change in a dispatch or plan revision is an undeclared deviation; a mutation matching a recorded tactical or strategic classification is neither — ordinary DAG growth (`PR-DAG-MUT-7`) is not drift. This document does not define a detector; it establishes that the record makes the attribution possible. |

**Recorded shape as detector input**: the execution history's capacity to feed those analyses is a property of what `PR-DAG-MUT-4`, `PR-DAG-MUT-9` and `PR-DAG-TMP-1` require to be recorded in the first release. Their consumers in `PRD_OBSERVABILITY` apply from the first release.

## 3. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| EOF, inactivity, or disconnection in an active session | No inferred terminal outcome; the same execution-history prefix produces the same projection (`PR-DAG-REP-1`, `PR-DAG-TMP-4`). |
| Abrupt close without a terminal record | Capture remains uncertain, not agent failure. Reconciliation later appends liveness/termination evidence and closure; it does not reinterpret the old prefix (`PR-OBS-BUS-6`). |
| Confirmed closure with unfinished work | Recorded effective termination yields interrupted outcomes unless a supported different outcome applies; no in-flight work remains in the closed session (`PR-DAG-TMP-4`). |
| Two simultaneous admissions or duplicate requests | Show the single reservation per accepted request and visible denial where full; duplicates create neither execution nor additional consumption (`PR-HAR-16`..`17`). |
| Launch uncertain after communication failure | Show pending/uncertain execution and retain capacity until reconciliation, never infer that a retry is safe (`PR-HAR-17`). |
| Cancellation or backtracking while execution can still act | Record termination pending; ownership and capacity remain. Controls to resolve it require no additional execution slot (`PR-HAR-18`). |
| Cyclic, stale, or identity-invalid plan revision | Record the invalid declaration and reason, keep the complete last valid plan, and preserve any observed execution even if inconsistent (`PR-DAG-MUT-6`). |
| Revision attempts to change already-admitted work | Reject the declaration, not the observed facts; executed history and admitted contract/prerequisites remain intact (`PR-DAG-MUT-6`). |
| Planned A withdrawn while B references A | Preserve A and B's reference; no automatic reconnection or satisfaction. Explicitly changing retained B's prerequisites is strategic (`PR-DAG-MUT-4`, `PR-DAG-MUT-6`). |
| Revision groups a retained unit with newly added work | Changing that retained unit's prerequisites is strategic regardless of grouping; contract-only planned changes or additions without reconnection are tactical (`PR-DAG-MUT-4`). |
| Retry or focused repair observed | Preserve the plan; retry adds an attempt to the same unit, repair adds associated work, neither receives a mutation class (`PR-DAG-MUT-4`, `PR-DAG-MUT-8`). |
| Late passing verdict for an old revision | Retain historical evidence; do not override the current revision's outcome or authorize consolidation (`PR-DAG-TMP-6`, `PR-VFS-CSL-7`). |
| R demonstrates A's unchanged contract after A failed | Expose linked contract satisfaction and A's historical failure together; B still references A, and no abandoned branch reactivates (`PR-DAG-TMP-8`). |
| Contest substitutes different invariants or repaired artifacts | It cannot resolve the targeted historical failure; evaluate only its applicable scope (`PR-DAG-TMP-7`). |
| Last recovery attempt admitted | Permit completion and evidence while action allowance remains, not another attempt. Once all admitted attempts end without the result and no attempt allowance remains, or the action budget is exhausted first, force backtracking (`PR-HAR-6`). |
| Backtracking recorded before termination or restoration | Show those stages separately; preserve unrelated progress and do not claim restoration early (`PR-DAG-TMP-5`, `PR-VFS-STG-6`). |
| Acceptance fails after consolidation | Record failure and forward correction; do not restore across completed consolidation (`PR-VFS-CSL-6`). |
| Uncertain work lacks a result, budget, or recoverable point | Record what is absent, not an invented bounded recovery or inferred mutation class (`PR-DAG-MUT-9`). |
| Fourth unplanned unit or third contest admitted | The respective bound is reached; next uncovered unit or contest is denied without an enabling resolution. Upheld contests count (`PR-HAR-19`..`20`). |
| Stop declaration or escalation without a response at the bound | Record it distinctly from an enabling decision; no fresh allowance. |
| Second consecutive failed recovery of the same objective | Record the reached bound and escalation; further recovery remains blocked pending an enabling user decision (`PR-HAR-21`). |
| Active workflow requires commitment before delegation | Record the requirement and deny uncovered delegation until valid commitment, independently of remaining volume (`PR-HAR-19`). |
| Continuity declared for a closed session | Preserve identities, terminal history, and carried restrictions; no automatic reactivation (`PR-DAG-USE-1`). |
