# PRD: Dreaming

## 1. Problem

Without consolidation, memory decays over an operating horizon in three ways: raw entries accumulate without bound and degrade recall precision; no higher-level insight is ever synthesized from repeated sessions; and knowledge stays trapped in the Workspace where it was written, so an organization's other repositories, services, and past pitfalls never inform new work. Dreaming organizes long-term memory against this decay without losing content or provenance. It is a permanent, harness-controlled maintenance process with its own modalities and obligations, independent of [GC](PRD_GC.md): sharing the maintenance role class (`PR-CRW-16`) carries none of GC's own obligations.

[ARCH_MEMORY](../context/ARCH_MEMORY.md) §6 governs what consolidation may do to entries; this document governs scheduling and execution. Its obligations are `PR-DRM-1`..`14` and `16`..`21`, and the dreaming-specific `PR-MNT-2`, `PR-MNT-22` and `PR-MNT-23`.

## 2. Functional Requirements

### 2.1 Dispatch and Control

| ID | Requirement |
| --- | --- |
| PR-DRM-1 | The harness schedules and dispatches dreaming through the maintenance class (`PR-CRW-16`), never the orchestrator as an ordinary work unit. The orchestrator MUST NOT suppress, defer, reprioritize, or absorb a scheduled cycle under `PR-CRW-13`. |
| PR-DRM-2 | Dreaming MUST run outside active task execution. The harness enforces a maintenance barrier that halts further task dispatch until the scheduled cycle completes; no work unit runs concurrently with a dreaming cycle. |

### 2.2 Cycle Scope and Recording

| ID | Requirement |
| --- | --- |
| PR-DRM-3 | Cycles MUST be small and frequent, with a bounded memory scope declared before they start, naming exactly one tier — Workspace or Global (`MEM-SCP-1`) — never both in the same cycle. This constraint governs Periodic Simple Mode and Deep Dream; Macro-Dreaming (`PR-MNT-22`) reads closed-session narratives across the organization by design (`MEM-SCP-4`) and is not bound by it. Excess work remains for a subsequent cycle rather than one large intervention. GC mandate classes and the causal closure of workspace deltas do not define this scope. |
| PR-DRM-4 | Every dreaming cycle — scheduled, requested, skipped, or aborted — MUST be recorded on the control bus with its trigger, declared scope, and outcome (`PR-OBS-CTL-4`). |

### 2.3 Scheduling Modalities

| ID | Requirement |
| --- | --- |
| PR-DRM-5 | Periodic Simple Mode uses a dynamic cadence proportional to implementation pace (work units dispatched and workspace mutation throughput), not error rate, subject to the elapsed-interval and memory-growth conditions of `MEM-DRM-3`. Cadence parameters are policy data (`PR-OBS-BUS-4`), not execution paths. Macro-Dreaming follows `PR-MNT-22`; Deep Dream follows `MEM-DRM-2`. |
| PR-DRM-6 | Lossless background cycles MUST be schedulable without the user present. Their only permitted network egress is to the provider of the model assigned to the dreaming specialist, carrying only memory entries of the declared scope (`PR-DRM-12`); indexing, candidate generation, and scoring run locally with zero egress (`PR-DRM-20`). When that provider is unreachable, the cycle completes its local phases and records the judgment work as pending for a subsequent cycle; it is not a failure and never falls back to a lossy path. Deep Dream instead requires the user's request and presence; every judgment-based deletion requires individual validation (`PR-MNT-2`, `MEM-DRM-2`). |
| PR-MNT-23 | Periodic Simple Mode MAY remove, without user presence or per-item validation, only three objective mechanical cases: byte-identical duplicates, semantic duplicates above a fixed high-confidence threshold (policy data, `PR-OBS-BUS-4`), and orphaned links with no ambiguous relinking candidate (`PR-DRM-9`). Semantic duplication is decided by the base local capabilities (`PR-DRM-20`), never by the specialist's generation or by an acceleration capability alone (`PR-DRM-21`). No other removal, reduction, or judgment call MAY occur outside Deep Dream (`MEM-DRM-2`). |
| PR-MNT-22 | Macro-Dreaming: on a macro cadence (every $N$ closed sessions or milestone objectives), the harness triggers the playbook derivation defined in `MEM-DRM-7`. |

### 2.4 Isolated Phased Execution

| ID | Requirement |
| --- | --- |
| PR-DRM-7 | Every non-skipped cycle MUST run in an isolated maintenance session whose conversation never pollutes an active crew conversation. It reads only the declared memory scope through targeted retrieval; it MUST NOT perform full re-reads of session transcripts or workspace files to find signal. |
| PR-DRM-8 | Every non-skipped cycle executes as one bounded phased pass — orient (read what exists inside the declared scope), gather signal (retrieval over unconsolidated entries only), consolidate (group, link, or relocate per `ARCH_MEMORY` §6, `MEM-DRM-3` and `MEM-DRM-5`), report (`PR-DRM-13`). A phase that finds nothing ends the pass early under `PR-DRM-11`. Phases MUST NOT widen the declared scope (`PR-DRM-3`). |
| PR-DRM-9 | Gathering covers user corrections, preference changes, formalized decisions, recurring patterns, and open threads. Relative dates are normalized to absolute form at write or consolidation time. Dead references (links naming entries that do not exist) are relinked where the target is unambiguous, otherwise flagged as proposals awaiting Deep Dream — never silently dropped. Signal extraction MUST NOT promote an entry's nature (`MEM-TYP-6`): a proposal or hypothesis stays one until its authority is recorded. |

### 2.5 Deference and Empty-Scope Fast Path

| ID | Requirement |
| --- | --- |
| PR-DRM-10 | The harness MAY defer a scheduled background cycle while the user was active inside the policy quiet window. Deference is a recorded skip with its reason (`PR-DRM-4`), never orchestrator suppression (`PR-DRM-1`); it leaves the scheduled cadence unaffected. The quiet-window length is policy data (`PR-OBS-BUS-4`). |
| PR-DRM-11 | When the scope check finds no unconsolidated growth, the cycle MUST exit through a fast path at a fraction of a full pass's cost; the skip is still recorded (`PR-DRM-4`). On such a skip the harness MAY surface one previously stored entry as a recall reminder; surfacing is relayed through the interface holder and performs no consolidation. |

### 2.6 Cycle Safety Invariants

| ID | Requirement |
| --- | --- |
| PR-DRM-12 | Source entries are immutable for the duration of the cycle: consolidation writes only new grouping or relation links and relocations out of the active retrieval path (`MEM-DRM-5`). Before any cycle whose declared scope exceeds the policy change threshold, the harness MUST record a restore point. The cycle reads memory entries only, and MUST NOT carry file contents, prompts, model responses, or secrets beyond the content-free envelope posture (`PR-OBS-DAT-1`). The cycle leaves no trace in memory beyond the links and relocations it applies: its own record lives on the control bus (`PR-DRM-4`), never as memory sessions or entries. |

### 2.7 Cycle Report and Diary

| ID | Requirement |
| --- | --- |
| PR-DRM-13 | Every non-skipped cycle emits a cycle report recorded against its bus entry (`PR-DRM-4`): counts scanned, grouped, relocated, and mechanically removed by case (`PR-MNT-23`); health signals over the consolidated scope (freshness, coverage, coherence, efficiency, reachability); a bounded number of non-obvious insights; and the proposal queue awaiting Deep Dream. The report records the pinned identity of each local model and of the specialist's model, and whether acceleration was active; a change in any of them establishes a new baseline (`PR-MNT-11`). A human-readable diary entry correlated to the same bus record accompanies it. Signal formulas, thresholds, the insight bound, and notification depth (silent, summary, full) are policy data (`PR-OBS-BUS-4`), not execution paths. |

### 2.8 Deep Dream Procedure

| ID | Requirement |
| --- | --- |
| PR-MNT-2 | The user MAY request a cycle at any time, and MAY participate in one. |
| PR-DRM-14 | Deep Dream processes a per-item validation queue: each deletion, content reduction, contradiction resolution, verbose-entry demotion, or index rebuild step requires individual user validation (`MEM-DRM-2`). Unvalidated items remain intact and stay queued for a later Deep cycle. Contradiction resolution invalidates through recorded supersession with evolution links; it MUST NOT rewrite either entry's history. |

### 2.9 Layered Execution

| ID | Requirement |
| --- | --- |
| PR-DRM-16 | The dreaming specialist never mutates memory. It emits typed dreaming operations from a closed set: group, link (evolution, scoped exception, or unresolved disagreement), relocate out of the active retrieval path, propose a removal for Deep Dream, and attach a label or insight recorded as derived. The harness validates each operation against the declared scope (`PR-DRM-3`), source immutability (`PR-DRM-12`), the mechanical boundary (`PR-MNT-23`), and entry nature (`MEM-TYP-6`), then applies it or refuses it naming the admissible continuation. |
| PR-DRM-17 | The harness generates the candidate set the specialist judges: groups, possible supersession or contradiction pairs, ambiguous relinks, and the signals of `PR-DRM-9`, each with its evidence and score. The same scope, pinned local models, and policy data MUST yield the same candidate set; grouping MUST NOT depend on random initialization. Operations naming entries or relations outside the candidate set are refused. |
| PR-DRM-18 | Memory content reaches the specialist only as data. Its sole tool is operation submission (`PR-DRM-16`). A local injection screen runs over entries before any generative judgment; flagged entries are withheld from that judgment and queued for Deep Dream with the flag recorded. |
| PR-DRM-19 | Local capabilities score every candidate, and calibrated thresholds (policy data, `PR-OBS-BUS-4`) route it: resolved locally only for the mechanical cases of `PR-MNT-23`, sent to the specialist, or queued for Deep Dream. |
| PR-DRM-20 | The base local capabilities — semantic indexing (`MEM-RET-7`), entailment and contradiction scoring, and injection screening — MUST run on commodity CPU-only hardware with zero network egress. Every non-skipped cycle requires them; a cycle whose base capabilities are unavailable is refused before any mutation rather than degraded. |
| PR-DRM-21 | An optional hardware-accelerated decision capability MAY refine scoring and routing when the host supports it (`PR-SET-34`). It changes only cost, latency, and which items reach the specialist; it never widens what a cycle may do, never alone authorizes a mechanical removal (`PR-MNT-23`), and its absence removes no obligation of this document. |

## 3. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| Work units do not reach the maintenance barrier | Recorded as a stalled barrier; the harness halts subsequent task dispatch and escalates. |
| The fast path is taken while unconsolidated growth exists | Recorded as a failed cycle; the growth remains for the next cycle. |
| The report or diary write fails | Recorded as a cycle with a report gap; links already written stand (append-only) and the gap stays visible. |

## 4. Pattern References (informative)

The phased pass of `PR-DRM-8`..`9` follows the pattern of OpenClaw Auto-Dream's collect → consolidate → evaluate cycle and of the `dream-skill` 4-phase pass (orient, gather signal from recent transcripts with targeted retrieval, consolidate with date normalization and contradiction handling, prune and index). The deference and fast path of `PR-DRM-10`..`11` follow Auto-Dream's smart skip and the Hermes Dreaming proposal's quiet-activity respect; the report and diary of `PR-DRM-13` follow the Hermes dream diary (`run` / `status` / `diary`) and Auto-Dream's dream report with health signals. Archival-never-deletion in background operation follows Auto-Dream's forgetting curve (archive, never delete), constrained here to relocation without content loss in Simple Mode (`MEM-DRM-3`, `MEM-DRM-5`) and judgment-based removal only in user-validated Deep Dream, with the bounded mechanical exception of `PR-MNT-23`. The objective mechanical removals of `PR-MNT-23` follow Mem0's automatic ADD/UPDATE/DELETE resolution, narrowed to cases requiring no judgment call. The separation of a dedicated maintenance agent running outside active work follows Letta's sleep-time compute; the proposal-and-application split of `PR-DRM-16` follows Anthropic's Managed Agents Dreams, whose output store is reviewable while the input store stays intact; supersession without deletion follows Zep/Graphiti's temporal invalidation; screening generated associations against contradiction follows SCM (Sleep-Consolidated Memory); abstractive replacement of a memory region, as in Auto-Dreamer, informs Macro-Dreaming only, since Periodic Simple Mode is lossless. These are pattern references, not requirements to adopt their technologies or infrastructure.

**Reference selection (informative, not a requirement).** Semantic indexing: EmbeddingGemma-300M, or Granite-Embedding-311M-multilingual-R2 where an Apache-2.0 license is required. Entailment and contradiction: mDeBERTa-v3-base-xnli. Injection screening: Prompt Guard 2 86M. Acceleration: Kev-0.8B, which needs Apple Silicon or a GPU and has no CPU path. Specialist: Haiku or the model assigned to the dreaming specialist.
