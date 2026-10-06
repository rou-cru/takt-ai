// Takt DAG — managed by takt-ai; edits are overwritten on sync.
// Read-only live projection of the execution DAG (PRD_DAG_TUI.md). The view
// renders what `takt-ai dag status` reports and never selects, admits or
// mutates work.
/** @jsxImportSource @opentui/solid */
import { createHash } from "node:crypto"
import { Plugin } from "@opencode/plugin/tui"
import { For, Show, createEffect, createMemo, createSignal, onCleanup, onMount, type Accessor } from "solid-js"

const TAKT_AI = "__TAKT_AI_BINARY__"

// POLL_INTERVAL_MS is a transport cadence only: it never feeds node state,
// progress budgets, detection or enforcement (PRD_DAG_TUI.md §9).
const POLL_INTERVAL_MS = 2000

// ---------------------------------------------------------------------------
// Wire DTO (PRD_DAG_TUI.md §8), mirroring takt/history/snapshot.go.
// ---------------------------------------------------------------------------

type DagNodeState = "planned" | "in_flight" | "settled" | "withdrawn"
type DagFlight = "pending_launch" | "observed_running" | "suspended" | "cancellation_pending" | "uncertain"
type DagOutcome = "completed" | "failed" | "backtracked" | "interrupted"
type DagActivityKind = "orchestrator" | "maintenance"
type Capture = "current" | "empty" | "uncertain" | "unavailable"
const DAG_STATES: readonly DagNodeState[] = ["planned", "in_flight", "settled", "withdrawn"]
const DAG_FLIGHTS: readonly DagFlight[] = ["pending_launch", "observed_running", "suspended", "cancellation_pending", "uncertain"]
const DAG_OUTCOMES: readonly DagOutcome[] = ["completed", "failed", "backtracked", "interrupted"]
const DAG_CAPTURES: readonly Capture[] = ["current", "empty", "uncertain", "unavailable"]
const DAG_ACTIVITY_KINDS: readonly DagActivityKind[] = ["orchestrator", "maintenance"]

interface DagNode {
  readonly id: string
  /** Always emitted by the current snapshot producer; absent in legacy v1 snapshots. */
  readonly node_kind?: "delegated"
  readonly session_id?: string
  readonly attempt_id?: string
  readonly state: DagNodeState
  /** Present only while state === "in_flight". */
  readonly flight?: DagFlight
  /** Present only while state === "settled". */
  readonly outcome?: DagOutcome
  readonly launched: boolean
  readonly contract?: string
  readonly prerequisites?: readonly string[]
}

interface DagActivity {
  readonly activity_id: string
  readonly node_kind: DagActivityKind
  readonly state: "in_flight" | "settled"
  readonly flight?: "observed_running"
  readonly outcome?: DagOutcome
}

interface DagEdge {
  readonly from: string
  readonly to: string
}

interface DagUnavailable {
  readonly capture: "unavailable"
}
type DagResponse = DagSnapshot | DagUnavailable

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value)
const isString = (value: unknown): value is string => typeof value === "string"
const oneOf = <T extends string>(value: unknown, values: readonly T[]): value is T => {
  const candidates: readonly string[] = values
  return typeof value === "string" && candidates.includes(value)
}
// isOptional accepts an absent field or one that passes check.
const isOptional = <T,>(value: unknown, check: (value: unknown) => value is T): value is T | undefined =>
  value === undefined || check(value)
const isOneOf = <T extends string>(values: readonly T[]) => (value: unknown): value is T => oneOf(value, values)
const isNonNegativeInteger = (value: unknown): value is number =>
  typeof value === "number" && Number.isInteger(value) && value >= 0
const isStringArray = (value: unknown): value is string[] => Array.isArray(value) && value.every(isString)

export function decodeSnapshot(value: unknown): DagResponse {
  const invalid = (): never => { throw new Error("invalid DAG snapshot JSON") }
  if (isRecord(value) && value.capture === "unavailable") {
    if (Object.keys(value).length !== 1) return invalid()
    return { capture: "unavailable" }
  }
  if (!isRecord(value) || value.schema_version !== 1 ||
      !isNonNegativeInteger(value.projection_revision) || !isNonNegativeInteger(value.history_position) ||
      !isString(value.session_id) || !oneOf(value.capture, DAG_CAPTURES) ||
      (value.plan_version !== undefined && !isString(value.plan_version)) ||
      !Array.isArray(value.nodes) || !Array.isArray(value.edges) ||
      (value.activities !== undefined && !Array.isArray(value.activities))) return invalid()
  const nodes: DagNode[] = value.nodes.map((raw): DagNode => {
    if (!isRecord(raw) || !isString(raw.id) || raw.id.length === 0 || !oneOf(raw.state, DAG_STATES) ||
        !isOptional(raw.node_kind, isOneOf(["delegated"] as const)) ||
        !isOptional(raw.session_id, isString) ||
        !isOptional(raw.attempt_id, isString) ||
        !isOptional(raw.flight, isOneOf(DAG_FLIGHTS)) ||
        !isOptional(raw.outcome, isOneOf(DAG_OUTCOMES)) ||
        typeof raw.launched !== "boolean" ||
        !isOptional(raw.contract, isString) ||
        !isOptional(raw.prerequisites, isStringArray) ||
        (raw.state !== "in_flight" && raw.flight !== undefined) ||
        (raw.state !== "settled" && raw.outcome !== undefined)) return invalid()
    return { id: raw.id, ...(raw.node_kind === undefined ? {} : { node_kind: raw.node_kind }),
      ...(raw.session_id === undefined ? {} : { session_id: raw.session_id }),
      ...(raw.attempt_id === undefined ? {} : { attempt_id: raw.attempt_id }), state: raw.state,
      ...(raw.flight === undefined ? {} : { flight: raw.flight }),
      ...(raw.outcome === undefined ? {} : { outcome: raw.outcome }), launched: raw.launched,
      ...(raw.contract === undefined ? {} : { contract: raw.contract }),
      ...(raw.prerequisites === undefined ? {} : { prerequisites: raw.prerequisites }) }
  })
  const edges: DagEdge[] = value.edges.map((raw): DagEdge => {
    if (!isRecord(raw) || !isString(raw.from) || !isString(raw.to)) return invalid()
    return { from: raw.from, to: raw.to }
  })
  const activities: DagActivity[] | undefined = value.activities?.map((raw: unknown): DagActivity => {
    if (!isRecord(raw) || !isString(raw.activity_id) ||
        !oneOf(raw.node_kind, DAG_ACTIVITY_KINDS) ||
        (raw.state !== "in_flight" && raw.state !== "settled") ||
        (raw.flight !== undefined && raw.flight !== "observed_running") ||
        (raw.outcome !== undefined && !oneOf(raw.outcome, DAG_OUTCOMES)) ||
        (raw.state !== "in_flight" && raw.flight !== undefined) ||
        (raw.state !== "settled" && raw.outcome !== undefined)) return invalid()
    return { activity_id: raw.activity_id, node_kind: raw.node_kind, state: raw.state,
      ...(raw.flight === undefined ? {} : { flight: "observed_running" }),
      ...(raw.outcome === undefined ? {} : { outcome: raw.outcome }) }
  })
  return { schema_version: 1, projection_revision: value.projection_revision,
    history_position: value.history_position, session_id: value.session_id,
    capture: value.capture, ...(value.plan_version === undefined ? {} : { plan_version: value.plan_version }),
    nodes, edges, ...(activities === undefined ? {} : { activities }) }
}

interface DagSnapshot {
  readonly schema_version: number
  readonly projection_revision: number
  readonly history_position: number
  readonly session_id: string
  readonly capture: Capture
  readonly plan_version?: string
  readonly nodes: readonly DagNode[]
  readonly edges: readonly DagEdge[]
  /** Absent only on legacy v1 snapshots. Activities are not work units or edge endpoints. */
  readonly activities?: readonly DagActivity[]
}

// ---------------------------------------------------------------------------
// Topology check (PRD_DAG_TUI.md §11): an invalid snapshot is rejected whole,
// never rendered partially.
// ---------------------------------------------------------------------------

export function activityError(activities: readonly DagActivity[]): string | undefined {
  const activityIDs = new Set<string>()
  for (const activity of activities) {
    if (!activity.activity_id || activityIDs.has(activity.activity_id)) return "duplicate or empty activity identity"
    activityIDs.add(activity.activity_id)
  }
  return undefined
}

export function topologyError(snapshot: DagSnapshot): string | undefined {
  const ids = new Set(snapshot.nodes.map((n) => n.id))
  if (ids.size !== snapshot.nodes.length) return "duplicate node identity"
  const invalidActivity = activityError(snapshot.activities ?? [])
  if (invalidActivity) return invalidActivity
  for (const e of snapshot.edges) {
    if (!ids.has(e.from) || !ids.has(e.to)) return `edge ${e.from} -> ${e.to} names an unknown node`
  }
  const children = new Map<string, string[]>()
  for (const e of snapshot.edges) children.set(e.from, [...(children.get(e.from) ?? []), e.to])
  const done = new Set<string>()
  const path = new Set<string>()
  const cyclic = (id: string): boolean => {
    if (path.has(id)) return true
    if (done.has(id)) return false
    path.add(id)
    const found = (children.get(id) ?? []).some(cyclic)
    path.delete(id)
    done.add(id)
    return found
  }
  return [...ids].some(cyclic) ? "prerequisite cycle" : undefined
}

// ---------------------------------------------------------------------------
// Layout — deterministic layered depth (PRD_DAG_TUI.md §5):
// depth(A) = 0 without prerequisites; depth(B) = max(depth(parent)) + 1.
// Layers are columns, left to right; the nodes of a layer are stacked.
// Positions and edge routes depend on topology only, so a state-only update
// never moves a node (the whole Layout is reused).
//
// Edge routing is orthogonal. Every node owns three rows no other node
// shares: a departure row and an arrival row on its box, and a lane in the row
// gap below it that multi-layer edges run along. Each edge gets its own column
// in every gap it turns in, so no two edges share a vertical run. Junctions
// (├ ┤ ┌ ┐ └ ┘) therefore only appear where an edge leaves or enters its own
// node's row, and ┼ only where two unrelated lines cross.
// ---------------------------------------------------------------------------

type Point = readonly [x: number, y: number]

// The graph is a left-to-right timeline. Routing works in (across, along)
// coordinates (across = perpendicular to the flow, along = with it); the final
// step swaps them onto the screen.
interface Geometry {
  /** Node extent perpendicular to the flow. */
  readonly across: number
  /** Node extent along the flow. */
  readonly along: number
  /** Distance between the nodes of one layer, perpendicular to the flow. */
  readonly pitch: number
  /** Perpendicular offset where edges leave a node / arrive at it. */
  readonly depart: number
  readonly arrive: number
}

interface LayoutNode {
  readonly id: string
  readonly depth: number
  readonly x: number
  readonly y: number
}

interface Layout {
  readonly nodes: ReadonlyMap<string, LayoutNode>
  /** One orthogonal polyline per edge: node side to arrowhead. */
  readonly paths: readonly (readonly Point[])[]
  /** Node box size on screen. */
  readonly nodeWidth: number
  readonly nodeHeight: number
  readonly width: number
  readonly height: number
  /** Node-id/kind set + edge set this layout was computed from; never lifecycle state. */
  readonly topologyKey: string
}

// A node is a bordered box holding one line, `<state glyph> <id>`. NODE_ROWS
// is that line plus the two border rows; NODE_CHROME accounts for the glyph,
// its space and the two border columns, while
// geometry adds the longest label+identity in terminal cells. NODE_GAP is the empty rows between
// stacked nodes of one layer; the lane of a layer-skipping edge runs in it.
const NODE_ROWS = 3
// Kind glyph, state glyph, kind label and the two border columns.
const NODE_CHROME = 4
const NODE_GAP = 2
// Edges arrive on the interior row and depart on the bottom border row. The
// ports must differ, or an unrelated departure and arrival on the same row of
// one gap would read as a single line.
const ARRIVE_ROW = 1
const DEPART_ROW = 2

/** Every box fits its longest kind label and identity: neither is truncated. */
function geometryFor(snapshot: DagSnapshot): Geometry {
  const longest = Math.max(0, ...snapshot.nodes.map((n) => Bun.stringWidth(n.id)))
  return { across: NODE_ROWS, along: longest + NODE_CHROME, pitch: NODE_ROWS + NODE_GAP, depart: DEPART_ROW, arrive: ARRIVE_ROW }
}

export function ordinal(a: string, b: string): number {
  if (a < b) return -1
  if (a > b) return 1
  return 0
}

export function topologyKey(snapshot: DagSnapshot): string {
  const nodeIds = snapshot.nodes.map((n) => `${n.id}:${nodeKindFor(n)}`).sort(ordinal)
  const edgeKeys = snapshot.edges.map((e) => `${e.from}->${e.to}`).sort(ordinal)
  return `${nodeIds.join(",")}|${edgeKeys.join(",")}`
}

function computeDepths(snapshot: DagSnapshot): Map<string, number> {
  const parents = new Map<string, string[]>()
  for (const e of snapshot.edges) parents.set(e.to, [...(parents.get(e.to) ?? []), e.from])
  const depths = new Map<string, number>()
  // Acyclic by topologyError, so the recursion terminates.
  const depthOf = (id: string): number => {
    const cached = depths.get(id)
    if (cached !== undefined) return cached
    const depth = Math.max(-1, ...(parents.get(id) ?? []).map(depthOf)) + 1
    depths.set(id, depth)
    return depth
  }
  for (const node of snapshot.nodes) depthOf(node.id)
  return depths
}

/**
 * Node ids grouped by connectivity (edges read undirected): disconnected
 * graphs are separate components and are drawn apart. Ordered by smallest id,
 * members by id, so the result is deterministic.
 */
function connectedComponents(snapshot: DagSnapshot): string[][] {
  const neighbours = new Map<string, string[]>()
  for (const e of snapshot.edges) {
    neighbours.set(e.from, [...(neighbours.get(e.from) ?? []), e.to])
    neighbours.set(e.to, [...(neighbours.get(e.to) ?? []), e.from])
  }
  const seen = new Set<string>()
  const components: string[][] = []
  for (const id of snapshot.nodes.map((n) => n.id).sort(ordinal)) {
    if (seen.has(id)) continue
    const members: string[] = []
    const pending = [id]
    seen.add(id)
    for (let next = pending.pop(); next !== undefined; next = pending.pop()) {
      members.push(next)
      for (const n of neighbours.get(next) ?? []) if (!seen.has(n)) { seen.add(n); pending.push(n) }
    }
    components.push(members.toSorted(ordinal))
  }
  return components.sort((a, b) => ordinal(a[0], b[0]))
}

/** A horizontal run an edge takes inside one inter-layer gap. */
interface Turn {
  readonly edge: DagEdge
  readonly gap: number
  /** direct: out → in; leave: out → lane; enter: lane → in. */
  readonly kind: "direct" | "leave" | "enter"
}

function collectTurns(edges: readonly DagEdge[], depthOf: (id: string) => number): Map<number, Turn[]> {
  const turns = new Map<number, Turn[]>()
  const addTurn = (turn: Turn) => turns.set(turn.gap, [...(turns.get(turn.gap) ?? []), turn])
  for (const edge of edges) {
    const from = depthOf(edge.from)
    const to = depthOf(edge.to)
    if (to === from + 1) addTurn({ edge, gap: from, kind: "direct" })
    else {
      addTurn({ edge, gap: from, kind: "leave" })
      addTurn({ edge, gap: to - 1, kind: "enter" })
    }
  }
  return turns
}

// A gap holds a stem row, one row per turn, and an arrowhead row.
function layerOffsets(turns: ReadonlyMap<number, readonly Turn[]>, maxDepth: number, along: number): number[] {
  const layerY: number[] = []
  for (let d = 0, y = 0; d <= maxDepth; d++) {
    layerY[d] = y
    const count = turns.get(d)?.length ?? 0
    y += along + (count === 0 ? 1 : count + 2)
  }
  return layerY
}

// componentColumns gives each connected component a band of its own, stacked
// across the flow, so disconnected graphs never share a column. Within a layer
// nodes are ordered by id: identity only, so stable.
function componentColumns(snapshot: DagSnapshot, depthOf: (id: string) => number): Map<string, number> {
  const column = new Map<string, number>()
  let bandStart = 0
  for (const members of connectedComponents(snapshot)) {
    const layers = new Map<number, string[]>()
    for (const id of members) layers.set(depthOf(id), [...(layers.get(depthOf(id)) ?? []), id])
    for (const ids of layers.values()) ids.forEach((id, index) => column.set(id, bandStart + index))
    bandStart += Math.max(...[...layers.values()].map((ids) => ids.length))
  }
  return column
}

export function computeLayout(snapshot: DagSnapshot, previous?: Layout): Layout {
  const key = topologyKey(snapshot)
  if (previous?.topologyKey === key) return previous
  const g = geometryFor(snapshot)

  const depths = computeDepths(snapshot)
  const depthOf = (id: string) => depths.get(id) ?? 0
  const column = componentColumns(snapshot, depthOf)
  // Perpendicular position of a node: its row on screen.
  const xOf = (id: string) => (column.get(id) ?? 0) * g.pitch

  const turns = collectTurns(snapshot.edges, depthOf)
  // Longest horizontal run first: a fan-out's far branch leaves above the
  // near one and a fan-in's near branch joins above the far one, so neither
  // crosses its own sibling. Ties break on identity for determinism.
  const span = (t: Turn) => {
    const out = t.kind === "enter" ? xOf(t.edge.from) + g.across : xOf(t.edge.from) + g.depart
    const inn = t.kind === "leave" ? xOf(t.edge.from) + g.across : xOf(t.edge.to) + g.arrive
    return Math.abs(inn - out)
  }
  const rowIndex = new Map<Turn, number>()
  for (const list of turns.values()) {
    list.sort((a, b) => span(b) - span(a) || `${a.edge.from}>${a.edge.to}>${a.kind}`.localeCompare(`${b.edge.from}>${b.edge.to}>${b.kind}`))
    list.forEach((turn, index) => rowIndex.set(turn, index))
  }

  const maxDepth = Math.max(0, ...depths.values())
  const layerY = layerOffsets(turns, maxDepth, g.along)
  const stemRow = (gap: number) => layerY[gap] + g.along
  const arrowRow = (gap: number) => layerY[gap + 1] - 1
  const turnRow = (turn: Turn) => stemRow(turn.gap) + 1 + (rowIndex.get(turn) ?? 0)

  const place = (across: number, along: number): Point => [along, across]

  const nodes = new Map<string, LayoutNode>()
  for (const node of snapshot.nodes) {
    const [x, y] = place(xOf(node.id), layerY[depthOf(node.id)])
    nodes.set(node.id, { id: node.id, depth: depthOf(node.id), x, y })
  }

  const paths: Point[][] = []
  const byEdge = new Map<DagEdge, Turn[]>()
  for (const list of turns.values()) for (const t of list) byEdge.set(t.edge, [...(byEdge.get(t.edge) ?? []), t])
  for (const edge of snapshot.edges) {
    const out = xOf(edge.from) + g.depart
    const inn = xOf(edge.to) + g.arrive
    const start = place(out, stemRow(depthOf(edge.from)))
    const end = place(inn, arrowRow(depthOf(edge.to) - 1))
    const edgeTurns = byEdge.get(edge) ?? []
    const direct = edgeTurns.find((t) => t.kind === "direct")
    if (direct) {
      const row = turnRow(direct)
      paths.push([start, place(out, row), place(inn, row), end])
      continue
    }
    const lane = xOf(edge.from) + g.across
    const leaveTurn = edgeTurns.find((t) => t.kind === "leave")
    const enterTurn = edgeTurns.find((t) => t.kind === "enter")
    if (!leaveTurn || !enterTurn) throw new Error("invalid DAG layout turns")
    const leave = turnRow(leaveTurn)
    const enter = turnRow(enterTurn)
    paths.push([start, place(out, leave), place(lane, leave), place(lane, enter), place(inn, enter), end])
  }

  let acrossTotal = g.pitch
  for (const id of column.keys()) acrossTotal = Math.max(acrossTotal, xOf(id) + g.pitch)
  const alongTotal = layerY[maxDepth] + g.along
  const [width, height] = place(acrossTotal, alongTotal)
  const [nodeWidth, nodeHeight] = place(g.across, g.along)
  return { nodes, paths, nodeWidth, nodeHeight, width, height, topologyKey: key }
}

const UP = 1
const DOWN = 2
const LEFT = 4
const RIGHT = 8

const LINE_GLYPHS: Record<number, string> = {
  [UP]: "│", [DOWN]: "│", [UP | DOWN]: "│",
  [LEFT]: "─", [RIGHT]: "─", [LEFT | RIGHT]: "─",
  [DOWN | RIGHT]: "┌", [DOWN | LEFT]: "┐", [UP | RIGHT]: "└", [UP | LEFT]: "┘",
  [UP | DOWN | RIGHT]: "├", [UP | DOWN | LEFT]: "┤",
  [LEFT | RIGHT | DOWN]: "┬", [LEFT | RIGHT | UP]: "┴",
  [UP | DOWN | LEFT | RIGHT]: "┼",
}

/** Rasterizes every edge path into text rows; cells record their links. */
export function edgeRows(layout: Layout): string[] {
  const mask: number[][] = Array.from({ length: layout.height }, () => new Array<number>(layout.width).fill(0))
  const link = ([ax, ay]: Point, [bx, by]: Point) => {
    for (let y = Math.min(ay, by); y < Math.max(ay, by); y++) {
      mask[y][ax] |= DOWN
      mask[y + 1][ax] |= UP
    }
    for (let x = Math.min(ax, bx); x < Math.max(ax, bx); x++) {
      mask[ay][x] |= RIGHT
      mask[ay][x + 1] |= LEFT
    }
  }
  for (const path of layout.paths) {
    if (path.length === 0) throw new Error("invalid DAG layout: empty edge path")
    const [sx, sy] = path[0]
    mask[sy][sx] |= LEFT // the stem leaves the parent box
    for (let i = 1; i < path.length; i++) link(path[i - 1], path[i])
    const end = path.at(-1)
    if (!end) throw new Error("invalid DAG layout: empty edge path")
    const [ex, ey] = end
    mask[ey][ex] |= RIGHT // the edge runs into the child box
  }
  return mask.map((row) => row.map((m) => LINE_GLYPHS[m] ?? " ").join(""))
}

interface Port {
  readonly x: number
  readonly y: number
  readonly glyph: string
}

/**
 * Where edges meet their boxes, drawn over the borders: the departure joins
 * the parent's bottom-right corner (┘ becomes ┴) and the arrowhead replaces
 * the child's left border.
 */
export function edgePorts(layout: Layout): Port[] {
  const ports = new Map<string, Port>()
  for (const path of layout.paths) {
    if (path.length === 0) throw new Error("invalid DAG layout: empty edge path")
    const [sx, sy] = path[0]
    const end = path.at(-1)
    if (!end) throw new Error("invalid DAG layout: empty edge path")
    const [ex, ey] = end
    ports.set(`${sx - 1},${sy}`, { x: sx - 1, y: sy, glyph: "┴" })
    ports.set(`${ex + 1},${ey}`, { x: ex + 1, y: ey, glyph: "▶" })
  }
  return [...ports.values()]
}

// ---------------------------------------------------------------------------
// Node glyphs (PRD_DAG_TUI.md §4 legend, §6 state table). The §6
// table also names `suspended`, `pending termination` and `interrupted`,
// which the §4 legend has no glyph for; each gets its own so no in-flight or
// settled distinction collapses into another.
// ---------------------------------------------------------------------------

export function glyphFor(node: DagNode): string {
  switch (node.state) {
    case "planned":
      return "◌"
    case "withdrawn":
      return "×"
    case "settled":
      switch (node.outcome) {
        case "failed":
          return "✗"
        case "backtracked":
          return "↩"
        case "interrupted":
          return "⊘"
        default:
          return "✓"
      }
    case "in_flight":
      switch (node.flight) {
        case "pending_launch":
          return "○"
        case "uncertain":
          return "!"
        case "suspended":
          return "⏸"
        case "cancellation_pending":
          return "↯"
        default:
          return "●"
      }
  }
}

export function nodeKindFor(node: DagNode): "delegated" {
  return node.node_kind ?? "delegated"
}

// A node reads as its state and identity; the kind is omitted while
// "delegated" is the only kind, since it would tell nothing apart.
export function nodeText(node: DagNode): string {
  return `${glyphFor(node)} ${node.id}`
}

export function activityText(activity: DagActivity): string {
  const glyph = activity.state === "in_flight" ? "◆" : "◇"
  const label = activity.node_kind === "orchestrator" ? "direct activity" : "GC activity"
  const status = activity.state === "in_flight" ? "running" : activity.outcome ?? "settled"
  return `${glyph} ${label} ${activity.activity_id} · ${status}`
}

export function activityBoxWidth(activity: DagActivity): number {
  return Bun.stringWidth(activityText(activity)) + 2
}

// The glyph legend, shown on demand in the route view (`?`).
const LEGEND_LINES = [
  "✓ completed   ● running   ◌ planned   ○ admitted   ! uncertain",
  "✗ failed   ↩ backtracked   × withdrawn   ⏸ suspended   ↯ stopping   ⊘ interrupted",
  "◆ direct activity   ◇ GC activity",
]

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

/** Display health: the snapshot's own capture, or the view's failure state. */
type Health = "waiting" | "confirmed" | "stale" | "unavailable" | "invalid"

/**
 * The graph: layers left to right, every node one typed, state-bearing box.
 * On the route it fills the space above the prompt and scrolls both ways with
 * the keyboard focused on it. In the sidebar it is as tall as the graph, since
 * the host's sidebar scrolls vertically, and scrolls sideways within the
 * column; the extra row is the horizontal scrollbar.
 */
export function GraphView(props: { readonly snapshot: DagSnapshot; readonly mode: "route" | "sidebar" }) {
  // Solid runs a component body once per mount, so this closure carries the
  // last layout across reactive re-runs and a state-only update reuses it.
  let previous: Layout | undefined
  const layout = createMemo(() => (previous = computeLayout(props.snapshot, previous)))
  const hasNodes = createMemo(() => props.snapshot.nodes.length > 0)
  const rows = createMemo(() => (hasNodes() ? edgeRows(layout()) : []))
  const ports = createMemo(() => edgePorts(layout()))
  const workHeight = createMemo(() => (hasNodes() ? layout().height : 0))
  const activities = createMemo(() => props.snapshot.activities ?? [])
  const activityLaneWidth = createMemo(() => activities().reduce((width, activity) => width + activityBoxWidth(activity) + 1, 0))
  const activityHeight = createMemo(() => activities().length === 0 ? 0 : 4)
  const activityLeft = (index: number) => activities().slice(0, index).reduce((left, activity) => left + activityBoxWidth(activity) + 1, 0)
  const graphWidth = createMemo(() =>
    Math.max(hasNodes() ? layout().width : 0, activityLaneWidth()),
  )
  const graphHeight = createMemo(() => workHeight() + activityHeight())
  const viewport = () =>
    props.mode === "route"
      ? { scrollY: true, focused: true, flexGrow: 1, minWidth: 0, minHeight: 0 }
      : { scrollY: false, height: graphHeight() + 1, flexShrink: 0 }

  return (
    <scrollbox scrollX width="100%" {...viewport()} horizontalScrollbarOptions={{ showArrows: true }}>
      {/* flexShrink 0: the graph keeps its size and overflows the viewport
          instead of being squeezed into it. */}
      <box width={graphWidth()} height={graphHeight()} flexShrink={0}>
        <For each={rows()}>
          {(row, y) => (
            <text position="absolute" left={0} top={y()}>
              {row}
            </text>
          )}
        </For>
        <For each={props.snapshot.nodes}>
          {(node) => (
            <Show when={layout().nodes.get(node.id)}>
              {(pos: Accessor<LayoutNode>) => (
                <box
                  position="absolute"
                  left={pos().x}
                  top={pos().y}
                  width={layout().nodeWidth}
                  height={layout().nodeHeight}
                  border
                >
                  <text>{nodeText(node)}</text>
                </box>
              )}
            </Show>
          )}
        </For>
        <For each={ports()}>
          {(port) => (
            <text position="absolute" left={port.x} top={port.y} zIndex={1}>
              {port.glyph}
            </text>
          )}
        </For>
        <For each={activities()}>
          {(activity, index) => (
            <box
              position="absolute"
              left={activityLeft(index())}
              top={workHeight() + 1}
              width={activityBoxWidth(activity)}
              height={3}
              border
            >
              <text>{activityText(activity)}</text>
            </box>
          )}
        </For>
      </box>
    </scrollbox>
  )
}

/** Work units or activities: an activity-only snapshot still has a lane to draw. */
export function hasContent(snapshot: DagSnapshot): boolean {
  return snapshot.nodes.length > 0 || (snapshot.activities?.length ?? 0) > 0
}

// progressLine counts what a person tracks: units done and running.
export function progressLine(snapshot: DagSnapshot): string {
  const done = snapshot.nodes.filter((n) => n.state === "settled" && (n.outcome ?? "completed") === "completed").length
  const running = snapshot.nodes.filter((n) => n.state === "in_flight" && (n.flight ?? "observed_running") === "observed_running").length
  const parts = [`${done}/${snapshot.nodes.length} done`]
  if (running > 0) parts.push(`${running} running`)
  return parts.join(" · ")
}

// headerLine names the view, then whatever needs attention: a capture that is
// not current, otherwise the progress counts. Projection and plan revisions
// are internal and stay out.
export function headerLine(snapshot: DagSnapshot | undefined, health: Health): string {
  const capture = snapshot && health === "confirmed" ? snapshot.capture : health
  if (capture !== "current") return `Takt DAG · ${capture}`
  if (!snapshot || snapshot.nodes.length === 0) return "Takt DAG"
  return `Takt DAG · ${progressLine(snapshot)}`
}

/** The sidebar header is the same short line: the list is the content. */
export function sidebarHeader(snapshot: DagSnapshot | undefined, health: Health): string {
  return headerLine(snapshot, health)
}

// sidebarRows lists the DAG for a narrow column without implying structure it
// lacks: one block per disconnected graph (blank row between), units in
// dependency order (same-layer units are adjacent: parallel), each followed by
// its real prerequisites; then activities.
export function sidebarRows(snapshot: DagSnapshot): string[] {
  const depths = computeDepths(snapshot)
  const byId = new Map(snapshot.nodes.map((n) => [n.id, n]))
  const prerequisites = new Map<string, string[]>()
  for (const e of snapshot.edges) prerequisites.set(e.to, [...(prerequisites.get(e.to) ?? []), e.from])
  const blocks = connectedComponents(snapshot).map((members) =>
    members
      .toSorted((a, b) => (depths.get(a) ?? 0) - (depths.get(b) ?? 0) || ordinal(a, b))
      .map((id) => {
        const from = (prerequisites.get(id) ?? []).sort(ordinal)
        return nodeText(byId.get(id) as DagNode) + (from.length > 0 ? ` ← ${from.join(", ")}` : "")
      }),
  )
  const rows = blocks.flatMap((block, i) => (i > 0 ? ["", ...block] : block))
  const activities = snapshot.activities ?? []
  if (activities.length > 0 && rows.length > 0) rows.push("")
  return rows.concat(activities.map(activityText))
}

/**
 * The sidebar's list, or nothing: unlike the route view it never explains
 * why (no confirmed projection, empty DAG), the header line already does.
 * Every branch below must resolve to a <text>, never to `false`/`undefined`
 * directly under <box> — Show's own off-state placeholder needs one too.
 */
export function SidebarGraph(props: { readonly snapshot: DagSnapshot | undefined }) {
  return (
    <Show when={props.snapshot} fallback={<text></text>}>
      {(shown: Accessor<DagSnapshot>) => (
        <Show when={hasContent(shown())} fallback={<text></text>}>
          <box flexDirection="column">
            <For each={sidebarRows(shown())}>{(row) => <text>{row}</text>}</For>
          </box>
        </Show>
      )}
    </Show>
  )
}

// ---------------------------------------------------------------------------
// Snapshot query
// ---------------------------------------------------------------------------

// stateDir is the private store takt-vfs.ts binds to this workspace: slug +
// 8 hex chars of the absolute path's SHA-256, outside the workspace itself.
function stateDir(workspace: string): string {
  const abs = workspace.startsWith("/") ? workspace : `${process.env.HOME}/${workspace}`
  const slug = abs.split("/").findLast(Boolean) ?? "workspace"
  const hash = createHash("sha256").update(abs).digest("hex").slice(0, 8)
  return `${process.env.HOME}/.local/share/takt-ai/vfs/${slug}-${hash}`
}

export default Plugin.define({
  id: "takt.dag",
  setup(context) {
    const workspace = (context.location ?? context.data.location.default()).directory
    const [snapshot, setSnapshot] = createSignal<DagSnapshot | undefined>()
    const [health, setHealth] = createSignal<Health>("waiting")
    const [problem, setProblem] = createSignal<string | undefined>()
    // The private state directory is per workspace, so its history spans every
    // session that ever ran there; the graph shown is only this root session's.
    const [session, setSession] = createSignal<string | undefined>()

    // A failed query keeps the last confirmed graph and marks it stale;
    // without one there is nothing to keep, so Takt is unavailable (§11).
    const fail = (reason: string) => {
      setProblem(reason)
      setHealth(snapshot() ? "stale" : "unavailable")
    }

    const accept = (next: DagResponse) => {
      // Takt answering that it cannot project is not a failed query: the last
      // graph stays on screen, but the capture is unavailable, not stale (§11).
      if (next.capture === "unavailable") {
        setProblem("takt cannot provide a projection")
        setHealth("unavailable")
        return
      }
      const invalid = topologyError(next)
      if (invalid) {
        setProblem(`projection rejected: ${invalid}`)
        setHealth("invalid")
        return
      }
      const shown = snapshot()
      // Only a strictly newer revision replaces the graph; an unchanged one
      // leaves every signal untouched, so nothing redraws.
      if (!shown || next.projection_revision > shown.projection_revision) setSnapshot(next)
      if (health() !== "confirmed") setHealth("confirmed")
      if (problem() !== undefined) setProblem(undefined)
    }

    let running: ReturnType<typeof Bun.spawn> | undefined
    const poll = async () => {
      const root = session()
      if (running || root === undefined) return // no overlapping snapshot requests
      const proc = Bun.spawn(
        [TAKT_AI, "dag", "status", "--workspace", workspace, "--state", stateDir(workspace), "--session", root, "--format", "json"],
        { cwd: workspace, stdin: "ignore", stdout: "pipe", stderr: "pipe" },
      )
      running = proc
      try {
        const [out, err, code] = await Promise.all([
          new Response(proc.stdout).text(),
          new Response(proc.stderr).text(),
          proc.exited,
        ])
        if (running !== proc) return // disposed while in flight
        if (code !== 0) return fail(err.trim() || `dag status exited ${code}`)
        const parsed: unknown = JSON.parse(out)
        accept(decodeSnapshot(parsed))
      } catch (error) {
        if (running === proc) fail(error instanceof Error ? error.message : "Unknown DAG status error")
      } finally {
        if (running === proc) running = undefined
      }
    }

    // Polling lives exactly as long as a view (the route or the sidebar) is
    // mounted (§9); views share one poll. The last confirmed snapshot stays in
    // setup's closure, so remounting a view shows it until a newer revision
    // arrives.
    let timer: ReturnType<typeof setInterval> | undefined
    let viewers = 0
    const stopPolling = () => {
      clearInterval(timer)
      timer = undefined
      running?.kill()
      running = undefined
    }
    // A different session shares nothing with the graph on screen: drop it
    // and any query still in flight for it, then ask for the new one.
    const follow = (sessionID: string) => {
      const root = context.data.session.root(sessionID)
      if (root === session()) return
      running?.kill()
      running = undefined
      setSession(root)
      setSnapshot(undefined)
      setProblem(undefined)
      setHealth("waiting")
      if (viewers > 0) void poll()
    }
    const acquire = () => {
      if (viewers++ > 0) return
      void poll()
      timer = setInterval(() => void poll(), POLL_INTERVAL_MS)
    }
    const release = () => {
      viewers = Math.max(0, viewers - 1)
      if (viewers === 0) stopPolling()
    }

    const DagView = (props: { readonly sessionID?: string }) => {
      createEffect(() => props.sessionID !== undefined && follow(props.sessionID))
      onMount(acquire)
      onCleanup(release)
      const [legend, setLegend] = createSignal(false)
      // The route replaces the session view, so Esc must lead back to it.
      context.keymap.layer(() => ({
        mode: "global",
        commands: [
          {
            id: "takt.dag.legend",
            title: "Show or hide the DAG legend",
            bind: "?",
            run: () => setLegend((shown) => !shown),
          },
          {
            id: "takt.dag.close",
            title: "Back to session",
            bind: "escape",
            run: () =>
              context.ui.router.navigate(
                props.sessionID !== undefined ? { type: "session", sessionID: props.sessionID } : { type: "home" },
              ),
          },
        ],
      }))
      return (
        // The route box fills the host's route area; only the graph shrinks,
        // so the header and legend always stay visible above the prompt.
        <box flexDirection="column" flexGrow={1} minWidth={0} minHeight={0}>
          <text flexShrink={0} fg={context.theme.text.base}>{headerLine(snapshot(), health())}</text>
          <text flexShrink={0}>{problem() ?? ""}</text>
          <text flexShrink={0}> </text>
          <Show
            when={snapshot()}
            fallback={<text flexShrink={0}>{health() === "waiting" ? "waiting for confirmed projection" : "no confirmed projection"}</text>}
          >
            {(shown: Accessor<DagSnapshot>) => (
              <Show when={hasContent(shown())} fallback={<text flexShrink={0}>empty DAG · no planned work</text>}>
                <GraphView snapshot={shown()} mode="route" />
              </Show>
            )}
          </Show>
          <Show when={legend()} fallback={<text flexShrink={0}></text>}>
            <text flexShrink={0}> </text>
            <For each={LEGEND_LINES}>{(line) => <text flexShrink={0}>{line}</text>}</For>
          </Show>
        </box>
      )
    }

    const SidebarView = (props: { readonly sessionID: string }) => {
      createEffect(() => follow(props.sessionID))
      onMount(acquire)
      onCleanup(release)
      return (
        <box flexDirection="column">
          <text fg={context.theme.text.base}>{sidebarHeader(snapshot(), health())}</text>
          <text>{problem() ?? ""}</text>
          <SidebarGraph snapshot={snapshot()} />
        </box>
      )
    }

    const unregisterRoute = context.ui.router.register({ name: "takt.dag", render: (input) => <DagView sessionID={input.data?.sessionID} /> })

    // A keymap layer belongs to the component that creates it, so the command
    // lives in an app-slot component for the plugin's whole lifetime.
    const releaseCommands = context.ui.slot({
      append: "app",
      render: () => {
        context.keymap.layer(() => ({
          mode: "global",
          commands: [
            {
              id: "takt.dag.open",
              title: "Open Takt DAG",
              // ctrl+shift+d is OpenCode's default input.delete.line.
              bind: "<leader>d",
              palette: true,
              slash: { name: "takt-dag" },
              run: () => {
                const route = context.ui.router.current()
                context.ui.router.navigate({
                  type: "plugin",
                  name: "takt.dag",
                  data: route.type === "session" ? { sessionID: route.sessionID } : undefined,
                })
              },
            },
          ],
        }))
        return null
      },
    })

    const releaseSidebar = context.ui.slot({ prepend: "sidebar.content", render: (input) => <SidebarView sessionID={input.sessionID} /> })

    return () => {
      viewers = 0
      stopPolling()
      releaseSidebar()
      releaseCommands()
      unregisterRoute()
    }
  },
})
