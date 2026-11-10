// Takt memory — managed by takt-ai; edits are overwritten on sync.
// Before each crew memory call this plugin vets the call, asks the user where required, and tells the
// memory service who is calling. It registers no tools; the model reaches the service as MCP tools.
import { randomUUID } from "node:crypto"
import { Plugin } from "@opencode/plugin"
import { OpenCode } from "@opencode/client"
import { Service } from "@opencode/client/service"

const TAKT_AI = "__TAKT_AI_BINARY__"
// The catalog instances, rendered from the catalog; only they may use memory.
const CREW_ROSTER: string[] = "__TAKT_CREW_ROSTER__" as unknown as string[]

// Wire names of the memory service, rendered from the Go constants that define them.
const WIRE: { tool_prefix: string; record: string; bind: string; describe: string; admin_path: string } = "__TAKT_MEMORY_WIRE__" as unknown as never
const TOOL_PREFIX = WIRE.tool_prefix
const RECORD_TOOL = `${TOOL_PREFIX}${WIRE.record}`
// The transport dials the socket; the host is a placeholder.
const ADMIN_URL = `http://memory${WIRE.admin_path}`
const BIND = WIRE.bind
const DESCRIBE = WIRE.describe

// Confirmation actions the user is asked about.
const GLOBAL_ACTION = "memory_global"
const USER_ORDER_ACTION = "memory_user_order"
// A permission request is created within this time; the human's answer has no deadline.
const PERMISSION_CREATE_TIMEOUT_MS = 60_000

const CODE_FENCE = "```"

// Traceability matrix titles are fixed by the memory skills of qa, tpm and verify (PR-QA-9).
const MATRIX_NAME = "Traceability Matrix"
const MATRIX_PREFIX = `${MATRIX_NAME}: `
const MATRIX_ROOT_AUTHOR = "qa"
// How a column entry is placed in the matrix, as the refusals word it.
const supplementRootHint = (own: string) => `relate your first ${own} column entry to the matrix root with supplements`
const SUPERSEDE_HEAD_HINT = "relate a later version to the head of your column chain with supersedes"
const MATRIX_COLUMNS: Record<string, string> = { tpm: "Owning node", verify: "Evidence" }

type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue }
type Call = { sessionID: string; agent: string; messageID: string; id: string }
type Described = { exists: boolean; title?: string; outgoing?: Array<{ entry: number; kind: string }> }

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

const text = (value: unknown) => (typeof value === "string" ? value : "")

// A socket that does not answer; the only failure that `takt-ai memory start` can repair.
class Unreachable extends Error {}

const columnOf = (title: string) => Object.values(MATRIX_COLUMNS).find(column => title.startsWith(MATRIX_PREFIX) && title.endsWith(`: ${column}`))
const isRootTitle = (title: string) => title.startsWith(MATRIX_PREFIX) && title.length > MATRIX_PREFIX.length && columnOf(title) === undefined
// The root author records the root under its title and never relates it to a column.
function checkRootRecord(title: string, targetTitle: string) {
  if (!isRootTitle(title)) throw new Error(`The traceability matrix root is titled \`${MATRIX_PREFIX}<objective>\`; record it with that title.`)
  if (columnOf(targetTitle)) throw new Error("That entry is a matrix column kept by tpm or verify; record your own fact under another title and use disputes to state a contradiction.")
}
const isMatrixTitle = (title: string) => title.startsWith(MATRIX_NAME)

export default Plugin.define({
  id: "takt.memory",
  async setup(ctx) {
    const workspaceDirectory = ctx.location.directory
    let workspace: string | undefined
    let socket: string | undefined

    // `memory start` ensures the owner is running and prints one JSON line: the workspace key of the
    // directory it runs in, and the service socket.
    async function start() {
      const proc = Bun.spawn([TAKT_AI, "memory", "start"], { cwd: workspaceDirectory, stdin: "ignore", stdout: "pipe", stderr: "pipe" })
      const [out, err, code] = await Promise.all([new Response(proc.stdout as BodyInit).text(), new Response(proc.stderr as BodyInit).text(), proc.exited])
      if (code !== 0 || !out.trim()) throw new Error((err || out).trim() || `exit ${code}`)
      const started: unknown = JSON.parse(out)
      if (!isObject(started) || !text(started.workspace) || !text(started.socket)) throw new Error(`memory start printed an unexpected result: ${out.trim()}`)
      workspace = text(started.workspace)
      socket = text(started.socket)
    }
    // A failure here is retried by the first call that needs the service.
    await start().catch(() => undefined)

    const unavailable = (reason: string) => new Error(`The Takt memory service is unavailable (${reason}). Record nothing more: holding the chat, tell the user; in a delegation, end your turn stating this failure. Use Takt memory again only once it is reported running.`)

    async function rpc(tool: string, args: Record<string, unknown>): Promise<unknown> {
      let res: Response
      try {
        res = await fetch(ADMIN_URL, {
          method: "POST",
          unix: socket,
          headers: { "Content-Type": "application/json", Accept: "application/json, text/event-stream" },
          body: JSON.stringify({ jsonrpc: "2.0", id: randomUUID(), method: "tools/call", params: { name: tool, arguments: args } }),
        })
      } catch (error) {
        throw new Unreachable(error instanceof Error ? error.message : String(error))
      }
      const body: unknown = await res.json().catch(() => undefined)
      if (!isObject(body)) throw new Error(`The Takt memory service answered ${tool} with HTTP ${res.status} and no result.`)
      if (isObject(body.error)) throw new Error(text(body.error.message) || `The Takt memory service refused ${tool}.`)
      const result = isObject(body.result) ? body.result : {}
      if (result.isError === true) {
        const first = Array.isArray(result.content) ? result.content[0] : undefined
        throw new Error(isObject(first) && text(first.text) ? text(first.text) : `The Takt memory service refused ${tool}.`)
      }
      return result.structuredContent
    }

    // An admin call; when the socket does not answer, the service is started once and the call retried.
    async function admin(tool: string, build: (workspace: string) => Record<string, unknown>, retried = false): Promise<unknown> {
      try {
        if (workspace === undefined || socket === undefined) throw new Unreachable("no workspace key")
        return await rpc(tool, build(workspace))
      } catch (error) {
        if (!(error instanceof Unreachable)) throw error
        if (retried) throw unavailable(error.message)
        try { await start() } catch (startError) { throw unavailable(startError instanceof Error ? startError.message : String(startError)) }
        return admin(tool, build, true)
      }
    }

    async function describe(id: number): Promise<Described> {
      const result = await admin(DESCRIBE, () => ({ ids: [id] }))
      const entry = isObject(result) && Array.isArray(result.entries) ? result.entries[0] : undefined
      if (!isObject(entry) || typeof entry.exists !== "boolean") throw new Error("The Takt memory service answered describe with an unexpected result.")
      return entry as Described
    }

    // The permission API is reached through the local OpenCode service, the endpoint every built-in
    // approval rides on; the plugin context offers hooks and replies but not permission.create.
    let endpoint: Awaited<ReturnType<typeof Service.discover>>
    let client: ReturnType<typeof OpenCode.make> | undefined
    async function confirmations() {
      if (client) return client
      endpoint = await Service.discover()
      if (!endpoint) throw new Error("cannot reach the local OpenCode service to ask for confirmation")
      client = OpenCode.make({ baseUrl: endpoint.url, headers: Service.headers(endpoint) })
      return client
    }

    // Replies arrive as events, so a pending confirmation parks here until its request id comes back.
    const pending = new Map<string, { session: string; controller: AbortController; resolve: (reply: string) => void; reject: (error: Error) => void }>()
    const abort = new AbortController()
    let eventsRunning = false
    let disposed = false
    function endApprovals(reason: string, session?: string) {
      for (const [id, waiter] of pending) {
        if (session && waiter.session !== session) continue
        pending.delete(id)
        waiter.controller.abort()
        waiter.reject(new Error(`${reason}; no approval was granted; request approval again through the original operation`))
      }
    }
    // confirm blocks until the user answers. A denial throws, which fails the tool call: nothing is
    // recorded without an explicit approval.
    async function confirm(action: string, title: string, metadata: { [key: string]: JsonValue }, c: Call) {
      startEvents()
      if (disposed) throw new Error("approvals are no longer available; no approval was granted; request approval again")
      const api = await confirmations()
      // The permission service accepts only its own `per` identifier prefix.
      const id = `per_${randomUUID()}`
      const controller = new AbortController()
      const reply = new Promise<string>((resolve, reject) => pending.set(id, { session: c.sessionID, controller, resolve, reject }))
      void reply.catch(() => undefined)
      const denied = () => new Error(`The user declined: ${action}. Record this entry only if the user later asks for it.`)
      try {
        const request = await Promise.race([api.permission.create({ id, sessionID: c.sessionID, action, resources: [title], metadata, agent: c.agent,
          source: { type: "tool", messageID: c.messageID, id: c.id } }, { signal: AbortSignal.any([abort.signal, controller.signal, AbortSignal.timeout(PERMISSION_CREATE_TIMEOUT_MS)]) }), reply.then(() => new Promise<never>(() => {}))])
        if (disposed || !eventsRunning || !pending.has(id)) throw new Error("confirmation channel ended; no approval granted; request approval again")
        if (request.id !== id) throw new Error("permission service changed the confirmation correlation")
        if (request.effect === "deny") throw denied()
        if (request.effect !== "allow" && await reply === "reject") throw denied()
      } finally { controller.abort(); pending.delete(id) }
    }

    function startEvents() {
      if (eventsRunning || disposed) return
      eventsRunning = true
      void (async () => {
        for await (const event of ctx.event.subscribe({ signal: abort.signal })) {
          if (event.type === "permission.replied") pending.get(event.data.requestID)?.resolve(event.data.reply)
          else if (event.type === "session.deleted") endApprovals("session deleted", event.data.sessionID)
        }
      })().catch(error => console.error("Takt memory event stream disconnected", error)).finally(() => {
        eventsRunning = false; client = undefined
        endApprovals("confirmation event stream ended")
      })
    }
    startEvents()

    // The top of the parentID chain; a lent session has no parent and is its own claimed root.
    async function claimedRoot(sessionID: string): Promise<string> {
      let current = sessionID
      for (;;) {
        const info = await ctx.session.get({ sessionID: current })
        if (!info.parentID) return current
        current = info.parentID
      }
    }

    // A matrix column chain starts with the entry that supplements the matrix root and continues with
    // the entries that supersede or correct it, whoever wrote them; this finds its start and the root title.
    async function chainStart(head: Described): Promise<Described> {
      const previous = head.outgoing?.find(r => r.kind === "supersedes" || r.kind === "corrects")
      return previous ? chainStart(await describe(previous.entry)) : head
    }

    async function columnChain(head: Described): Promise<{ start: string; root: string } | undefined> {
      const current = await chainStart(head)
      const supplemented = current.outgoing?.find(r => r.kind === "supplements")
      if (!supplemented) return undefined
      const root = await describe(supplemented.entry)
      return root.exists && isRootTitle(text(root.title)) ? { start: text(current.title), root: text(root.title) } : undefined
    }

    // Matrix ownership: only qa creates the root; tpm and verify each keep one column entry that first
    // supplements the root and later supersedes the head of its own chain. Disputes stay open to all.
    async function checkMatrix(agent: string, args: Record<string, unknown>) {
      const title = text(args.title)
      const relation = isObject(args.relates_to) ? args.relates_to : undefined
      if (relation?.relation === "disputes") return
      const target = relation && typeof relation.id === "number" ? await describe(relation.id) : undefined
      const targetTitle = target?.exists ? text(target.title) : ""
      if (!isMatrixTitle(title) && !isMatrixTitle(targetTitle)) return
      const own = MATRIX_COLUMNS[agent]
      if (agent === MATRIX_ROOT_AUTHOR) return checkRootRecord(title, targetTitle)
      if (own === undefined) throw new Error("Only qa, tpm and verify write the traceability matrix; record your fact under another title and use disputes to state a contradiction.")
      if (isRootTitle(title)) throw new Error(`Only ${MATRIX_ROOT_AUTHOR} creates the traceability matrix root; record your column titled \`<matrix title>: ${own}\`.`)
      if (!relation || !target?.exists) throw new Error(`Relate your ${own} column to the matrix: ${supplementRootHint(own)}; ${SUPERSEDE_HEAD_HINT}.`)
      const expected = await expectedColumnTitle(own, relation, target, targetTitle)
      if (title !== expected) throw new Error(`Title this entry exactly \`${expected}\`.`)
    }

    // The title a column entry must carry, from the relation that places it in the matrix.
    async function expectedColumnTitle(own: string, relation: Record<string, unknown>, target: Described, targetTitle: string): Promise<string> {
      if (relation.relation === "supplements") {
        if (!isRootTitle(targetTitle)) throw new Error(`A column supplements the matrix root, titled \`${MATRIX_PREFIX}<objective>\`; ${supplementRootHint(own)}.`)
        return `${targetTitle}: ${own}`
      }
      if (relation.relation !== "supersedes") throw new Error(`A matrix column uses supplements for its first version or supersedes for a later one; change the relation.`)
      const chain = await columnChain(target)
      if (!chain) throw new Error(`The related entry is not a matrix column chain; ${supplementRootHint(own)}, or ${SUPERSEDE_HEAD_HINT}.`)
      const expected = `${chain.root}: ${own}`
      if (chain.start !== expected) throw new Error(`The related entry belongs to the column \`${chain.start}\`, which is not yours; relate to the head of your own column \`${expected}\` with supersedes, or use disputes to state a contradiction.`)
      return expected
    }

    // Vets a record and returns the grants the user approved for it.
    async function vetRecord(event: Call & { input: unknown }): Promise<Record<string, unknown>[]> {
      if (!isObject(event.input)) return []
      const args = event.input
      const { title, content, evidence } = { title: text(args.title), content: text(args.content), evidence: text(args.evidence) }
      const isGlobal = args.scope === "global"
      if (isGlobal && [title, content, evidence].some(field => field.includes(CODE_FENCE) || field.includes(workspaceDirectory))) {
        throw new Error("The global tier holds cross-project preferences, habits and environment quirks, never workspace content. Record this entry with scope workspace, or remove the code and the workspace directory from its title, content, and evidence.")
      }
      const userOrder = args.user_order === true
      // The matrix check only reads, so a record it refuses never costs the user an answer.
      await checkMatrix(event.agent, args)
      if (isGlobal || userOrder) {
        const shown = { title, content }
        if (isGlobal) await confirm(GLOBAL_ACTION, title, shown, event)
        if (userOrder) await confirm(USER_ORDER_ACTION, title, { ...shown, question: "Did you order recording this?" }, event)
      }
      return isGlobal || userOrder ? [args] : []
    }

    await ctx.tool.hook("execute.before", async (event) => {
      if (!event.tool.startsWith(TOOL_PREFIX)) return
      if (!CREW_ROSTER.includes(event.agent)) throw new Error(`"${event.agent}" is not a Takt crew member and cannot use Takt memory; use Takt memory from a crew agent's session.`)
      const grants = event.tool === RECORD_TOOL ? await vetRecord(event) : []
      const root = await claimedRoot(event.sessionID)
      await admin(BIND, key => ({ host_session: event.sessionID, agent: event.agent, claimed_root: root, workspace: key, ...(grants.length ? { grants } : {}) }))
    })

    // Exiting OpenCode is not a session end: a resumed session (--continue) must keep recording.
    return () => { disposed = true; endApprovals("approvals are no longer available"); abort.abort() }
  },
})
