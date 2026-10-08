// Takt DAG — managed by takt-ai; edits are overwritten on sync.
// Read-only live projection of the execution DAG (PRD_DAG_TUI.md). The view
// renders what `takt-ai dag status` reports and never selects, admits or
// mutates work.
/** @jsxImportSource @opentui/solid */
import { resolve } from "node:path"
import { Plugin } from "@opencode/plugin/tui"
import type { RGBA } from "@opentui/core"
import { useTerminalDimensions } from "@opentui/solid"
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
  readonly node_kind: "delegated"
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
  /** Short label of the specialist admitted for the current attempt; absent until admitted. */
  readonly agent?: string
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
      !Array.isArray(value.activities)) return invalid()
  const nodes: DagNode[] = value.nodes.map((raw): DagNode => {
    if (!isRecord(raw) || !isString(raw.id) || raw.id.length === 0 || !oneOf(raw.state, DAG_STATES) ||
        raw.node_kind !== "delegated" ||
        !isOptional(raw.session_id, isString) ||
        !isOptional(raw.attempt_id, isString) ||
        !isOptional(raw.flight, isOneOf(DAG_FLIGHTS)) ||
        !isOptional(raw.outcome, isOneOf(DAG_OUTCOMES)) ||
        typeof raw.launched !== "boolean" ||
        !isOptional(raw.contract, isString) ||
        !isOptional(raw.prerequisites, isStringArray) ||
        !isOptional(raw.agent, isString) ||
        (raw.state !== "in_flight" && raw.flight !== undefined) ||
        (raw.state !== "settled" && raw.outcome !== undefined)) return invalid()
    return { id: raw.id, node_kind: raw.node_kind,
      ...(raw.session_id === undefined ? {} : { session_id: raw.session_id }),
      ...(raw.attempt_id === undefined ? {} : { attempt_id: raw.attempt_id }), state: raw.state,
      ...(raw.flight === undefined ? {} : { flight: raw.flight }),
      ...(raw.outcome === undefined ? {} : { outcome: raw.outcome }), launched: raw.launched,
      ...(raw.contract === undefined ? {} : { contract: raw.contract }),
      ...(raw.prerequisites === undefined ? {} : { prerequisites: raw.prerequisites }),
      ...(raw.agent === undefined ? {} : { agent: raw.agent }) }
  })
  const edges: DagEdge[] = value.edges.map((raw): DagEdge => {
    if (!isRecord(raw) || !isString(raw.from) || !isString(raw.to)) return invalid()
    return { from: raw.from, to: raw.to }
  })
  const activities: DagActivity[] = value.activities.map((raw: unknown): DagActivity => {
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
    nodes, edges, activities }
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
  /** Activities are not work units or edge endpoints. */
  readonly activities: readonly DagActivity[]
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
  const invalidActivity = activityError(snapshot.activities)
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
// Layers are columns, flowing left to right. A column that would overflow the
// viewport width starts a new band below, which flows the opposite way, and so
// on while the height allows; the edges between two bands run down a gutter at
// the side where the flow turns. An edge that skips layers crosses each
// intermediate column on a one-row pass-through slot, so every edge is a chain
// of edges between adjacent columns. The edges of one gap share a vertical bus
// lane when they share an endpoint. Positions and routes depend on topology and
// viewport only, so a state-only update never moves a node (the whole Layout
// is reused).
// ---------------------------------------------------------------------------

type Point = readonly [x: number, y: number]

interface LayoutNode {
  readonly id: string
  readonly depth: number
  readonly x: number
  readonly y: number
  /** Box width: its column's longest identity plus chrome. */
  readonly width: number
}

interface Port {
  readonly x: number
  readonly y: number
  readonly glyph: string
}

interface Viewport {
  readonly width: number
  readonly height: number
}

interface Layout {
  readonly nodes: ReadonlyMap<string, LayoutNode>
  /** One orthogonal polyline per edge: from beside the source box to beside the target box. */
  readonly paths: readonly (readonly Point[])[]
  /** Arrowhead cells, one per edge. */
  readonly heads: readonly Port[]
  /** Node box height on screen. */
  readonly nodeHeight: number
  readonly width: number
  readonly height: number
  /** Widest component's band count; 1 when nothing wraps. */
  readonly bands: number
  /** Node-id/kind set + edge set this layout was computed from; never lifecycle state. */
  readonly topologyKey: string
  readonly viewport: Viewport
}

// A node is a bordered box holding one line, `<state glyph> <id>`. NODE_ROWS
// is that line plus the two border rows; NODE_CHROME is the glyph, its space
// and the two border columns around the identity.
const NODE_ROWS = 3
const NODE_CHROME = 4
// Rows of a pass-through slot, and its minimum width in cells.
const PASS_ROWS = 1
const PASS_WIDTH = 3
// Empty rows between stacked slots of one column, between bands, and between
// disconnected components.
const SLOT_GAP = 1
const BAND_GAP = 2
const COMPONENT_GAP = 2
// Cells of a gap besides its bus lanes: a stub and a margin before the lanes,
// a margin and the arrowhead cell after them.
const GAP_CHROME = 4
// Columns kept free on each side of a wrapping band for the turn gutters.
const WRAP_RESERVE = 8
// Down/up passes of the crossing-reduction ordering.
const ORDER_SWEEPS = 4
const UNBOUNDED: Viewport = { width: Infinity, height: Infinity }

export function ordinal(a: string, b: string): number {
  if (a < b) return -1
  if (a > b) return 1
  return 0
}

export function topologyKey(snapshot: DagSnapshot): string {
  const nodeIds = snapshot.nodes.map((n) => `${n.id}:${n.node_kind}`).sort(ordinal)
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

/** A node box or a pass-through of a layer-skipping edge, placed in one column. */
interface Slot {
  readonly id: string
  readonly depth: number
  readonly node?: string
  readonly height: number
}

/** An edge between slots of adjacent columns. */
interface Link {
  readonly a: string
  readonly b: string
}

interface LaneSeg {
  readonly a: string
  readonly b: string
  readonly lo: number
  readonly hi: number
}

// assignLanes gives every edge of one gap the vertical bus it runs along.
// Edges sharing an endpoint form one group and share a lane (a fork or a
// join reads as one trunk); groups whose row spans overlap get lanes of their
// own, so unrelated edges never merge into one line.
function assignLanes(segs: readonly LaneSeg[]): { lanes: number[]; count: number } {
  const parent = new Map<string, string>()
  const find = (key: string): string => {
    const up = parent.get(key) ?? key
    if (up === key) return key
    const root = find(up)
    parent.set(key, root)
    return root
  }
  for (const s of segs) parent.set(find(`a:${s.a}`), find(`b:${s.b}`))
  const groups = new Map<string, { lo: number; hi: number; members: number[] }>()
  segs.forEach((s, index) => {
    const root = find(`a:${s.a}`)
    const group = groups.get(root) ?? { lo: s.lo, hi: s.hi, members: [] }
    group.lo = Math.min(group.lo, s.lo)
    group.hi = Math.max(group.hi, s.hi)
    group.members.push(index)
    groups.set(root, group)
  })
  const lanes = new Array<number>(segs.length).fill(0)
  const ends: number[] = []
  for (const group of [...groups.values()].sort((x, y) => x.lo - y.lo || x.hi - y.hi)) {
    let lane = ends.findIndex((end) => end < group.lo)
    if (lane < 0) lane = ends.length
    ends[lane] = group.hi
    for (const index of group.members) lanes[index] = lane
  }
  return { lanes, count: ends.length }
}

interface Piece {
  readonly nodes: LayoutNode[]
  readonly paths: Point[][]
  readonly heads: Port[]
  readonly width: number
  readonly height: number
  readonly bands: number
}

// layoutComponent lays one connected component out as columns, wrapped into
// bands no wider than width.
function layoutComponent(snapshot: DagSnapshot, members: readonly string[], depthOf: (id: string) => number, width: number): Piece {
  const inside = new Set(members)
  const edges = snapshot.edges.filter((e) => inside.has(e.from))
  const slots = new Map<string, Slot>()
  const slot = (id: string) => slots.get(id) as Slot
  for (const id of members) slots.set(`n:${id}`, { id: `n:${id}`, depth: depthOf(id), node: id, height: NODE_ROWS })
  const chains = edges.map((e, index) => {
    const chain = [`n:${e.from}`]
    for (let depth = depthOf(e.from) + 1; depth < depthOf(e.to); depth++) {
      const id = `p:${index}:${depth}`
      slots.set(id, { id, depth, height: PASS_ROWS })
      chain.push(id)
    }
    return [...chain, `n:${e.to}`]
  })
  const links: Link[] = chains.flatMap((chain) => chain.slice(1).map((b, i) => ({ a: chain[i], b })))
  const parents = new Map<string, string[]>()
  const children = new Map<string, string[]>()
  for (const l of links) {
    parents.set(l.b, [...(parents.get(l.b) ?? []), l.a])
    children.set(l.a, [...(children.get(l.a) ?? []), l.b])
  }

  const lastDepth = Math.max(...[...slots.values()].map((s) => s.depth))
  const cols: string[][] = Array.from({ length: lastDepth + 1 }, () => [])
  for (const id of [...slots.keys()].sort(ordinal)) cols[slot(id).depth].push(id)

  // Order each column by the mean position of its neighbours, sweeping both
  // ways, so edges cross as little as the layering allows.
  const index = new Map<string, number>()
  const reindex = (c: number) => cols[c].forEach((id, i) => index.set(id, i))
  cols.forEach((_, c) => reindex(c))
  const reorder = (c: number, neighbours: ReadonlyMap<string, string[]>) => {
    const mean = (id: string) => {
      const around = neighbours.get(id) ?? []
      return around.length === 0 ? (index.get(id) as number) : around.reduce((sum, n) => sum + (index.get(n) as number), 0) / around.length
    }
    const keyed = cols[c].map((id) => [id, mean(id)] as const)
    keyed.sort((x, y) => x[1] - y[1] || (index.get(x[0]) as number) - (index.get(y[0]) as number))
    cols[c] = keyed.map(([id]) => id)
    reindex(c)
  }
  for (let sweep = 0; sweep < ORDER_SWEEPS; sweep++) {
    for (let c = 1; c <= lastDepth; c++) reorder(c, parents)
    for (let c = lastDepth - 1; c >= 0; c--) reorder(c, children)
  }

  // Rows: each slot sits on the mean row of its neighbours, pushed down just
  // enough to clear the slot above it.
  const top = new Map<string, number>()
  const centre = (id: string) => (top.get(id) as number) + (slot(id).height - 1) / 2
  const place = (c: number, neighbours: ReadonlyMap<string, string[]>) => {
    let floor = 0
    for (const id of cols[c]) {
      const height = slot(id).height
      const around = (neighbours.get(id) ?? []).filter((n) => top.has(n))
      const want = around.length === 0 ? (top.get(id) ?? floor) : Math.round(around.reduce((sum, n) => sum + centre(n), 0) / around.length - (height - 1) / 2)
      const row = Math.max(want, floor)
      top.set(id, row)
      floor = row + height + SLOT_GAP
    }
  }
  for (let c = 0; c <= lastDepth; c++) place(c, parents)
  for (let c = lastDepth - 1; c >= 0; c--) place(c, children)
  for (let c = 0; c <= lastDepth; c++) place(c, parents)

  const colWidth = cols.map((col) =>
    Math.max(PASS_WIDTH, ...col.map((id) => { const node = slot(id).node; return node === undefined ? 0 : Bun.stringWidth(node) + NODE_CHROME })),
  )
  const depthOfSlot = (id: string) => slot(id).depth
  const stripRow = (id: string) => (top.get(id) as number) + (slot(id).height - 1) / 2
  const segOf = (l: Link, row: (id: string) => number): LaneSeg => {
    const [from, to] = [row(l.a), row(l.b)]
    return { a: l.a, b: l.b, lo: Math.min(from, to), hi: Math.max(from, to) }
  }

  // Lanes of every gap between adjacent columns, as if the flow never wrapped.
  const laneOf = new Map<Link, number>()
  const gapWidth = Array.from({ length: lastDepth }, (_, c) => {
    const gap = links.filter((l) => depthOfSlot(l.a) === c)
    const { lanes, count } = assignLanes(gap.map((l) => segOf(l, stripRow)))
    gap.forEach((l, i) => laneOf.set(l, lanes[i]))
    return Math.max(1, count) + GAP_CHROME
  })

  // Wrap: a column that would overflow starts the next band.
  const total = colWidth.reduce((sum, w) => sum + w, 0) + gapWidth.reduce((sum, w) => sum + w, 0)
  const budget = total <= width ? Infinity : Math.max(width - 2 * WRAP_RESERVE, ...colWidth)
  const bands = wrapBands(colWidth, gapWidth, budget)
  const bandOf = new Array<number>(lastDepth + 1)
  bands.forEach((cs, band) => cs.forEach((c) => { bandOf[c] = band }))
  // Even bands flow right, odd ones left.
  const flow = (c: number) => (bandOf[c] % 2 === 0 ? 1 : -1)

  const shift: number[] = []
  let bottom = 0
  bands.forEach((cs, band) => {
    const ids = cs.flatMap((c) => cols[c])
    const lo = Math.min(...ids.map((id) => top.get(id) as number))
    const hi = Math.max(...ids.map((id) => (top.get(id) as number) + slot(id).height))
    shift[band] = bottom - lo
    bottom += hi - lo + BAND_GAP
  })
  const height = bottom - BAND_GAP
  const rowOf = (id: string) => stripRow(id) + shift[bandOf[depthOfSlot(id)]]

  // The edges between two bands run down a gutter at the side where the flow turns.
  const wraps = links.filter((l) => bandOf[depthOfSlot(l.a)] !== bandOf[depthOfSlot(l.b)])
  const gutterLanes = (side: number) => {
    const turning = wraps.filter((l) => flow(depthOfSlot(l.a)) === side)
    const { lanes, count } = assignLanes(turning.map((l) => segOf(l, rowOf)))
    turning.forEach((l, i) => laneOf.set(l, lanes[i]))
    return count
  }
  const rightLanes = gutterLanes(1)
  const leftLanes = gutterLanes(-1)

  const leftMargin = leftLanes > 0 ? 2 + leftLanes : 0
  const bandWidth = bands.map((cs) => cs.reduce((sum, c, i) => sum + colWidth[c] + (i < cs.length - 1 ? gapWidth[c] : 0), 0))
  const right = leftMargin + Math.max(...bandWidth)
  const colX = columnX(bands, colWidth, gapWidth, leftMargin, right)

  const nodes: LayoutNode[] = members.map((id) => {
    const depth = depthOf(id)
    return { id, depth, x: colX[depth], y: (top.get(`n:${id}`) as number) + shift[bandOf[depth]], width: colWidth[depth] }
  })

  const linkOf = new Map(links.map((l) => [`${l.a}|${l.b}`, l]))
  const paths: Point[][] = []
  const heads: Port[] = []
  for (const chain of chains) {
    const points: Point[] = []
    const push = (x: number, y: number) => {
      const last = points.at(-1)
      if (last?.[0] !== x || last[1] !== y) points.push([x, y])
    }
    let arrival: Point = [0, 0]
    chain.slice(1).forEach((to, i) => {
      const from = chain[i]
      const [ca, cb] = [depthOfSlot(from), depthOfSlot(to)]
      const dir = flow(ca)
      const lane = laneOf.get(linkOf.get(`${from}|${to}`) as Link) ?? 0
      const start = dir > 0 ? colX[ca] + colWidth[ca] : colX[ca] - 1
      let bus: number
      let end: number
      if (bandOf[ca] === bandOf[cb]) {
        bus = start + dir * (2 + lane)
        end = dir > 0 ? colX[cb] - 1 : colX[cb] + colWidth[cb]
      } else if (dir > 0) {
        bus = right + 2 + lane
        end = right
      } else {
        bus = leftMargin - 3 - lane
        end = leftMargin - 1
      }
      const [sy, ty] = [rowOf(from), rowOf(to)]
      push(start, sy)
      push(bus, sy)
      push(bus, ty)
      push(end, ty)
      arrival = [end, ty]
    })
    paths.push(points)
    heads.push({ x: arrival[0], y: arrival[1], glyph: flow(depthOfSlot(chain.at(-1) as string)) > 0 ? "▶" : "◀" })
  }

  return { nodes, paths, heads, width: right + (rightLanes > 0 ? 2 + rightLanes : 0), height, bands: bands.length }
}

// wrapBands groups the columns into bands no wider than budget: a column that
// would overflow starts the next band.
function wrapBands(colWidth: readonly number[], gapWidth: readonly number[], budget: number): number[][] {
  const bands: number[][] = [[0]]
  let used = colWidth[0]
  for (let c = 1; c < colWidth.length; c++) {
    const need = gapWidth[c - 1] + colWidth[c]
    if (used + need > budget) {
      bands.push([c])
      used = colWidth[c]
    } else {
      (bands.at(-1) as number[]).push(c)
      used += need
    }
  }
  return bands
}

// columnX places every column: even bands run rightwards from leftMargin, odd
// ones leftwards from right.
function columnX(bands: readonly number[][], colWidth: readonly number[], gapWidth: readonly number[], leftMargin: number, right: number): number[] {
  const colX: number[] = []
  bands.forEach((cs, band) => {
    if (band % 2 === 0) {
      let x = leftMargin
      for (const c of cs) { colX[c] = x; x += colWidth[c] + (gapWidth[c] ?? 0) }
    } else {
      let edge = right
      for (const c of cs) { colX[c] = edge - colWidth[c]; edge = colX[c] - (gapWidth[c] ?? 0) }
    }
  })
  return colX
}

// buildLayout stacks the graph's connected components top to bottom, each
// laid out in bands no wider than width.
function buildLayout(snapshot: DagSnapshot, width: number): Omit<Layout, "topologyKey" | "viewport"> {
  const depths = computeDepths(snapshot)
  const depthOf = (id: string) => depths.get(id) ?? 0
  const nodes = new Map<string, LayoutNode>()
  const paths: Point[][] = []
  const heads: Port[] = []
  let y = 0
  let maxWidth = 0
  let bands = 1
  for (const members of connectedComponents(snapshot)) {
    const piece = layoutComponent(snapshot, members, depthOf, width)
    for (const node of piece.nodes) nodes.set(node.id, { ...node, y: node.y + y })
    for (const path of piece.paths) paths.push(path.map(([px, py]): Point => [px, py + y]))
    for (const head of piece.heads) heads.push({ ...head, y: head.y + y })
    y += piece.height + COMPONENT_GAP
    maxWidth = Math.max(maxWidth, piece.width)
    bands = Math.max(bands, piece.bands)
  }
  return { nodes, paths, heads, nodeHeight: NODE_ROWS, width: maxWidth, height: Math.max(0, y - COMPONENT_GAP), bands }
}

// computeLayout wraps into bands while the viewport has the height for them;
// otherwise the graph keeps one band and overflows sideways.
export function computeLayout(snapshot: DagSnapshot, previous?: Layout, viewport: Viewport = UNBOUNDED): Layout {
  const key = topologyKey(snapshot)
  if (previous?.topologyKey === key && previous.viewport.width === viewport.width && previous.viewport.height === viewport.height) return previous
  let layout = buildLayout(snapshot, viewport.width)
  if (layout.bands > 1 && layout.height > viewport.height) layout = buildLayout(snapshot, Infinity)
  return { ...layout, topologyKey: key, viewport }
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
    for (let i = 1; i < path.length; i++) link(path[i - 1], path[i])
  }
  return mask.map((row) => row.map((m) => LINE_GLYPHS[m] ?? " ").join(""))
}

/** Where edges meet their boxes: the arrowhead beside the target, in the flow's direction. */
export function edgePorts(layout: Layout): Port[] {
  return [...layout.heads]
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

// A node reads as its state and identity; the kind is omitted while
// "delegated" is the only kind, since it would tell nothing apart.
export function nodeText(node: DagNode): string {
  return `${glyphFor(node)} ${node.id}`
}

const activityLabel = (activity: DagActivity) => (activity.node_kind === "orchestrator" ? "direct activity" : "GC activity")

export function activityText(activity: DagActivity): string {
  const glyph = activity.state === "in_flight" ? "◆" : "◇"
  const label = activityLabel(activity)
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

// Columns and rows the route spends outside the graph: the scrollbar and
// margin, then the header, problem line, spacer, scrollbar row, legend and prompt.
const ROUTE_SIDE_COLUMNS = 2
const ROUTE_CHROME_ROWS = 12

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
  const activities = createMemo(() => props.snapshot.activities)
  const activityHeight = createMemo(() => activities().length === 0 ? 0 : 4)
  const terminal = props.mode === "route" ? useTerminalDimensions() : undefined
  // The route wraps the graph into bands to fit the terminal; the sidebar's
  // graph is never wrapped.
  const viewport = (): Viewport => {
    const size = terminal?.()
    return size ? { width: size.width - ROUTE_SIDE_COLUMNS, height: size.height - ROUTE_CHROME_ROWS - activityHeight() } : UNBOUNDED
  }
  const layout = createMemo(() => (previous = computeLayout(props.snapshot, previous, viewport())))
  const hasNodes = createMemo(() => props.snapshot.nodes.length > 0)
  const rows = createMemo(() => (hasNodes() ? edgeRows(layout()) : []))
  const ports = createMemo(() => edgePorts(layout()))
  const workHeight = createMemo(() => (hasNodes() ? layout().height : 0))
  const activityLaneWidth = createMemo(() => activities().reduce((width, activity) => width + activityBoxWidth(activity) + 1, 0))
  const activityLeft = (index: number) => activities().slice(0, index).reduce((left, activity) => left + activityBoxWidth(activity) + 1, 0)
  const graphWidth = createMemo(() =>
    Math.max(hasNodes() ? layout().width : 0, activityLaneWidth()),
  )
  const graphHeight = createMemo(() => workHeight() + activityHeight())
  const scrollport = () =>
    props.mode === "route"
      ? { scrollY: true, focused: true, flexGrow: 1, minWidth: 0, minHeight: 0 }
      : { scrollY: false, height: graphHeight() + 1, flexShrink: 0 }

  return (
    <scrollbox scrollX width="100%" {...scrollport()} horizontalScrollbarOptions={{ showArrows: true }}>
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
                  width={pos().width}
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
  return snapshot.nodes.length > 0 || snapshot.activities.length > 0
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

// The OpenCode v2 sidebar is 42 columns wide (SESSION_SIDEBAR_WIDTH) less its
// padding, leaving 37 usable columns; the host fixes it and never passes it
// to the slot. Every sidebar row fits it, so nothing ever wraps.
const SIDEBAR_COLUMNS = 37
// Graph lanes the sidebar gutter may use, two columns each. A layer with more
// units than this is drawn as one grouped row instead.
const MAX_SIDEBAR_LANES = 3
// Rows the sidebar graph may use. It is pinned outside the host's scrollbox,
// so an unbounded graph would squeeze the scrolling sections to nothing.
const MAX_SIDEBAR_ROWS = 16
// Gutter cell of a grouped layer row; its members' own glyphs follow it.
const GROUP_GLYPH = "≡"
// Gutter marker of a unit that alone follows the unit above it, and the
// indent per further link of the same chain.
const CHAIN_GLYPH = "└─"
const CHAIN_INDENT = "  "
// Deepest indent a chain reaches, in CHAIN_INDENT steps; further links keep
// it, so a long chain still fits SIDEBAR_COLUMNS.
const MAX_CHAIN_INDENT = 4

export type SidebarTone = "muted" | "error"
export interface SidebarRow {
  readonly text: string
  readonly tone?: SidebarTone
}

const isCompleted = (n: DagNode) => n.state === "settled" && (n.outcome ?? "completed") === "completed"

/** Cuts text to width terminal cells, marking the cut with an ellipsis. */
export function fit(text: string, width: number): string {
  if (Bun.stringWidth(text) <= width) return text
  let kept = ""
  for (const char of text) {
    if (Bun.stringWidth(kept + char) > width - 1) break
    kept += char
  }
  return `${kept}…`
}

/**
 * What follows the sidebar's bold title: a capture that is not current,
 * otherwise completed over planned units (withdrawn ones are no longer work).
 */
export function sidebarDetail(snapshot: DagSnapshot | undefined, health: Health): string {
  const capture = snapshot && health === "confirmed" ? snapshot.capture : health
  if (capture !== "current") return capture
  const units = snapshot?.nodes.filter((n) => n.state !== "withdrawn") ?? []
  return units.length === 0 ? "" : `${units.filter(isCompleted).length}/${units.length}`
}

/** One row of the frontier graph: a unit, or a layer too wide for its lanes. */
interface FrontierItem {
  readonly id: string
  readonly glyph: string
  readonly label: string
  readonly tone?: SidebarTone
}

function sidebarNode(node: DagNode): FrontierItem {
  const glyph = glyphFor(node)
  let tone: SidebarTone | undefined
  if (glyph === "✗") tone = "error"
  else if (isCompleted(node)) tone = "muted"
  return { id: node.id, glyph, label: node.id, ...(tone ? { tone } : {}) }
}

function appendSidebarLayer(
  layer: readonly string[],
  group: string,
  byId: ReadonlyMap<string, DagNode>,
  itemOf: Map<string, string>,
  order: FrontierItem[],
): void {
  if (layer.length <= MAX_SIDEBAR_LANES) {
    for (const id of layer) {
      itemOf.set(id, id)
      order.push(sidebarNode(byId.get(id) as DagNode))
    }
    return
  }
  for (const id of layer) itemOf.set(id, group)
  const members = layer.map((id) => sidebarNode(byId.get(id) as DagNode))
  // A failure anywhere in the group shows; the group is muted only once all of it is done.
  let tone: SidebarTone | undefined
  if (members.some((m) => m.tone === "error")) tone = "error"
  else if (members.every((m) => m.tone === "muted")) tone = "muted"
  const glyphs = members.map((m) => m.glyph).join("")
  order.push({ id: group, glyph: GROUP_GLYPH, label: `${glyphs} ${layer.length} parallel`, ...(tone ? { tone } : {}) })
}

// sidebarGraph is every unit that is not withdrawn, with the edges between
// them: a unit changes glyph as it progresses but never leaves the graph.
// Within each connected component a layer wider than the lane budget becomes
// one item; its units share a depth, so no edge joins them and the contracted
// graph stays acyclic.
function sidebarGraph(snapshot: DagSnapshot): { order: FrontierItem[]; children: Map<string, string[]> } {
  const nodes = snapshot.nodes.filter((n) => n.state !== "withdrawn")
  const ids = new Set(nodes.map((n) => n.id))
  const frontier: DagSnapshot = { ...snapshot, nodes, edges: snapshot.edges.filter((e) => ids.has(e.from) && ids.has(e.to)) }
  const depths = computeDepths(frontier)
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const itemOf = new Map<string, string>()
  const order: FrontierItem[] = []
  connectedComponents(frontier).forEach((members, component) => {
    const layers = new Map<number, string[]>()
    for (const id of members) layers.set(depths.get(id) ?? 0, [...(layers.get(depths.get(id) ?? 0) ?? []), id])
    for (const depth of [...layers.keys()].sort((a, b) => a - b)) {
      appendSidebarLayer((layers.get(depth) ?? []).toSorted(ordinal), `${component}:${depth}`, byId, itemOf, order)
    }
  })
  const position = new Map(order.map((item, index) => [item.id, index]))
  const children = new Map<string, string[]>()
  for (const e of frontier.edges) {
    const from = itemOf.get(e.from) as string
    const to = itemOf.get(e.to) as string
    const known = children.get(from) ?? []
    if (!known.includes(to)) children.set(from, [...known, to])
  }
  for (const list of children.values()) list.sort((a, b) => (position.get(a) ?? 0) - (position.get(b) ?? 0))
  return { order, children }
}

// connectorRow draws, between two unit rows, lanes joining the unit's lane
// `at`: merging into it from above, or forking from it below. Lanes passing
// by stay vertical; a joining line crossing one draws ┼.
function connectorRow(lanes: readonly (string | undefined)[], at: number, others: readonly number[], kind: "merge" | "fork"): SidebarRow {
  const count = Math.max(lanes.length, at + 1, ...others.map((o) => o + 1))
  const mask = new Array<number>(count * 2 - 1).fill(0)
  lanes.forEach((target, lane) => {
    if (target !== undefined && lane !== at && !others.includes(lane)) mask[lane * 2] |= UP | DOWN
  })
  mask[at * 2] |= UP | DOWN
  for (const other of others) {
    mask[other * 2] |= kind === "merge" ? UP : DOWN
    const [low, high] = [Math.min(at, other) * 2, Math.max(at, other) * 2]
    for (let cell = low; cell <= high; cell++) {
      if (cell > low) mask[cell] |= LEFT
      if (cell < high) mask[cell] |= RIGHT
    }
  }
  return { text: mask.map((m) => LINE_GLYPHS[m] ?? " ").join("").trimEnd() }
}

// isChainLink reports a unit that alone follows the unit on the row above,
// with no other lane open, and does not fork itself.
function isChainLink(
  item: FrontierItem,
  previous: FrontierItem | undefined,
  incoming: readonly number[],
  at: number,
  lanes: readonly (string | undefined)[],
  children: ReadonlyMap<string, readonly string[]>,
): boolean {
  return (
    incoming.length === 1 &&
    previous !== undefined &&
    (children.get(previous.id) ?? []).join() === item.id &&
    (children.get(item.id) ?? []).length <= 1 &&
    lanes.every((target, lane) => lane === at || target === undefined)
  )
}

// unitRow draws a unit's row: its glyph in lane `at` among the open lanes, or,
// for the chain-th link of a chain, the indented chain marker; then its label.
function unitRow(item: FrontierItem, at: number, lanes: readonly (string | undefined)[], chain: number): SidebarRow {
  const cells = lanes.map((target, lane) => (lane === at ? item.glyph : target === undefined ? " " : "│"))
  const gutter = chain > 0 ? `${CHAIN_INDENT.repeat(Math.min(chain - 1, MAX_CHAIN_INDENT))}${CHAIN_GLYPH}${item.glyph}` : cells.join(" ").trimEnd()
  return { text: `${gutter} ${fit(item.label, SIDEBAR_COLUMNS - Bun.stringWidth(gutter) - 1)}`, ...(item.tone ? { tone: item.tone } : {}) }
}

// laneRows lays the graph out like `git log --graph`, top-down: the state
// glyph is the node, sitting in its lane, and every edge is a lane carried
// down to the unit it leads to. A unit that alone follows the unit on the row
// above, with no other lane open, has no connector row to show the edge, so it
// is marked `└─` and indented one level per link, up to MAX_CHAIN_INDENT;
// a unit that forks stays in its lane so its fork row lines up under it.
// Undefined when the walk needs more lanes than the budget.
function laneRows(order: readonly FrontierItem[], children: ReadonlyMap<string, readonly string[]>): SidebarRow[] | undefined {
  const lanes: (string | undefined)[] = []
  const rows: SidebarRow[] = []
  const free = () => (lanes.includes(undefined) ? lanes.indexOf(undefined) : lanes.length)
  let previous: FrontierItem | undefined
  let chain = 0
  for (const item of order) {
    const incoming = lanes.flatMap((target, lane) => (target === item.id ? [lane] : []))
    const at = incoming[0] ?? free()
    if (incoming.length > 1) {
      rows.push(connectorRow(lanes, at, incoming.slice(1), "merge"))
      for (const lane of incoming.slice(1)) lanes[lane] = undefined
    }
    chain = isChainLink(item, previous, incoming, at, lanes, children) ? chain + 1 : 0
    previous = item
    lanes[at] = item.id
    rows.push(unitRow(item, at, lanes, chain))
    const [first, ...rest] = children.get(item.id) ?? []
    lanes[at] = first
    const forks = rest.map((child) => {
      const lane = free()
      lanes[lane] = child
      return lane
    })
    if (forks.length > 0) rows.push(connectorRow(lanes, at, forks, "fork"))
    while (lanes.length > 0 && lanes.at(-1) === undefined) lanes.pop()
    if (lanes.length > MAX_SIDEBAR_LANES) return undefined
  }
  return rows
}

/**
 * The DAG for the sidebar's narrow column: the whole graph as lanes (a flat
 * list in dependency order when it needs more lanes than the budget), then
 * running activities. Every row fits SIDEBAR_COLUMNS and the total never
 * exceeds MAX_SIDEBAR_ROWS: the oldest rows fold into a leading "… N more".
 */
export function sidebarRows(snapshot: DagSnapshot): SidebarRow[] {
  const { order, children } = sidebarGraph(snapshot)
  const graph =
    laneRows(order, children) ??
    order.map((item) => ({ text: fit(`${item.glyph} ${item.label}`, SIDEBAR_COLUMNS), ...(item.tone ? { tone: item.tone } : {}) }))
  const activities = snapshot.activities
    .filter((a) => a.state === "in_flight")
    .map((a) => ({ text: fit(`◆ ${activityLabel(a)}`, SIDEBAR_COLUMNS) }))
  const rows = [...graph, ...activities]
  if (rows.length <= MAX_SIDEBAR_ROWS) return rows
  const hidden = rows.length - (MAX_SIDEBAR_ROWS - 1)
  return [{ text: `… ${hidden} more`, tone: "muted" }, ...rows.slice(hidden)]
}

/**
 * The sidebar's rows, or nothing: unlike the route view it never explains
 * why (no confirmed projection, empty DAG), the title line already does.
 * Every branch below must resolve to a <text>, never to `false`/`undefined`
 * directly under <box> — Show's own off-state placeholder needs one too.
 */
export function SidebarGraph(props: {
  readonly snapshot: DagSnapshot | undefined
  readonly tones?: Partial<Record<SidebarTone, RGBA>>
}) {
  return (
    <Show when={props.snapshot} fallback={<text></text>}>
      {(shown: Accessor<DagSnapshot>) => (
        <Show when={hasContent(shown())} fallback={<text></text>}>
          <box flexDirection="column">
            <For each={sidebarRows(shown())}>
              {(row) => (
                <text wrapMode="none" fg={row.tone ? props.tones?.[row.tone] : undefined}>
                  {row.text}
                </text>
              )}
            </For>
          </box>
        </Show>
      )}
    </Show>
  )
}

// ---------------------------------------------------------------------------
// Snapshot query
// ---------------------------------------------------------------------------

function stateDir(workspace: string): string {
  return resolve(workspace, ".takt-ai", "vfs")
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
          <text wrapMode="none" fg={context.theme.text.base}>
            <b>DAG</b> <span style={{ fg: context.theme.text.muted }}>{sidebarDetail(snapshot(), health())}</span>
          </text>
          <text>{problem() ?? ""}</text>
          <SidebarGraph
            snapshot={snapshot()}
            tones={{ muted: context.theme.text.muted, error: context.theme.text.feedback.error.base }}
          />
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

    const releaseSidebar = context.ui.slot({ append: "sidebar.footer", render: (input) => <SidebarView sessionID={input.sessionID} /> })

    return () => {
      viewers = 0
      stopPolling()
      releaseSidebar()
      releaseCommands()
      unregisterRoute()
    }
  },
})
