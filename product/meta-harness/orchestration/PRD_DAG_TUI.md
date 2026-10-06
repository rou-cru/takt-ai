# PRD: Live DAG Projection in the OpenCode v2 TUI

## 1. Outcome

Takt MUST provide a read-only OpenCode v2 TUI view that renders the execution DAG as a graph, not as a list. The view makes the orchestrator's declared route and the harness-observed execution state visible together without allowing the TUI to select, authorize, or mutate work.

The first implementation is an MVP. Its purpose is to validate faithful live projection of the DAG. Session navigation, node actions, and graph editing are out of scope.

## 2. Authority and invariants

1. The orchestrator owns route decisions and DAG declarations.
2. The harness is the sole sequencer of execution history.
3. The DAG is a projection of append-only history, never a mutable UI model.
4. The TUI is a read-only consumer of a Takt projection.
5. The TUI MUST NOT infer execution from missing events, elapsed time, agent text, or OpenCode UI state.
6. A planned node is not evidence that the node will execute.
7. A launch reservation is not evidence that execution started.
8. A finish claim is not sufficient to report successful closure when effective termination or prevailing verification is unresolved.

The projection is atemporal: the same history prefix and policy data MUST produce the same graph state. Wall-clock time, refresh age, and transport latency MUST NOT change a node's execution state or authorize a transition. They MAY be displayed as capture metadata only.

## 3. MVP scope

### Included

- A TUI plugin loaded through OpenCode v2's TUI plugin mechanism.
- A `takt.dag` route.
- A `/takt-dag` command and optional `Ctrl+Shift+D` binding.
- A layered graph with visible nodes and directed prerequisite edges.
- A compact form of the graph in the OpenCode sidebar, visible without leaving the session and never wider than the sidebar.
- Fan-out, fan-in, multiple roots, and parallel branches.
- Automatic replacement of the graph when a newer Takt projection revision is available.
- Explicit capture states: current, stale, unavailable, and uncertain.
- Node states for planned, in-flight sub-states, settled outcomes, and withdrawn work.
- A content-free snapshot contract.
- Read-only operation.

### Excluded

- Opening or selecting an agent session.
- Handoff, abort, pause, resume, retry, or other controls.
- DAG editing or route selection.
- Cost dashboards and token analytics in the graph view.
- A general-purpose graph editor.
- Reading OpenCode's private database.
- Treating a timer or polling interval as a progress or enforcement rule.

## 4. Required wireframe

The MVP MUST render a graph with explicit edges. A dependency list or bullet list does not satisfy this requirement.

```text
Takt DAG · projection 42 · capture: current

                         ┌───────────────┐
                         │ ✓ domain      │
                         │   settled     │
                         └───────┬───────┘
                                 │
                         ┌───────▼───────┐
                         │ ✓ storage     │
                         │   settled     │
                         └───────┬───────┘
                                 │
                    ┌────────────▼────────────┐
                    │ ● api                   │
                    │   observed running      │
                    └──────────┬─────────┬────┘
                               │         │
                    ┌──────────▼───┐ ┌───▼──────────┐
                    │ ◌ api-tests  │ │ ◌ api-docs   │
                    │   planned    │ │   planned    │
                    └────────┬────┘ └──────┬───────┘
                             │             │
                             └──────┬──────┘
                                    ▼
                           ┌─────────────────┐
                           │ ◌ integration   │
                           │   planned       │
                           └─────────────────┘

✓ settled/completed   ● observed running   ◌ planned
○ admitted/pending    ! uncertain          ✗ failed
↩ backtracked         × withdrawn
```

The renderer MAY use a different layout or terminal drawing primitives, but it MUST preserve the same graph semantics: nodes and directed edges MUST remain visible, including joins and branches.

Graphs with no prerequisite path between them are drawn as separate blocks, never interleaved by layer. The route's rendering flows left to right, one column per layer, and each node is a single-line box holding its source glyph, state glyph, source label and identity. The state glyph carries temporal state; the explicit source glyph/label distinguishes delegated work, direct orchestrator work and GC/maintenance. The box width accounts for the full label and identity. No agent name, wave or width annotation is drawn on the route; the legend stays there.

The sidebar has 37 usable columns, fixed by the host, and scrolls only vertically, so its form is compact and no row ever wraps:

- A bold `DAG` title followed by completed over live units, or by the capture health when it is not current.
- One muted row folding every completed unit into the short labels of the agents that did the work, in dependency order; an agent's repeated work is counted (`dev×3`), not repeated.
- The frontier (every unit neither completed nor withdrawn) as a lane graph read top-down: the state glyph is the node, each edge is a two-column lane carried down to its dependent, and joins and branches are drawn as connector rows. Edges from completed units are satisfied and omitted. Active and planned units show their identity only, never their agent.
- A layer wider than the lane budget is one grouped row of its members' glyphs and count; a frontier that still needs more lanes is listed flat in dependency order.
- Identities are cut with an ellipsis to the remaining width. Running activities follow as one row each; settled activities and withdrawn units stay on the route.

## 5. Graph semantics

The graph direction is:

```text
prerequisite ─────▶ dependent work unit
```

For every declared prerequisite `A` of unit `B`, the snapshot MUST contain an edge `A -> B`. A prerequisite edge describes the orchestrator's declared structure. It does not grant the harness authority to schedule or block `B`.

The layout SHOULD use deterministic layers, drawn as columns or as rows:

```text
depth(A) = 0                         when A has no prerequisites
depth(B) = max(depth(parent)) + 1    otherwise
```

The layout MUST be stable when only node state changes. A topology revision MAY cause a new layout. The renderer MUST NOT reorder the graph on every lifecycle or telemetry update.

An activity kind change may resize the separate activity lane because labels differ; it MUST NOT alter work-unit layout or order nodes by lifecycle state.

## 6. Node states

The projection MUST preserve the temporal distinctions defined by `PR-DAG-TMP-1` and expose them visually.

| Projection fact | TUI state |
|---|---|
| Declared and not admitted | `planned` |
| Admitted but not launched | `admitted / pending launch` |
| Execution observed | `observed running` |
| Termination or cancellation remains unresolved | `pending termination` |
| Launch or capture cannot be reconciled | `uncertain` |
| Effective termination with prevailing result | `settled` plus `completed`, `failed`, `backtracked`, or `interrupted` |
| Withdrawn before admission | `withdrawn` |

The view MUST NOT collapse all in-flight states into `running` when the projection distinguishes them.

### 6.1 Work source

Every work-unit node MUST have `node_kind: "delegated"`; direct orchestrator and non-delegated GC/maintenance execution are activity records, never work-unit nodes.

| Record | `node_kind` | Meaning |
|---|---|---|
| Work-unit node | `delegated` | Ordinary work actually delegated through dispatch. GC work belongs here only when it truly was delegated. |
| Activity | `orchestrator` | Direct work performed by the orchestrator itself. Direct work is allowed and is not an implicit policy violation. |
| Activity | `maintenance` | Harness-run GC/maintenance that was not delegated as ordinary work. |

This classification is independent of temporal state and does not affect authority, scheduling, declared prerequisites or layout depth. Preserve the actual recorded identity. Do not synthesize work units for session, plan/revision or GC-cycle records; non-unit activity may be shown only under its own true identity and must not be relabeled as a work unit. Unplanned delegated units retain the execution-order inference; activity records do not gain inferred edges. Explicit prerequisites on committed work are always preserved. Legacy history without a discriminator projects as `delegated`, preserving the meaning of existing dispatched work.

Direct implementation and harness-run GC are non-work-unit activity records, kept outside `Projection.Units` and exposed in the snapshot's separate `activities` array. Their true `activity_id` (for GC, the actual `CycleID`) is the identity. The TUI renders them in a separate activity lane with activity-specific glyphs and no prerequisite edges; they do not add or group work units.

## 7. Observability required by the view

The TUI requires a complete Takt projection, not isolated OpenCode events. The following signals MUST be available to the projection before the view can claim fidelity.

### 7.1 Topology

- Valid plan commitment or plan revision.
- Work-unit identity.
- Explicit prerequisite identities.
- Plan revision identity and expected base revision.
- Additions, withdrawals, and prerequisite changes.
- Invalid plan declarations and their reasons, without replacing the last valid plan.

### 7.2 Lifecycle

- Admission and denial dispositions.
- Launch observed by the harness.
- Effective termination.
- Pending cancellation or suspension.
- Reconciliation of uncertain launches.
- Session closure and capture uncertainty.
- Settled outcome and prevailing verification result.
- Backtracking and restoration facts.
- Direct activity start/finish and GC cycle start/finish/abort, correlated by activity ID rather than work-unit ID.

### 7.3 Correlation

Each applicable record MUST retain:

- session ID;
- work-unit ID;
- attempt ID;
- acting agent;
- semantic author;
- event sequence or history position;
- plan revision;
- relevant causal or evidence references.

An activity history entry instead carries `session_id`, `activity_id`, `node_kind`, event sequence, and activity lifecycle state. It MUST leave `work_unit_id` and `attempt_id` absent; activity identity is never copied into a work-unit field. The snapshot's top-level `session_id` scopes its activity array.

A session- or plan-scoped event MUST NOT be assigned an invented work unit.

### 7.4 Capture health

The projection MUST expose enough information to distinguish:

- a current confirmed snapshot;
- a stale display of a previously confirmed snapshot;
- an unavailable control-plane query;
- an uncertain execution state caused by missing reconciliation;
- an empty DAG.

A query failure MUST NOT be rendered as an empty or completed DAG.

### 7.5 Optional enrichment

The following MAY be included after the topology/lifecycle path is proven:

- agent name and role;
- tool-action count;
- token usage;
- model and provider;
- cost;
- acceptance-check status;
- VFS delta references.

These values MUST remain content-free. Prompts, responses, file contents, arguments, secrets, and credentials MUST NOT enter the snapshot.

## 8. Snapshot contract

Takt MUST expose a read-only snapshot command or local API. The initial implementation SHOULD use:

```text
takt-ai dag status --workspace <workspace> --session <root-session> --format json
```

The execution history is per workspace and outlives sessions, so `--session` scopes the projection to one root session; a session that has recorded nothing yet is an empty DAG, never an earlier session's graph. The TUI passes the root of the session it is showing and drops the displayed graph when that session changes.

Work-unit nodes are the orchestrator's graph for the session: a committed plan is drawn as declared; ordinary work delegated without a commitment is drawn as it ran, so units run in sequence chain and units run together are siblings, each following the units already settled when it was first admitted. The separate activity lane may show directly recorded orchestrator and GC activity without adding work-unit nodes or edges.

The response MUST contain:

```json
{
  "schema_version": 1,
  "projection_revision": 42,
  "history_position": 187,
  "session_id": "root-session",
  "capture": "current",
  "nodes": [
    {
      "id": "api",
      "node_kind": "delegated",
      "state": "in_flight",
      "flight": "observed_running",
      "session_id": "api-session",
      "attempt_id": "1",
      "launched": true,
      "prerequisites": ["storage"],
      "agent": "arch"
    }
  ],
  "edges": [
    { "from": "storage", "to": "api" }
  ],
  "activities": [
    {
      "activity_id": "a13f86e2b7c94e6d",
      "node_kind": "maintenance",
      "state": "in_flight",
      "flight": "observed_running"
    }
  ]
}
```

`projection_revision` MUST be monotonic for a projection scope. The TUI MUST replace its snapshot only when the received revision is newer than the displayed revision.

`history_position` is an ordering and provenance value. It MUST NOT be interpreted as elapsed time or a timeout.

`node_kind` is an additive field in schema version 1. A work-unit history record (`planned` or `admitted`) may omit it for legacy compatibility or set it to `delegated`; every work-unit node in a snapshot emits `delegated`. Direct orchestrator work uses the `orchestrator` activity kind, and non-delegated GC/maintenance uses `maintenance`, both in the separate activity record path. Consumers treat a missing work-unit `node_kind` from a legacy v1 producer as `delegated`; session, plan and cycle bookkeeping MUST NOT be given a fabricated work-unit identity. The discriminator is descriptive only and leaves committed prerequisites and delegated execution-order edges intact.

`agent` is additive in schema version 1: the catalog's short label of the specialist admitted for the unit's current attempt, absent until the unit is first admitted. Plans do not declare it.

`activities` is additive in schema version 1 and may be absent from legacy snapshots (the TUI treats it as empty). Each record contains only `activity_id`, `node_kind` (`orchestrator` or `maintenance`), `state`, and applicable `flight`/`outcome`. `activities` never appears in `nodes`, `Projection.Units`, or `edges`.

The plugin lane records direct activity through the ordinary dispatch CLI:

```text
takt-ai dispatch --workspace <workspace> --state <private-state> --request '{"action":"activity_start","session":"<root-session>","activity_id":"<stable-activity-id>","node_kind":"orchestrator"}'
takt-ai dispatch --workspace <workspace> --state <private-state> --request '{"action":"activity_finish","session":"<root-session>","activity_id":"<same-activity-id>","node_kind":"orchestrator","outcome":"completed"}'
```

`outcome` accepts `completed`, `failed`, or `interrupted`. The start and finish actions are idempotent for the same identity and disposition. Ordinary delegated execution continues through `admit`/`finish`; it MUST NOT be represented as an activity. The harness records a GC maintenance activity through the same history API using the exact `CycleID`: start immediately after `Coordinator.Advance` creates the cycle; finish `completed` after successful acceptance or no-change closure; finish `interrupted` after a successful abort/discard. A blocked or unresolved abort leaves the activity in flight. GC uses the activity API directly, not a fake dispatch or work-unit ID.

## 9. Live update behavior

The MVP MAY use bounded polling to obtain snapshots. The polling interval is a transport concern only; it MUST NOT participate in DAG state, progress budgets, detection, or enforcement.

The TUI MUST:

- avoid overlapping snapshot requests;
- ignore older revisions;
- render a complete snapshot atomically;
- preserve the last confirmed graph when a later query fails;
- mark the display stale when the last query fails;
- stop polling when the last view showing the graph, the route or the sidebar, is disposed; views that are open together share one poll;
- avoid redrawing when the projection revision is unchanged.

A later implementation MAY replace polling with a local Takt snapshot stream. That change MUST preserve the same snapshot and revision contract.

## 10. OpenCode v2 capabilities used

The implementation SHOULD use the supported OpenCode v2 TUI plugin surface:

| OpenCode capability | Use in this feature |
|---|---|
| `@opencode/plugin/tui` (`Plugin.define`) | TUI plugin entrypoint and types |
| Plugin discovery at `<config>/plugins/takt-dag/tui.tsx` | Loads the plugin with no config entry |
| `context.ui.router.register` | Register `takt.dag` |
| `context.ui.router.navigate` | Open the DAG route from the command |
| `context.keymap.layer` (command with `slash` and `bind`) | Register `/takt-dag` and the optional shortcut |
| `context.ui.slot` (`prepend: "sidebar.content"`) | Show the graph in the sidebar |
| OpenTUI/Solid rendering | Render graph nodes, edges, header, and capture state |
| Cleanup returned by `setup`, route component cleanup | Stop polling, cancel an active snapshot request, release resources |
| `context.data.on` | Optional refresh trigger for relevant OpenCode events |
| Plugin-local state | Store the displayed snapshot and revision |
| `context.ui.toast` | Optional non-blocking error or stale notification |

The plugin MUST NOT depend on undocumented internal TUI components when the supported route, keymap, lifecycle, and rendering APIs are sufficient.

OpenCode's server-plugin events such as `message.updated`, `session.updated`, `tool.execute.before`, and `tool.execute.after` MAY trigger a refresh, but they are not authoritative DAG state. Takt's execution history remains authoritative.

Opening sessions from the view (`context.ui.router.navigate({ type: "session" })`, `context.ui.tabs`) MAY be added in a later feature. It is explicitly excluded from this MVP.

## 11. Failure behavior

| Condition | Required display |
|---|---|
| No snapshot exists | `waiting for confirmed projection` |
| Query succeeds | Render the returned graph and revision |
| Query fails after a valid snapshot | Keep graph; mark it `stale` |
| Takt cannot provide a projection | Mark `unavailable`; do not infer state |
| Projection contains uncertain execution | Render the affected node as `uncertain` |
| Empty valid plan | Render an explicit empty DAG state |
| Invalid topology | Reject the snapshot and display a projection error; do not render a partial graph |
| OpenCode event stream is unavailable | Continue from Takt snapshot; do not attribute failure to an agent |

Telemetry or TUI failure MUST NOT interrupt the governed execution path.

## 12. Acceptance criteria

- [ ] OpenCode v2 loads the Takt TUI plugin from its plugin discovery path.
- [ ] The plugin opens a dedicated DAG route.
- [ ] The route renders nodes and directed edges, not a bullet list.
- [ ] A linear chain renders correctly.
- [ ] A fan-out renders correctly.
- [ ] A fan-in renders correctly.
- [ ] Multiple roots and parallel branches remain visible.
- [ ] Planned, in-flight, settled, withdrawn, and uncertain states are distinguishable.
- [ ] Delegated, direct orchestrator, and GC/maintenance nodes have explicit kinds, labels and glyphs independent of temporal state.
- [ ] Direct and GC activities are projected separately from work units and rendered in an edge-free activity lane with their original activity IDs.
- [ ] State changes do not unnecessarily move existing nodes.
- [ ] A topology revision updates nodes and edges atomically.
- [ ] The display uses a Takt projection derived from append-only history.
- [ ] The display does not infer closure, failure, or execution from silence.
- [ ] A failed refresh produces a stale or unavailable indicator.
- [ ] The plugin does not expose prompts, responses, file contents, arguments, or secrets.
- [ ] The TUI remains usable when Takt is unavailable.
- [ ] The MVP requires no session selector and no execution control.

## 13. Implementation sequence

1. Add the content-free DAG snapshot DTO and `takt-ai dag status`.
2. Adapt `history.Project` without introducing a second DAG state model.
3. Add projection fixtures for chain, fan-out, fan-in, withdrawal, settlement, and uncertainty.
4. Implement deterministic layered layout and edge routing.
5. Implement the OpenCode v2 TUI route and graph renderer.
6. Add revision-aware refresh and stale handling.
7. Validate the plugin against the supported OpenCode v2 build.

The MVP is complete when the TUI can show a changing, faithful DAG projection during a real Takt execution without requiring the user to inspect individual sessions or issue execution commands.
