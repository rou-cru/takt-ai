import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"

// Same mocking style as vfs-dispatch.mjs: only Bun.spawn is a double, the
// rendered artifact runs for real.
const calls = []
const coordinator = () => ({ version: 1, units: 0, mutations: 0, cursor: -1, deferrals: 0, next_mandate: 0, requested: false, draining: false })
let respond = (_call) => undefined
globalThis.Bun = {
  spawn(argv, options) {
    assert.equal(argv[0], "/test/takt-ai")
    const call = { verb: argv[1], argv, options }
    calls.push(call)
    const answer = () => respond(call) ?? { stdout: JSON.stringify(defaultAnswer(call)), stderr: "", code: 0 }
    return {
      stdin: { write(json) { call.stdin = JSON.parse(json) }, end() {} },
      get stdout() { return answer().stdout },
      get stderr() { return answer().stderr },
      get exited() { return Promise.resolve(answer().code) },
    }
  },
}
// defaultAnswer is what takt-ai prints on success when no scenario overrides it.
function defaultAnswer(call) {
  if (call.argv[1] === "gc" && call.argv[2] === "coordinate") return coordinator()
  if (call.argv[1] === "dispatch") {
    const action = requestOf(call).action
    return action === "validate_results" || action === "switch" ? null : coordinator()
  }
  if (call.argv[2] === "bind") return { ok: true, key: "k1", attempt_id: "1" }
  return { ok: true }
}
function requestOf(call) {
  const idx = call.argv.indexOf("--request")
  return JSON.parse(call.argv[idx + 1])
}

const store = new Map()
const tools = {}
const hooks = {}
const sessionHooks = {}
const promptedSessions = []
let onPrompt = async (_sessionID, _text) => {}
let onWait = async (_sessionID) => {}
await plugin.setup({
  location: { directory: "/workspace" },
  storage: { async get(key) { return store.get(key) }, async set(key, value) { store.set(key, value) } },
  session: {
    async get({ sessionID }) { return sessionID === "root" ? {} : { parentID: "root", title: sessionID } },
    hook: async (name, callback) => { sessionHooks[name] ??= []; sessionHooks[name].push(callback); return { dispose() {} } },
    create: async () => ({ id: "lent" }),
    prompt: async ({ sessionID, text }) => { promptedSessions.push(sessionID); await onPrompt(sessionID, text) },
    wait: async ({ sessionID }) => onWait(sessionID),
    synthetic: async () => {},
    interrupt: async () => {},
  },
  permission: { hook: async () => () => {} },
  agent: { get: async () => ({ data: { permissions: [] } }) },
  shell: { hook: async () => () => {} },
  tool: {
    hook: async (name, callback) => { hooks[name] ??= []; hooks[name].push(callback); return { dispose() {} } },
    transform: async (callback) => {
      callback({ add: (definition) => { tools[definition.name] = definition }, namespace() {}, list: () => [], get: () => undefined, update() {}, remove() {} })
      return { dispose() {} }
    },
  },
  event: { subscribe: () => ({ [Symbol.asyncIterator]: () => ({ next: () => new Promise(() => {}) }) }) },
})

const delegate = async (phase, description, id, agent) => {
  for (const hook of hooks[phase]) {
    await hook({ tool: "subagent", sessionID: "root", agent: "takt", messageID: "m", id,
      input: { agent, description, prompt: "p" }, status: "completed", result: {} })
  }
}
const dispatched = () => calls.filter(c => c.verb === "dispatch").map(requestOf)

const scenario = process.env.TAKT_VFS_TEST_SCENARIO

if (scenario === "retry") {
  // Baseline: a clean, single-shot delivery.
  let mark = dispatched().length
  await delegate("execute.before", "clean-ok", "call-clean", "pm")
  await tools.deliver_result.execute({ result_ids: [1] }, { sessionID: "clean-ok", agent: "pm" })
  await delegate("execute.after", "clean-ok", "call-clean", "pm")
  const cleanFinish = dispatched().slice(mark).find(r => r.action === "finish")
  assert.ok(cleanFinish, "clean delivery never finished")

  // A malformed first deliver_result call, corrected by a retry within the
  // same delegation, must finish exactly as the clean case above did.
  mark = dispatched().length
  await delegate("execute.before", "retry-ok", "call-retry", "pm")
  respond = (call) => call.verb === "dispatch" && requestOf(call).action === "validate_results"
    ? { stdout: "", stderr: "handoff requires an Engram ID", code: 1 } : undefined
  await assert.rejects(tools.deliver_result.execute({ result_ids: [] }, { sessionID: "retry-ok", agent: "pm" }), /Engram ID/)
  respond = (_call) => undefined
  await tools.deliver_result.execute({ result_ids: [2] }, { sessionID: "retry-ok", agent: "pm" })
  await delegate("execute.after", "retry-ok", "call-retry", "pm")
  const retryFinish = dispatched().slice(mark).find(r => r.action === "finish")
  assert.ok(retryFinish, "corrected retry never finished")

  // The orchestrator's own view of completion carries no trace of the
  // earlier failed attempt: identical shape to a clean first try.
  const byName = (a, b) => a.localeCompare(b)
  assert.deepEqual(Object.keys(retryFinish).sort(byName), Object.keys(cleanFinish).sort(byName))
  assert.deepEqual(Object.keys(retryFinish).sort(byName), ["action", "dispatch", "event", "session"])
  const validateCalls = dispatched().slice(mark).filter(r => r.action === "validate_results")
  assert.equal(validateCalls.length, 2, "expected one failed and one corrected validate_results call")
} else if (scenario === "async_delivery") {
  await delegate("execute.before", "async-result", "call-async", "pm")
  const nudgeEvent = { agent: "pm", sessionID: "async-result", tools: {}, system: [] }
  for (const callback of sessionHooks.context) await callback(nudgeEvent)
  let resume
  const resumed = new Promise(resolve => { resume = resolve })
  let release
  const turn = new Promise(resolve => { release = resolve })
  onPrompt = async () => { resume() }
  onWait = async () => {
    await turn
    await tools.deliver_result.execute({ result_ids: [2] }, { sessionID: "async-result", agent: "pm" })
  }
  let settled = false
  const completion = delegate("execute.after", "async-result", "call-async", "pm").finally(() => { settled = true })
  await resumed
  await Promise.resolve()
  assert.equal(settled, false, "the parent finished while the producer was still active")
  assert.equal(dispatched().filter(r => r.action === "finish").length, 0)
  release()
  await completion
  assert.deepEqual(dispatched().map(r => r.action), ["admit", "launch", "validate_results", "finish"])
} else if (scenario === "async_missing") {
  await delegate("execute.before", "async-missing", "call-async-missing", "pm")
  const nudgeEvent = { agent: "pm", sessionID: "async-missing", tools: {}, system: [] }
  for (const callback of sessionHooks.context) await callback(nudgeEvent)
  let resume
  const resumed = new Promise(resolve => { resume = resolve })
  let release
  const turn = new Promise(resolve => { release = resolve })
  onPrompt = async () => { resume() }
  onWait = async () => { await turn }
  let settled = false
  const completion = delegate("execute.after", "async-missing", "call-async-missing", "pm")
    .then(() => { settled = true; return undefined }, error => { settled = true; return error })
  await resumed
  await Promise.resolve()
  assert.equal(settled, false, "the parent was told the producer failed before its turn ended")
  release()
  const error = await completion
  assert.match(error.message, /delegation ended without delivering a result via deliver_result/)
  assert.deepEqual(dispatched().map(r => r.action), ["admit", "launch", "finish"])
} else if (scenario === "fallback") {
  // A validate_results transport/network failure must read nothing like a
  // never-delivered fallback failure.
  respond = (call) => call.verb === "dispatch" && requestOf(call).action === "validate_results"
    ? { stdout: "", stderr: "engram service unreachable: connect ECONNREFUSED", code: 1 } : undefined
  let transportError
  try {
    await tools.deliver_result.execute({ result_ids: [1] }, { sessionID: "transport-fail", agent: "pm" })
  } catch (error) { transportError = error }
  assert.ok(transportError, "a transport failure did not reject deliver_result")
  assert.match(transportError.message, /ECONNREFUSED/)
  respond = (_call) => undefined

  // A producer that never calls deliver_result is nudged exactly once, then
  // fails distinctly from the transport failure above.
  await delegate("execute.before", "never-delivers", "call-nudge", "pm")
  const nudgeEvent = { agent: "pm", sessionID: "never-delivers", tools: {}, system: [] }
  for (const callback of sessionHooks.context) await callback(nudgeEvent)
  const promptedBefore = promptedSessions.length
  let fallbackError
  try {
    await delegate("execute.after", "never-delivers", "call-nudge", "pm")
  } catch (error) { fallbackError = error }
  assert.ok(fallbackError, "a never-delivered producer finished silently")
  assert.match(fallbackError.message, /delegation ended without delivering a result via deliver_result/)
  const nudges = promptedSessions.slice(promptedBefore)
  assert.deepEqual(nudges, ["never-delivers"], "expected exactly one bounded nudge")

  assert.ok(!fallbackError.message.includes("ECONNREFUSED"), "fallback error borrowed the transport failure's wording")
  assert.ok(!transportError.message.includes("delivering a result via deliver_result"), "transport failure borrowed the fallback's wording")
} else if (scenario === "tpm") {
  // A successful tpm delivery reaches validate_results with its IDs: the
  // producer catalog's tpm entry is wired through end to end, not merely
  // present in a static list somewhere.
  let mark = dispatched().length
  await delegate("execute.before", "tpm-brief", "call-tpm-ok", "tpm")
  await tools.deliver_result.execute({ result_ids: [777, 778] }, { sessionID: "tpm-brief", agent: "tpm" })
  const validateCall = dispatched().slice(mark).find(r => r.action === "validate_results")
  assert.ok(validateCall, "tpm's deliver_result never reached validate_results")
  assert.deepEqual(validateCall.result_ids, [777, 778])
  await delegate("execute.after", "tpm-brief", "call-tpm-ok", "tpm")
  const finished = dispatched().slice(mark).find(r => r.action === "finish")
  assert.ok(finished, "tpm's delivered delegation never finished")

  // The old hardcoded producer list omitted tpm, so a tpm delegation that
  // never delivered would finish anyway. It must now fail like any other
  // producer's missing delivery, and the unit must still settle (finish)
  // rather than being stranded in flight for a retry to hit.
  mark = dispatched().length
  await delegate("execute.before", "tpm-missing", "call-tpm-missing", "tpm")
  await assert.rejects(() => delegate("execute.after", "tpm-missing", "call-tpm-missing", "tpm"),
    /delegation ended without delivering a result via deliver_result/)
  assert.deepEqual(dispatched().slice(mark).map(r => r.action), ["admit", "launch", "finish"])
} else {
  throw new Error(`unknown scenario ${JSON.stringify(scenario)}`)
}
