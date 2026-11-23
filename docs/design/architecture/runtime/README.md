# Runtime views (C4 dynamic)

Flows below are traced from code at the pinned revision. Where the product corpus describes more than the code does, the gap is stated.

| View | Diagram | Type |
| :--- | :--- | :--- |
| Install, sync, uninstall | [install-sequence.html](diagrams/install-sequence.html) | Sequence |
| VFS staged revision | [vfs-revision.html](diagrams/vfs-revision.html) | Lifecycle |
| Dispatched work unit | [work-unit.html](diagrams/work-unit.html) | Lifecycle |
| GC cycle | [gc-cycle.html](diagrams/gc-cycle.html) | Lifecycle |
| Memory session | [memory-session.html](diagrams/memory-session.html) | Sequence |

Observability and TUI state machines are notes only; they have no diagram.

## Install, sync, uninstall

Entry is `takt-ai setup install|sync|uninstall|image`. Without `--yes` nothing is applied. Conflicts block unless unrelated, or accepted with a matching path, hash and impact.

Install order: OpenCode handshake, acquire the verified local memory model and resolve or acquire CodeGraph, begin the operation record, apply plans and ownership records, register the Takt Memory adapter, start the memory service and confirm readiness, register CodeGraph, install sandbox dependencies, record the installation, reload OpenCode, then verify. A prerequisite failure restores any managed copy set aside before deployment; a failure after deployment reports the work already applied.

| Concern | Behavior | Evidence |
| :--- | :--- | :--- |
| Rollback | Files are staged and renamed; a commit error restores backups, removes new files and created directories. | `takt/setup/deploy.go` |
| Cancellation | Applied files stay; the rest are listed as not applied. Outcomes: completed, cancelled-partial, cancelled-nothing-applied. | `takt/lifecycle/lifecycle.go` |
| Reload failure | Reported in the result, not rolled back. | `takt/lifecycle/lifecycle.go` |
| Sandbox runtime | An npm install failure is non-fatal; the result lists it as incomplete. | `takt/lifecycle/lifecycle.go` |
| Crash | A leftover operation record triggers a notice that points to `takt-ai restore`; nothing is rolled back automatically. | `takt/setup/operation_record.go` |
| Sync | Files whose hash differs from the manifest are skipped as locally edited; deleted files are redeployed. | `takt/setup/operations.go` |
| Uninstall | Per file: keep, preserve or remove; edited files are preserved or restored from backup. | `takt/setup/operations.go` |
| Restore | Rewrites entries that have a backup path. | `takt/setup/restore.go` |
| Drift | A scan compares the installed preview with current files; correction applies the selected paths and re-registers Takt Memory and CodeGraph. | `takt/tui/runtime/runtime.go` |

## VFS staged revision

A claim assigns a scope; binding captures the base of each scoped path. `claim_assign` without an `author_key` never takes a path another root's claim holds: that work continues with its `author_key`, or is released or discarded first. A same-root overlap is a collision, and with the `author_key` the existing staged work continues. Reassign hands staged work to a new scope and clears the verdict; revoke frees paths and is refused while work is staged. Writes, deletes and shell calls (`shell-prepare`, then `shell-complete`) stage a delta over disk and clear any verdict. Verifier gates are prepared bound at delegation from `author_keys`; a verifier with the verify grant, who is not the author, issues a verdict bound to the revision and delta hash. Discarding staged work (`vfs_discard`) is orchestrator-only. Consolidation is orchestrator-only and flushes through a manifest, per-file replacement and base verification. A partial flush blocks every call. Offline, `takt-ai vfs recover --workspace <dir> --state <private-dir> --restore` restores a nondivergent partial flush and refuses when files diverged; `--state` is the existing private VFS state directory. In a session, invoking the original normal operation requests approval to restore that exact snapshot, or, if files diverged, to keep current files and close the incomplete effect as interrupted with evidence retained; the original operation is then reevaluated. Repair approval never authorizes consolidation. A maintenance cycle's flush is retained until the cycle completes, and until then the cycle can be discarded back to its pre-cycle state, unless a consolidated file changed since: the discard is then refused and the files stay as they are, and with approval the current files are kept and the cycle closes as interrupted with evidence retained.

Ordinary shell network is scoped by `TAKT_SANDBOX_ALLOWED_DOMAINS`: comma-separated domains are trimmed into the shell plan, and the adapter builds a strict proxy allowlist when the setting is supplied. An absent or empty setting leaves network unrestricted; the adapter API's explicit `[]` denies domains. This describes the ordinary shell path, not a universal default deny.

## Dispatched work unit

Unit state is a fold over the append-only execution history: planned, in flight, settled or withdrawn. A settled unit carries the outcome completed, failed, unverified, backtracked or interrupted; whether a line is pursued is a separate live or superseded axis. In flight is pending launch, observed running, suspended, cancellation pending, launch uncertain or unmonitored; `launched` is the event that moves pending launch to running. Capture-unmonitored retains in-flight capacity. A result applies only to its own attempt and never changes lifecycle. Assurance is carried separately from outcome: verified, failed, blocked, impossible or unverified; a completed finish with no evaluated result appends unverified. Only stream loss marks watched executions and open activities unmonitored; inactivity or EOF alone proves neither failure nor closure. Admission denials are recorded and change no state. Contest, recovery, restoration and stop events change session accounting, not unit state. Budgets come from `takt/dispatch/admission_policy.yaml` (specialists 4, unplanned units 4, contests 3, recovery failures 2; each declared recovery at most 200 actions and 3 attempts). A lent interface is switched, handed off or aborted, one holder at a time. Resume is conduct described in the prompt; the code has no resume verb. `takt-ai dag status` projects the history into a snapshot whose capture is current, empty, uncertain or unavailable; the DAG view marks its own failed query stale.

## GC cycle

Preparation is refused during a cycle; it freezes the reviewed project configuration, locks and analyzers that baseline and acceptance load. Phase is a plain string in the cycle record. Phases run baseline, investigate, collect, verify, consolidating and acceptance, ending closed. The VFS guard lets a cycle-bound binding mutate only in collect and only inside the authorized scope; a binding outside a cycle is not gated by the cycle phase. Maintenance state lives with the VFS bindings, not in a second coordinator file. Closing a cycle publishes the mandate reversion rate from successful journal entries: distinct consolidated `(cycle,path)` pairs form the denominator, and attributed create/patch/delete re-edits the numerator. Reads, repeated edits and unrelated work do not inflate it, and zero registered work publishes no rate; the count, denominator and rate travel together. Manual verbs `takt-ai gc plan|findings|refute|acceptance` run outside the coordinator. Manual plan widens its scope with CodeGraph dependents, or stays journal-only and records the gap; coordinator cycles are journal-only. Acceptance run by hand is refused while a coordinator cycle exists.

## Memory session

`takt-ai memory serve` is an independent process and the sole owner of `memory.db`. MCP runs over its private Unix socket. OpenCode registers `takt_memory` as `takt-ai memory mcp --crew`; external clients use `takt-ai memory mcp [--workspace <dir>]`, with no crew or OpenCode dependency. Both expose `record`, `search`, `read`, `sessions`, `continue_session` and `close_session`.

Before each crew call, the memory plugin vets required approvals and binds the host session, catalog agent, claimed root and workspace through `/admin`. The service resolves this binding when the tool arrives on `/crew`. When OpenCode rejects a memory call for its arguments, the plugin adds to that error where the tool's contract is. External adapters instead supply their own session identity on `/external`; external clients cannot write into or close crew sessions. Neither client opens the store.

The first committed entry establishes the session. Continuity with a closed session remains pending until that first entry; it can be replaced or withdrawn before then. Only a root session closes or declares continuity. A close stores objective, current state and the range of entries since the last close. A later entry reopens the session without changing earlier closes; closing with nothing new is refused without recording anything. Session listings derive chronology from entries. Exiting or deleting an OpenCode session does not automatically close memory.

Startup is explicit and idempotent (`memory start`; installation initializes an absent store). Missing, corrupt or newer stores are reported rather than silently replaced. The semantic worker backfills derived vectors; entries remain searchable by terms when the model is unavailable, and diagnostics expose semantic coverage. Uninstall retention obtains a consistent backup through the service before stopping the owner and retiring its store.

**Dreaming is planned, not implemented.** No dream cycles, diary or scheduler exist in code.

## Observability

Plugins send events to `takt-ai obs ingest`, which validates the envelope (version, event class and source plane, attribute allowlist, content ban) and appends to `events.db`. The Go side also writes VFS deltas, collisions and re-edits, and GC appends control records directly.

| Item | State |
| :--- | :--- |
| Event store (events, control actions) | Implemented. |
| Event classes written | Dispatch, unit lifecycle, tool activity, model usage, stream liveness, VFS delta, collision, cycle re-edit, problem rate with count and denominator, control contain (GC mandate reversion rate). |
| Event classes defined, never emitted | Unapplied GC finding, throttle, rollback, gate. |
| Escalate | Written by GC as a control record, not as a `control_escalate` event. |
| Reader of events | No generic event-control consumer exists. The GC mandate reversion rate is derived from the VFS journal at cycle close, not from the event store. |
| Reader of control actions | None. No intervention consumer exists in code. |

## TUI state machines

The top level is a screen stack: the menu opens a route, and closing a screen returns to the one below it, the menu at depth one; a jump between flows replaces the stack, so closing the destination returns to the menu.

| Route | Opens |
| :--- | :--- |
| menu | Main menu |
| install | Install flow |
| models | Model reassignment |
| drift | Drift check |
| uninstall | Uninstall flow |
| diagnostics | Functional checks |

All five flows run through one generic table-driven FSM.

| Flow | States |
| :--- | :--- |
| install | Components, Conflicts, Review, Result |
| uninstall | Modified, Memory, Review, Result, NotInstalled |
| drift | Report, Select, Review, Result, NotInstalled |
| models | Picker, Result |
| diagnostics | Collecting, Report, NotInstalled |

Install: Components confirms into Review; Review confirms into the install or back to Components to personalize; Result returns to Review on retry or leaves to the menu, models or quit. Uninstall moves Modified, Memory, Review, Result on confirm, and back reverses. Drift moves Report, Select, Review, Result, and a rescan returns to Report. Models stays in Picker while an action runs. Diagnostics moves Collecting to Report, and repair opens the repair flow.
