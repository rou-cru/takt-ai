import { describe, expect, test } from "bun:test"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import {
  asString,
  gcCoordinateResponse,
  isBoolean,
  isCoordinator,
  isCoordinatorResponse,
  isGCCycle,
  isGCCycleResponse,
  isGCPlan,
  isHandoffEnvelope,
  isOptional,
  isResponseObject,
  isStagedView,
  isString,
  isStringArray,
  isToolInput,
  orchestratorOnly,
  parseClaim,
  parseCoordinationResponse,
  parseVFSShell,
  responseObject,
  toolInput,
  vfsResponse,
} from "./takt-vfs"

const contracts = JSON.parse(readFileSync(resolve(import.meta.dir, "../testdata/contracts.json"), "utf8"))

describe("type guards", () => {
  test("isToolInput / toolInput", () => {
    expect(isToolInput({})).toBe(true)
    expect(isToolInput({ a: 1 })).toBe(true)
    expect(isToolInput([])).toBe(false)
    expect(isToolInput(null)).toBe(false)
    expect(isToolInput("x")).toBe(false)
    expect(toolInput({ a: 1 })).toEqual({ a: 1 })
    expect(() => toolInput("x")).toThrow("tool input must be an object")
  })

  test("isResponseObject / isStringArray / isOptional / isString / isBoolean", () => {
    expect(isResponseObject({})).toBe(true)
    expect(isResponseObject([])).toBe(false)
    expect(isResponseObject(null)).toBe(false)
    expect(isStringArray(["a", "b"])).toBe(true)
    expect(isStringArray(["a", 1])).toBe(false)
    expect(isStringArray("a")).toBe(false)
    expect(isOptional(undefined, isString)).toBe(true)
    expect(isOptional("x", isString)).toBe(true)
    expect(isOptional(1, isString)).toBe(false)
    expect(isString("x")).toBe(true)
    expect(isString(1)).toBe(false)
    expect(isBoolean(true)).toBe(true)
    expect(isBoolean("true")).toBe(false)
  })

  test("orchestratorOnly", () => {
    const ORCHESTRATOR_ID = "__TAKT_ORCHESTRATOR_ID__"
    expect(() => orchestratorOnly({ agent: ORCHESTRATOR_ID }, "claim")).not.toThrow()
    expect(() => orchestratorOnly({ agent: "someone-else" }, "claim")).toThrow("claim belongs to the orchestrator")
    expect(() => orchestratorOnly({}, "claim")).toThrow("claim belongs to the orchestrator")
  })

  test("asString", () => {
    expect(asString("x", "fallback")).toBe("x")
    expect(asString(1, "fallback")).toBe("fallback")
    expect(asString(undefined, "fallback")).toBe("fallback")
  })
})

describe("responseObject", () => {
  test("passes an object through and rejects non-objects", () => {
    expect(responseObject({ a: 1 }, "vfs bind")).toEqual({ a: 1 })
    expect(() => responseObject("x", "vfs bind")).toThrow("takt-ai vfs bind returned a non-object response")
    expect(() => responseObject(null, "vfs bind")).toThrow("takt-ai vfs bind returned a non-object response")
    expect(() => responseObject([1], "vfs bind")).toThrow("takt-ai vfs bind returned a non-object response")
  })
})

describe("parseVFSShell", () => {
  test("accepts a well-formed shell decision with every optional field", () => {
    const raw = { decision: "ask", reason: "confirm", cwd: "/work", scratch: "/tmp/x", confirm: "run it?", capture: true, writable: ["a"], protected: ["b"], private: ["c"] }
    expect(parseVFSShell(raw)).toEqual({ decision: "ask", reason: "confirm", cwd: "/work", scratch: "/tmp/x", confirm: "run it?", capture: true, writable: ["a"], protected: ["b"], private: ["c"] })
  })
  test("accepts the minimal required shape", () => {
    expect(parseVFSShell({ decision: "deny", reason: "no" })).toEqual({ decision: "deny", reason: "no" })
  })
  test("rejects an invalid decision, a missing reason, or a wrongly typed optional field", () => {
    expect(parseVFSShell({ decision: "maybe", reason: "x" })).toBeUndefined()
    expect(parseVFSShell({ decision: "allow" })).toBeUndefined()
    expect(parseVFSShell({ decision: "allow", reason: "x", capture: "yes" })).toBeUndefined()
    expect(parseVFSShell({ decision: "allow", reason: "x", writable: ["a", 1] })).toBeUndefined()
    expect(parseVFSShell("not an object")).toBeUndefined()
  })
})

describe("parseClaim", () => {
  test("keeps a well-formed claim with its optional fields", () => {
    const raw = { key: "k", root_session_id: "r", work_unit_id: "u", agent_id: "a", target_instance: "t", pending: true, active: true, scope: ["x", 1], author_key: "auth" }
    expect(parseClaim(raw)).toEqual([{ key: "k", root_session_id: "r", work_unit_id: "u", agent_id: "a", target_instance: "t", pending: true, active: true, scope: ["x"], author_key: "auth" }])
  })
  test("drops anything missing a required field or not an object", () => {
    expect(parseClaim({ key: "k" })).toEqual([])
    expect(parseClaim("not an object")).toEqual([])
    expect(parseClaim(null)).toEqual([])
  })
})

describe("vfsResponse", () => {
  test("matches the real vfs_bind wire contract", () => {
    const { ok, ...rest } = contracts.vfs_bind
    expect(vfsResponse(rest, ok)).toEqual({ ok: true, key: "bind-opaque", attempt_id: "1", invariants_version: "inv1:abc" })
  })
  test("carries a shell decision and claims when present", () => {
    const parsed = { revision: 3, delta_hash: "h", shell: { decision: "allow", reason: "ok" }, claims: [{ key: "k", root_session_id: "r", work_unit_id: "u", agent_id: "a", target_instance: "t" }, "garbage"] }
    expect(vfsResponse(parsed, true)).toEqual({
      ok: true, revision: 3, delta_hash: "h",
      shell: { decision: "allow", reason: "ok" },
      claims: [{ key: "k", root_session_id: "r", work_unit_id: "u", agent_id: "a", target_instance: "t" }],
    })
  })
  test("omits every field the response does not set", () => {
    expect(vfsResponse({}, false)).toEqual({ ok: false })
  })
})

describe("gc coordination shapes", () => {
  const plan = { session_id: "s", cycle_id: "c", mandate_class: "complexity", delta: [], closure: ["a.go"], reachability: "codegraph" }
  const cycle = { phase: "collect", plan, scope: ["a.go"], sessions: { collector: "s1" }, report: null, started: "2024-01-01T00:00:00Z" }
  const stagedView = { revision: 1, delta_hash: "h", files: { "a.go": "content", "b.go": null } }
  const coordinator = { version: 1, units: 1, mutations: 0, cursor: 0, deferrals: 0, next_mandate: 1, requested: false, draining: false }

  test("isGCPlan / isGCCycle / isStagedView / isCoordinator accept well-formed shapes", () => {
    expect(isGCPlan(plan)).toBe(true)
    expect(isGCCycle(cycle)).toBe(true)
    expect(isStagedView(stagedView)).toBe(true)
    expect(isCoordinator(coordinator)).toBe(true)
    expect(isCoordinator({ ...coordinator, cycle })).toBe(true)
    expect(isCoordinator({ ...coordinator, history: [cycle] })).toBe(true)
  })

  test("isGCPlan / isGCCycle / isStagedView / isCoordinator reject malformed shapes", () => {
    expect(isGCPlan({ ...plan, closure: ["a.go", 1] })).toBe(false)
    expect(isGCCycle({ ...cycle, plan: { ...plan, session_id: 1 } })).toBe(false)
    expect(isStagedView({ ...stagedView, files: { "a.go": 1 } })).toBe(false)
    expect(isCoordinator({ ...coordinator, version: "1" })).toBe(false)
    expect(isCoordinator({ ...coordinator, cycle: { phase: "collect" } })).toBe(false)
  })

  test("gcCoordinateResponse dispatches by action", () => {
    expect(gcCoordinateResponse(plan, "prepare")).toEqual(plan)
    expect(gcCoordinateResponse({ notAPlan: true }, "prepare")).toBeUndefined()
    expect(gcCoordinateResponse(cycle, "findings")).toEqual(cycle)
    expect(gcCoordinateResponse(cycle, "investigate")).toEqual(cycle)
    expect(gcCoordinateResponse(cycle, "authorize")).toEqual(cycle)
    expect(gcCoordinateResponse(stagedView, "collected")).toEqual(stagedView)
    expect(gcCoordinateResponse(stagedView, "delta")).toEqual(stagedView)
    expect(gcCoordinateResponse(coordinator, "attach")).toEqual(coordinator)
    expect(gcCoordinateResponse({ not: "a coordinator" }, "attach")).toBeUndefined()
  })

  test("gcCoordinateResponse matches the real coordinator wire contract", () => {
    expect(isCoordinatorResponse(contracts.coordinator)).toBe(true)
    expect(isGCCycleResponse(contracts.coordinator.cycle)).toBe(true)
    expect(gcCoordinateResponse(contracts.coordinator, "attach")).toEqual(contracts.coordinator)
  })
})

describe("isHandoffEnvelope", () => {
  test("matches the real handoff and abort wire contracts", () => {
    expect(isHandoffEnvelope(contracts.handoff)).toBe(true)
    expect(isHandoffEnvelope(contracts.abort)).toBe(true)
  })
  test("rejects a malformed envelope", () => {
    expect(isHandoffEnvelope({ result: "Standard", additional_context: "x", extra_artifacts: [1], memory: [] })).toBe(false)
    expect(isHandoffEnvelope({ result: "Standard", additional_context: "x", extra_artifacts: [], memory: ["not a number"] })).toBe(false)
  })
})

describe("parseCoordinationResponse", () => {
  test("passes a null response through", () => {
    expect(parseCoordinationResponse(null, ["gc", "coordinate"], "prepare")).toBeNull()
  })
  test("recognizes a handoff envelope regardless of verb", () => {
    expect(parseCoordinationResponse(contracts.handoff, ["dispatch"], "handoff")).toEqual(contracts.handoff)
  })
  test("routes gc coordinate through gcCoordinateResponse", () => {
    const cycle = { phase: "collect", plan: { session_id: "s", cycle_id: "c", mandate_class: "complexity", delta: [], closure: [], reachability: "codegraph" }, scope: [], sessions: {}, report: null, started: "2024-01-01T00:00:00Z" }
    expect(parseCoordinationResponse(cycle, ["gc", "coordinate"], "findings")).toEqual(cycle)
  })
  test("treats an empty or ok dispatch switch/validate_results response as an acknowledgement", () => {
    expect(parseCoordinationResponse({}, ["dispatch"], "switch")).toBeNull()
    expect(parseCoordinationResponse({ ok: true }, ["dispatch"], "validate_results")).toBeNull()
  })
  test("accepts a coordinator response for a non-acknowledgement dispatch action", () => {
    expect(parseCoordinationResponse(contracts.coordinator, ["dispatch"], "handoff")).toEqual(contracts.coordinator)
  })
  test("rejects a response matching no known shape", () => {
    expect(() => parseCoordinationResponse({ garbage: true }, ["dispatch"], "handoff")).toThrow("dispatch handoff returned an unexpected response shape")
  })
  test("rejects a non-object response before shape matching", () => {
    expect(() => parseCoordinationResponse("not an object", ["vfs", "bind"], undefined)).toThrow("takt-ai vfs bind returned a non-object response")
  })
})
