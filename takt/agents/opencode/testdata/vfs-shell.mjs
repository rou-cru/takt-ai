import assert from "node:assert/strict"
import { existsSync, readFileSync } from "node:fs"
import plugin from "./takt-vfs.ts"

// The rendered plugin is executed as it ships: only the coordinator IPC and the
// sandbox adapter are doubles. Shell capture must never be reproduced here.
const messages = []
let plan
globalThis.Bun = {
  spawn(argv) {
    const command = argv[2]
    const body = { bind: { ok: true, key: "author-key", attempt_id: "1", invariants_version: "inv1:test" },
      "shell-prepare": { ok: true, shell: plan },
      "shell-import": { ok: true, revision: 9, delta_hash: "imported-delta" } }[command] ?? { ok: true }
    return {
      stdin: { write(json) { messages.push({ command, request: JSON.parse(json) }) }, end() {} },
      stdout: JSON.stringify(body),
      exited: Promise.resolve(0),
    }
  },
}

const hooks = { permission: {}, shell: {}, tool: {} }
const tools = {}
const register = (bag) => async (name, callback) => {
  (bag[name] ??= []).push(callback)
  return { dispose() {} }
}
await plugin.setup({
  location: { directory: "/workspace" },
  storage: { async get() {}, async set() {} },
  session: {
    async get({ sessionID }) { return sessionID === "root" ? {} : { parentID: "root", title: sessionID } },
    hook: async () => ({ dispose() {} }),
  },
  permission: { hook: register(hooks.permission) },
  shell: { hook: register(hooks.shell) },
  tool: {
    hook: register(hooks.tool),
    transform: async (callback) => {
      callback({ add: (t) => { tools[t.name] = t }, namespace() {}, list: () => [], get: () => undefined, update() {}, remove() {} })
      return { dispose() {} }
    },
  },
  event: { subscribe: () => ({ [Symbol.asyncIterator]: () => ({ next: () => new Promise(() => {}) }) }) },
})

// Rendered with shell enforcement off, the plugin must leave every shell to
// OpenCode while file edits keep their VFS tools.
if (process.env.TAKT_EXPECT_SHELL_OFF) {
  assert.equal(hooks.shell["create.before"], undefined)
  assert.equal(hooks.permission.evaluate, undefined)
  assert.ok(tools.vfs_write, "file edits stay VFS-governed")
  process.exit(0)
}

const author = { sessionID: "author-a", agent: "dev" }
await tools.vfs_bind.execute({ scope: ["data.txt"] }, author)

// OpenCode 2.x runs a shell call in this order: tool execute.before (knows the
// session and agent), shell create.before (knows only the command), then the
// permission check over the command create.before left behind.
const before = async (command, ctx = author) => {
  for (const hook of hooks.tool["execute.before"]) {
    await hook({ tool: "shell", sessionID: ctx.sessionID, agent: ctx.agent, messageID: "m", id: "c", input: { command } })
  }
}
const create = async (command) => {
  const input = { command, cwd: "/workspace", env: { HOME: "/home/real" }, timeout: 1000, shell: "/bin/sh" }
  for (const hook of hooks.shell["create.before"]) await hook(input)
  return input
}
const evaluate = async (resource, ctx = author, effect = "allow") => {
  const event = { sessionID: ctx.sessionID, agent: ctx.agent, action: "shell", resources: [resource], effect }
  for (const hook of hooks.permission.evaluate) await hook(event)
  return event
}
const finish = async (tool = "shell", ctx = author) => {
  for (const hook of hooks.tool["execute.after"]) {
    await hook({ tool, sessionID: ctx.sessionID, agent: ctx.agent, messageID: "m", id: "c", input: {}, status: "completed", result: {} })
  }
}
const wrapperOf = (input) => readFileSync(input.shell, "utf8")

const capturing = {
  decision: "allow", reason: "workspace mutation captured as a staged VFS delta: printf staged > data.txt",
  cwd: "/projection", scratch: "/private/shell/scratch", confirm: "/private/shell/confirm",
  writable: ["/projection/data.txt"], protected: [], private: ["/private", "/workspace"], capture: true,
}

// 1. A workspace mutation is admitted before the shell exists, runs through a
//    sandboxed shell against the coordinator's projection, and its result is
//    imported as one VFS transaction. The command text is never rewritten,
//    since OpenCode parses it for its own permission check.
plan = capturing
await before("printf staged > data.txt")
const prepared = messages.at(-1)
assert.equal(prepared.command, "shell-prepare")
assert.equal(prepared.request.ipc_version, 4)
assert.equal(prepared.request.command, "printf staged > data.txt")
assert.equal(prepared.request.author_key, "author-key")
const wrapped = await create("printf staged > data.txt")
assert.equal(wrapped.command, "printf staged > data.txt", "the command text must stay as admitted")
assert.equal(wrapped.cwd, "/workspace", "OpenCode keeps authorizing the real working directory")
const script = wrapperOf(wrapped)
assert.match(script, /takt-sandbox printf staged > data\.txt/, "the command must run inside the sandbox")
assert.match(script, /supervised/, "the command must run supervised")
assert.match(script, /cd '\/projection'/)
assert.match(script, /export HOME='\/private\/shell\/scratch'/)
assert.equal((await evaluate("printf staged > data.txt")).effect, "allow", "a capturable mutation must not need approval")
// Every path the wrapper received came from the coordinator, none from the model.
assert.deepEqual(globalThis.__taktWrap.at(-1).options.writable, ["/projection/data.txt"])
assert.deepEqual(globalThis.__taktWrap.at(-1).options.privatePaths, ["/private", "/workspace"])
assert.equal(globalThis.__taktWrap.at(-1).options.callID, prepared.request.call_id)
await finish()
const imported = messages.at(-1)
assert.equal(imported.command, "shell-import")
assert.equal(imported.request.call_id, prepared.request.call_id)
assert.equal(imported.request.author_key, "author-key")
assert.equal(existsSync(wrapped.shell), false, "the sandboxed shell is removed once the call ended")

// 2. Read-only inspection runs without approval and without a projection.
plan = { decision: "allow", reason: "inspection: grep -rn todo .", cwd: "/workspace", scratch: "/private/shell/scratch",
  confirm: "/private/shell/confirm", writable: ["/private/shell/scratch"], protected: ["/workspace"], private: ["/private"], capture: true }
await before("grep -rn todo .")
const inspected = await create("grep -rn todo .")
assert.match(wrapperOf(inspected), /cd '\/workspace'/)
assert.doesNotMatch(wrapperOf(inspected), /export HOME=/, "inspection keeps the real home")
assert.equal((await evaluate("grep -rn todo .")).effect, "allow")
await finish()

// 3. An uncapturable mutation is refused before the shell exists.
plan = { decision: "deny", reason: "this agent does not modify the workspace through the shell: printf x > out.txt" }
await assert.rejects(before("printf x > out.txt"), /printf x > out\.txt/)
const refused = await create("printf x > out.txt")
assert.match(refused.command, /exit 122$/, "a denied command must not reach the shell")
assert.equal(refused.shell, "/bin/sh")

// 4. Effects outside the workspace are approval-gated, and every execution is
//    gated again: no approval may broaden a later command.
plan = { decision: "ask", reason: "privilege escalation, which the VFS cannot capture: sudo rm /etc/hosts",
  cwd: "/workspace", scratch: "/private/shell/scratch", confirm: "/private/shell/confirm",
  writable: ["/"], protected: ["/workspace"], private: ["/private"], capture: true }
for (const attempt of [1, 2]) {
  await before("sudo rm /etc/hosts")
  await create("sudo rm /etc/hosts")
  const gated = await evaluate("sudo rm /etc/hosts")
  assert.equal(gated.effect, "ask", `execution ${attempt} must carry its own approval`)
  assert.match(gated.message, /sudo rm \/etc\/hosts/)
  await finish()
}

// 5. A command the harness never admitted cannot run at all, and a permission
//    check with no admission behind it is denied.
const unadmitted = await create("printf sneaky > data.txt")
assert.match(unadmitted.command, /exit 122$/)
assert.equal((await evaluate("printf sneaky > data.txt")).effect, "deny")

// 6. A denial the ruleset already resolved is never weakened.
plan = capturing
await before("printf staged > blocked.txt")
await create("printf staged > blocked.txt")
assert.equal((await evaluate("printf staged > blocked.txt", author, "deny")).effect, "deny")
await finish()

// 7. A coordinator failure refuses rather than falling back to the host.
plan = undefined
await assert.rejects(before("ls"))
assert.match((await create("ls")).command, /exit 122$/)

// 8. An identical command already pending for another session is refused: the
//    shell hook could not tell whose sandbox it belongs to.
plan = capturing
await before("printf staged > data.txt")
await assert.rejects(before("printf staged > data.txt", { sessionID: "author-b", agent: "dev" }), /another session/)
await assert.rejects(before("printf staged > data.txt", { sessionID: "root", agent: "takt" }), /pending/)
await create("printf staged > data.txt")
await evaluate("printf staged > data.txt")
await finish()

// 9. The orchestrator keeps OpenCode's native shell permissions. It does not
//    need a VFS binding, shell plan, sandbox wrapper, or delta import.
const orchestrator = { sessionID: "root", agent: "takt" }
const callsBeforeOrchestrator = messages.length
const importsBeforeOrchestrator = messages.filter((message) => message.command === "shell-import").length
const wrapsBeforeOrchestrator = globalThis.__taktWrap.length
await before("npm create vite@latest", orchestrator)
const directInput = await create("npm create vite@latest")
assert.equal((await evaluate("npm create vite@latest", orchestrator)).effect, "allow")
assert.equal(messages.length, callsBeforeOrchestrator, "orchestrator shell must not invoke VFS IPC")
assert.equal(directInput.command, "npm create vite@latest", "orchestrator command runs without VFS replacement")
assert.equal(directInput.shell, "/bin/sh")
assert.equal(directInput.env.HOME, "/home/real")
assert.equal(globalThis.__taktWrap.length, wrapsBeforeOrchestrator, "orchestrator shell must not be sandbox-wrapped")
await finish("shell", orchestrator)
assert.equal(messages.filter((message) => message.command === "shell-import").length, importsBeforeOrchestrator,
  "orchestrator shell must not import a VFS delta")
// A specialist cannot ride the orchestrator's pending direct path.
await before("npm test", orchestrator)
await assert.rejects(before("npm test"), /another session/)
await create("npm test")

// The VFS bypass never weakens a native permission denial for the orchestrator.
assert.equal((await evaluate("npm create vite@latest", orchestrator, "deny")).effect, "deny")
