# PRD: Observability — Session Feedback Control Plane

## 1. Functional Requirements

### 1.1 Telemetry Envelope

| ID | Requirement |
| --- | --- |
| PR-OBS-ENV-1 | All session telemetry — OpenCode v2-native events and Takt-native events — MUST be normalized into a single envelope schema before any evaluation. The envelope carries: schema version, session and event timestamps, source plane and runtime, acting agent identifier and role, event class, correlation identifiers (session, work unit, dispatch, related journal entry), and redacted attributes. Work-unit and dispatch correlations apply only when that scope exists; session or global events MUST NOT invent a work unit. |
| PR-OBS-ENV-2 | The canonical definition MUST pin the adopted OpenCode v2 event schema version and MUST document the adopted attribute subset and every Takt-defined extension. Envelope schema version and pinned event schema version MUST advance together through canonical-definition change, never implicitly. |
| PR-OBS-ENV-3 | OpenCode v2-native signals MUST be captured through OpenCode v2 mechanisms (native hooks or telemetry configuration projected by Takt) and MUST NOT require unsupported runtime modification. Any gap is documented, never silently absent. |
| PR-OBS-ENV-4 | Takt-native signals (VFS journal entries, harness denials, approval gates, collision events, dispatch and consolidation decisions) MUST feed the same envelope. The envelope MUST reference Action Journal entries, not duplicate them. |
| PR-OBS-ENV-5 | Every event MUST attribute the acting agent. When an event records a user decision, the event MUST distinguish the acting agent from the authority behind the decision's content. |

### 1.2 Control Bus

| ID | Requirement |
| --- | --- |
| PR-OBS-BUS-1 | The control plane MUST operate during crew execution as a single deterministic control point per session, owned by Takt-managed infrastructure and independent of any single platform runtime. |
| PR-OBS-BUS-2 | The control bus is local and mandatory: envelope evaluation, detector evaluation, and control decisions MUST function with zero network egress. External export is an optional consumer and MUST NOT be on the control path. |
| PR-OBS-BUS-4 | Policy tables — monitored signals, derived conditions, thresholds, and actions — are data within the canonical definition, not code paths embedded in orchestration logic. |
| PR-OBS-BUS-5 | The control plane MUST degrade visibly, never silently: failure to operate is observable through the system health surface, and the fallback posture is the static deterministic budgets and admission limits. Loss of a sensor stream MUST emit a stream-liveness event onto the control bus, and the affected execution is recorded as unmonitored. |
| PR-OBS-BUS-3 | Envelope events MUST be evaluated in order. Detector evaluation MUST be a deterministic function of the signal stream, the policy data, and previously recorded control state: replaying the same recorded stream against the same policy data MUST reproduce the same decisions. |
| PR-OBS-BUS-6 | EOF in a captured stream, inactivity, transport disconnection, and session closure MUST be distinct observations. None of the first three proves closure or agent failure. Session closure and post-failure reconciliation MUST be recorded with the affected scope, evidence, and capture uncertainty. After abrupt loss, the control plane MUST reconcile execution liveness, uncertain launches, and effective termination before reporting closure or releasing capacity. Unproven interruption remains capture uncertainty, not an attributed agent failure. If failure prevents immediate emission, recovery MUST append the discovered gap and reconciliation rather than imply complete capture. |
| PR-OBS-BUS-7 | When capture fails, preserve the last confirmed evidence and mark the affected execution unmonitored. In-flight work remains under known static bounds. New work uses serialized execution only when capacity and consumption are established; otherwise admission waits for reconciliation. A missing stream is never an empty or completed session. |

### 1.3 Progress Definition and Action Accounting

| ID | Requirement |
| --- | --- |
| PR-OBS-PRG-1 | Functional progress is a newly verified acceptance criterion or feature-checklist change supported by revision-bound evidence. Consolidated deltas record delivery; tool activity, dispatches, and liveness record execution, not functional progress. Planning progress is an artifact whose declared contract passes a recorded deterministic check. |
| PR-OBS-PRG-3 | A specialist's claim of completion or progress MUST be corroborated by recorded result events or consolidated deltas before it governs work continuation. |
| PR-OBS-PRG-4 | Per-work-unit baselines — duration, token consumption, and event rates by task type — MUST be derivable from recorded envelopes. These baselines feed milestone calibration. Recorded durations and envelope timestamps are calibration and analysis data only: they MUST NOT constitute a budget, an enforcement window, or an expiry that forces a control action. |
| PR-OBS-PRG-2 | One action is one tool execution recorded on the control bus within the scope being measured. All action-denominated budgets and enforcement windows MUST use the action count the harness records in the execution history; no agent's own account of its steps is authoritative, and no clock governs a budget, window, or expiry. Recorded consumption MUST be nondecreasing, and an interval without recorded actions consumes nothing; runtime restart MUST NOT reset it or silently pause it. Consumption observations, budget origins, and exhaustion decisions needed for replay MUST be recorded, and replay MUST use those records and never the consumer's current state. If consumption cannot be established after failure, the uncertainty MUST be visible and further budgeted admission withheld until reconciled, rather than granting a fresh allowance. |

### 1.4 Control Actions and Autonomy Split

| ID | Requirement |
| --- | --- |
| PR-OBS-CTL-1 | Control actions form an ordered taxonomy by intrusion: OBSERVE, THROTTLE, CONTAIN, ROLLBACK, GATE, ESCALATE. Policy MUST select the minimum sufficient action for the detected condition. OBSERVE is the recording posture of the control bus itself, GATE is the harness approval gate, and ESCALATE is the interface holder's escalation to the user. Policy selects GATE or ESCALATE only under the escalation conditions of `IR-18`. |
| PR-OBS-CTL-2 | Control actions execute within the harness execution boundary and remaining allowances. A proposed action needing wider authorization uses the applicable approval gate. |
| PR-OBS-CTL-4 | Every control action MUST be recorded with: action class, triggering condition, policy reference, acting agent, and correlation identifiers. Actions affecting file operations MUST correlate to the Action Journal. |
| PR-OBS-CTL-5 | A control action's effect MUST be observable: every action beyond OBSERVE produces a typed event back onto the control bus, and later evaluation sees post-action state. |
| PR-OBS-CTL-6 | An ESCALATE payload includes the recorded evidence that triggered the detector. |
| PR-OBS-CTL-3 | ROLLBACK applies only to unconsolidated staged deltas. |

### 1.5 Data Governance

| ID | Requirement |
| --- | --- |
| PR-OBS-DAT-1 | The envelope MUST NOT carry file contents, prompts, model responses, secrets, or credential material. Identification is by path, hash, event class, and counters. |
| PR-OBS-DAT-2 | Content capture beyond the redacted envelope is opt-in, explicit, and scoped to a declared purpose. The default posture is content-free. |
| PR-OBS-DAT-3 | Hot telemetry is session-scoped. |
| PR-OBS-DAT-5 | Telemetry is not agent memory: it MUST NOT enter the memory tiers and MUST NOT govern behavior beyond the control plane's recorded policy decisions. |
| PR-OBS-DAT-4 | External export MUST be explicitly opted into per destination and MUST fail without affecting the control loop. Export failures are counted and visible, never silent. |

### 1.6 Measurement of Session Stability

| ID | Requirement |
| --- | --- |
| PR-MNT-7 | The harness records problem rate and stability. |
| PR-MNT-9 | The problem rate MUST be derived by the harness from deterministic signals already on the control bus: previously passing checks that start failing, files re-edited after being consolidated, work units discarded or redone, and repeated collisions. An agent's own account of how its work went MUST NOT contribute to the rate. The denominator MUST be recorded work — work units dispatched, consolidations, and attempts — never elapsed time; an interval without activity leaves the rate unchanged. The derivation MUST record the raw signal counts and the denominator value alongside the rate, for the same evaluated interval. |
| PR-MNT-10 | The controlled variable is the **stability** of the rate, not its level: a flat rate at any value indicates a stable system; a rising rate indicates destabilization. Evaluation MUST be against the session's own established baseline, over comparable intervals of recorded work rather than of elapsed time. |
| PR-MNT-11 | A change in level — a different model, a harness change — establishes a new baseline rather than proving improvement. A change that lowers the mean rate while increasing its oscillation is a regression. A level change MUST be checked against the recorded counts and denominator before it is read as anything else; a rate that fell because its denominator grew is not an improvement. |
| PR-MNT-12 | A rising problem rate selects the minimum sufficient policy action, including bounded correction or safe closure. |

### 1.7 Deterministic Detection

| ID | Requirement |
| --- | --- |
| PR-OBS-DET-1 | Detector evaluation MUST be declarative and complete: for every reachable (signal, control state) pair the policy tables define exactly one outcome. Policy completeness MUST be verifiable without executing a crew session. |
| PR-OBS-DET-2 | The policy catalog MUST include a defined policy for each of the following families: dispatch yield (repeated dispatches with no functional progress under `PR-OBS-PRG-1`), stagnation over a window of recorded actions and result events, cost overrun (cumulative usage against unit budget), collision recurrence (repeated VFS collisions for the same file pair), permission friction (harness denials and repeated identical approval requests per agent and action), contest recurrence (repeated contests by the same agent), and report integrity (claim-evidence mismatch). |
| PR-OBS-DET-3 | Every window MUST be declared on a work axis — recorded actions, dispatches, or consolidations — and a threshold expressed per unit of time MUST NOT be admitted as policy data. |
| PR-OBS-DET-4 | Infrastructure failure MUST be distinguishable from agent misbehavior: detectors MUST NOT classify unmonitored execution as hung, stalled, or anomalous. |

### 1.8 Result Sensing and Advanced Control

| ID | Requirement |
| --- | --- |
| PR-OBS-RES-1 | Every local check, build, lint, typecheck, acceptance check, verifier result, and end-to-end run emits a typed result onto the control bus and into the execution history: verified, failed, blocked, impossible, or unverified. The event identifies its entity, attempt, evaluated revision, invariant version, command outcome, and evidence reference. Missing results never imply success. Each harness denial is counted as a permission-friction defect, retaining the acting role and the continuation the denial named; it is not by itself evidence of agent failure. |
| PR-OBS-RES-2 | The harness records the stagnation count in the execution history from result events and feature-checklist changes; repeated activity without either does not reset it. The control plane evaluates it, and at the policy threshold the control plane requests bounded replanning with a fresh executor context; finite correction and admission limits remain binding. |
| PR-OBS-SMC-1 | Advanced control operates only on established result sensors and reproducible baselines. Before those prerequisites hold, deterministic counters and static bounds govern execution. SMC and Kev do not replace those bounds. |
| PR-OBS-SMC-2 | SMC controls the orchestrator. It samples context occupancy, repeated-call share, malformed outputs, and harness denials at recorded work events. Policy defines normalization, event windows, references, weights, and a one-sided sliding surface. Occupancy alone does not trigger compaction or failure attribution. Missing telemetry is capture uncertainty. |
| PR-OBS-SMC-3 | The observed surface change after each intervention is evaluated against its target reaching response. A boundary layer and persistent-drift detection prevent repeated switching on noise. The controller selects the least intrusive legal action: continue, interrupt unproductive repetition, repair, or checkpoint. Failed corrections consume a finite policy budget; they do not justify indefinite retries. |
| PR-OBS-SMC-4 | Kev receives only fresh structured state, goal, applicable invariants, and explicit candidate actions, not transcripts or previous answers. Its model is pinned and its thresholds are calibrated on labeled traces. The deterministic controller validates the evaluated state version and action permissions; late, absent, or invalid recommendations cause no semantic action and leave static control active. A score alone cannot close a session. |
| PR-OBS-SMC-5 | A policy-bounded sequence of unsuccessful corrections plus evidence-backed drift confirmation initiates controlled closure. Closure records pending work and specialist state, preserves progress, terminates all remaining ability to act, and provides a structured continuation package to a fresh session, which reconciles it against owner state before resuming. When the semantic sensor is unavailable, static exhaustion closes the affected scope without claiming semantic drift. |

