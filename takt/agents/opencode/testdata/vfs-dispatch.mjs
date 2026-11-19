import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"
import {historyFixture} from "./history-fixture.mjs"
const orphan={dispatch:"old:call-0",root:"old-root",unit:"orphan",attempt:"1",agent:"dev",caller:"old-root",inputs:[],admitted_at:0}
const owner=historyFixture({executions:{"old:call-0":orphan}})
let coreClaims=[]
const released=[]

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
    let cached
    const answer = () => {
      if(cached)return cached
      const custom=respond(call)
      if(custom?.code || (custom?.stdout && JSON.parse(custom.stdout).ok===false)){ cached = custom; return cached }
      if(call.argv[2]==="release" && (!custom || JSON.parse(custom.stdout).ok!==false))released.push(call.stdin.key)
      const fact=call.verb==="dispatch"?owner.answer("dispatch",requestOf(call).action,requestOf(call),coreClaims):undefined
      cached = custom??(fact?{stdout:fact.out??"",stderr:fact.err??"",code:fact.code}:{stdout:JSON.stringify(defaultAnswer(call)),stderr:"",code:0})
      return cached
    }
    return {
      // VFS commands take their request on stdin; coordination takes --request.
      stdin: { write(json) { call.stdin = JSON.parse(json) }, end() {} },
      get stdout() { return answer().stdout },
      get stderr() { return answer().stderr },
      get exited() { return Promise.resolve(answer().code) },
    }
  },
}
// defaultAnswer is what takt-ai prints on success when no scenario overrides it.
function defaultAnswer(call) {
  if (call.argv[1] === "dispatch") return ["switch", "validate_results"].includes(requestOf(call).action) ? null : { ok: true }
  if (call.argv[2] === "bind") return { ok: true, key: "k1", attempt_id: "1" }
  return { ok: true }
}
function requestOf(call) {
  const idx = call.argv.indexOf("--request")
  return JSON.parse(call.argv[idx + 1])
}

// A delegation left by a process that died is already in storage at start.
const store = new Map([["takt/vfs/delegations", { "old:call-0": { unit: "orphan", root: "old-root" } }]])
const events = []
let wake
async function* stream() {
  while (true) {
    if(!events.length) await new Promise(resolve=>{wake=resolve})
    while(events.length) yield events.shift()
  }
}
const tools = {}
const hooks = {}
const permissionHooks = {}
const sessionHooks = {}
const synthetic = []
const interrupted = []
const unreachable = new Set()
const promptedSessions = []
await plugin.setup({
  location: { directory: "/workspace" },
  storage: { async get(key) { return store.get(key) }, async set(key, value) { store.set(key, value) } },
  session: {
    context: async () => [],
    async get({ sessionID }) { return sessionID === "root" ? {} : { parentID: "root", title: sessionID } },
    hook: async (name, callback) => { sessionHooks[name] ??= []; sessionHooks[name].push(callback); return { dispose() {} } },
    create: async () => ({ id: "lent" }),
    prompt: async ({ sessionID }) => { promptedSessions.push(sessionID) },
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
  event: { subscribe: () => ({ [Symbol.asyncIterator]: stream }) },
})

const root = { sessionID: "root", agent: "takt" }

// Claim control is explicit and bound to the same root/unit/specialist tuple
// the dispatch admission will consume.
const claim = { key: "claim-current", root_session_id: "root", caller_session: "root", caller_agent: "takt", work_unit_id: "u1", agent_id: "dev", target_instance: "dev", pending: true, active: false, scope: ["a.txt"] }
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [claim] }), stderr: "", code: 0 } : undefined
await tools.dispatch_inputs.execute({ work_unit_id: "u1", result_ids: [7] }, root)
await tools.claim_assign.execute({ work_unit_id: "u1", agent: "dev", scope: ["a.txt"] }, root)
assert.deepEqual(calls.at(-1).stdin, { ipc_version: 5, session_id: "root", caller_session: "root", caller_agent: "takt", work_unit_id: "u1", agent_id: "dev", specialist: "dev", scope: ["a.txt"] })

// Release selects only an exact key listed by claim_list. Claims nobody works
// under need no confirmation; a claim whose unit is still running is denied
// until that unit has ended.
const priorClaim = { ...claim, key: "claim-prior", root_session_id: "prior-root", active: true }
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [claim, priorClaim] }), stderr: "", code: 0 } : undefined
let releaseAt = calls.length
await tools.claim_release.execute({ claim_key: "claim-prior" }, root)
assert.deepEqual(calls.slice(releaseAt).filter(c=>c.verb==="vfs").map(c => [c.argv[2], c.stdin]), [
  ["release", { ipc_version: 5, session_id: "root", caller_session: "root", caller_agent: "takt", key: "claim-prior" }],
])
releaseAt = calls.length
await tools.claim_release.execute({ claim_key: "claim-current" }, root)
assert.deepEqual(calls.at(-1).stdin, { ipc_version: 5, session_id: "root", caller_session: "root", caller_agent: "takt", key: "claim-current" })
const activeClaim = { ...claim, active: true }
coreClaims=[claim,{...claim,work_unit_id:"u2"}]
let unitEnded = false
respond = (call) => call.argv[2] === "release" && !unitEnded ? {stdout:JSON.stringify({ok:false,error:"unit still running"}),stderr:"",code:0}:undefined
releaseAt = calls.length
await assert.rejects(tools.claim_release.execute({ claim_key: "claim-current" }, root))
assert.deepEqual(released,["claim-prior","claim-current"],"denied active claim was released")
unitEnded = true
await tools.claim_release.execute({ claim_key: "claim-current" }, root)
assert.deepEqual(calls.at(-1).stdin, { ipc_version: 5, session_id: "root", caller_session: "root", caller_agent: "takt", key: "claim-current" })

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
// Only the user's agreement records it: with no user to ask, nothing is recorded.
await assert.rejects(tools.dispatch_exception.execute({ event: "u1", bound: "budget/unplanned-units", allowance: 1, evidence: "four units pending" }, root))
assert.ok(!calls.some(call => call.verb === "dispatch" && requestOf(call).action === "exception"), "an exception was recorded without the user's agreement")

await tools.dispatch_contest.execute({ event: "u1" }, root)
assert.equal(calls.at(-1).verb, "dispatch")
assert.deepEqual(requestOf(calls.at(-1)), { action: "contest", session: "root", event: "u1", attempt: "" })

// A non-root caller never reaches the harness for these: rejected before any IPC.
const before = calls.length
await assert.rejects(tools.dispatch_commit.execute({ version: "v1", plan: [] }, { sessionID: "child", agent: "dev" }), /root orchestrator/)
assert.equal(calls.slice(before).filter(c=>c.verb==="dispatch" && requestOf(c).action==="commit").length,0,"non-root caller committed")

// A delegation executes the work unit its description names: admission,
// launch and the end of the call all speak for that unit, and the host call
// identity travels as the dispatch so a retry is told from a repetition.
const delegate = async (phase, description, id = "call-1", agent = "dev") => {
  const result = { metadata: { sessionID: description.trim(), status: "running" } }
  if (phase === "execute.before" && description.trim()) await tools.dispatch_inputs.execute({ work_unit_id: description.trim(), result_ids: [] }, root)
  for (const hook of hooks[phase]) {
    await hook({ tool: "subagent", sessionID: "root", agent: "takt", messageID: "m", id,
      input: { agent, description, prompt: "p" }, status: "completed", result })
  }
  if (phase === "execute.after") {
    assert.equal(owner.executions[`root:${id}`].settled, undefined, "native launch ended active work")
    // Drive the existing reconciliation from the native child's idle event.
    events.push({id:`idle-${id}`,created:1,type:"session.idle",data:{sessionID:result.metadata.sessionID}})
    wake?.()
    for(let i=0;i<100 && !owner.executions[`root:${id}`].settled;i++) await new Promise(resolve=>setTimeout(resolve,2))
    assert.ok(owner.executions[`root:${id}`].settled, "idle child was not reconciled")
  }
}
const dispatched = () => calls.filter(c => c.verb === "dispatch").map(requestOf)
const mark = dispatched().length
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [claim, { ...claim, work_unit_id: "u2" }] }), stderr: "", code: 0 } : undefined
await delegate("execute.before", "  u1  ")
await delegate("execute.after", "  u1  ")
const lifecycle = dispatched().slice(mark).filter(r => ["prepare", "finish"].includes(r.action))
assert.deepEqual(lifecycle.map(r => [r.action, r.event]), [["prepare", "u1"], ["finish", "u1"]])
assert.equal(lifecycle[0].dispatch, "root:call-1")
assert.equal(lifecycle[1].dispatch, "root:call-1")
await assert.rejects(() => delegate("execute.before", "   ", "call-2"), /work unit/)

// A restart is not host evidence of termination. The unobserved exact
// dispatch stays in history; independent units remain usable.
assert.equal(owner.executions[orphan.dispatch].settled,undefined)
assert.equal(dispatched().slice(mark).some(r=>["uncertain","reconcile"].includes(r.action)),false)

// A denied admission never ran, so nothing is remembered for it.
respond = (call) => {
  if (call.argv[2] === "claims") {
    return { stdout: JSON.stringify({ ok: true, claims: [claim, { ...claim, work_unit_id: "u2" }] }), stderr: "", code: 0 }
  }
  if (call.verb === "dispatch" && requestOf(call).action === "prepare") {
    return { stdout: "", stderr: "harness: concurrent specialist ceiling of 4 reached", code: 1 }
  }
  return undefined
}
await assert.rejects(() => delegate("execute.before", "u2", "call-3"), /ceiling/)
respond = () => undefined
assert.equal(owner.executions["root:call-3"],undefined)

// Only an agent designed to work through the VFS needs its scope reserved
// before launch: a planning lane is admitted without any claim, a VFS lane
// without one is refused before admission.
respond = (call) => call.argv[2] === "claims" ? { stdout: JSON.stringify({ ok: true, claims: [] }), stderr: "", code: 0 } : undefined
let planningAt = dispatched().length
await delegate("execute.before", "brief", "call-4", "pm")
await tools.deliver_result.execute({ result_ids: [123, 456] }, { sessionID: "brief", agent: "pm" })
await delegate("execute.after", "brief", "call-4", "pm")
assert.deepEqual(dispatched().slice(planningAt).filter(r=>["prepare","deliver","finish"].includes(r.action)).map(r => r.action), ["prepare", "deliver", "finish"])
assert.deepEqual(dispatched().slice(planningAt).find(r=>r.action==="deliver").result_ids, [123, 456])

// deliver_result's own failure is an ordinary failed tool call: no special
// casing, the caller sees it and can retry in the same turn.
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "validate_results"
  ? { stdout: "", stderr: "handoff requires a memory entry ID", code: 1 } : undefined
await assert.rejects(tools.deliver_result.execute({ result_ids: [] }, { sessionID: "unreported", agent: "pm" }), /memory entry ID/)
respond = () => undefined

// Idle settles termination without inventing a delivered result or a verdict.
planningAt = dispatched().length
await delegate("execute.before", "missing-result", "call-6", "pm")
await delegate("execute.after", "missing-result", "call-6", "pm")
assert.deepEqual(owner.executions["root:call-6"].delivery ?? [], [])
assert.deepEqual(dispatched().slice(planningAt).filter(r=>["prepare","deliver","finish"].includes(r.action)).map(r => r.action), ["prepare", "finish"])

// A result delivered after launch is credited to its own child, without a nudge.
planningAt = dispatched().length
await delegate("execute.before", "late", "call-7", "pm")
const lateContext = { agent: "pm", sessionID: "late", tools: {}, system: [] }
for (const callback of sessionHooks.context) await callback(lateContext)
await tools.deliver_result.execute({ result_ids: [789] }, { sessionID: "late", agent: "pm" })
await delegate("execute.after", "late", "call-7", "pm")
assert.deepEqual(promptedSessions, [])
assert.deepEqual(dispatched().slice(planningAt).filter(r=>["prepare","deliver","finish"].includes(r.action)).map(r => r.action), ["prepare", "deliver", "finish"])

coreClaims=[]
planningAt = dispatched().length
await assert.rejects(() => delegate("execute.before", "u3", "call-5"), /no matching pending clean claim/)
assert.equal(dispatched().slice(planningAt).filter(r => r.action === "prepare" && owner.executions[r.dispatch]).length, 0, "unclaimed VFS lane was admitted")
respond = () => undefined

// The author binds under its delegation's unit; consolidation is the
// orchestrator's alone.
coreClaims=[claim]
await delegate("execute.before","u1","call-bind","dev")
owner.executions["root:call-bind"].child="u1"
const dev = { sessionID: "u1", agent: "dev" }
await tools.vfs_bind.execute({ scope: ["a.txt"] }, dev)
assert.equal(calls.findLast(c => c.verb === "vfs").stdin.work_unit_id, "u1")
await assert.rejects(tools.vfs_consolidate.execute({ author_key: "k", checkpoint: "cp" }, dev), /orchestrator/)

// A refused consolidation says why: takt-ai's reason travels on stderr.
respond = (call) => call.argv[2] === "consolidate" ? { stdout: "", stderr: "no passed verdict", code: 1 } : undefined
await assert.rejects(tools.vfs_consolidate.execute({ author_key: "k1", checkpoint: "cp" }, root), /no passed verdict/)

// The orchestrator's context carries no maintenance notice.
const contextOf = async (agent) => {
  const event = { agent, sessionID: "root", system: [] }
  for (const callback of sessionHooks.context) await callback(event)
  return event.system.map(part => part.text)
}
assert.deepEqual(await contextOf("takt"), [])

// From here the user accepts every transition the harness asks about.
globalThis.createPermission = async (r) => ({ id: r.id, effect: "allow" })

// A switch registers with Go, carrying the created child session id, before
// the child is ever prompted; a registration failure stops that
// never-prompted child instead of leaving it dangling.
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "switch"
  ? { stdout: "", stderr: "harness: the interlocutor interface is already held; no chaining", code: 1 } : undefined
const switchAt = calls.length
await assert.rejects(tools.dispatch_switch.execute({ target_agent: "pm", objective: "o", requirements: [] }, root), /no chaining/)
assert.deepEqual(requestOf(calls.slice(switchAt).find(c => c.verb === "dispatch" && requestOf(c).action === "switch")), { action: "switch", session: "root", child: "lent", agent: "pm", requirements: [] })
assert.equal(promptedSessions.includes("lent"), false, "a never-registered switch child was prompted")
assert.deepEqual(interrupted, ["lent"])
interrupted.length = 0
respond = () => undefined

// A lent interface comes back to the orchestrator: an accepted handoff writes
// the envelope into the root session and gives the holder nothing, then ends
// its turn; a refused one tells only the holder why.
await tools.dispatch_switch.execute({ target_agent: "pm", objective: "o", requirements: [] }, root)
assert.deepEqual(requestOf(calls.findLast(c => c.verb === "dispatch" && requestOf(c).action === "switch")), { action: "switch", session: "root", child: "lent", agent: "pm", requirements: [] })
const holder = { sessionID: "lent", agent: "pm" }
const handoff = { result: "Standard", additional_context: "ctx", extra_artifacts: [] }
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "handoff"
  ? { stdout: "", stderr: "handoff denied: a completed result needs at least one entry ID", code: 1 } : undefined
await assert.rejects(tools.dispatch_handoff.execute(handoff, holder), /needs at least one entry ID/)
assert.equal(synthetic.length, 0, "a refused handoff reached the orchestrator")
const envelope = { result: "Standard", additional_context: "ctx", extra_artifacts: [], memory: [] }
const notice = "The interface is back from pm. Envelope: ..."
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "handoff"
  ? { stdout: JSON.stringify({ envelope, notice }), stderr: "", code: 0 } : undefined
const returned = await tools.dispatch_handoff.execute(handoff, holder)
assert.equal(returned.content, "", "the holder received the envelope")
assert.equal(synthetic.length, 1)
assert.equal(synthetic[0].sessionID, "root")
assert.equal(synthetic[0].text, notice, "the root reads the core's notice")
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
respond=(call)=>call.argv[2]==="edit-guard" && call.stdin.resources.some(r=>r.replace(/^\/workspace\/|^\.\//,"")==="a.txt")?{stdout:JSON.stringify({ok:false,error:"a.txt is held by work unit u1 (agent dev)"}),stderr:"",code:0}:undefined
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
respond = (call) => call.argv[2] === "edit-guard" ? { stdout: "", stderr: "store locked", code: 1 } : undefined
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
respond = (call) => call.verb === "dispatch" && requestOf(call).action === "prepare"
  ? { stdout: "", stderr: "harness: concurrent specialist ceiling of 4 reached", code: 1 } : undefined
await assert.rejects(() => delegate("execute.before", "brief-2", "call-10", "pm"), /ceiling/)
respond = () => undefined
await new Promise(resolve => setTimeout(resolve, 10))
const refusals = calls.filter(c => c.verb === "obs" && c.stdin?.attributes?.decision === "refused")
  .map(c => [c.stdin.attributes.reason_code, c.stdin.attributes.specialist, c.stdin.work_unit_id])
for (const want of [["claimed_path", "takt", ""], ["missing_unit", "dev", ""], ["missing_claim", "dev", "u9"], ["admission_denied", "pm", "brief-2"]]) {
  assert.ok(refusals.some(r => JSON.stringify(r) === JSON.stringify(want)), `refusal ${want} not observed; saw ${JSON.stringify(refusals)}`)
}

// GC is no part of the session: the plugin registers none of its tools.
assert.deepEqual(Object.keys(tools).filter(name => name.startsWith("gc_")), [])
