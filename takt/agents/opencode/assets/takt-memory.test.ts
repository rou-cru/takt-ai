import { describe, expect, test } from "bun:test"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import memoryPlugin, { closeResult, isObject, recordResult } from "./takt-memory"

const contracts = JSON.parse(readFileSync(resolve(import.meta.dir, "../testdata/contracts.json"), "utf8"))
type MemoryTool = { name: string; execute: (input: unknown, context: never) => Promise<{ content: string }> }

describe("plugin setup", () => {
  test("registers memory tools and sends valid requests to takt-ai", async () => {
    const runtime = globalThis as unknown as { Bun: { spawn: (...args: never[]) => unknown } }
    const originalSpawn = runtime.Bun.spawn
    const tools = new Map<string, { execute: (input: unknown, context: never) => Promise<{ content: string }> }>()
    const requests: Array<{ command: string; request: Record<string, unknown> }> = []
    runtime.Bun.spawn = ((argv: string[]) => {
      let request: Record<string, unknown> = {}
      const command = argv[2]
      return {
        stdin: { write(value: string) { request = JSON.parse(value) }, end() {} },
        get stdout() {
          requests.push({ command, request })
          const result = command === "record" ? { id: 42, deduplicated: false } : { end_anchor_id: 43, entries: 1 }
          return JSON.stringify({ ok: true, result })
        },
        stderr: "",
        exited: Promise.resolve(0),
      }
    }) as typeof originalSpawn

    try {
      await memoryPlugin.setup({
        location: { directory: "/workspace" },
        session: { async get({ sessionID }: { sessionID: string }) { return { id: sessionID } } },
        tool: { async transform(callback: (editor: { add: (definition: MemoryTool) => void }) => void) {
          callback({ add(definition) { tools.set(definition.name, definition) } })
          return { dispose() {} }
        } },
        event: { subscribe() { return { [Symbol.asyncIterator]() { return { next: () => new Promise<never>(() => {}) } } } } },
      } as never)

      const context = { sessionID: "root", agent: "takt", messageID: "message", id: "tool-call" } as never
      expect((await tools.get("memory_record")!.execute({ nature: "observation", scope: "project", title: "title", content: "content" }, context)).content).toBe("Recorded #42")
      expect((await tools.get("memory_continue_session")!.execute({ previous_session_id: "prior" }, context)).content).toBe("Continues prior")
      expect((await tools.get("memory_close_session")!.execute({ objective: "objective", state: "done" }, context)).content).toBe("Closed session as #43 with 1 entries")
      expect(requests.map(({ command }) => command)).toEqual(["record", "continue", "close"])
      expect(requests[0].request).toMatchObject({ author: "takt", session: "root", directory: "/workspace", title: "title" })
    } finally {
      runtime.Bun.spawn = originalSpawn
    }
  })
})

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
