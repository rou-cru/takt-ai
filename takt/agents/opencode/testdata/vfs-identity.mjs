import assert from "node:assert/strict"
import plugin from "./takt-vfs.ts"
import {historyFixture} from "./history-fixture.mjs"

const identity = (ctx, unit) => ({
  ipc_version: 5, caller_session:ctx.sessionID, caller_agent:ctx.agent,
  session_id: "root", ...(unit?{work_unit_id:unit}:{}),
})
function checkIdentity(request, ctx, unit) {
  for (const [field, value] of Object.entries(identity(ctx, unit))) {
    assert.equal(request[field], value, `${field}: ${JSON.stringify(request)}`)
  }
  // Neither the attempt nor the invariant set's version is the caller's to
  // declare: the harness issues both from the binding key.
  for (const field of ["attempt_id", "invariants_hash"]) {
    assert.equal(request[field], undefined, `${field}: ${JSON.stringify(request)}`)
  }
}
// bind registers ctx as the specialist of unit through the plugin and returns the
// binding key the harness answered with.
async function bind(tools, messages, ctx, unit, authorKey) {
  const before = messages.length
  const result = await tools.vfs_bind.execute({
    scope: authorKey ? [] : ["file.txt"], author_key: authorKey,
    // Model-supplied identity must not override the harness context.
    agent_id: "impostor", specialist: "impostor",
  }, ctx)
  assert.equal(messages.length, before + 1)
  const message = messages.at(-1)
  assert.equal(message.command, "bind")
  checkIdentity(message.request, ctx, unit)
  // An author's bind declares the invariants its unit consumes (none here); a
  // verifier's gate takes its author's from the harness.
  assert.equal(message.request.invariants,undefined)
  // A verifier's bind names the staged work it judges; an author's names none.
  assert.equal(message.request.author_key, undefined)
  assert.ok(result.content.includes("at version inv1:test"))
  assert.ok(result.content.endsWith(`author_key ${message.key}`))
  return message.key
}

// Keys are opaque transport responses, not a second implementation of bindings.
for (const reverse of [false, true]) {
  const owner=historyFixture({executions:Object.fromEntries(["author-a","author-b","review-a","review-b","review-c"].map(child=>[child,{dispatch:child,root:"root",unit:child,attempt:"1",agent:child.startsWith("author")?"dev":"verify",caller:"root",child,inputs:[],admitted_at:0}]))})
  const gates=new Map()
  const authorsByCaller=new Map()
  const messages = []
  let claims = []
  const contextHooks = []
  let sequence = 0
  let attempt = 0
  let omitMutationResult = false
  // mutationResult is the revision a mutating command reports, unless a
  // scenario simulates a harness that omits it.
  const mutationResult = command => {
    if (command !== "op" && command !== "shell-complete") return {}
    return omitMutationResult ? {} : { revision: 1, delta_hash: "identity-delta" }
  }
  let codegraphReady = false
  globalThis.Bun = {
    spawn(argv, options) {
      assert.equal(argv[0], "/test/takt-ai")
      if (argv[1] === "codegraph") {
        assert.equal(argv[2], "ensure-index")
        assert.equal(options.cwd, "/workspace")
        codegraphReady = true
        return { stdout: "", stderr: "", exited: Promise.resolve(0) }
      }
      if(argv[1]==="dispatch") {const r=JSON.parse(argv[argv.indexOf("--request")+1]);const a=owner.answer("dispatch",r.action,r);return {stdout:a?.out??"null",stderr:a?.err??"",exited:Promise.resolve(a?.code??0)}}
      if(argv[1]==="gc")return {stdout:JSON.stringify({version:1,units:0,mutations:0,cursor:0,deferrals:0,next_mandate:0,requested:false}),stderr:"",exited:Promise.resolve(0)}
      if(argv[1]==="obs")return {stdin:{write(){},end(){}},stdout:"{}",stderr:"",exited:Promise.resolve(0)}
      assert.equal(argv[1], "vfs")
      assert.equal(options.stdin, "pipe")
      const command = argv[2]
      const key = `opaque-${++sequence}`
      return {
        stdin: {
          write(json) {
            const request=JSON.parse(json);messages.push({command,request,key})
            if(command==="bind")authorsByCaller.set(request.caller_session,key)
          },
          end() {},
        },
        // The attempt and the invariant set's version are issued by the harness,
        // so the plugin can only learn them from this answer.
        get stdout() {
          const r=messages.at(-1).request
          if(command==="verify" && !gates.get(`${r.caller_session}:${r.caller_agent}`)?.has(r.author_key))return JSON.stringify({ok:false,error:"not assigned to this verifier delegation; return to the orchestrator to declare author_keys"})
          if(command==="adopt-gates") {
            const assigned=claims.filter(c=>c.agent_id===r.caller_agent&&c.work_unit_id===r.work_unit_id)
            const selected=gates.get(`${r.caller_session}:${r.caller_agent}`)??new Set()
            assigned.forEach(c=>selected.add(c.author_key));gates.set(`${r.caller_session}:${r.caller_agent}`,selected)
            return JSON.stringify({ok:true,gates:assigned.map(c=>({key:c.key,author_key:c.author_key,scope:["file.txt"]}))})
          }
          return JSON.stringify({ ok: true, ...(command === "claims" ? { claims } : {}), key, attempt_id: String(command === "bind" ? ++attempt : 0), invariants_version: "inv1:test", ...mutationResult(command) }) },
        stderr:"", exited: Promise.resolve(0),
      }
    },
  }
  const parents = {
    "author-a": "root", "author-b": "root",
    "review-a": "root", "review-b": "root", "review-c":"root", "unbound-session": "root",
  }
  // The V2 plugin registers its tools through ctx.tool.transform, so the
  // harness collects them the way OpenCode's registry does.
  // One durable store shared by every instance of the plugin in this run: the
  // binding correlation must survive a runtime restart.
  const stored = new Map()
  const storage = { async get(key) { return stored.get(key) }, async set(key, value) { stored.set(key, value) } }
  const start = async () => {
    const tools = {}
    await plugin.setup({
    location: { directory: "/workspace" },
    storage,
    session: {
    context: async () => [],
      hook: async (name, callback) => {
        if (name === "context") contextHooks.push(callback)
        return { dispose() {} }
      },
      wait:async()=>new Promise(()=>{}),
      async get({ sessionID }) {
        assert.ok(sessionID === "root" || Object.hasOwn(parents, sessionID), sessionID)
        // The host titles a delegated session with its delegation's description,
        // which names the work unit it executes.
        return parents[sessionID] ? { parentID: parents[sessionID], title: sessionID } : { parentID: parents[sessionID] }
      },
    },
    agent: { get: async () => ({ data: { permissions: [{ action: "vfs_read", resource: "*", effect: "allow" }, { action: "vfs_verify", resource: "*", effect: "allow" }] } }) },
    permission: { hook: async () => () => {} },
    shell: { hook: async () => () => {} },
    tool: {
      hook: async () => () => {},
      transform: async (callback) => {
        assert.equal(codegraphReady, true, "CodeGraph must be prepared before tools register")
        callback({
          add: (definition) => { tools[definition.name] = definition },
          namespace() {}, list: () => [], get: () => undefined, update() {}, remove() {},
        })
        return { dispose() {} }
      },
    },
    event: { subscribe: () => ({ [Symbol.asyncIterator]: () => ({ next: () => new Promise(() => {}) }) }) },
    })
    return tools
  }
  const tools = await start()
  const authorA = { sessionID: "author-a", agent: "dev" }
  const authorB = { sessionID: "author-b", agent: "dev" }
  const reviewerA = { sessionID: "review-a", agent: "verify" }
  const reviewerB = { sessionID: "review-b", agent: "verify" }
  const reviewerC = { sessionID: "review-c", agent: "verify" }
  const authors = reverse ? [authorB, authorA] : [authorA, authorB]
  const keys = {}
  for (const author of authors) keys[author.sessionID] = await bind(tools, messages, author, author.sessionID)
  const registrations = [
    [reviewerA, "author-a"], [reviewerB, "author-a"],
    [reviewerC, "author-a"], [reviewerA, "author-b"],
  ]
  // A verifier runs as its own unit, the one its delegation is named after;
  // the author key alone links it to the staged work it judges.
  const expectations = []
  for (const [ctx, unit] of reverse ? registrations.toReversed() : registrations) {
    claims = [{ key: "pending", root_session_id: "root", work_unit_id: ctx.sessionID,
      agent_id: ctx.agent, target_instance: ctx.agent, pending: true, scope: [], author_key: keys[unit] }]
    // CodeMode supplies system context but no event.tools map.
    const event = { ...ctx, system: [] }
    for (const hook of contextHooks) await hook(event) // NOSONAR: hooks run in registration order over shared claims
    const bound = messages.findLast(message => message.command === "adopt-gates")
    checkIdentity(bound.request, ctx, ctx.sessionID)
    assert.equal(bound.request.author_key,undefined)
    assert.ok(gates.get(`${ctx.sessionID}:${ctx.agent}`).has(keys[unit]))
    expectations.push({ ctx, unit, key: bound.key })
  }
  // The verifier names only the author and its verdict; the judged revision
  // and hash stay with the core, never filled in by the plugin.
  const args = author_key => ({ author_key, pass: true, finding: "" })
  for (const { ctx, unit, key } of expectations) {
    const before = messages.length
    await tools.vfs_verify.execute(args(keys[unit]), ctx)
    assert.equal(messages.length, before + 1)
    const { command, request } = messages.at(-1)
    assert.equal(command, "verify")
    checkIdentity(request, ctx)
    assert.equal(request.verifier_key, undefined)
    assert.equal(request.author_key, keys[unit])
    assert.ok(request.call_id, "each verdict carries its own call identity")
    assert.equal(request.expected_revision,undefined)
  }
  // A verifier reads the staged view of the author it judges.
  await tools.vfs_read.execute({ path: "file.txt", call_id: "gate-read" }, reviewerB)
  assert.equal(messages.at(-1).command, "op")
  assert.equal(messages.at(-1).request.view_key,undefined)
  checkIdentity(messages.at(-1).request,reviewerB)
  // Other agents, other dispatches, and bindings for another unit cannot
  // authorize this caller. A rejection must happen before any IPC is sent.
  for (const [ctx, unit] of [
    [{ sessionID: "review-a", agent: "unbound-agent" }, "author-a"],
    [{ sessionID: "unbound-session", agent: "verify" }, "author-a"],
    [reviewerB, "author-b"],
  ]) {
    const before = messages.length
    await assert.rejects(tools.vfs_verify.execute(args(keys[unit]), ctx), /not assigned to this verifier delegation/) // NOSONAR: each rejection is checked against the message count before the next
    assert.equal(messages.length,before+1,"the core must decide verifier authority")
  }
  // A new attempt and a refreshed verifier binding must replace the old
  // identity, not reuse the first key registered for this caller and unit.
  // Discarding is the orchestrator's decision, never the author's.
  await assert.rejects(tools.vfs_discard.execute({ author_key: keys["author-a"], call_id: "self-discard" }, authorA), /orchestrator/)
  await tools.vfs_discard.execute({ author_key: keys["author-a"], call_id: "discard" }, { sessionID: "root", agent: "takt" })
  assert.equal(messages.at(-1).command, "discard")
  assert.equal(messages.at(-1).request.author_key, keys["author-a"])
  owner.executions["author-a"].attempt="2"
  owner.executions["review-a"].attempt="2"
  gates.delete("review-a:verify")
  const nextAuthor = await bind(tools, messages, authorA, "author-a")
  claims = [{ key: "retry-pending", root_session_id: "root", work_unit_id: "review-a",
    agent_id: reviewerA.agent, target_instance: reviewerA.agent, pending: true, scope: [], author_key: nextAuthor }]
  for (const hook of contextHooks) await hook({ ...reviewerA, system: [] }) // NOSONAR: hooks run in registration order over shared claims
  const nextVerifier = messages.findLast(message => message.command === "adopt-gates").key
  await tools.vfs_verify.execute(args(nextAuthor), reviewerA)
  const next = messages.at(-1)
  assert.equal(next.command, "verify")
  checkIdentity(next.request, reviewerA)
  assert.equal(next.request.verifier_key,undefined)
  assert.ok(gates.get("review-a:verify").has(nextAuthor))
  assert.equal(next.request.author_key, nextAuthor)

  // A restarted runtime keeps speaking for the same attempt: the rehydrated
  // binding writes under the key the harness already issued, with no rebind.
  const restarted = await start()
  await restarted.vfs_write.execute({ path: "file.txt", content: "x", call_id: "after-restart" }, authorA)
  const written = messages.at(-1)
  assert.equal(written.command, "op")
  checkIdentity(written.request, authorA)
  assert.equal(written.request.author_key,undefined)
  assert.equal(authorsByCaller.get("author-a"),nextAuthor)
  omitMutationResult = true
  await assert.rejects(() => restarted.vfs_write.execute({ path: "file.txt", content: "stale", call_id: "missing-mutation-result" }, authorA), /omitted revision or delta_hash/)
}
