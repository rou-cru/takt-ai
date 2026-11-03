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
  test("DAG assurance sample keeps unmonitored flight and the recorded assurance", () => {
    expect(decodeSnapshot(contracts.dag_assurance)).toMatchObject({
      nodes: [
        { id: "unit-1", state: "in_flight", flight: "unmonitored" },
        { id: "unit-2", state: "settled", outcome: "completed", assurance: { status: "failed", revision: 2, evidence: "journal/7" } },
      ],
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
    expect(contracts.memory_record).toMatchObject({ ok: true, result: { id: expect.any(Number), deduplicated: false } })
    expect(contracts.memory_close).toMatchObject({ ok: true, result: { end_anchor_id: expect.any(Number), entries: expect.any(Number) } })
    expect(contracts.memory_search).toMatchObject({ ok: true, result: { results: expect.any(Array), omitted_superseded: expect.any(Number) } })
    expect(contracts.memory_error).toMatchObject({ ok: false, error: expect.any(String) })
    expect(contracts.coordinator).toMatchObject({ version: expect.any(Number), cycle: { phase: "collect", sessions: { collector: expect.any(String), verifier: expect.any(String) }, plan: { cycle_id: expect.any(String), mandate_class: expect.any(String) } } })
    expect(contracts.handoff).toMatchObject({ envelope: { result: "Standard", additional_context: expect.any(String), extra_artifacts: expect.any(Array), memory: { entries: expect.any(Array) } }, artifact: { path: expect.any(String), present: expect.any(Boolean) } })
    expect(contracts.abort).toMatchObject({ envelope: { result: "Aborted", additional_context: expect.any(String), extra_artifacts: [], memory: { entries: expect.any(Array) } }, child: expect.any(String) })
    expect(Object.keys(contracts.handoff.envelope).sort()).toEqual(["additional_context", "extra_artifacts", "memory", "result"])
    expect(contracts.lent_entries).toMatchObject({ entries: expect.any(Array) })
    expect(contracts.dispatch_null).toBeNull()
    expect(contracts.dispatch_request).toHaveProperty("action", "handoff")
    expect(contracts.dispatch_switch_request).toHaveProperty("action", "switch")
    expect(contracts.dispatch_validate_request).toHaveProperty("action", "validate_results")
    expect(contracts.dispatch_inputs_request).toMatchObject({ action: "inputs", event:"unit-1", result_ids: expect.any(Array) })
  })
})