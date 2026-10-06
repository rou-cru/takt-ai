# PRD: Observability — Session Feedback Control Plane

**Actual implementation progress: 40%** — 35 own requirements: 6 complete, 16 partial, 13 not integrated. The envelope/bus/store trio is real and wired end to end: `takt/obs/obs.go` defines the schema (`Envelope.Validate`, forbidden-key and per-class allowlist checks, `SchemaVersion`), `takt/obs/store.go` persists it to a workspace-local SQLite (`events`/`control_actions`, WAL, owner-only modes, append order as the replay order), and producers actually call it: `takt-vfs.ts`'s `observe()` ships `dispatch`/`unit_lifecycle`, `tool_activity` (the `execute.after` tool hook), and `model_usage` (`observeModelUsage`) through native OpenCode hooks into `takt/cli/obs.go`'s `obs ingest`, while `takt/session/session.go`'s `compose` publishes `vfs_delta`/`vfs_collision`/`cycle_reedit` from the journal. Action accounting (`PR-OBS-PRG-2`) is genuinely enforced, just from `takt/history` (append-only, update/delete-blocked) rather than literally the obs bus, per `takt/dispatch/dispatch.go`'s `Account`. What is missing is everything the "control plane" framing promises beyond recording: `PR-OBS-CTL-1`'s THROTTLE/ROLLBACK/GATE action classes are typed in `obs.go` (`ActionThrottle`, `ActionRollback`, `ActionGate`) but never invoked by any caller; the only real detector is `takt/gc/investigation.go`'s `EvaluateMandateReversionRate` check (one GC-specific signal, a hardcoded 0.25 threshold, no baseline comparison), so none of `PR-OBS-DET-2`'s seven policy families, a declarative policy table, external export, retention, or a stream-liveness event class exist.

## 1. Problem

OpenCode v2 emits telemetry designed for external monitoring; without a Takt-owned consumer, that data leaves the system while the runtime remains blind. Bounded progress budgets (`PR-ORQ-13`, `PR-HAR-6`) require a defined progress measure and an authoritative count of the work consumed against them, deviation detection "in time" (`PR-ORQ-5`) requires a sensor, and cost containment requires visibility into execution economics.

Takt therefore requires a session feedback control plane: an always-on, local sensory and control layer that normalizes OpenCode v2-native and Takt-native events into a single stream, evaluates declarative detectors against it, and executes deterministic control actions during crew execution. External observability backends are optional consumers of this stream, never its purpose.

```text
        SENSORS                     CONTROL PLANE (Takt-owned)                  ACTUATORS
┌───────────────────────┐   ┌─────────────────────────────────────┐   ┌────────────────────────┐
│ OpenCode v2 events    │   │ Envelope normalization (PR-OBS-ENV) │   │ Deny / answer gate     │
│ (native hooks,        │──▶│ Internal bus: in-order, local       │──▶│ (OpenCode v2 hooks)   │
│  telemetry)           │   │ Detector tables (policy data)       │   │ Virtual rollback (VFS) │
│ VFS journal, denials, │──▶│ Action accounting + control state   │   │ Throttle / contain     │
│ gates, collisions     │   └────────────────┬────────────────────┘   │ (orchestrator)         │
└───────────────────────┘                    │                        │ Escalate (PIRS)        │
                                             ▼ optional export        └────────────────────────┘
                                   external backend (off control path)
```

Instructions align the agent on the happy path; the harness is the wall an agent hits when it acts outside its range of action or judgment, forcing it back into alignment. A single collision is a correction. Repeated collisions against the same wall are the degradation signal, and they are distinguishable from an isolated one because denials already reach the envelope (`PR-OBS-ENV-4`) and remain visible (`PR-HAR-17`). Detector-driven control closes the loop from the first release: declarative detectors evaluate the recorded stream and trigger the minimum sufficient control action.

## 2. Functional Requirements

### 2.1 Telemetry Envelope

| ID | Requirement |
| --- | --- |
| PR-OBS-ENV-1 | All session telemetry — OpenCode v2-native events and Takt-native events — MUST be normalized into a single envelope schema before any evaluation. The envelope carries: schema version, session and event timestamps, source plane and runtime, acting agent identifier and role, event class, correlation identifiers (session, work unit, dispatch, related journal entry), and redacted attributes. Work-unit and dispatch correlations apply only when that scope exists; session or global events MUST NOT invent a work unit. |
| PR-OBS-ENV-2 | The canonical definition MUST pin the adopted OpenCode v2 event schema version and MUST document the adopted attribute subset and every `takt.*` extension. Envelope schema version and pinned event schema version MUST advance together through canonical-definition change, never implicitly. |
| PR-OBS-ENV-3 | OpenCode v2-native signals MUST be captured through OpenCode v2 mechanisms (native hooks or telemetry configuration projected by Takt) and MUST NOT require unsupported runtime modification. Any gap is documented per `PR-PLT-3`, never silently absent. |
| PR-OBS-ENV-4 | Takt-native signals (VFS journal entries, harness denials, approval gates, collision events, dispatch and consolidation decisions) MUST feed the same envelope. The Action Journal remains the authoritative record of file operations; the envelope MUST reference journal entries, not duplicate them (`PR-VFS-JRN-1`..`3`). |
| PR-OBS-ENV-5 | Every event MUST attribute the acting agent. When an event records a user decision, the event MUST distinguish the acting agent from the authority behind the decision's content (the `MEM-AUT-2` discipline applied to telemetry). |

### 2.2 Internal Control Bus

| ID | Requirement |
| --- | --- |
| PR-OBS-BUS-1 | The control plane MUST operate during crew execution as a single deterministic control point per session, owned by Takt-managed infrastructure and independent of any single platform runtime. Its deployment form is a mechanism selection under `PR-MEC-1` and `PR-MEC-2`. |
| PR-OBS-BUS-2 | The internal bus is local and mandatory: envelope evaluation, detector evaluation, and control decisions MUST function with zero network egress. External export is an optional consumer and MUST NOT be on the control path. |
| PR-OBS-BUS-4 | Policy tables — monitored signals, derived conditions, thresholds, and actions — are data within the canonical definition, not code paths embedded in orchestration logic. |
| PR-OBS-BUS-5 | The control plane MUST degrade visibly, never silently: failure to operate is observable through the system health surface, and the fallback posture is the static deterministic budgets defined elsewhere in the corpus. Loss of a sensor stream MUST emit a stream-liveness event onto the bus, so that unmonitored execution is recorded as unmonitored rather than passing as normal. |
| PR-OBS-BUS-3 | Envelope events MUST be evaluated in order. Detector evaluation MUST be a deterministic function of the signal stream, the policy data, and previously recorded control state: replaying the same recorded stream against the same policy data MUST reproduce the same decisions. |
| PR-OBS-BUS-6 | EOF in a captured stream, inactivity, transport disconnection, and session closure MUST be distinct observations. None of the first three proves closure or agent failure. Session closure and post-failure reconciliation MUST be recorded with the affected scope, evidence, and capture uncertainty. After abrupt loss, the control plane MUST reconcile execution liveness, uncertain launches, and effective termination before reporting closure or releasing capacity (`PR-HAR-17`..`18`). Unproven interruption remains capture uncertainty, not an attributed agent failure. If failure prevents immediate emission, recovery MUST append the discovered gap and reconciliation rather than imply complete capture. |

### 2.3 Progress Definition and Action Accounting

Every budget, window, and rate that governs enforcement is measured in work, never in elapsed time. An agent cannot estimate how long something will take — token transmission latency, model speed, network, and runtime downtime are noise foreign to the task — while it can estimate the effort a scope demands in steps. Time also inflates without work: a human-in-the-loop absence, a network wait, or a restart would move a temporal measure while nothing was produced, relaxing a rate or expiring a budget over healthy work. Work axes are also facts already recorded, so enforcement decisions reproduce under replay.

| ID | Requirement |
| --- | --- |
| PR-OBS-PRG-1 | Progress is defined by the control plane as the combination of: consolidated VFS deltas, acceptance-check outcomes, tool-event activity, dispatch and lifecycle transitions, and OpenCode v2 liveness. These signals are the measure against which `PR-ORQ-13` bounded progress budgets and `PR-HAR-6` enforcement are evaluated. |
| PR-OBS-PRG-3 | Specialist reports MUST be verifiable against bus ground truth: a claim of completion or progress MUST be corroborable by recorded evidence (consolidated deltas, acceptance checks) before it governs work continuation. Uncorroborated claims are treated per `PR-ORQ-8`. |
| PR-OBS-PRG-4 | Per-work-unit baselines — duration, token consumption, and event rates by task type — MUST be derivable from recorded envelopes. These baselines feed milestone calibration (`PR-ORQ-12`) and cross-session degradation analysis (`PR-DAG-USE-3`). Recorded durations and envelope timestamps are calibration and analysis data only: they MUST NOT constitute a budget, an enforcement window, or an expiry that forces a control action. |
| PR-OBS-PRG-2 | An **action** is one tool execution recorded on the bus (`PR-OBS-PRG-1`) within the scope being measured. This is the corpus's single definition: every budget, window, or rate expressed in actions refers to it, and no other document redefines it. All bounded budgets and enforcement windows MUST be evaluated against the action count owned by the control plane; no agent's own account of its steps is authoritative, and no clock governs a budget, window, or expiry. Recorded consumption MUST be nondecreasing, and an interval without recorded actions consumes nothing; runtime restart MUST NOT reset it or silently pause it. Consumption observations, budget origins, and exhaustion decisions needed for replay MUST be recorded, and replay MUST use those records and never the consumer's current state, which is what makes an action budget reproducible under `PR-DAG-REP-1`. If consumption cannot be established after failure, the uncertainty MUST be visible and further budgeted admission withheld until reconciled, rather than granting a fresh allowance. |

### 2.4 Control Actions and Autonomy Split

| ID | Requirement |
| --- | --- |
| PR-OBS-CTL-1 | Control actions form an ordered taxonomy by intrusion: OBSERVE, THROTTLE, CONTAIN, ROLLBACK, GATE, ESCALATE. Policy MUST select the minimum sufficient action for the detected condition. All six are implemented and detector-driven: OBSERVE is the recording posture of the bus itself, GATE is the harness approval gate (`PR-HAR-9`), and ESCALATE is the interface holder's judgment (`IR-9`..`11`). |
| PR-OBS-CTL-2 | The autonomy split follows the Constitution deterministically: actions confined to dispatch scheduling or VFS-virtual state are autonomous (Articles 3 and 10); extending a budget, mutating physical state, or widening permissions MUST be approval-gated (Article 5, `PR-HAR-9`); unresolved course conditions MUST escalate per PIRS (`IR-9`..`11`). |
| PR-OBS-CTL-4 | Every control action MUST be recorded with: action class, triggering condition, policy reference, acting agent, and correlation identifiers. Actions affecting file operations MUST correlate to the Action Journal. |
| PR-OBS-CTL-5 | A control action's effect MUST be observable: every action beyond OBSERVE produces a typed event back onto the bus so later evaluation sees post-action state, closing the loop. |
| PR-OBS-CTL-6 | ESCALATE payloads MUST satisfy `IR-10`: context, implications, and analyzed alternatives, including the recorded evidence that triggered the detector. |
| PR-OBS-CTL-3 | ROLLBACK is virtual-only: containment reverts staged VFS deltas per `PR-VFS-STG-4` and MUST NOT perform destructive operations against the host filesystem. |

Bounded progress budgets remain enforceable: they are static, expressed in recorded actions and attempts (`PR-ORQ-13`, `PR-OBS-PRG-2`), and enforced by the harness (`PR-HAR-6`) together with detector evaluation.

### 2.5 Data Governance

| ID | Requirement |
| --- | --- |
| PR-OBS-DAT-1 | The envelope MUST NOT carry file contents, prompts, model responses, secrets, or credential material. Identification is by path, hash, event class, and counters, consistent with `PR-VFS-JRN-3` and the platform read-deny posture. |
| PR-OBS-DAT-2 | Content capture beyond the redacted envelope is opt-in, explicit, and scoped to a declared purpose. The default posture is content-free. |
| PR-OBS-DAT-3 | Hot telemetry is session-scoped. Durable behavior snapshots MUST live under the user-managed configuration protection class (`PR-CFG-1`) with a bounded retention window declared in the canonical definition. |
| PR-OBS-DAT-5 | Telemetry is not agent memory: it MUST NOT enter the memory tiers (`ARCH_MEMORY`) and MUST NOT govern behavior beyond the control plane's recorded policy decisions. |
| PR-OBS-DAT-4 | External export MUST be explicitly opted into per destination and MUST fail without affecting the control loop. Export failures are counted and visible, never silent. |

### 2.6 Measurement of Organizational Stability

What the system regulates is the stability of the problem rate, not its absolute level. A flat rate is stable; a rising rate calls for control action or safe closure. Lowering the base rate is an optimization, not this control objective.

The rate's denominator is work, not time. Its numerator signals are all tied to work, and a temporal denominator would fall whenever time inflated without work — an absent user, a network wait, a runtime restart — presenting dilution as improved stability, exactly the false improvement `PR-MNT-11` rejects. With a work denominator an interval without activity moves neither term and the rate is unchanged, which is the correct answer: nothing happened, so nothing was learned about stability.

`PR-MNT-7` and `PR-MNT-9`..`12` are owned here. GC cadence remains governed by `PRD_GC.md`; workspace analyzers are not control-bus detectors.

| ID | Requirement |
| --- | --- |
| PR-MNT-7 | The problem rate of `PR-MNT-9` MUST be measured and recorded from the first release, and detector-driven triggering governs when cycles run: the measured rate governs cycle timing together with cadence (`PR-MNT-6`, `PR-DRM-5`). Both compose on one axis: cadence is already proportional to dispatched work units and mutation throughput, and the rate's denominator is the same recorded work (`PR-MNT-9`). |
| PR-MNT-9 | The problem rate MUST be derived by the harness from deterministic signals already on the bus: previously passing checks that start failing, files re-edited after being consolidated, work units discarded or redone, and repeated collisions. An agent's own account of how its work went MUST NOT contribute to the rate. The denominator MUST be recorded work — work units dispatched, consolidations, and attempts — never elapsed time, so that an interval without activity leaves the rate unchanged. Because any rate can be falsified by inflating its denominator, the derivation MUST record the raw signal counts and the denominator value alongside the rate, for the same evaluated interval, so a change in level is attributable to a change in denominator instead of being read as improvement. |
| PR-MNT-10 | The controlled variable is the **stability** of the rate, not its level: a flat rate at any value indicates a stable system; a rising rate indicates destabilization. Evaluation MUST be against the session's own established baseline, over comparable intervals of recorded work rather than of elapsed time. |
| PR-MNT-11 | A change in level — a different model, a harness change — establishes a new baseline rather than proving improvement. A change that lowers the mean rate while increasing its oscillation is a regression. A level change MUST be checked against the recorded counts and denominator of `PR-MNT-9` before it is read as anything else: a rate that fell because its denominator grew is not an improvement. |
| PR-MNT-12 | When the problem rate rises against its baseline (indicating entropy degradation per Lehman's laws, not an unmet GC need), the harness MUST NOT increase collection cadence to compensate. Instead, the minimum sufficient control action applies (`PR-OBS-CTL-1`), escalating promptly (`IR-9`..`11`) so the session can be safely paused or closed before control is lost. |

### 2.7 Deterministic Detection

| ID | Requirement |
| --- | --- |
| PR-OBS-DET-1 | Detector evaluation MUST be declarative and complete: for every reachable (signal, control state) pair the policy tables define exactly one outcome. The completeness standard mirrors the transition-table requirements of `PR-TUI-6`..`10` and MUST be verifiable without executing a crew session. |
| PR-OBS-DET-2 | The policy catalogue MUST include a defined policy for each of the following families: dispatch yield (repeated dispatches with zero consolidated delta and zero acceptance pass), progress staleness (zero progress beyond a window of recorded actions versus budgeted slowness), cost overrun (cumulative usage against unit budget), collision recurrence (repeated VFS collisions for the same file pair), permission friction (repeated identical approval requests per agent and action), verdict contest recurrence (repeated contests of harness-declared failure by the same agent, `PR-DAG-TMP-7`), and report integrity (claim-evidence mismatch). |
| PR-OBS-DET-3 | Thresholds and windows live in policy data and MUST NOT be hardcoded in execution paths. Changing them is canonical-definition change, reviewable as data. Every window MUST be declared on a work axis — recorded actions (`PR-OBS-PRG-2`), dispatches, or consolidations — and a threshold expressed per unit of time MUST NOT be admitted as policy data. |
| PR-OBS-DET-4 | Infrastructure failure MUST be distinguishable from agent misbehavior: detectors MUST NOT classify unmonitored execution as hung, stalled, or anomalous. The stream-liveness signal this depends on is a first-release sensing obligation (`PR-OBS-BUS-5`), and the detector's use of it applies from the first release. |

## 3. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| Control plane unavailable at session start | The failure is visible before dispatch; crew execution proceeds only under the static deterministic budgets; the degraded posture is reportable by the system health surface. |
| Control plane fails mid-session | The gap is made visible and recorded immediately when possible, otherwise during recovery (`PR-OBS-BUS-6`). Specialists in flight remain under static budgets; new dispatch uses serialized execution and static budgets only where capacity and remaining budgets are known. Uncertain admission or action accounting waits for reconciliation (`PR-HAR-17`, `PR-OBS-PRG-2`). |
| Sensor stream lost for one specialist or platform | A stream-liveness event is emitted; the specialist is marked unmonitored; dispatch to unmonitored specialists uses serialized execution until the stream recovers; no anomaly is attributed to the agent. |
| Policy data is invalid or unresolvable | Visible failure before crew execution begins, mirroring `PR-INS-6`; no implicit or default policy is substituted. |
| EOF, inactivity, or client disconnect while the session is active | Record only the observation; do not infer closed session, terminal work, or agent failure (`PR-OBS-BUS-6`). |
| Abrupt runtime loss without a closing record | Mark capture uncertainty when detected; on recovery append the gap and reconciliation. Do not rewrite the earlier prefix as interrupted (`PR-OBS-BUS-6`, `PR-DAG-TMP-4`). |
| Action consumption cannot be reconciled after restart | No fresh action allowance; uncertainty is visible and new budgeted admissions wait for reconciliation (`PR-OBS-PRG-2`). |
| The problem rate rises against its baseline | Escalate with recorded evidence so the session can be safely paused or closed (`PR-MNT-12`); do not increase GC cadence to compensate. |
| External export fails | Non-blocking; retry is bounded; the failure is counted and visible; the control loop is unaffected. |
