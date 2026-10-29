# PRD: Virtual File System (VFS) and Action Journaling

## 1. Functional Requirements

### 1.1 Git Boundary & Mutation Restriction

| ID | Requirement |
| --- | --- |
| PR-VFS-GIT-1 | Execution specialists MUST NOT execute mutating or destructive Git operations, including commit, reset, restore, clean, checkout, and rebase. |
| PR-VFS-GIT-2 | Execution specialists MAY use Git for read-only inspection: diffs, logs, object display, blame, and status. |
| PR-VFS-GIT-3 | When a result event records a feature's computational acceptance and its advisory review closes without a reproduction-backed defect, the harness creates a local checkpoint commit on the authorized working branch. The checkpoint is that feature's rollback point: rolling back is a revert commit on the working branch. Consolidation and commit are separate operations. Failing or unverified work is never checkpointed. A user instruction may change checkpoint frequency. |
| PR-VFS-GIT-4 | Worktree creation for isolation is prohibited during standard DAG execution; isolation is guaranteed virtually by the VFS and contract boundaries. |
| PR-VFS-GIT-5 | Local commits within the authorized working branch require no per-commit confirmation. Git operations that publish, rewrite history, or discard physical work require explicit scoped authorization. Authorization for a local commit does not authorize push, force push, rebase, hard reset, clean, or restore. |
| PR-VFS-GIT-6 | Commits follow the Conventional Commits format, short, with no signature or attribution trailer, unless the user says otherwise for that commit. |

### 1.2 Active Virtualization & Staging

| ID | Requirement |
| --- | --- |
| PR-VFS-STG-1 | All file mutation actions (create, update, patch, delete) initiated by VFS-governed execution specialists MUST be captured and applied within the VFS layer prior to any physical disk mutation. The orchestrator's native operations are outside this requirement. |
| PR-VFS-STG-2 | For VFS-governed specialist work, the physical filesystem MUST remain untouched during in-flight task execution until explicit consolidation conditions are met. This does not restrict the orchestrator's direct native operations. |
| PR-VFS-STG-3 | A staged read resolves against an identified physical base overlaid with the authorized staged deltas. The returned view identifies its revision. Concurrent changes do not alter an inspection already in progress. |
| PR-VFS-STG-4 | Reverting an erroneous agent mutation MUST be performed purely within VFS by discarding the corresponding virtual transaction delta, requiring zero destructive operations on the host filesystem. |
| PR-VFS-STG-5 | Workspace mutations produced by execution-specialist shell commands MUST be captured into the same staging layer as mutations issued through file tools. Orchestrator shell follows its native OpenCode v2 permission profile and is not required to use VFS. |
| PR-VFS-STG-6 | A reversible recovery point MUST identify the prior unconsolidated VFS state and the deltas belonging to the declared recovery scope. Restoration MUST discard or restore only those virtual changes, preserve unrelated progress (including later concurrent work), and record confirmation with the resulting state reference in the Action Journal. Scope deltas MUST NOT be consolidated while that recovery is unresolved; its declared result must be evidenced first. Restoration MUST wait until affected executions can no longer act. If restoration cannot be confirmed, affected work remains blocked and the failure visible; recording abandonment does not claim restoration. This guarantee does not undo completed consolidation or effects outside the VFS. |
| PR-VFS-STG-7 | Ordinary partial work is retained after dissatisfaction or a failed verdict. The orchestrator may explicitly discard it when repair is demonstrably costlier than redoing it. This ordinary retention rule does not override mandatory recovery restoration under `PR-VFS-STG-6` or the full-cycle discard of a GC cycle. |
| PR-VFS-STG-8 | A retry or a scope correction keeps the staged delta: the orchestrator reassigns the same work with its author key, and only added paths are checked for collisions. Reassigning only part of the staged paths hands that part, with its staged content, to the new owner under a new author key; the rest keeps its owner. Releasing a claim never leaves a delta without an owner; a claim holding staged work is consolidated, reassigned or explicitly discarded, and a claim with no staged work releases without asking the user, except that while its unit is in flight the release is refused, naming the abandonment call. |

### 1.3 Concurrency Protection & Safety Net

| ID | Requirement |
| --- | --- |
| PR-VFS-COL-1 | The VFS enforces exclusive write ownership per file scope. Read-only inspection does not acquire write ownership. |
| PR-VFS-COL-2 | The VFS denies conflicting writes and emits a typed collision event. Assignment refusal identifies the path, requested target, and existing owner session and unit. Reads, searches, and code-graph queries use consistent read-only views and do not block writers or fail merely because a foreign claim exists. Unbounded queries use the materialized repository view; staged inspection names its authorized authors and revision. A denial leaves no reservation that blocks unrelated work. |
| PR-VFS-COL-3 | Concurrency conflicts intercepted by VFS MUST NOT leak partial or corrupted state into the physical filesystem. |
| PR-VFS-COL-4 | The trusted orchestrator MUST be able to inspect claims (key, target instance, root session, work unit, scope, and pending/active status), atomically assign a requested scope before target binding, and explicitly revoke ownership by key. The harness validates only against existing held/pending claims and refuses conflicts with exact evidence; it does not plan, transfer, expire, or clean up claims automatically. |
| PR-VFS-COL-5 | Revoking or releasing a claim follows `PR-VFS-STG-8`. Claims MUST persist across closing and reopening the VFS. |
| PR-VFS-COL-6 | Each VFS operation requires its exact instance capability: bind, claim listing, claim assignment, claim release, read, write, delete, discard, verify, or consolidate. Role classes do not grant VFS capabilities. Assignment requires bind and write; a mutation requires ownership of its path. Read authorization is independent of write ownership. |
| PR-VFS-COL-7 | A unit declares the materialized or staged views its inspection needs. The harness grants those views before dispatch, excluding protected oracle or implementation content for that reader. A request for unavailable content returns the missing scope and the call to request a valid view. It does not impose a read mutex on the workspace. |

### 1.4 Physical Consolidation (1:1 Materialization)

| ID | Requirement |
| --- | --- |
| PR-VFS-CSL-1 | Consolidation flushes the released staged revision 1:1 onto the physical filesystem, atomically with respect to Takt operations. Observers and mutations pause during the flush; external multi-file atomicity is not promised. Record a durable recovery manifest and pre-flush state before mutation. On partial failure, block affected new work until explicit recovery succeeds. Retain backups until restoration is confirmed; stop on incompatible external changes without overwriting them. |
| PR-VFS-CSL-2 | A specialist delivers an identified staged revision with its local-check evidence. The orchestrator resolves its release before dispatching a dependent consumer. Independent ready work may proceed while the delivery awaits checks or consolidation. |
| PR-VFS-CSL-3 | Consolidation requires current passing local-check evidence for executable changes, a passing verification verdict where the plan requires one, valid ownership, and an unchanged physical base. Independent verification receives the applicable invariant set and a revision-bound view for each author; each verification remains separately attributable. The verifier receives its view already bound and never manages author bindings. Maintenance additionally requires independent confirmation that its delta preserves behavior. |
| PR-VFS-CSL-4 | After consolidation, physical workspace contents match the released staged revision. Materialization alone does not establish acceptance. |
| PR-VFS-CSL-5 | A reproduced failing check blocks consolidation, preserves its evidence, and opens correction work. Delta disposition follows PR-VFS-STG-7. |
| PR-VFS-CSL-6 | Execution specialists run bounded computational self-checks in their work loop. Each staged modification triggers the applicable local checks in an isolated merged view; generated outputs remain staged or disposable. Global suites and independent slice acceptance run on the materialized workspace. A local pass does not replace independent acceptance. Failure after consolidation requires forward correction, never destructive restoration of the host. |
| PR-VFS-CSL-7 | Every gate verdict and acceptance result MUST identify every work unit and attempt actually evaluated, the exact artifact revision/delta or materialized state evaluated, and the applicable invariant set and its version. A pass applies only to that evaluated scope; any changed artifact or applicable invariant set requires new verification before the evidence governs that new revision. Late evidence remains attached to its original subject and MUST NOT authorize consolidation or decide the outcome of a different revision. Evidence may explicitly establish that repair R satisfies A's unchanged contract against the applicable invariants; it MUST identify both the repaired contract and the evaluated repaired artifact. The DAG records this satisfaction separately from A's historical failure. |

### 1.5 Action Journaling & Audit Trail

| ID | Requirement |
| --- | --- |
| PR-VFS-JRN-1 | The harness MUST maintain an immutable, append-only Action Journal recording every operation performed against the VFS. |
| PR-VFS-JRN-2 | Each journal entry MUST record: timestamp, authoring agent identifier/role, target file path, operation type (read/create/patch/delete/flush/rollback), and cryptographic content hashes (before/after). |
| PR-VFS-JRN-3 | The Action Journal MUST be accessible for post-mortem analysis, orchestrator replanning, and verification audit without exposing sensitive credentials. |
| PR-VFS-JRN-4 | Shell journaling records one transaction per command, with its input revision, resulting delta hashes, identity, and command outcome. It does not observe every internal read or system call. A failing command may leave a retained, unverified delta. |
