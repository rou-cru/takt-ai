# PRD: Virtual File System (VFS) and Action Journaling

**Actual implementation progress: 90%** — 24 own requirements: 19 complete, 5 partial, 0 not integrated. A full read of `takt/vfs/{vfs,operation,shell,storage,recovery,cycle,paths}.go` and the 903-line `takt/agents/opencode/assets/takt-vfs.ts` shows the core is real and code-enforced, not aspirational: specialist shell mutations are captured in a private sandbox projection before the workspace is touched and imported as one staged transaction (`shell.go:107-251`, `takt-vfs.ts:227-304`); physical mutation happens only inside `materializeLocked`/`applyManifest` behind a durable pre-flush manifest, and the FS's own exclusive advisory lock plus the plugin's serialized IPC queue pause every governed operation for the duration (`recovery.go:64-107`, `vfs.go:332-356`, `storage.go:88-102`); the journal is an append-only SQLite table whose triggers reject UPDATE/DELETE (`storage.go:161-165`); a verifier can never be the author (`operation.go:513-523`, `ErrSelfVerification`); a failing verdict retains the delta instead of discarding it for execution work (`operation.go:485-505`); and `dispatch_restore` → `dispatch.Restore` → `fs.Apply(OpRollback)` (`dispatch.go:512-543`, wired at `cmd/takt-ai/dispatch.go:143-145`) closes PR-VFS-STG-6 exactly as declared. Native `edit` is denied for VFS-governed roles while the orchestrator retains direct edit capability (`components.go:93`) and mutating Git is blocked for every non-orchestrator role in both the static permission table and the dynamic per-command classifier (`renderer.go:226-243`, `shell.go:287-307`). The 5 partials mark real, narrower gaps: the invariant set a verifier judges against still declares only the user-directives document, not the work unit's contract or the framework's planning artifacts, by the plugin's own admission (`takt-vfs.ts:30-33`); ordinary workspace reads now pass the same exclusive ownership check as mutations, while verifier reads remain pinned to the authorized author staged view; and neither the milestone-commit-only-after-a-passing-acceptance ordering nor PR-DAG-TMP-8's repair-evidence binding has any code tying commit timing to an acceptance result or recording a repair distinct from historical failure — both stay orchestrator/DAG judgment calls with no enforcing mechanism.

## 1. Problem

Direct physical disk mutation, combined with routine use of destructive Git commands, introduces severe failure modes in multi-agent work: a hallucinating agent can wipe out uncommitted user work, unplanned overlapping writes corrupt the workspace immediately, and physical mutations preserve no granular record of which agent changed what and why.

The VFS is the active, transactional execution layer for VFS-governed specialist file operations. It intercepts those mutations, provides rollback, and consolidates to the physical disk only once changes achieve verified correctness. The orchestrator retains native OpenCode filesystem and shell capabilities; its direct operations are outside VFS and are not required to pass through it.

Isolation is virtual rather than Git-based because a workspace is not necessarily a Git repository, and because branch-level isolation does not isolate the uncommitted state at stake here: it does not carry untracked or ignored files, and reintegrating it requires commits that `PR-VFS-GIT-3` reserves to the orchestrator at verified milestones. What matters is the state that reaches disk, not the branch it came from.

## 2. Functional Requirements

### 2.1 Git Boundary & Mutation Restriction

| ID | Requirement |
| --- | --- |
| PR-VFS-GIT-1 | Execution specialists MUST NOT execute mutating or destructive Git commands (including `git commit`, `git reset`, `git restore`, `git clean`, `git checkout`, or `git rebase`). |
| PR-VFS-GIT-2 | Execution specialists MAY use Git for read-only historical inspection (`git diff`, `git log`, `git show`, `git blame`, and status queries). |
| PR-VFS-GIT-3 | Git commit creation is reserved to the orchestrator. Consolidation and commit are distinct steps: the milestone commit occurs only after the acceptance checks run against the materialized workspace have passed. A workspace left temporarily failing while the DAG corrects it MUST NOT be committed as a milestone. |
| PR-VFS-GIT-4 | Worktree creation for isolation is prohibited during standard DAG execution; isolation is guaranteed virtually by the VFS and contract boundaries. |
| PR-VFS-GIT-5 | Mutating Git actions other than the milestone commit are approval-gated for the orchestrator. |

### 2.2 Active Virtualization & Staging

| ID | Requirement |
| --- | --- |
| PR-VFS-STG-1 | All file mutation actions (create, update, patch, delete) initiated by VFS-governed execution specialists MUST be captured and applied within the VFS layer prior to any physical disk mutation. The orchestrator's native operations are outside this requirement. |
| PR-VFS-STG-2 | For VFS-governed specialist work, the physical filesystem MUST remain untouched during in-flight task execution until explicit consolidation conditions are met. This does not restrict the orchestrator's direct native operations. |
| PR-VFS-STG-3 | Authorized file read operations by active agents MUST transparently resolve against the merged view of the physical base state overlaid with active VFS staging changes, subject to exclusive file ownership between concurrent agents. |
| PR-VFS-STG-4 | Reverting an erroneous agent mutation MUST be performed purely within VFS by discarding the corresponding virtual transaction delta, requiring zero destructive operations on the host filesystem. |
| PR-VFS-STG-5 | Workspace mutations produced by execution-specialist shell commands MUST be captured into the same staging layer as mutations issued through file tools, per `PR-HAR-15`. No specialist execution path may write to the physical workspace outside consolidation. Orchestrator shell follows its native OpenCode permission profile and is not required to use VFS. |
| PR-VFS-STG-6 | A reversible recovery point MUST identify the prior unconsolidated VFS state and the deltas belonging to the declared recovery scope (`PR-ORQ-13`). Restoration MUST discard or restore only those virtual changes, preserve unrelated progress (including later concurrent work), and record confirmation with the resulting state reference in the Action Journal. Scope deltas MUST NOT be consolidated while that recovery is unresolved; its declared result must be evidenced first. Restoration MUST wait until affected executions can no longer act (`PR-HAR-18`). If restoration cannot be confirmed, affected work remains blocked and the failure visible; recording abandonment does not claim restoration. This guarantee does not undo completed consolidation or effects outside the VFS. |

### 2.3 Concurrency Protection & Safety Net

| ID | Requirement |
| --- | --- |
| PR-VFS-COL-1 | The VFS MUST act as the deterministic safety net of the Harness for exclusive file ownership, independently of prior planning intended to keep concurrent agents' file access disjoint in every mode. |
| PR-VFS-COL-2 | If an agent attempts to access a file owned by another agent, the VFS MUST immediately intercept and block the conflicting operation. Runtime conflicts emit a typed collision event; a refused prelaunch assignment returns a denial identifying the requested path, target, and existing owner agent/session/unit without creating unresolved work that blocks either unit. This covers mutations and cross-agent reads of an owned file. |
| PR-VFS-COL-3 | Concurrency conflicts intercepted by VFS MUST NOT leak partial or corrupted state into the physical filesystem. |
| PR-VFS-COL-4 | The trusted orchestrator MUST be able to inspect claims (key, target instance, root session, work unit, scope, and pending/active status), atomically assign a requested scope before target binding, and explicitly revoke ownership by key. The harness validates only against existing held/pending claims and refuses conflicts with exact evidence; it does not plan, transfer, expire, or clean up claims automatically. |
| PR-VFS-COL-5 | Revoking ownership releases only the path claim. It MUST preserve the binding and staged delta for inspection or recovery; an unowned staged delta cannot be consolidated. Destructive discard and rollback remain separate operations. Claims MUST persist across `FS.Close` and reopen. |
| PR-VFS-COL-6 | Every VFS operation MUST be authorized by the exact target instance's explicit catalog capability (`bind`, `extend`, `read`, `write`, `delete`, `discard`, `verify`, or `consolidate`, as applicable); RoleClass-derived VFS methods are descriptive and never authorize an operation. Scoped assignment requires explicit `bind` and `write`. A normal workspace read requires the path to be owned by that binding; verifier `ReadAs` retains its separately authorized staged-view access. |

### 2.4 Physical Consolidation (1:1 Materialization)

| ID | Requirement |
| --- | --- |
| PR-VFS-CSL-1 | Consolidation MUST flush verified VFS staging state 1:1 onto the physical filesystem in a deterministic operation atomic with respect to Takt operations. Takt operations that could observe or alter the workspace MUST pause throughout consolidation. External processes may observe intermediate replacements; external multi-file atomicity is not promised. A durable recovery manifest and pre-flush state MUST precede physical mutation; incomplete consolidation MUST block new work until explicit recovery succeeds. |
| PR-VFS-CSL-2 | Consolidation is triggered by a verified functional milestone or checkpoint; an intermediate state being technically functional does not by itself require materialization. |
| PR-VFS-CSL-3 | Consolidation is gated by a verification agent that judges the staged delta against reference invariants before any physical mutation. The invariants are, in order of precedence, the goal and directives the user set, the work unit's contract, and the framework's planning artifacts (`PR-ORQ-19`). A claim by the agent that produced the delta is never an invariant. Consolidation MUST NOT execute without a passing verdict, or while unresolved conflicts exist in the VFS. |
| PR-VFS-CSL-4 | Once consolidation is complete, the physical workspace state MUST exactly match the verified VFS projection, ready for immediate compilation, packaging, or Git commit. |
| PR-VFS-CSL-5 | A failing verdict MUST NOT discard the staged delta. The delta remains in the VFS with the verifier's finding attached, the physical workspace stays untouched, and the orchestrator opens correction work in the DAG. Discarding the delta remains the orchestrator's explicit backtracking decision under `PR-VFS-STG-4`. |
| PR-VFS-CSL-6 | Acceptance checks that execute commands — builds, test suites, linters — run against the materialized workspace after consolidation, never against staged state. Their failure is DAG work for the orchestrator to plan, not a rollback of the completed consolidation. |
| PR-VFS-CSL-7 | Every gate verdict and acceptance result MUST identify every work unit and attempt actually evaluated, the exact artifact revision/delta or materialized state evaluated, and the applicable invariant set and its version. A pass applies only to that evaluated scope; any changed artifact or applicable invariant set requires new verification before the evidence governs that new revision. Late evidence remains attached to its original subject and MUST NOT authorize consolidation or decide the outcome of a different revision. Evidence may explicitly establish that repair R satisfies A's unchanged contract against the applicable invariants; it MUST identify both the repaired contract and the evaluated repaired artifact. The DAG records this satisfaction separately from A's historical failure (`PR-DAG-TMP-8`). |

The artifact binding follows the pattern illustrated by [SLSA's attestation model](https://slsa.dev/spec/v1.2/attestation-model): evidence names its subject rather than implicitly following later artifacts. This reference does not require SLSA formats, signing infrastructure, or technology adoption.

### 2.5 Action Journaling & Audit Trail

| ID | Requirement |
| --- | --- |
| PR-VFS-JRN-1 | The Harness MUST maintain an immutable, append-only Action Journal recording every operation performed against the VFS. |
| PR-VFS-JRN-2 | Each journal entry MUST record: timestamp, authoring agent identifier/role, target file path, operation type (read/create/patch/delete/flush/rollback), and cryptographic content hashes (before/after). |
| PR-VFS-JRN-3 | The Action Journal MUST be accessible for post-mortem analysis, orchestrator replanning, and verification audit without exposing sensitive credentials. |

Shell journaling is one transaction per command, with its input revision, resulting delta hashes, identity and command outcome. It does not claim to observe every internal read or syscall. A failing command may leave a retained, unverified delta.

## 3. Failure Behavior

| Situation | Expected Behavior |
| --- | --- |
| Execution specialist attempts a mutating Git command | Deterministically blocked by the Harness with an explicit policy violation error. |
| Staged delta fails the verification gate | Delta is retained in VFS with the finding attached; physical disk remains untouched; the orchestrator opens correction work. |
| Acceptance checks fail after consolidation | The materialized workspace stays as it is, the milestone is not committed, and correction is planned as DAG work. |
| Unplanned concurrent write collision in VFS | Intercepted immediately by VFS; the affected work unit is suspended and escalated to the orchestrator. |
| Passing verdict arrives after the delta or invariants change | Preserve it for the evaluated revision; require new evidence for the current revision (`PR-VFS-CSL-7`). |
| Repair R passes against A's unchanged contract | Bind the evidence to the repaired artifact and A's contract; do not erase A's historical failure (`PR-VFS-CSL-7`, `PR-DAG-TMP-8`). |
| Recovery result requires a post-consolidation test | It cannot close a reversible recovery; scope deltas stay unconsolidated until its preconsolidation result is demonstrated. Later acceptance failure is forward correction (`PR-VFS-STG-6`, `PR-VFS-CSL-6`). |
| Backtracking recorded but execution or restoration is unconfirmed | No restoration success is claimed; preserve unrelated progress and keep affected work blocked (`PR-VFS-STG-6`). |
| Failure during physical consolidation flush | Durable pre-flush state and progress are preserved; Takt halts visibly and requires explicit recovery. Unconfirmed restoration MUST retain backups. Incompatible external changes MUST stop recovery without being overwritten. |
