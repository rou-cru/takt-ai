import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"

// The orchestrator's dispatch tools must reach `takt-ai dispatch`, never
// `takt-ai gc coordinate`: that CLI split is the whole point of the change
// this test guards.
const calls = []
// respond lets a scenario make one command fail the way takt-ai does: exit
// non-zero with the reason on stderr and nothing on stdout.
let respond = (_call) => undefined
globalThis.Bun = {
  spawn(argv, options) {
    assert.equal(argv[0], "/test/takt-ai")
    const call = { verb: argv[1], argv, options }
    calls.push(call)
    const answer = () => respond(call) ?? { stdout: JSON.stringify(defaultAnswer(call)), stderr: "", code: 0 }
    return {
      // VFS commands take their request on stdin; coordination takes --request.
      stdin: { write(json) { call.stdin = JSON.parse(json) }, end() {} },
      get stdout() { return answer().stdout },
      get stderr() { return answer().stderr },
      get exited() { return Promise.resolve(answer().code) },
    }
  },
}
const coordinator = () => ({ version: 1, units: 0, mutations: 0, cursor: -1, deferrals: 0, next_mandate: 0, requested: false })
// defaultAnswer is what takt-ai prints on success when no scenario overrides it.
function defaultAnswer(call) {
  if (call.argv[1] === "dispatch") return ["switch", "validate_results", "validate_inputs"].includes(requestOf(call).action) ? null : coordinator()
  if (call.argv[1] === "gc" && call.argv[2] === "coordinate") return coordinator()
  if (call.argv[2] === "bind") return { ok: true, key: "k1", attempt_id: "1" }
  return { ok: true }
}
function requestOf(call) {
  const idx = call.argv.indexOf("--request")
  return JSON.parse(call.argv[idx + 1])
}

// A delegation left by a process that died is already in storage at start.
const store = new Map([["takt/vfs/delegations", { "old:call-0": { unit: "orphan", root: "old-root" } }]])
const tools = {}
const hooks = {}
const permissionHooks = {}
const sessionHooks = {}
const synthetic = []
const interrupted = []
const unreachable = new Set()
const promptedSessions = []
let onPrompt = async (_sessionID, _text) => {}
await plugin.setup({
  location: { directory: "/workspace" },
  storage: { async get(key) { return store.get(key) }, async set(key, value) { store.set(key, value) } },
  session: {
    async get({ sessionID }) { return sessionID === "root" ? {} : { parentID: "root", title: sessionID } },
    hook: async (name, callback) => { sessionHooks[name] ??= []; sessionHooks[name].push(callback); return { dispose() {} } },
    create: async () => ({ id: "lent" }),
    prompt: async ({ sessionID, text }) => { promptedSessions.push(sessionID); await onPrompt(sessionID, text) },
    wait: async () => {},
    synthetic: async (message) => { synthetic.push(message) },
    interrupt: async ({ sessionID }) => {
      if (unreachable.has(sessionID)) throw new Error(`session ${sessionID} unreachable`)
      interrupted.push(sessionID)
    },
  },
  permission: { hook: async (name, callback) => { permissionHooks[name] ??= []; permissionHooks[name].push(callback); return { dispose() {} } } },
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

const root = { sessionID: "root", agent: "takt" }

// Claim control is explicit and bound to the same root/unit/specialist tuple
// the dispatch admission will consume.
const claim = { key: "claim-current", root_session_id: "root", work_unit_id: "u1", agent_id: "dev", target_instance: "dev", pending: true, active: false, scope: ["a.txt"] }
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [claim] }), stderr: "", code: 0 } : undefined
await tools.dispatch_inputs.execute({ work_unit_id: "u1", result_ids: [7] }, root)
await tools.claim_assign.execute({ work_unit_id: "u1", agent: "dev", scope: ["a.txt"] }, root)
assert.deepEqual(calls.at(-1).stdin, { ipc_version: 4, session_id: "root", work_unit_id: "u1", agent_id: "dev", specialist: "dev", invariants: ["engram:7"], scope: ["a.txt"] })

// Release selects only an exact key listed by claim_list. Claims nobody works
// under (prior-root, or a current-root reservation not yet launched) need no
// confirmation; a current-root claim an agent is active under is denied until
// the orchestrator's native question mechanism obtains the user's yes.
const priorClaim = { ...claim, key: "claim-prior", root_session_id: "prior-root", active: true }
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [claim, priorClaim] }), stderr: "", code: 0 } : undefined
let releaseAt = calls.length
await tools.claim_release.execute({ claim_key: "claim-prior" }, root)
assert.deepEqual(calls.slice(releaseAt).map(c => [c.argv[2], c.stdin]), [
  ["claims", { ipc_version: 4, session_id: "root" }],
  ["release", { ipc_version: 4, session_id: "root", key: "claim-prior" }],
])
releaseAt = calls.length
await tools.claim_release.execute({ claim_key: "claim-current" }, root)
assert.deepEqual(calls.at(-1).stdin, { ipc_version: 4, session_id: "root", key: "claim-current" })
const activeClaim = { ...claim, active: true }
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [activeClaim, priorClaim] }), stderr: "", code: 0 } : undefined
releaseAt = calls.length
await assert.rejects(tools.claim_release.execute({ claim_key: "claim-current", confirmed: false }, root), /WARNING: agent dev \(instance dev, active\) owns \[a\.txt\]/)
assert.equal(calls.slice(releaseAt).filter(c => c.argv[2] === "release").length, 0, "denied active claim was released")
await tools.claim_release.execute({ claim_key: "claim-current", confirmed: true }, root)
assert.deepEqual(calls.at(-1).stdin, { ipc_version: 4, session_id: "root", key: "claim-current" })

await tools.dispatch_commit.execute({ version: "v1", plan: [{ unit: "u1", contract: "c1" }] }, root)
assert.equal(calls.at(-1).verb, "dispatch")
assert.deepEqual(requestOf(calls.at(-1)), { action: "commit", session: "root", version: "v1", plan: [{ unit: "u1", contract: "c1", prerequisites: [] }] })
await tools.dispatch_commit.execute({ version: "v2", base_version: "v1", plan: [{ unit: "u4", contract: "c4" }], withdraw: ["u1"] }, root)
assert.deepEqual(requestOf(calls.at(-1)), { action: "commit", session: "root", version: "v2", base_version: "v1", plan: [{ unit: "u4", contract: "c4", prerequisites: [] }], withdrawals: ["u1"] })

await tools.dispatch_declare_recovery.execute({ objective: "obj1", result: "res1", point: "p1", scope: ["u1"], actions: 5, attempts: 2 }, root)
assert.equal(calls.at(-1).verb, "dispatch")
assert.deepEqual(requestOf(calls.at(-1)), { action: "recovery", session: "root", event: "obj1", objective: "obj1", result: "res1", point: "p1", scope: ["u1"], actions: 5, attempts: 2 })

await tools.dispatch_close_recovery.execute({ objective: "obj1", evidence: "ev1", demonstrated: true }, root)
assert.equal(calls.at(-1).verb, "dispatch")
assert.deepEqual(requestOf(calls.at(-1)), { action: "recovered", session: "root", event: "obj1", objective: "obj1", evidence: "ev1", pass: true })

await tools.dispatch_restore.execute({ objective: "obj1" }, root)
assert.equal(calls.at(-1).verb, "dispatch")
assert.deepEqual(requestOf(calls.at(-1)), { action: "restore", session: "root", objective: "obj1" })

// The exception names its bound exactly as the history records it.
assert.ok(tools.dispatch_exception.input.properties.bound.enum.includes("budget/unplanned-units"))
await tools.dispatch_exception.execute({ event: "u1", bound: "budget/unplanned-units", allowance: 1 }, root)
assert.equal(calls.at(-1).verb, "dispatch")
assert.deepEqual(requestOf(calls.at(-1)), { action: "exception", session: "root", event: "u1", bound: "budget/unplanned-units", allowance: 1 })

await tools.dispatch_contest.execute({ event: "u1" }, root)
assert.equal(calls.at(-1).verb, "dispatch")
assert.deepEqual(requestOf(calls.at(-1)), { action: "contest", session: "root", event: "u1", attempt: "" })

// A non-root caller never reaches the harness for these: rejected before any IPC.
const before = calls.length
await assert.rejects(tools.dispatch_commit.execute({ version: "v1", plan: [] }, { sessionID: "child", agent: "dev" }), /root orchestrator/)
assert.equal(calls.length, before, "non-root caller sent IPC")

// A delegation executes the work unit its description names: admission,
// launch and the end of the call all speak for that unit, and the host call
// identity travels as the dispatch so a retry is told from a repetition.
const delegate = async (phase, description, id = "call-1", agent = "dev", result = {}) => {
  if (phase === "execute.before" && description.trim()) await tools.dispatch_inputs.execute({ work_unit_id: description.trim(), none: true }, root)
  for (const hook of hooks[phase]) {
    await hook({ tool: "subagent", sessionID: "root", agent: "takt", messageID: "m", id,
      input: { agent, description, prompt: "p" }, status: "completed", result })
  }
}
const dispatched = () => calls.filter(c => c.verb === "dispatch").map(requestOf)
const mark = dispatched().length
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [claim, { ...claim, work_unit_id: "u2" }] }), stderr: "", code: 0 } : undefined
await delegate("execute.before", "  u1  ")
await delegate("execute.after", "  u1  ")
const lifecycle = dispatched().slice(mark).filter(r => ["admit", "launch", "finish"].includes(r.action))
assert.deepEqual(lifecycle.map(r => [r.action, r.event]), [["admit", "u1"], ["launch", "u1"], ["finish", "u1"]])
assert.equal(lifecycle[0].dispatch, "root:call-1")
assert.equal(lifecycle[2].dispatch, "root:call-1")
await assert.rejects(() => delegate("execute.before", "   ", "call-2"), /work unit/)

// The orphan left by the dead process is ended before any new admission: its
// liveness is declared uncertain and reconciled as not running.
const restart = dispatched().slice(mark)
assert.deepEqual(restart.slice(0, 2), [
  { action: "uncertain", event: "orphan", dispatch: "old:call-0", session: "old-root" },
  { action: "reconcile", event: "orphan", dispatch: "old:call-0", session: "old-root", pass: false },
])
assert.equal(restart[2].action, "admit")
assert.deepEqual(store.get("takt/vfs/delegations"), {})

// A denied admission never ran, so nothing is remembered for it.
respond = (call) => {
  if (call.argv[2] === "claims") {
    return { stdout: JSON.stringify({ ok: true, claims: [claim, { ...claim, work_unit_id: "u2" }] }), stderr: "", code: 0 }
  }
  if (call.verb === "dispatch" && requestOf(call).action === "admit") {
    return { stdout: "", stderr: "harness: concurrent specialist ceiling of 4 reached", code: 1 }
  }
  return undefined
}
await assert.rejects(() => delegate("execute.before", "u2", "call-3"), /ceiling/)
respond = () => undefined
assert.deepEqual(store.get("takt/vfs/delegations"), {})

// A delegation ends a running cleanup cycle only once it is admitted: a denied
// one leaves the cycle's lanes running, an admitted one stops them, and a lane
// that cannot be interrupted never refuses the admitted delegation.
const runningCycle = { ...coordinator(), cycle: { plan: { session_id: "root", cycle_id: "yield-cycle", mandate_class: "dead-code", delta: [], closure: [], reachability: "codegraph" },
  phase: "collect", scope: [], sessions: { collector: "lane-c", verifier: "lane-v" }, report: {}, started: "2026-01-01T00:00:00Z" } }
let admission
respond = (call) => {
  if (call.argv[2] === "claims") return { stdout: JSON.stringify({ ok: true, claims: [] }), stderr: "", code: 0 }
  if (!call.argv.includes("--request")) return undefined
  const action = requestOf(call).action
  if (action === "status") return { stdout: JSON.stringify(runningCycle), stderr: "", code: 0 }
  return action === "admit" ? admission : undefined
}
admission = { stdout: "", stderr: "harness: concurrent specialist ceiling of 4 reached", code: 1 }
await assert.rejects(() => delegate("execute.before", "yield-denied", "call-y1", "pm"), /ceiling/)
assert.deepEqual(interrupted, [], "a denied delegation stopped the cycle's lanes")
admission = undefined
unreachable.add("lane-c")
await delegate("execute.before", "yield-admitted", "call-y2", "pm")
assert.deepEqual(interrupted, ["lane-v"], "an admitted delegation left the cycle's lanes running")
assert.ok(store.get("takt/vfs/delegations")["root:call-y2"], "a failed lane interruption refused the admitted delegation")
unreachable.clear()
interrupted.length = 0
respond = () => undefined
await tools.deliver_result.execute({ result_ids: [1] }, { sessionID: "yield-admitted", agent: "pm" })
await delegate("execute.after", "yield-admitted", "call-y2", "pm")

// Only an agent designed to work through the VFS needs its scope reserved
// before launch: a planning lane is admitted without any claim, a VFS lane
// without one is refused before admission.
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [] }), stderr: "", code: 0 } : undefined
let planningAt = dispatched().length
await delegate("execute.before", "brief", "call-4", "pm")
await tools.deliver_result.execute({ result_ids: [123, 456] }, { sessionID: "brief", agent: "pm" })
await delegate("execute.after", "brief", "call-4", "pm")
assert.deepEqual(dispatched().slice(planningAt).map(r => r.action), ["admit", "launch", "validate_results", "finish"])
assert.deepEqual(dispatched().slice(planningAt)[2].result_ids, [123, 456])

// deliver_result's own failure is an ordinary failed tool call: no special
// casing, the caller sees it and can retry in the same turn.
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "validate_results"
  ? { stdout: "", stderr: "handoff requires an Engram ID", code: 1 } : undefined
await assert.rejects(tools.deliver_result.execute({ result_ids: [] }, { sessionID: "unreported", agent: "pm" }), /Engram ID/)
respond = () => undefined

// A producer that never calls deliver_result never silently finishes: the
// delegation fails distinctly instead, but its unit still settles so a
// retry is not blocked behind a stranded in-flight state.
planningAt = dispatched().length
await delegate("execute.before", "missing-result", "call-6", "pm")
await assert.rejects(() => delegate("execute.after", "missing-result", "call-6", "pm"), /the specialist ended without delivering its result/)
assert.deepEqual(dispatched().slice(planningAt).map(r => r.action), ["admit", "launch", "finish"])

// The context hook records a producer's own session as its unit's child. A
// delegation that ends without a delivery re-prompts that exact child once;
// a delivery made on that nudge still reaches finish.
planningAt = dispatched().length
await delegate("execute.before", "nudge", "call-7", "pm")
const nudgeEvent = { agent: "pm", sessionID: "nudge", tools: {}, system: [] }
for (const callback of sessionHooks.context) await callback(nudgeEvent)
onPrompt = async (sessionID) => { await tools.deliver_result.execute({ result_ids: [789] }, { sessionID, agent: "pm" }) }
await delegate("execute.after", "nudge", "call-7", "pm")
onPrompt = async () => {}
assert.deepEqual(promptedSessions.slice(-1), ["nudge"])
assert.deepEqual(dispatched().slice(planningAt).map(r => r.action), ["admit", "launch", "validate_results", "finish"])

planningAt = dispatched().length
await assert.rejects(() => delegate("execute.before", "u3", "call-5"), /no matching pending clean claim/)
assert.equal(dispatched().slice(planningAt).filter(r => r.action === "admit").length, 0, "unclaimed VFS lane was admitted")
respond = () => undefined

// The author binds under its delegation's unit; consolidation is the
// orchestrator's alone.
const dev = { sessionID: "u1", agent: "dev" }
await tools.vfs_bind.execute({ scope: ["a.txt"] }, dev)
assert.equal(calls.findLast(c => c.verb === "vfs").stdin.work_unit_id, "u1")
await assert.rejects(tools.vfs_consolidate.execute({ author_key: "k", checkpoint: "cp" }, dev), /orchestrator/)

// A refused consolidation says why: takt-ai's reason travels on stderr.
respond = (call) => call.argv[2] === "consolidate" ? { stdout: "", stderr: "no passed verdict", code: 1 } : undefined
await assert.rejects(tools.vfs_consolidate.execute({ author_key: "k1", checkpoint: "cp" }, root), /no passed verdict/)

// The orchestrator's context carries a maintenance notice only while a cycle
// is due or running, one closing notice after it, and nothing otherwise.
const contextFor = async (agent, status) => {
  const state = { version: 1, units: 0, mutations: 0, cursor: -1, deferrals: 0, next_mandate: 0, requested: false, ...status }
  if (status.cycle) state.cycle = { plan: { session_id: "root", cycle_id: "context-cycle", mandate_class: "dead-code", delta: [], closure: [], reachability: "codegraph" }, phase: status.cycle.phase, scope: [], sessions: {}, report: {}, started: "2026-01-01T00:00:00Z" }
  respond = (call) => requestOf(call).action === "status" ? { stdout: JSON.stringify(state), stderr: "", code: 0 } : undefined
  const event = { agent, sessionID: "root", system: [] }
  for (const callback of sessionHooks.context) await callback(event)
  return event.system.map(part => part.text)
}
assert.deepEqual(await contextFor("takt", {}), [])
assert.match((await contextFor("takt", { cycle: { phase: "baseline" } }))[0], /^A cleanup cycle is running/)
assert.match((await contextFor("takt", { cycle: { phase: "collect" } }))[0], /^A cleanup cycle is running/)
assert.match((await contextFor("takt", {}))[0], /^The cleanup cycle concluded/)
assert.deepEqual(await contextFor("takt", {}), [])
assert.deepEqual(await contextFor("dev", { cycle: { phase: "baseline" } }), [])

// A switch registers with Go, carrying the created child session id, before
// the child is ever prompted; a registration failure stops that
// never-prompted child instead of leaving it dangling.
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "switch"
  ? { stdout: "", stderr: "harness: the interlocutor interface is already held; no chaining", code: 1 } : undefined
const switchAt = calls.length
await assert.rejects(tools.dispatch_switch.execute({ target_agent: "pm", objective: "o" }, root), /no chaining/)
assert.deepEqual(requestOf(calls.slice(switchAt).find(c => c.verb === "dispatch")), { action: "switch", session: "root", child: "lent", agent: "pm" })
assert.equal(promptedSessions.includes("lent"), false, "a never-registered switch child was prompted")
assert.deepEqual(interrupted, ["lent"])
interrupted.length = 0
respond = () => undefined

// A lent interface comes back to the orchestrator: an accepted handoff writes
// the envelope into the root session and gives the holder nothing, then ends
// its turn; a refused one tells only the holder why.
await tools.dispatch_switch.execute({ target_agent: "pm", objective: "o", expected_artifact: "brief.md" }, root)
assert.deepEqual(requestOf(calls.findLast(c => c.verb === "dispatch")), { action: "switch", session: "root", child: "lent", agent: "pm", artifact: "brief.md" })
const holder = { sessionID: "lent", agent: "pm" }
const handoff = { result: "Standard", additional_context: "ctx", extra_artifacts: [] }
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "handoff"
  ? { stdout: "", stderr: "handoff denied: standard artifact \"brief.md\" not found; produce it before handoff", code: 1 } : undefined
await assert.rejects(tools.dispatch_handoff.execute(handoff, holder), /standard artifact "brief.md" not found/)
assert.equal(synthetic.length, 0, "a refused handoff reached the orchestrator")
const envelope = { result: "Standard", additional_context: "ctx", extra_artifacts: [], memory: [] }
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "handoff"
  ? { stdout: JSON.stringify(envelope), stderr: "", code: 0 } : undefined
const returned = await tools.dispatch_handoff.execute(handoff, holder)
assert.equal(returned.content, "", "the holder received the envelope")
assert.equal(synthetic.length, 1)
assert.equal(synthetic[0].sessionID, "root")
assert.match(synthetic[0].text, /back from pm/)
assert.deepEqual(JSON.parse(synthetic[0].text.slice(synthetic[0].text.indexOf("{"))), envelope)
await new Promise(resolve => setTimeout(resolve, 10))
assert.deepEqual(interrupted, ["lent"])
respond = () => undefined

// A finished direct activity names one of the three outcomes the record accepts.
assert.deepEqual(tools.dispatch_activity_finish.input.properties.outcome.enum, ["completed", "failed", "interrupted"])

// A native edit never lands on a path a VFS claim holds; any other path keeps
// the decision the ruleset already took, and a deny is never weakened.
const editEvaluate = async (resource, effect = "allow", agent = "takt") => {
  const event = { sessionID: "root", agent, action: "edit", resources: [resource], effect }
  for (const hook of permissionHooks.evaluate) await hook(event)
  return event
}
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [claim, { ...priorClaim, active: false }] }), stderr: "", code: 0 } : undefined
const heldEdit = await editEvaluate("/workspace/a.txt")
assert.equal(heldEdit.effect, "deny")
assert.match(heldEdit.message, /a\.txt is held by work unit u1 \(agent dev\)/)
assert.equal((await editEvaluate("./a.txt", "allow", "pm")).effect, "deny")
assert.equal((await editEvaluate("docs/brief.md")).effect, "allow")
assert.equal((await editEvaluate("docs/brief.md", "ask")).effect, "ask")
// Only claims still pending or active hold a path.
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [{ ...claim, pending: false }] }), stderr: "", code: 0 } : undefined
assert.equal((await editEvaluate("a.txt")).effect, "allow")
// Ownership that cannot be read is not free to take.
respond = (call) => call.argv[2] === "claims" ? { stdout: "", stderr: "store locked", code: 1 } : undefined
assert.equal((await editEvaluate("docs/brief.md")).effect, "deny")
respond = () => undefined
// Shell decisions are not this hook's.
const shellEvent = { sessionID: "root", agent: "takt", action: "shell", resources: ["cat a.txt"], effect: "allow" }
for (const hook of permissionHooks.evaluate) await hook(shellEvent)
assert.equal(shellEvent.effect, "allow")

// Every refusal is observed as a dispatch decision with its reason class, so
// the operation shows where conduct and controls disagree.
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [] }), stderr: "", code: 0 } : undefined
await assert.rejects(() => delegate("execute.before", "u9", "call-9"), /no matching pending clean claim/)
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "admit"
  ? { stdout: "", stderr: "harness: concurrent specialist ceiling of 4 reached", code: 1 } : undefined
await assert.rejects(() => delegate("execute.before", "brief-2", "call-10", "pm"), /ceiling/)
respond = () => undefined
await new Promise(resolve => setTimeout(resolve, 10))
const refusals = calls.filter(c => c.verb === "obs" && c.stdin?.attributes?.decision === "refused")
  .map(c => [c.stdin.attributes.reason_code, c.stdin.attributes.specialist, c.stdin.work_unit_id])
for (const want of [["claimed_path", "takt", "u1"], ["missing_unit", "dev", ""], ["missing_claim", "dev", "u9"], ["admission_denied", "pm", "brief-2"]]) {
  assert.ok(refusals.some(r => JSON.stringify(r) === JSON.stringify(want)), `refusal ${want} not observed; saw ${JSON.stringify(refusals)}`)
}

// GC prepare returns a Plan directly, not a Coordinator wrapper. Its full
// producer payload must survive the process boundary and reach the tool.
const plan = { session_id: "root", cycle_id: "cycle-1", mandate_class: "dead-code", delta: [], closure: ["a.go"], reachability: "codegraph", analyzer_metadata: { source: "fixture" } }
respond = call => call.verb === "gc" && requestOf(call).action === "prepare"
  ? { stdout: JSON.stringify(plan), stderr: "", code: 0 } : undefined
const prepared = JSON.parse((await tools.gc_prepare.execute({}, root)).content)
assert.deepEqual(prepared, plan)

// The remaining GC operations deliberately return three other Go producer
// shapes: a full Cycle, a StagedView, and Coordinator. Verify routing and that
// reports/baselines/history survive unchanged across the JSON boundary.
let gcRequested = false
let gcPhase = "investigate"
const fullCycle = (phase = gcPhase, extra = {}) => ({ plan, phase, scope: ["a.go"], sessions: { collector: "gc-collector", verifier: "gc-verifier" }, author_key: "author-key", verifier_key: "verifier-key", baseline: [{ name: "baseline", passed: true }], acceptance: [], report: { findings: [{ id: "finding-1" }] }, investigations: [{ finding: "finding-1", outcome: "confirmed", evidence: "test" }], reason: "", started: "2026-01-01T00:00:00Z", ...extra })
const fullCoordinator = () => ({ ...coordinator(), cycle: fullCycle(), history: [fullCycle("closed")] })
const stagedView = { revision: 7, delta_hash: "delta-hash", files: { "a.go": "contents", "deleted.go": null } }
respond = call => {
  if (call.verb !== "gc") return undefined
  const action = requestOf(call).action
  if (action === "status") return { stdout: JSON.stringify(gcRequested ? fullCoordinator() : coordinator()), stderr: "", code: 0 }
  if (action === "request") { gcRequested = true; return { stdout: JSON.stringify(coordinator()), stderr: "", code: 0 } }
  if (["findings", "investigate", "authorize"].includes(action)) return { stdout: JSON.stringify(fullCycle(action === "authorize" ? "collect" : gcPhase)), stderr: "", code: 0 }
  if (["collected", "delta"].includes(action)) {
    if (action === "collected") { gcPhase = "verify" }
    return { stdout: JSON.stringify(stagedView), stderr: "", code: 0 }
  }
  if (action === "verdict") return { stdout: JSON.stringify(fullCoordinator()), stderr: "", code: 0 }
  return undefined
}
await tools.gc_request.execute({}, root)
await new Promise(resolve => setTimeout(resolve, 20))
const collector = { sessionID: "gc-collector", agent: "simplify" }
assert.deepEqual(JSON.parse((await tools.gc_findings.execute({}, collector)).content), fullCycle())
assert.deepEqual(JSON.parse((await tools.gc_investigate.execute({ finding: "finding-1", outcome: "confirmed", evidence: "test" }, collector)).content), fullCycle())
assert.deepEqual(JSON.parse((await tools.gc_authorize.execute({}, collector)).content), fullCycle("collect"))
assert.deepEqual(JSON.parse((await tools.gc_collected.execute({}, collector)).content), stagedView)
await new Promise(resolve => setTimeout(resolve, 20))
const verifier = { sessionID: "gc-verifier", agent: "verify" }
assert.deepEqual(JSON.parse((await tools.gc_delta.execute({}, verifier)).content), stagedView)
assert.deepEqual(JSON.parse((await tools.gc_verdict.execute({ pass: true, evidence: "verified" }, verifier)).content), fullCoordinator())
