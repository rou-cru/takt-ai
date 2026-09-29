# PRD: Dreaming — Memory Maintenance

**Actual implementation progress: 5%** — 22 own requirements: 0 complete, 2 partial, 20 not integrated. Memory infrastructure and GC cycles exist in the repository, but they are different domains. I found no production path that dispatches Simple/Deep/Macro Dream, schedules or isolates its phases, applies deference/fast path, writes the diary/report, and enforces its invariants; references or isolated tools do not amount to integrated dreaming.

## 1. Problem

Without consolidation, memory decays over an operating horizon in three ways: raw entries accumulate without bound and degrade recall precision; no higher-level insight is ever synthesized from repeated sessions; and knowledge stays trapped in the Workspace where it was written, so an organization's other repositories, services, and past pitfalls never inform new work. Dreaming organizes long-term memory against this decay without losing content or provenance. It is a permanent, harness-controlled maintenance process with its own modalities and obligations, independent of [GC](PRD_GC.md): sharing the maintenance role class (`PR-CRW-16`) carries none of GC's own obligations.

[ARCH_MEMORY](../context/ARCH_MEMORY.md) §6 governs what consolidation may do to entries; this document governs scheduling and execution. Its obligations are `PR-DRM-1`..`15` and the dreaming-specific `PR-MNT-2` and `PR-MNT-18`..`23`.

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
| PR-DRM-5 | Periodic Simple Mode uses a dynamic cadence proportional to implementation pace (work units dispatched and workspace mutation throughput), not error rate, subject to the elapsed-interval and memory-growth conditions of `MEM-DRM-3`. Cadence parameters are policy data (`PR-OBS-BUS-4`), not execution paths. Macro-Dreaming follows `PR-MNT-22`; Deep Dream follows `PR-MNT-21`. |
| PR-DRM-6 | Lossless background cycles MUST be schedulable without the user present and without network egress. Deep Dream instead requires the user's request and presence; every judgment-based deletion requires individual validation (`PR-MNT-2`, `PR-MNT-21`). |
| PR-MNT-18 | Dreaming conducts the consolidation lifecycle of `ARCH_MEMORY` §6. What it may group, what it MUST preserve, and what only the user may authorize are governed there; this document governs only when it runs and who dispatches it. |
| PR-MNT-19 | Periodic Simple Mode (`MEM-DRM-3`) runs on the cadence of `PR-DRM-5` and is lossless: it groups related entries while preserving each one's complete content, authorship, and evolution links, subject only to the mechanical exception of `PR-MNT-23`. |
| PR-MNT-20 | An entry that no longer governs behavior (`MEM-DRM-5`, `MEM-RET-5`) MAY be moved out of the active retrieval path while remaining fully retrievable with its original attribution. |
| PR-MNT-21 | Deep Dream (`MEM-DRM-2`) runs only with the user present and is the only mode permitted to delete entries or reduce their retained content on judgment, each removal individually validated. The objective mechanical removals of `PR-MNT-23` are the sole exception and never require Deep Dream. |
| PR-MNT-23 | Periodic Simple Mode MAY remove, without user presence or per-item validation, only three objective mechanical cases: byte-identical duplicates, semantic duplicates above a fixed high-confidence threshold (policy data, `PR-OBS-BUS-4`), and orphaned links with no ambiguous relinking candidate (`PR-DRM-9`). No other removal, reduction, or judgment call MAY occur outside Deep Dream (`PR-MNT-21`). |
| PR-MNT-22 | Macro-Dreaming: on a macro cadence (every $N$ closed sessions or milestone objectives), the harness triggers the playbook derivation defined in `MEM-DRM-7`. |

### 2.4 Isolated Phased Execution

| ID | Requirement |
| --- | --- |
| PR-DRM-7 | Every non-skipped cycle MUST run in an isolated maintenance session whose conversation never pollutes an active crew conversation. It reads only the declared memory scope through targeted retrieval; it MUST NOT perform full re-reads of session transcripts or workspace files to find signal. |
| PR-DRM-8 | Every non-skipped cycle executes as one bounded phased pass — orient (read what exists inside the declared scope), gather signal (retrieval over unconsolidated entries only), consolidate (group, link, or relocate per `ARCH_MEMORY` §6 and `PR-MNT-19`..`20`), report (`PR-DRM-13`). A phase that finds nothing ends the pass early under `PR-DRM-11`. Phases MUST NOT widen the declared scope (`PR-DRM-3`). |
| PR-DRM-9 | Gathering covers user corrections, preference changes, formalized decisions, recurring patterns, and open threads. Relative dates are normalized to absolute form at write or consolidation time. Dead references (links naming entries that do not exist) are relinked where the target is unambiguous, otherwise flagged as proposals awaiting Deep Dream — never silently dropped. Signal extraction MUST NOT promote an entry's nature (`MEM-TYP-6`): a proposal or hypothesis stays one until its authority is recorded. |

### 2.5 Deference and Empty-Scope Fast Path

| ID | Requirement |
| --- | --- |
| PR-DRM-10 | The harness MAY defer a scheduled background cycle while the user was active inside the policy quiet window. Deference is a recorded skip with its reason (`PR-DRM-4`), never orchestrator suppression (`PR-DRM-1`); it leaves the scheduled cadence unaffected. The quiet-window length is policy data (`PR-OBS-BUS-4`). |
| PR-DRM-11 | When the scope check finds no unconsolidated growth, the cycle MUST exit through a fast path at a fraction of a full pass's cost; the skip is still recorded (`PR-DRM-4`). On such a skip the harness MAY surface one previously stored entry as a recall reminder; surfacing is relayed through the interface holder and performs no consolidation. |

### 2.6 Cycle Safety Invariants

| ID | Requirement |
| --- | --- |
| PR-DRM-12 | Source entries are immutable for the duration of the cycle: consolidation writes only new grouping or relation links and relocations out of the active retrieval path (`PR-MNT-20`). Before any cycle whose declared scope exceeds the policy change threshold, the harness MUST record a restore point. The cycle reads memory entries only, and MUST NOT carry file contents, prompts, model responses, or secrets beyond the content-free envelope posture (`PR-OBS-DAT-1`). |

### 2.7 Cycle Report and Diary

| ID | Requirement |
| --- | --- |
| PR-DRM-13 | Every non-skipped cycle emits a cycle report recorded against its bus entry (`PR-DRM-4`): counts scanned, grouped, relocated, and mechanically removed by case (`PR-MNT-23`); health signals over the consolidated scope (freshness, coverage, coherence, efficiency, reachability); a bounded number of non-obvious insights; and the proposal queue awaiting Deep Dream. A human-readable diary entry correlated to the same bus record accompanies it. Signal formulas, thresholds, the insight bound, and notification depth (silent, summary, full) are policy data (`PR-OBS-BUS-4`), not execution paths. |

### 2.8 Deep Dream Procedure

| ID | Requirement |
| --- | --- |
| PR-MNT-2 | The user MAY request a cycle at any time, and MAY participate in one. User participation is the expected mode for anything requiring judgment, and is mandatory before any deletion (`MEM-DRM-2`). |
| PR-DRM-14 | Deep Dream processes a per-item validation queue: each deletion, content reduction, contradiction resolution, verbose-entry demotion, or index rebuild step requires individual user validation (`PR-MNT-21`, `MEM-DRM-2`). Unvalidated items remain intact and stay queued for a later Deep cycle. Contradiction resolution invalidates through recorded supersession with evolution links; it MUST NOT rewrite either entry's history. |

### 2.9 Macro Derivation Inputs

| ID | Requirement |
| --- | --- |
| PR-DRM-15 | Macro derivation under `PR-MNT-22` consumes closed session narratives (`MEM-OPS-4`) and bus outcomes, including the DAG mutation classification (`PR-DAG-MUT-4`), comparing observed execution against the orchestrator's declared rationale. Each playbook rule MUST cite its constituent session anchors, and derivation MUST NOT alter or delete underlying episodic or declarative entries (`MEM-DRM-7`). |

## 3. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| The orchestrator dispatches dreaming as a work unit | Refused deterministically and visibly; the harness's scheduled cycle is unaffected. |
| Work units do not reach the maintenance barrier | Recorded as a stalled barrier; the harness halts subsequent task dispatch and escalates. |
| Deletion or content reduction is proposed without user validation in Deep Dream | Refused; the entry remains intact and the proposal waits for user-validated Deep Dream. |
| A background cycle encounters a question requiring human judgment | Preserve entries and report it for user-guided review, not autonomous deletion or resolution. |
| Deference inside the quiet window is read as suppression | Treated as a recorded skip with its reason (`PR-DRM-4`, `PR-DRM-10`); the scheduled cadence is unaffected. |
| The fast path is taken while unconsolidated growth exists | Recorded as a failed cycle; the growth remains for the next cycle. |
| The report or diary write fails | Recorded as a cycle with a report gap; links already written stand (append-only) and the gap stays visible. |
| The change threshold is exceeded with no restore point | The cycle is refused before any mutation. |
| A Deep Dream item is applied without individual validation | Refused; the entry remains intact and the item stays queued (`PR-DRM-14`). |
| Gathering promotes an entry's nature | Refused; the entry keeps its recorded nature (`MEM-TYP-6`). |
| A non-mechanical removal is attempted outside Deep Dream | Refused; the entry remains intact and the case is redirected to the Deep Dream queue (`PR-MNT-23`). |
| A scope declares both Workspace and Global tiers in one cycle | Refused before dispatch; the cycle is rejected and must redeclare a single tier (`PR-DRM-3`). |

## 4. Pattern References (informative)

The phased pass of `PR-DRM-8`..`9` follows the pattern of OpenClaw Auto-Dream's collect → consolidate → evaluate dream cycle and of the `dream-skill` 4-phase pass (orient, gather signal from recent transcripts with targeted retrieval, consolidate with date normalization and contradiction handling, prune and index). The deference and fast path of `PR-DRM-10`..`11` follow Auto-Dream's smart skip and the Hermes Dreaming proposal's quiet-activity respect; the report and diary of `PR-DRM-13` follow the Hermes dream diary (`run` / `status` / `diary`) and Auto-Dream's dream report with health signals. Archival-never-deletion in background operation follows Auto-Dream's forgetting curve (archive, never delete), constrained here to relocation without content loss in Simple Mode (`PR-MNT-19`..`20`) and judgment-based removal only in user-validated Deep Dream, with the bounded mechanical exception of `PR-MNT-23`. The objective mechanical removals of `PR-MNT-23` follow Mem0's automatic ADD/UPDATE/DELETE resolution, narrowed to cases requiring no judgment call. These are pattern references, not requirements to adopt their technologies or infrastructure.
