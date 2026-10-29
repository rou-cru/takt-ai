# PRD: Dreaming

## 1. Functional Requirements

### 1.1 Dispatch and Control

| ID | Requirement |
| --- | --- |
| PR-DRM-1 | The harness schedules and dispatches dreaming through the maintenance class, never the orchestrator as an ordinary work unit. The orchestrator MUST NOT suppress, defer, or reprioritize a scheduled cycle, or perform it as its own work. |
| PR-DRM-2 | Dreaming MUST run outside active task execution. The harness enforces a maintenance barrier that halts further task dispatch until the scheduled cycle completes; no work unit runs concurrently with a dreaming cycle. |

### 1.2 Cycle Scope and Recording

| ID | Requirement |
| --- | --- |
| PR-DRM-3 | Cycles MUST be small and frequent, with a bounded memory scope declared before they start, naming exactly one tier — Workspace or Global — never both in the same cycle. This constraint governs Periodic Simple Mode and Deep Dream; macro consolidation is not bound by it. Excess work remains for a subsequent cycle rather than one large intervention. |
| PR-DRM-4 | Every dreaming cycle — scheduled, requested, skipped, or aborted — MUST be recorded on the control bus with its trigger, declared scope, and outcome. |

### 1.3 Scheduling Modalities

| ID | Requirement |
| --- | --- |
| PR-DRM-5 | Periodic Simple Mode uses a dynamic cadence proportional to implementation pace (work units dispatched and workspace mutation throughput), not the problem rate. It requires a configured minimum elapsed interval since the previous cycle and unconsolidated memory growth. Cadence parameters are policy data. |
| PR-DRM-6 | Periodic Simple Mode and macro consolidation cycles MUST be schedulable without the user present. Their only permitted network egress is to the provider of the model assigned to the dreaming specialist, carrying only memory entries of the declared scope; indexing, candidate generation, and scoring run locally with zero egress. When that provider is unreachable, the cycle completes its local phases and records the judgment work as pending for a subsequent cycle; it is not a failure and never falls back to a lossy path. |
| PR-MNT-23 | Periodic Simple Mode MAY remove, without user presence or per-item validation, only three objective mechanical cases: byte-identical duplicates, semantic duplicates above a fixed high-confidence threshold (policy data), and orphaned links with no ambiguous relinking candidate. Semantic duplication is decided by the base local capabilities, never by the specialist's generation or by an acceleration capability alone. |
| PR-MNT-22 | The harness triggers macro consolidation every policy-defined number of closed sessions or milestone objectives. |

### 1.4 Isolated Phased Execution

| ID | Requirement |
| --- | --- |
| PR-DRM-7 | Every non-skipped cycle MUST run in an isolated maintenance session whose conversation never pollutes an active crew conversation. It reads only the declared memory scope through targeted retrieval; it MUST NOT perform full re-reads of session transcripts or workspace files to find signal. |
| PR-DRM-8 | Every non-skipped cycle executes as one bounded phased pass — orient (read what exists inside the declared scope), gather signal (retrieval over unconsolidated entries only), consolidate (group, link, or relocate), report. A phase that finds nothing ends the pass early under `PR-DRM-11`. Phases MUST NOT widen the declared scope. |
| PR-DRM-9 | Gathering covers user corrections, preference changes, formalized decisions, recurring patterns, and open threads. Relative dates are normalized to absolute form at write or consolidation time. Dead references (links naming entries that do not exist) are relinked where the target is unambiguous, otherwise flagged as proposals awaiting Deep Dream — never silently dropped. Signal extraction MUST NOT promote an entry's nature: a proposal or hypothesis stays one until its authority is recorded. |

### 1.5 Deference and Empty-Scope Fast Path

| ID | Requirement |
| --- | --- |
| PR-DRM-10 | The harness MAY defer a scheduled background cycle while the user is active within the policy quiet window. Deference is a recorded skip with its reason, never orchestrator suppression; it leaves the scheduled cadence unaffected. The quiet-window length is policy data. |
| PR-DRM-11 | When the scope check finds no unconsolidated growth, the cycle MUST exit through a fast path; the skip is still recorded. On such a skip the harness MAY surface one previously stored entry as a recall reminder; surfacing is relayed through the interface holder and performs no consolidation. |

### 1.6 Cycle Safety Invariants

| ID | Requirement |
| --- | --- |
| PR-DRM-12 | Before any cycle whose declared scope exceeds the policy change threshold, the harness MUST record a restore point. The cycle reads memory entries only, and MUST NOT carry file contents, prompts, model responses, or secrets. The cycle leaves no trace in memory beyond the links and relocations it applies: its own record lives on the control bus, never as memory sessions or entries. |

### 1.7 Cycle Report and Diary

| ID | Requirement |
| --- | --- |
| PR-DRM-13 | Every non-skipped cycle emits a cycle report recorded against its control bus entry: counts scanned, grouped, relocated, and mechanically removed by case; health signals over the consolidated scope (freshness, coverage, coherence, efficiency, reachability); a bounded number of non-obvious insights; and the proposal queue awaiting Deep Dream. The report records the pinned identity of each local model and of the specialist's model, and whether acceleration was active; a change in any of them establishes a new baseline. A human-readable diary entry correlated to the same control bus record accompanies it. Signal formulas, thresholds, the insight bound, and notification depth (silent, summary, full) are policy data, not execution paths. |

### 1.8 Deep Dream Procedure

| ID | Requirement |
| --- | --- |
| PR-MNT-2 | The user MAY request a cycle at any time, and MAY participate in one. |
| PR-DRM-14 | Deep Dream processes a per-item validation queue: each deletion, content reduction, contradiction resolution, verbose-entry demotion, or index rebuild step requires individual user validation. Unvalidated items remain intact and stay queued for a later Deep Dream. Contradiction resolution invalidates through recorded supersession with evolution links; it MUST NOT rewrite either entry. |

### 1.9 Layered Execution

| ID | Requirement |
| --- | --- |
| PR-DRM-16 | The dreaming specialist never mutates memory. It emits typed dreaming operations from a closed set: group, link (evolution, scoped exception, or unresolved disagreement), relocate out of the active retrieval path, propose a removal for Deep Dream, and attach a label or insight recorded as derived. The harness validates each operation against the declared scope, preservation rules, the mechanical boundary, and entry nature, then applies it or refuses it naming the admissible continuation. |
| PR-DRM-17 | The harness generates the candidate set the specialist judges: groups, possible supersession or contradiction pairs, ambiguous relinks, and the signals of `PR-DRM-9`, each with its evidence and score. The same scope, pinned local models, and policy data MUST yield the same candidate set; grouping MUST NOT depend on random initialization. Operations naming entries or relations outside the candidate set are refused. |
| PR-DRM-18 | Memory content reaches the specialist only as data. Outside Deep Dream, its sole tool is operation submission. A local injection screen runs over entries before any generative judgment; flagged entries are withheld from that judgment and queued for Deep Dream with the flag recorded. |
| PR-DRM-19 | Local capabilities score every candidate, and calibrated thresholds (policy data) route it: resolved locally only for the mechanical cases of `PR-MNT-23`, sent to the specialist, or queued for Deep Dream. |
| PR-DRM-20 | The base local capabilities — semantic indexing, entailment and contradiction scoring, and injection screening — MUST run on commodity CPU-only hardware with zero network egress. Every non-skipped cycle requires them; a cycle whose base capabilities are unavailable is refused before any mutation rather than degraded. |
| PR-DRM-21 | An optional hardware-accelerated decision capability MAY refine scoring and routing when the host supports it. It changes only cost, latency, and which items reach the specialist; it never widens what a cycle may do, and never alone authorizes a mechanical removal. |

## 2. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| Work units do not reach the maintenance barrier | Record the blocked maintenance scope and apply bounded control policy; do not infer agent failure or wait for a human unless authorization or value judgment is required. |
| The fast path is taken while unconsolidated growth exists | Recorded as a failed cycle; the growth remains for the next cycle. |
| The report or diary write fails | Recorded as a cycle with a report gap; links already written stand (append-only) and the gap stays visible. |
