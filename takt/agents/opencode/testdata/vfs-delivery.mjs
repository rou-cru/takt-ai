import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"

// Same mocking style as vfs-dispatch.mjs: only Bun.spawn is a double, the
// rendered artifact runs for real.
const calls = []
const coordinator = () => ({ version: 1, units: 0, mutations: 0, cursor: -1, deferrals: 0, next_mandate: 0, requested: false })
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

const scenario = process.env.TAKT_VFS_TEST_SCENARIO
// A verifier that is also a result producer judges an author bound earlier.
const author = { key: "author-1", agent: "dev", session: "root", dispatch: "dev-child", unit: "writer", revision: 1, deltaHash: "h1" }
const store = new Map(scenario === "dual_role" ? [["takt/vfs/bindings", [author]]] : [])
const titles = { "review-2": "review" }
const verifierGrants = ["vfs_bind", "vfs_read", "vfs_verify"].map(name => ({ action: name, resource: name, effect: "allow" }))
const tools = {}
const hooks = {}
const sessionHooks = {}
const promptedSessions = []
const waitedSessions = []
let onPrompt = async (_sessionID, _text) => {}
let onWait = async (_sessionID) => {}
await plugin.setup({
  location: { directory: "/workspace" },
  storage: { async get(key) { return store.get(key) }, async set(key, value) { store.set(key, value) } },
  session: {
    // The host titles a child after its delegation's description.
    async get({ sessionID }) { return sessionID === "root" ? {} : { parentID: "root", title: titles[sessionID] ?? sessionID } },
    hook: async (name, callback) => { sessionHooks[name] ??= []; sessionHooks[name].push(callback); return { dispose() {} } },
    create: async () => ({ id: "lent" }),
    prompt: async ({ sessionID, text }) => { promptedSessions.push(sessionID); await onPrompt(sessionID, text) },
    wait: async ({ sessionID }) => { waitedSessions.push(sessionID); await onWait(sessionID) },
    synthetic: async () => {},
    interrupt: async () => {},
  },
  permission: { hook: async () => () => {} },
  agent: { get: async ({ agentID }) => ({ data: { permissions: agentID === "verify" ? verifierGrants : [] } }) },
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

const delegate = async (phase, description, id, agent, status = "completed") => {
  if (phase === "execute.before" && description.trim()) await tools.dispatch_inputs.execute({ work_unit_id: description.trim(), none: true }, { sessionID: "root", agent: "takt" })
  for (const hook of hooks[phase]) {
    await hook({ tool: "subagent", sessionID: "root", agent: "takt", messageID: "m", id,
      input: { agent, description, prompt: "p" }, status, result: {} })
  }
}
const dispatched = () => calls.filter(c => c.verb === "dispatch").map(requestOf)

if (scenario === "wait_error" || scenario === "prompt_error") {
  const unit = "failed-resume"
  await delegate("execute.before", unit, "call-failed", "pm")
  for (const callback of sessionHooks.context) {
    await callback({ agent: "pm", sessionID: unit, tools: {}, system: [] })
  }
  const failure = new Error(`${scenario}: child session unavailable`)
  if (scenario === "prompt_error") {
    onPrompt = async () => { throw failure }
  } else {
    onWait = async (sessionID) => {
      assert.equal(sessionID, unit)
      // Even a result delivered before wait rejects must be cleared on exit.
      await tools.deliver_result.execute({ result_ids: [42] }, { sessionID, agent: "pm" })
      throw failure
    }
  }
  await assert.rejects(delegate("execute.after", unit, "call-failed", "pm"), error => error === failure)
  assert.deepEqual(promptedSessions, [unit])
  assert.deepEqual(waitedSessions, scenario === "wait_error" ? [unit] : [])
  const finishes = dispatched().filter(r => r.action === "finish")
  assert.deepEqual(finishes, [{ action: "finish", event: unit, dispatch: "root:call-failed", session: "root" }])

  // A repeated host notification must not settle the same delegation twice.
  await delegate("execute.after", unit, "call-failed", "pm")
  assert.deepEqual(dispatched().filter(r => r.action === "finish"), finishes)

  onPrompt = async () => { assert.fail("retry reused the previous child") }
  onWait = async () => { assert.fail("retry waited on the previous child") }
  await delegate("execute.before", unit, "call-retry", "pm")
  await assert.rejects(delegate("execute.after", unit, "call-retry", "pm"),
    /the specialist ended without delivering its result/)
  assert.equal(dispatched().filter(r => r.action === "finish").length, 2,
    "failed retry must also settle; a stale delivery must not satisfy it")
} else if (scenario === "no_unnecessary_wait") {
  onWait = async () => { assert.fail("no child turn was resumed") }
  for (const status of ["completed", "error", "cancelled"]) {
    const unit = `no-wait-${status}`
    await delegate("execute.before", unit, unit, "pm")
    for (const callback of sessionHooks.context) {
      await callback({ agent: "pm", sessionID: unit, tools: {}, system: [] })
    }
    if (status === "completed") {
      await tools.deliver_result.execute({ result_ids: [1] }, { sessionID: unit, agent: "pm" })
    }
    await delegate("execute.after", unit, unit, "pm", status)
  }
  assert.deepEqual(promptedSessions, [])
  assert.deepEqual(waitedSessions, [])
  assert.equal(dispatched().filter(r => r.action === "finish").length, 3)
} else if (scenario === "concurrent_delivery") {
  const turns = new Map()
  const entered = new Map()
  const releases = new Map()
  for (const unit of ["first", "second"]) {
    await delegate("execute.before", unit, `call-${unit}`, "pm")
    for (const callback of sessionHooks.context) {
      await callback({ agent: "pm", sessionID: unit, tools: {}, system: [] })
    }
    turns.set(unit, new Promise(resolve => { releases.set(unit, resolve) }))
    entered.set(unit, Promise.withResolvers())
  }
  onWait = async (sessionID) => {
    assert.ok(turns.has(sessionID), `unexpected child ${sessionID}`)
    entered.get(sessionID).resolve()
    await turns.get(sessionID)
    await tools.deliver_result.execute({ result_ids: [sessionID === "first" ? 1 : 2] }, { sessionID, agent: "pm" })
  }
  const first = delegate("execute.after", "first", "call-first", "pm")
  const second = delegate("execute.after", "second", "call-second", "pm")
  await Promise.all([...entered.values()].map(entry => entry.promise))
  assert.equal(dispatched().filter(r => r.action === "finish").length, 0)
  releases.get("second")()
  await second
  assert.deepEqual(dispatched().filter(r => r.action === "finish").map(r => r.event), ["second"],
    "one child's result must not finish another waiting delegation")
  releases.get("first")()
  await first
  assert.deepEqual(dispatched().filter(r => r.action === "finish").map(r => r.event), ["second", "first"])
  // Locale-independent ordering: the default `sort()` compares strings in
  // code-unit order, which is fine here but Sonar's S2871 wants the invariant
  // spelled out so the assertion never depends on host collation.
  const byCodeUnit = (a, b) => (a < b ? -1 : a > b ? 1 : 0)
  assert.deepEqual([...waitedSessions].sort(byCodeUnit), ["first", "second"])
  assert.deepEqual([...promptedSessions].sort(byCodeUnit), ["first", "second"])
} else if (scenario === "retry") {
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
  assert.deepEqual(waitedSessions, ["async-result"])
  assert.deepEqual(promptedSessions, ["async-result"])
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
  assert.deepEqual(waitedSessions, ["async-missing"])
  assert.deepEqual(promptedSessions, ["async-missing"])
  assert.match(error.message, /the specialist ended without delivering its result/)
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
  assert.match(fallbackError.message, /the specialist ended without delivering its result/)
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
    /the specialist ended without delivering its result/)
  assert.deepEqual(dispatched().slice(mark).map(r => r.action), ["admit", "launch", "finish"])
} else if (scenario === "dual_role") {
  // verify is a verifier and a result producer at once: it adopts its gate,
  // attaches its verdict, and its delegation still owes its delivered result.
  const claims = [
    { key: "author-1", root_session_id: "root", work_unit_id: "writer", agent_id: "dev", target_instance: "dev", active: true, scope: ["a.go"] },
  ]
  respond = (call) => {
    const reply = (value) => ({ stdout: JSON.stringify(value), stderr: "", code: 0 })
    if (call.argv[2] === "claims") return reply({ ok: true, claims })
    if (call.argv[2] === "assign-verifier") {
      claims.push({ key: "gate-1", root_session_id: "root", work_unit_id: "review", agent_id: "verify", target_instance: "verify", pending: true, scope: [], author_key: call.stdin.author_key })
      return reply({ ok: true, key: "gate-1" })
    }
    if (call.argv[2] === "bind") {
      claims[1].pending = false
      claims[1].active = true
      return reply({ ok: true, key: "gate-1", attempt_id: "1", revision: 0, delta_hash: "" })
    }
    return undefined
  }
  await tools.dispatch_inputs.execute({ work_unit_id: "review", none: true }, { sessionID: "root", agent: "takt" })
  const launch = async (phase, id) => {
    for (const hook of hooks[phase]) {
      await hook({ tool: "subagent", sessionID: "root", agent: "takt", messageID: "m", id,
        input: { agent: "verify", description: "review", prompt: "p", author_keys: ["author-1"] }, status: "completed", result: {} })
    }
  }
  await launch("execute.before", "call-review")
  const context = { agent: "verify", sessionID: "review", tools: {}, system: [] }
  for (const callback of sessionHooks.context) await callback(context)
  assert.ok(context.system.some(part => part.text.includes("You judge the staged work")), "the gate was not delivered bound")
  await tools.vfs_verify.execute({ author_key: "author-1", pass: true, finding: "reviewed" }, { sessionID: "review", agent: "verify" })
  const verdict = calls.find(c => c.argv[2] === "verify")
  assert.ok(verdict, "the verdict never reached the core")
  assert.equal(verdict.stdin.verifier_key, "gate-1")
  // Without its delivery the verifier's delegation fails like any producer's.
  await assert.rejects(launch("execute.after", "call-review"), /the specialist ended without delivering its result/)
  assert.deepEqual(promptedSessions, ["review"], "the verifier was not nudged once to deliver")
  // A second review that delivers ends cleanly with the result handed back.
  claims.splice(1)
  await launch("execute.before", "call-review-2")
  for (const callback of sessionHooks.context) await callback({ ...context, sessionID: "review-2", system: [] })
  await tools.deliver_result.execute({ result_ids: [55] }, { sessionID: "review-2", agent: "verify" })
  const event = { tool: "subagent", sessionID: "root", agent: "takt", messageID: "m", id: "call-review-2",
    input: { agent: "verify", description: "review", prompt: "p" }, status: "completed", result: { content: "Verdict attached." } }
  for (const hook of hooks["execute.after"]) await hook(event)
  assert.match(String(event.result.content), /Delivered results: Engram #55/)
} else {
  throw new Error(`unknown scenario ${JSON.stringify(scenario)}`)
}
