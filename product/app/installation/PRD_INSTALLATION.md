# PRD: Installation Experience

**Actual implementation progress: 95%** — 46 own requirements: 41 complete, 5 partial, 0 not integrated. Production paths verified against real code: `takt/setup/deploy.go` (staged temp files, backup-before-overwrite, ordered commit/rollback), `takt/setup/ownership.go` + `operations.go` (manifest-driven scope, `SafeJoin` escape protection, keep/preserve/remove classification), `takt/setup/uninstall_retention.go` (explicit keep/remove choices, `VACUUM INTO` Engram database handoff into one timestamped directory), `takt/setup/drift_correction.go` + `takt/tui/drift/drift.go` (path-scoped restore blocked outright on version mismatch via `SameInstalledVersion`), `takt/setup/operation_record.go` (incomplete-operation notice naming the exact `takt-ai restore --root <path>` command), and `cmd/takt-ai/output.go`'s `blockOnConflicts`/`blockOnModifiedFiles` (the non-interactive CLI refuses the risk decisions PR-INS-43 reserves to the TUI, before any mutation). The confirmed gap is structural, not cosmetic: there is no separate Upgrade operation anywhere in `takt/lifecycle/lifecycle.go` or `takt/tui/tui.go` — `sync` doubles as the upgrade vehicle. Its backup-before-mutate, risk-acceptance policy, and preserved-choices behavior all work, but only as `sync`'s general mechanisms, not because Upgrade exists as its own planned, authorized, and reported operation (PR-INS-28/30/31/46), so all four stay partial, alongside PR-INS-5's now-moot "canonical upgrade is already authorization" clause.

## 1. Problem

Installation damages trust when it expands beyond the destination the user selected, changes an environment before the intended result is visible, or leaves an ambiguous partial state after a failure. Takt requires a bounded, reviewable, and recoverable installation, explicit about its result.

Terms such as *scope*, *target*, *managed asset*, *user-managed configuration*, and *drift* carry the meanings defined in `product/GLOSSARY.md`.

## 2. Functional Requirements

### 2.1 Scope and Applicability

| ID | Requirement |
| --- | --- |
| PR-INS-1 | Every install, reinstall, update, restore, and removal action MUST be limited to the platform and target explicitly selected by the user or by command invocation. Discovery or detection MUST NOT expand that scope. |
| PR-INS-2 | Only managed assets applicable to the selected scope MAY be planned, preserved, modified, restored, or reported. Applicability MUST be resolved before the plan is presented and MUST NOT be inferred during execution. |
| PR-INS-3 | A reinstallation or update MUST preserve the user-managed configuration and MUST affect only managed assets within its declared scope. |

### 2.2 Plan and Authorization

| ID | Requirement |
| --- | --- |
| PR-INS-4 | Before any change, the installer MUST present a complete and inspectable plan that identifies the selected scope, planned changes, applicable exclusions, and preservation or restoration implications. |
| PR-INS-5 | The installer MUST have explicit authorization before changing the environment. First-install authorization follows the visible plan; applying a scoped adjustment authorizes that edit once. An ordinary canonical upgrade request is already authorization and MUST NOT trigger redundant confirmation. New consequences or external-change decisions require review before commitment. A non-interactive invocation MAY authorize the resolved plan, but MUST NOT supply the risk decisions reserved to the TUI by PR-INS-43. The plan MUST still be resolved, validated, and exposed before mutation; plan-only invocations MUST NOT mutate state. |
| PR-INS-6 | If the plan cannot be resolved or validated, the installer MUST fail visibly before changing the environment. |

For the first release, plan validation applies to the environment observed while preparing the plan. Continuous monitoring, revalidation solely because time elapsed while awaiting authorization, and renewed approval triggered by concurrent external edits are not required. Arbitrary external mutation between validation and application is outside the guaranteed concurrency coverage.

### 2.3 Preservation and Recovery

| ID | Requirement |
| --- | --- |
| PR-INS-7 | Before the first change, the installer MUST preserve the prior state sufficient to safely restore each managed change that can be restored. |
| PR-INS-8 | The installation result MUST identify available preservation or backup material and whether restoration is complete, partial, unavailable, or not required. |
| PR-INS-9 | Restoration MUST remain limited to the declared installation scope and MUST NOT silently overwrite the user-managed configuration. |

### 2.4 Result Integrity and Reinstallation

| ID | Requirement |
| --- | --- |
| PR-INS-10 | An installation MUST complete as the approved plan or fail with a visible result identifying completed, skipped, failed, and restored work. MUST NOT leave a silent partial state. |
| PR-INS-11 | When an atomic result is not achievable, the installer MUST make the resulting state and available recovery path explicit before reporting completion or failure. |
| PR-INS-12 | Repeating the same valid installation or reinstallation request MUST converge to the same declared result without duplicating managed assets, expanding scope, or overwriting the user-managed configuration. |
| PR-INS-13 | A retry after a visible failure MUST preserve the same scope and MUST distinguish previously completed work from work still requiring action. |

### 2.5 Observable CLI Contract

| ID | Requirement |
| --- | --- |
| PR-INS-14 | The CLI MUST expose the selected scope, plan, authorization requirement, execution result, and recovery information through observable output and an exit status. |
| PR-INS-15 | A CLI invocation lacking a valid explicit target or authorization MUST NOT perform any change and MUST indicate the reason in observable output. |
| PR-INS-16 | A successful exit status MUST signify that the approved result was achieved. A failed or partial result MUST be visible and MUST return a non-zero exit status. |
| PR-INS-42 | For the first release, the CLI MUST be non-interactive. A complete invocation MUST execute without prompts; if a required input, authorization, or decision is missing, it MUST fail with a non-zero exit status and identify what is missing. It MUST NOT wait for user input or automatically open the TUI. |
| PR-INS-43 | Missing decisions discovered during planning MUST cause failure before mutation. The non-interactive CLI MUST NOT accept drift-related risk or authorize discarding user content, whether inferred from a general request to execute or supplied explicitly by the invocation. If the plan requires such a decision, the invocation MUST fail with a non-zero exit status identifying the decision required. Those decisions belong to the TUI. |
| PR-INS-44 | The first-release CLI MUST provide human-readable linear text and the defined exit status, exposing plan, result, warnings, and recovery. Non-interactive or redirected output MUST NOT contain animations or terminal-control sequences and MUST remain understandable without color. Missing TUI-only decisions fail with an explanation, not a prompt. Structured output is not required, but existing structured interfaces MUST NOT be removed or degraded. |

### 2.6 External Changes and Explicit Reset

| ID | Requirement |
| --- | --- |
| PR-INS-17 | The application MUST detect external changes relevant to the requested operation and assess the actual environment, including required tool availability. Previously recorded installation choices MUST NOT, by themselves, be treated as evidence of the current state or its functionality. |
| PR-INS-45 | The application MUST maintain a persistent installation record per scope: explicit configuration choices (including exclusions and model assignments), installed version, managed asset inventory, and possible incomplete mutation. Managed choices define expected configuration and MUST NOT be classified as external drift. The record informs planning and recovery but does not replace PR-INS-17 environment assessment. |
| PR-INS-18 | Externally modified content MUST be preservable in full. An operation MUST NOT forcibly or silently overwrite it; its origin as a Takt-installed asset MUST NOT be treated as permission to discard subsequent user changes. |
| PR-INS-19 | Detected drift MUST NOT be treated as verified functional state. Known incompatibilities and functional uncertainty MUST be reported distinctly from verified functionality. |
| PR-INS-20 | Replacing or correcting externally modified content MUST require an explicit user decision covering that content, such as a scoped reset. General authorization to update or reconfigure MUST NOT implicitly authorize discarding external changes. The replacement and preservation implications MUST be included in the approved plan. |
| PR-INS-21 | Drift that does not affect the requested operation MUST NOT require a repair decision or interrupt that operation. The application MUST preserve the affected content while allowing the user to complete the unrelated task. |
| PR-INS-22 | When external drift introduces functional uncertainty, explain the affected outcome and offer scoped correction or informed retention if the requested composition remains feasible. Prior acceptance within the same scope MUST NOT be requested again without new evidence or consequences. A known deterministically incompatible composition MUST NOT be applied as valid: identify the impediment and a viable alternative. Preference for canonical defaults alone MUST NOT block a feasible custom configuration. |
| PR-INS-23 | Risk acceptance MUST NOT authorize overwriting retained changes or reporting uncertain functionality as verified. The plan MUST distinguish feasible actions from unavailable or uncertain outcomes, and the result MUST report what was achieved and what remains limited or unverified. Functional uncertainty caused by acknowledged drift MUST NOT, by itself, be treated as an inability to resolve or validate the plan under PR-INS-6. |

### 2.7 Operation Success and Functional Readiness

| ID | Requirement |
| --- | --- |
| PR-INS-24 | Operation completion and installation functional readiness MUST be reported as separate dimensions. If all approved changes are achieved while preserving the external changes the user chose to retain, the operation MUST be classified as successful even when acknowledged drift prevents guaranteeing subsequent agent behavior. That uncertainty MUST remain visible as a warning, not be classified as an operation failure merely because the resulting configuration differs from Takt's opinionated defaults. |
| PR-INS-25 | A CLI operation satisfying PR-INS-24 MUST return a successful exit status while exposing the functional warning in its observable result. Accepting drift-related risk MUST NOT conceal execution failures. |
| PR-INS-26 | A user who retained drift MUST remain able to return to the TUI and request its explicit correction. Previous risk acceptance MUST NOT prevent subsequent correction or authorize automatic correction on a later visit. |

### 2.8 Drift Correction

| ID | Requirement |
| --- | --- |
| PR-INS-27 | Drift correction MUST restore the affected managed assets against the expected definition for their installed version and the user's selected configuration. It MUST NOT reset unrelated customizations to Takt default. |
| PR-INS-29 | A correction plan MUST identify the affected managed assets and the version-specific reference to be restored. If that reference cannot be resolved, the application MUST report the limitation rather than silently substituting a newer definition or a global default reset. |

### 2.9 Clean Removal and Deliberate Retention

| ID | Requirement |
| --- | --- |
| PR-INS-32 | Within the selected removal scope, uninstallation without external modifications MUST remove Takt's installation footprint and undo its configuration contributions, leaving the environment as if that installation had not occurred. It MUST preserve unrelated user and third-party state rather than reverting unrelated changes made since installation. |
| PR-INS-33 | For an externally modified asset originally installed by Takt, uninstallation MUST let the user explicitly choose whether to remove it as part of cleanup or retain it as their modified content. Its Takt origin MUST NOT silently determine that choice. |
| PR-INS-34 | Uninstallation MUST carry out the approved cleanup and retention choices and report retained assets separately from failed or incomplete cleanup. Content deliberately retained by the user MUST NOT be classified as unwanted residue or a cleanup failure. Unintended leftovers MUST remain visible as incomplete work rather than being relabeled as deliberate retention. |
| PR-INS-35 | Data generated through normal use that may have independent value to the user, such as accumulated memory, MUST be distinguished from disposable installation content even when no drift exists. Before removing such data, uninstallation MUST ask whether the user wants to retain it; requesting uninstallation alone MUST NOT be interpreted as choosing to discard it. |
| PR-INS-36 | When the user chooses retention, uninstallation MUST preserve the selected valuable content in a single identified directory outside the installation footprint, and the result MUST report its location. That handoff separates the retained content from disposable surrounding installation material. The user MUST NOT be left to manually extract the retained content from unwanted installation residue. Deliberately retained data MUST be reported as an agreed result, not incomplete cleanup. |

### 2.10 Cooperative Cancellation

| ID | Requirement |
| --- | --- |
| PR-INS-37 | A cancellation request received during execution MUST stop further work as soon as a stable, manageable state can be reached. The application MUST NOT interrupt a mutation in a way that corrupts the affected state merely to stop immediately. |
| PR-INS-38 | If cancellation leaves the operation incomplete, the application MUST report the partial result and provide rollback or retention as explicit alternatives, identifying the supported recovery scope and whether it is complete or partial. Cancellation MUST NOT automatically roll back or imply restoration. If required recovery is absent or fails, report that gap and the actual available next step instead of advertising an unavailable rollback; this is not conformity with the recovery requirement. |
| PR-INS-39 | If the operation has already completed before cancellation can take effect, the application MUST report the actual completed result rather than invent a partial or cancelled state. Cancellation MUST NOT implicitly undo completed work. |

### 2.11 Interrupted Operations

| ID | Requirement |
| --- | --- |
| PR-INS-40 | After abrupt termination, subsequent operations MUST use the same current-state detection, drift and corruption assessment, preservation, and idempotent reconciliation rules as other operations. An interrupted execution MUST NOT require a separate recovery model or imply that previously recorded state is still accurate. |
| PR-INS-41 | The application MUST offer rollback before a clean retry when the installation record indicates a mutating operation may not have completed. That indication MUST NOT substitute for checking the actual environment or authorize automatic rollback; the user's existing preservation and recovery choices still apply. |

### 2.12 Upgrade

| ID | Requirement |
| --- | --- |
| PR-INS-28 | Drift correction and upgrade MUST be distinct operations in planning, authorization, and execution results. An upgrade MUST NOT be substituted for a requested correction, nor may authorization to correct drift be interpreted as authorization to adopt a newer version. |
| PR-INS-30 | Before an upgrade makes its first change, the application MUST have a backup of the prior state of the affected assets, including external modifications, sufficient to recover the content it may change. If this preservation cannot be completed, the upgrade MUST NOT begin mutation. |
| PR-INS-31 | An upgrade involving retained external modifications MUST follow the same explicit risk-acceptance policy as other configuration operations, per PR-INS-22 through PR-INS-24. |
| PR-INS-46 | An upgrade MUST preserve Takt-managed choices, including exclusions and model assignments, rather than reintroducing omitted capabilities or resetting to defaults. A canonical upgrade with no new consequence runs on the original request without redundant confirmation. New incompatibility or external-change uncertainty requires the scoped decision of PR-INS-22, not a blanket reset. |

## 3. Failure Behavior

This section is the canonical statement of what is reported when an operation does not complete cleanly. Requirements above do not restate it.

| Situation | Expected Behavior |
| --- | --- |
| A change fails after preservation has begun | Report the complete known result, including completed and failed work, and indicate the available restoration outcome. |
| All approved actions complete, but retained drift leaves functionality uncertain | Report operation success with a distinct functional warning; return a successful CLI exit status, not a partial result solely because of the acknowledged uncertainty. |
| Cancellation is received while changes are in progress | Stop at the earliest stable, manageable point, report the actual partial result, and offer retention and the actually available recovery, with its limits. |
| A previous process terminated before completing an operation | Reconcile the actual state using normal detection and idempotency rules; use the interruption indication to offer rollback before retry, without assuming corruption or rolling back automatically. |

## 4. Success Criteria

- Correcting drift restores the selected version-specific configuration only where required, without implicitly upgrading or resetting unrelated customizations.
- Uninstallation removes the selected installation footprint without altering unrelated state; externally modified content is removed or retained according to an explicit choice, and deliberate retention is distinguished from incomplete cleanup.
- Valuable usage data without drift receives an explicit retention choice; retained content is handed over in an orderly form while disposable installation material is cleaned up.
- Cancellation leaves a stable, observable result with retention and available recovery clearly distinguished; a request arriving after completion does not fabricate a cancellation or trigger an implicit undo.
