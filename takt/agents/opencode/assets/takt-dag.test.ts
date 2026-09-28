import { describe, expect, test } from "bun:test"
import { decodeSnapshot } from "./takt-dag"

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
})
