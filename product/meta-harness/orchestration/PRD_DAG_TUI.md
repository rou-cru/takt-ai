# PRD: Live DAG Projection in the OpenCode v2 TUI

## 1. Outcome

Takt MUST provide a read-only OpenCode v2 TUI view that renders the execution DAG as a graph, not as a list. The view makes the orchestrator's declared route and the harness-observed execution state visible together without allowing the TUI to select, authorize, or mutate work.

The view MUST NOT offer session navigation, node actions or execution controls, graph editing or route selection, cost or token analytics, or read OpenCode v2's private database.

## 2. Authority and invariants

The view consumes the history-derived topology, temporal states, and assurance of the execution history. It is not an execution authority. It MUST NOT infer execution, closure, or success from missing events, agent text, elapsed time, or OpenCode v2 UI state. Refresh age and transport latency are capture metadata only.

## 3. Entry points

The view is reachable as a full route through a command with an optional key binding, and as a compact graph in the session sidebar. It renders fan-out, fan-in, multiple roots, and parallel branches, and shows the capture states current, stale, unavailable, and uncertain.

## 4. Graph layout

The view MUST render nodes and directed prerequisite edges, including joins and branches. A list alone does not satisfy this contract.

Graphs with no prerequisite path between them are drawn as separate blocks, never interleaved by layer. The route's rendering flows left to right, one column per layer; a layer that would overflow the width starts a new band below that flows the opposite way, and so on. The route never overflows sideways: when the bands do not fit the height, they close up and then identities are cut with an ellipsis down to a floor, past which the route scrolls vertically. Running activities wrap into rows within the width the same way. Each work-unit node is a single-line box holding its delegated-source glyph, state glyph, source label and identity. Direct orchestrator work and GC/maintenance use distinct activity glyphs in the separate activity track. The box width accounts for the full label and identity unless the route has to cut it to fit. No agent name, wave or width annotation is drawn on the route; the legend stays there.

The sidebar has 37 usable columns, fixed by the host, and scrolls only vertically, so its form is compact and no row ever wraps:

- A bold `DAG` title followed by accepted over live units, or by the capture health when it is not current.
- One muted row folding every accepted unit into the short labels of the agents that did the work, in dependency order; an agent's repeated work is counted (`dev×3`), not repeated.
- The frontier (every unit neither accepted nor withdrawn) as a track graph read top-down: the state glyph is the node, each edge is a two-column track carried down to its dependent, and joins and branches are drawn as connector rows, and a unit that alone follows the unit on the row above, with no other track open, is marked `└─` and indented one level per link. Edges with verified contract satisfaction are omitted; termination alone does not satisfy a prerequisite. Active and planned units show their identity only, never their agent.
- A layer wider than the track budget is one grouped row of its members' glyphs and count; a frontier that still needs more tracks is listed flat in dependency order.
- Identities are cut with an ellipsis to the remaining width. Activities follow as one row each in their separate track, including after settlement; withdrawn units stay on the graph.

## 5. Graph semantics

Edges point from a prerequisite to its dependent work unit. For every declared prerequisite A of unit B, the snapshot MUST contain an edge from A to B. A prerequisite edge describes the orchestrator's declared structure. It does not grant the harness authority to schedule or block B.

The layout SHOULD use deterministic layers, drawn as columns or as rows. A unit without prerequisites has depth 0; any other unit has a depth one greater than its deepest prerequisite.

The layout MUST be stable when only node state changes. A topology revision MAY cause a new layout. The renderer MUST NOT reorder the graph on every lifecycle or telemetry update.

An activity kind change may resize the separate activity track because labels differ; it MUST NOT alter work-unit layout or order nodes by lifecycle state.

## 6. Node states

The view renders the temporal states and the separate assurance recorded in the execution history. It MUST NOT collapse distinct in-flight conditions into a single running state or show unverified delivery as accepted work.

### 6.1 Work source

Every work-unit node has the delegated kind; direct orchestrator and non-delegated GC/maintenance execution are activity records, never work-unit nodes.

| Record | Kind | Meaning |
|---|---|---|
| Work-unit node | delegated | Ordinary work delegated through dispatch, including simplification outside GC. |
| Activity | orchestrator | Direct work performed by the orchestrator itself. |
| Activity | maintenance | Harness-run GC/maintenance that was not delegated as ordinary work. |

This classification is independent of temporal state and does not affect authority, scheduling, declared prerequisites or layout depth. Session, plan/revision, and GC-cycle records never become work units. An activity is shown only under its activity identity (for GC, the cycle identity), in the separate activity track with activity-specific glyphs and no prerequisite edges.

## 7. Observability required by the view

The view consumes a complete snapshot of the topology, lifecycle, and evidence recorded in the execution history, not isolated OpenCode v2 events. Additional role, action-count, token, model, provider, cost, and VFS metadata remain content-free.

## 8. Snapshot contract

Takt MUST expose a read-only snapshot query that takes the workspace, the private state, the root session, and the output format, and changes nothing.

The execution history is per workspace and outlives sessions, so the root-session parameter scopes the projection to one root session; a session that has recorded nothing yet is an empty DAG, never an earlier session's graph. The TUI passes the root of the session it is showing and drops the displayed graph when that session changes.

The response MUST contain:

| Field | Content |
| :--- | :--- |
| Schema version | Contract version, 1. |
| Projection revision | Monotonic revision of the projection. |
| History position | Position in the execution history the projection reflects. |
| Session | The root session the projection is scoped to. |
| Capture | Capture state of the snapshot, current in the nominal case. |
| Nodes | One record per work unit: identity, kind, temporal state, in-flight condition, session, attempt, launch status, prerequisites, assurance, and agent once the unit is admitted. Assurance identifies the evaluated revision, acceptance status, and evidence references. |
| Edges | One record per dependency, with source and target unit identities. |
| Activities | Always present, empty when no activities exist. Each record carries activity identity, kind, state, and the applicable in-flight condition or outcome. |

The projection revision MUST be monotonic for a projection scope. The TUI MUST replace its snapshot only when the received revision is newer than the displayed revision.

The history position is an ordering and provenance value. It MUST NOT be interpreted as elapsed time or a timeout.

The agent field contains the short label of the specialist admitted for the unit's current attempt, absent until the unit is first admitted. Plans do not declare it.

## 9. Live update behavior

The view MAY use bounded polling to obtain snapshots. The polling interval is a transport concern only; it MUST NOT participate in DAG state, progress budgets, detection, or enforcement.

The TUI MUST:

- avoid overlapping snapshot requests;
- ignore older revisions;
- render a complete snapshot atomically;
- stop polling when the last view showing the graph, the route or the sidebar, is disposed; views that are open together share one poll;
- avoid redrawing when the projection revision is unchanged.

## 10. OpenCode v2 surface

The view uses only the supported OpenCode v2 TUI plugin surface: plugin discovery, route registration and navigation, command and key binding, the sidebar slot, lifecycle cleanup that stops polling and cancels an active request, and optional non-blocking notifications. It MUST NOT depend on undocumented internal TUI components when the supported surface is sufficient.

OpenCode v2 events MAY trigger a refresh, but they are not authoritative DAG state. The view MUST NOT open sessions.

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
| OpenCode v2 event stream is unavailable | Continue from Takt snapshot; do not attribute failure to an agent |

Telemetry or TUI failure MUST NOT interrupt the governed execution path.
