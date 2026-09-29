import { describe, expect, test } from "bun:test"
import dagPlugin, { decodeSnapshot } from "./takt-dag"

describe("DAG snapshot wire contract", () => {
  test("accepts Go's exact unavailable response", () => {
    expect(decodeSnapshot(JSON.parse('{"capture":"unavailable"}'))).toEqual({ capture: "unavailable" })
  })

  test("rejects unavailable responses with extra fields and partial normal snapshots", () => {
    expect(() => decodeSnapshot({ capture: "unavailable", nodes: [] })).toThrow("invalid DAG snapshot JSON")
    expect(() => decodeSnapshot({ capture: "current", nodes: [], edges: [] })).toThrow("invalid DAG snapshot JSON")
  })

  test("accepts a local snapshot JSON fixture", () => {
    const snapshotFixture = JSON.stringify({
      schema_version: 1,
      projection_revision: 2,
      history_position: 2,
      session_id: "root-session",
      capture: "current",
      plan_version: "plan-1",
      nodes: [{ id: "unit-1", node_kind: "delegated", session_id: "root-session", attempt_id: "attempt-1", state: "settled", outcome: "completed", launched: true, prerequisites: [] }],
      edges: [],
      activities: [],
    })
    expect(decodeSnapshot(JSON.parse(snapshotFixture))).toMatchObject({ capture: "current", nodes: [{ id: "unit-1", outcome: "completed" }] })
  })

  test("normalizes optional node and activity metadata while preserving valid wire fields", () => {
    const decoded = decodeSnapshot({
      schema_version: 1, projection_revision: 0, history_position: 0, session_id: "root", capture: "empty", plan_version: "plan-2",
      nodes: [{ id: "active", node_kind: "delegated", session_id: "child", attempt_id: "try-1", state: "in_flight", flight: "observed_running", launched: true, contract: "ship safely", prerequisites: ["first"] },
        { id: "done", state: "settled", outcome: "completed", launched: false }],
      edges: [{ from: "active", to: "done" }],
      activities: [{ activity_id: "work-1", node_kind: "orchestrator", state: "in_flight", flight: "observed_running" },
        { activity_id: "maintenance-1", node_kind: "maintenance", state: "settled", outcome: "interrupted" }],
    })
    expect(decoded).toMatchObject({
      capture: "empty", plan_version: "plan-2",
      nodes: [{ id: "active", flight: "observed_running", prerequisites: ["first"] }, { id: "done", outcome: "completed" }],
      edges: [{ from: "active", to: "done" }],
      activities: [{ activity_id: "work-1", flight: "observed_running" }, { activity_id: "maintenance-1", outcome: "interrupted" }],
    })
  })

  test("rejects malformed snapshot, node, edge, and activity fields", () => {
    const base = { schema_version: 1, projection_revision: 0, history_position: 0, session_id: "root", capture: "current", nodes: [], edges: [] }
    const invalidNodes: unknown[] = [
      null, { id: "", state: "planned", launched: false }, { id: "unit", state: "unknown", launched: false },
      { id: "unit", node_kind: "orchestrator", state: "planned", launched: false },
      { id: "unit", session_id: 3, state: "planned", launched: false }, { id: "unit", attempt_id: {}, state: "planned", launched: false },
      { id: "unit", state: "planned", flight: "suspended", launched: false },
      { id: "unit", state: "planned", outcome: "failed", launched: false },
      { id: "unit", state: "in_flight", flight: "unknown", launched: false },
      { id: "unit", state: "settled", outcome: "unknown", launched: false },
      { id: "unit", state: "planned", launched: "false" }, { id: "unit", state: "planned", launched: false, contract: 1 },
      { id: "unit", state: "planned", launched: false, prerequisites: ["valid", 3] },
    ]
    for (const node of invalidNodes) {
      expect(() => decodeSnapshot({ ...base, nodes: [node] })).toThrow("invalid DAG snapshot JSON")
    }
    for (const snapshot of [
      null,
      { ...base, schema_version: 2 },
      { ...base, projection_revision: -1 },
      { ...base, history_position: 1.5 },
      { ...base, session_id: 1 },
      { ...base, capture: "missing" },
      { ...base, plan_version: 1 },
      { ...base, nodes: {} },
      { ...base, activities: {} },
      { ...base, edges: [null] },
      { ...base, edges: [{ from: 1, to: "unit" }] },
      { ...base, activities: [{ activity_id: 1, node_kind: "orchestrator", state: "in_flight" }] },
      { ...base, activities: [{ activity_id: "a", node_kind: "agent", state: "in_flight" }] },
      { ...base, activities: [{ activity_id: "a", node_kind: "orchestrator", state: "planned" }] },
      { ...base, activities: [{ activity_id: "a", node_kind: "orchestrator", state: "in_flight", flight: "suspended" }] },
      { ...base, activities: [{ activity_id: "a", node_kind: "orchestrator", state: "in_flight", outcome: "failed" }] },
      { ...base, activities: [{ activity_id: "a", node_kind: "orchestrator", state: "settled", flight: "observed_running" }] },
      { ...base, activities: [{ activity_id: "a", node_kind: "orchestrator", state: "settled", outcome: "unknown" }] },
    ]) {
      expect(() => decodeSnapshot(snapshot)).toThrow("invalid DAG snapshot JSON")
    }
  })
})

describe("DAG plugin registration", () => {
  test("registers route, sidebar, and keymap command and releases them on shutdown", async () => {
    let route: { name: string; render: (input: { data?: { sessionID?: string } }) => unknown } | undefined
    const slots: Array<{ append?: string; prepend?: string; render: (...args: never[]) => unknown }> = []
    let layerFactory: (() => { commands: Array<{ id: string; run: () => void }> }) | undefined
    let current: { type: string; sessionID?: string } = { type: "session", sessionID: "session-1" }
    let navigated: unknown
    let releases = 0

    const cleanup = await dagPlugin.setup({
        location: { directory: "/workspace" },
        data: { location: { default: () => ({ directory: "/workspace" }) }, session: { root: (id: string) => id } },
        theme: { text: { base: "white" } },
        ui: {
          router: {
            register(value: typeof route & object) { route = value; return () => { releases++ } },
            current() { return current },
            navigate(value: unknown) { navigated = value },
          },
          slot(value: (typeof slots)[number]) { slots.push(value); return () => { releases++ } },
        },
        keymap: { layer(value: NonNullable<typeof layerFactory>) { layerFactory = value; return () => { releases++ } } },
    } as never)

    expect(route?.name).toBe("takt.dag")
    expect(slots.map(({ append, prepend }) => [append, prepend])).toEqual([["app", undefined], [undefined, "sidebar.content"]])

    slots[0].render()
    const command = layerFactory?.().commands.find(({ id }) => id === "takt.dag.open")
    expect(command).toBeDefined()
    command?.run()
    expect(navigated).toEqual({ type: "plugin", name: "takt.dag", data: { sessionID: "session-1" } })

    current = { type: "plugin" }
    command?.run()
    expect(navigated).toEqual({ type: "plugin", name: "takt.dag", data: undefined })

    if (typeof cleanup === "function") cleanup()
    expect(releases).toBe(3)
  })
})
