import { describe, expect, test } from "bun:test"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { closeResult, isObject, recordResult } from "./takt-memory"

const contracts = JSON.parse(readFileSync(resolve(import.meta.dir, "../testdata/contracts.json"), "utf8"))

describe("isObject", () => {
  test("accepts plain objects only", () => {
    expect(isObject({})).toBe(true)
    expect(isObject([])).toBe(false)
    expect(isObject(null)).toBe(false)
    expect(isObject("x")).toBe(false)
  })
})

describe("recordResult", () => {
  test("matches the real memory_record wire contract", () => {
    expect(recordResult(contracts.memory_record.result)).toEqual({ id: 42, deduplicated: false })
  })
  test("rejects a non-integer, non-positive, or wrongly typed id", () => {
    expect(() => recordResult({ id: 0, deduplicated: false })).toThrow("takt-ai memory record returned an invalid result")
    expect(() => recordResult({ id: 1.5, deduplicated: false })).toThrow("takt-ai memory record returned an invalid result")
    expect(() => recordResult({ id: "1", deduplicated: false })).toThrow("takt-ai memory record returned an invalid result")
    expect(() => recordResult({ id: 1, deduplicated: "no" })).toThrow("takt-ai memory record returned an invalid result")
    expect(() => recordResult(null)).toThrow("takt-ai memory record returned an invalid result")
  })
})

describe("closeResult", () => {
  test("matches the real memory_close wire contract", () => {
    expect(closeResult(contracts.memory_close.result)).toEqual({ end_anchor_id: 43, entries: 2 })
  })
  test("accepts zero entries but rejects a negative count or a non-positive anchor", () => {
    expect(closeResult({ end_anchor_id: 1, entries: 0 })).toEqual({ end_anchor_id: 1, entries: 0 })
    expect(() => closeResult({ end_anchor_id: 1, entries: -1 })).toThrow("takt-ai memory close returned an invalid result")
    expect(() => closeResult({ end_anchor_id: 0, entries: 1 })).toThrow("takt-ai memory close returned an invalid result")
    expect(() => closeResult({ end_anchor_id: 1.5, entries: 1 })).toThrow("takt-ai memory close returned an invalid result")
  })
})
