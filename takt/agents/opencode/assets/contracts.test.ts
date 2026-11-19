import { describe, expect, test } from "bun:test"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { decodeSnapshot } from "./takt-dag"

const contracts = JSON.parse(readFileSync(resolve(import.meta.dir, "../testdata/contracts.json"), "utf8"))

describe("shared Go/TypeScript wire contracts", () => {
  test("DAG snapshot samples decode", () => {
    expect(decodeSnapshot(contracts.dag_normal)).toMatchObject({ capture: "current", nodes: [{ id: "unit-1" }] })
    expect(decodeSnapshot(contracts.dag_unavailable)).toEqual({ capture: "unavailable" })
  })
  test("DAG states sample keeps the unmonitored flight, the layers and the counts", () => {
    expect(decodeSnapshot(contracts.dag_states)).toMatchObject({
      counts: { planned: 1, in_flight: 1, completed: 1 },
      nodes: [
        { id: "unit-1", state: "in_flight", flight: "unmonitored", layer: 0 },
        { id: "unit-2", state: "settled", outcome: "completed", layer: 0 },
        { id: "unit-3", state: "planned", prerequisites: ["unit-2"], layer: 1 },
      ],
      edges: [{ from: "unit-2", to: "unit-3" }],
    })
  })
  test("DAG activity sample keeps an unmonitored activity in flight", () => {
    expect(decodeSnapshot(contracts.dag_activity_unmonitored)).toMatchObject({
      nodes: [],
      activities: [{ activity_id: "activity-1", node_kind: "orchestrator", state: "in_flight", flight: "unmonitored" }],
    })
  })
  test("non-DAG contract discriminants and required fields stay present", () => {
    expect(contracts.vfs_bind).toMatchObject({ ok: true, key: expect.any(String), attempt_id: expect.any(String), invariants_version: expect.any(String) })
    expect(contracts.dispatch_ack).toEqual({ ok: true })
    expect(contracts.handoff).toMatchObject({ envelope: { result: "Standard", additional_context: expect.any(String), extra_artifacts: expect.any(Array), memory: expect.any(Array) } })
    expect(contracts.abort).toMatchObject({ envelope: { result: "Aborted", additional_context: expect.any(String), extra_artifacts: [], memory: [] }, child: expect.any(String) })
    expect(Object.keys(contracts.handoff.envelope).sort()).toEqual(["additional_context", "extra_artifacts", "memory", "result"])
    expect(contracts.dispatch_null).toBeNull()
    expect(contracts.dispatch_request).toHaveProperty("action", "handoff")
    expect(contracts.dispatch_switch_request).toMatchObject({ action: "switch", requirements: null })
    expect(contracts.dispatch_validate_request).toHaveProperty("action", "validate_results")
    expect(contracts.dispatch_inputs_request).toMatchObject({ action: "inputs", event:"unit-1", result_ids: expect.any(Array) })
  })
})