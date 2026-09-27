// Takt memory — managed by takt-ai; edits are overwritten on sync.
// The harness, not the model, supplies author, session, directory and every user confirmation.
import { Plugin } from "@opencode/plugin"
import { OpenCode } from "@opencode/client"
import { Service } from "@opencode/client/service"

const TAKT_AI = "__TAKT_AI_BINARY__"
const CONTRACT = "Follow the Takt memory contract skill (takt-memory-contract)."

const str = (description?: string) => (description ? { type: "string", description } : { type: "string" })

async function takt(command: string, request: Record<string, unknown>): Promise<{ code: number; ok: boolean; result?: any; error?: string }> {
  const proc = Bun.spawn([TAKT_AI, "memory", command], { stdin: "pipe", stdout: "pipe", stderr: "pipe" })
  proc.stdin.write(JSON.stringify(request))
  proc.stdin.end()
  const [out, code] = await Promise.all([new Response(proc.stdout).text(), proc.exited])
  try {
    return { code, ...JSON.parse(out) }
  } catch {
    return { code, ok: false, error: `takt-ai memory ${command} exited ${code}: ${out || await new Response(proc.stderr).text()}` }
  }
}

async function fallbackClose(session: string, directory: string) {
  await takt("close", { author: "takt", session, directory, fallback: true }).catch(() => undefined)
}

export default Plugin.define({
  id: "takt.memory",
  async setup(ctx) {
    const pluginDirectory = ctx.location.directory
    const touched = new Map<string, string>() // root session -> directory

    // The plugin context exposes permission hooks and replies but not
    // permission.create, so confirmations go through the local service's own
    // HTTP endpoint — the same one every built-in tool's approval rides on.
    let endpoint: Awaited<ReturnType<typeof Service.discover>>
    let client: ReturnType<typeof OpenCode.make> | undefined
    async function confirmations() {
      if (client) return client
      endpoint = await Service.discover()
      if (!endpoint) throw new Error("cannot reach the local OpenCode service to ask for confirmation")
      client = OpenCode.make({ baseUrl: endpoint.url, headers: Service.headers(endpoint) })
      return client
    }

    // Replies arrive as events, so a pending confirmation parks here until its
    // request id comes back.
    const pending = new Map<string, (reply: string) => void>()
    const abort = new AbortController()
    void (async () => {
      for await (const event of ctx.event.subscribe({ signal: abort.signal })) {
        if (event.type === "permission.replied") {
          pending.get(event.data.requestID)?.(event.data.reply)
          continue
        }
        if (event.type !== "session.deleted") continue
        // Only root sessions are recorded, and a deleted session can no longer
        // be read back, so the map is the only source for its directory.
        const directory = touched.get(event.data.sessionID)
        if (!directory) continue
        await fallbackClose(event.data.sessionID, directory)
        touched.delete(event.data.sessionID)
      }
    })().catch(() => undefined) // an aborted subscription is an ordinary shutdown

    // confirm blocks until the user answers. A denial throws, which fails the
    // tool call: nothing is recorded without an explicit approval.
    async function confirm(action: string, title: string, metadata: Record<string, unknown>, c: { sessionID: string; agent: string; messageID: string; id: string }) {
      const api = await confirmations()
      const request = await api.permission.create({
        sessionID: c.sessionID,
        action,
        resources: [title],
        metadata,
        agent: c.agent,
        source: { type: "tool", messageID: c.messageID, id: c.id },
      })
      if (request.effect === "allow") return
      if (request.effect === "deny") throw new Error(`Permission denied: ${action}`)
      const reply = await new Promise<string>((resolve) => pending.set(request.id, resolve))
      pending.delete(request.id)
      if (reply === "reject") throw new Error(`Permission denied: ${action}`)
    }

    async function rootSession(id: string): Promise<string> {
      const seen = new Set<string>()
      let current = id
      while (!seen.has(current)) {
        seen.add(current)
        const info = await ctx.session.get({ sessionID: current })
        // A switch-created session has no parentID; its root is found through
        // the takt_switch metadata the switch itself recorded instead.
        const next = info.parentID ?? info.metadata?.takt_switch
        if (!next) return current
        current = next
      }
      throw new Error(`session ${id} has a parent cycle`)
    }

    async function call(command: string, c: { sessionID: string; agent: string }, fields: Record<string, unknown>) {
      const session = await rootSession(c.sessionID)
      touched.set(session, pluginDirectory)
      const res = await takt(command, { author: c.agent, session, directory: pluginDirectory, ...fields })
      if (!res.ok) throw new Error(res.error ?? `takt-ai memory ${command} failed`)
      return res.result
    }

    await ctx.tool.transform((editor) => {
      editor.add({
        name: "memory_record",
        description: `Record one memory entry for the work you just finished. ${CONTRACT}`,
        input: {
          type: "object",
          properties: {
            nature: { type: "string", enum: ["proposal", "decision", "observation", "hypothesis"] },
            scope: {
              type: "string",
              enum: ["project", "personal"],
              description: "project = workspace tier (this repository); personal = global tier, cross-project user preferences and never repository content",
            },
            title: str(),
            content: str(),
            evidence: str(),
            relates_to: {
              type: "object",
              properties: {
                id: { type: "number" },
                relation: { type: "string", enum: ["corrects", "supersedes", "supplements", "exception-to", "disputes"] },
              },
              required: ["id", "relation"],
              additionalProperties: false,
            },
            user_order: { type: "boolean", description: "Only when the user directly ordered recording this; the user is asked to confirm." },
          },
          required: ["nature", "scope", "title", "content"],
          additionalProperties: false,
        },
        async execute(args: { nature: string; scope: string; title: string; content: string; evidence?: string; relates_to?: unknown; user_order?: boolean }, c) {
          let confirmed = false
          let userOrder = false
          if (args.scope === "personal") {
            await confirm("memory_personal", args.title, { title: args.title, content: args.content }, c)
            confirmed = true
          }
          if (args.user_order === true) {
            await confirm("memory_user_order", args.title, { title: args.title, content: args.content, question: "Did you order recording this?" }, c)
            userOrder = true
          }
          const result = await call("record", c, {
            nature: args.nature,
            scope: args.scope,
            title: args.title,
            content: args.content,
            evidence: args.evidence,
            relates_to: args.relates_to,
            confirmed,
            user_order: userOrder,
          })
          return { content: result.deduplicated ? `Already recorded #${result.id}` : `Recorded #${result.id}` }
        },
      })
      editor.add({
        name: "memory_continue_session",
        description: `Link this session to the previous session it resumes. ${CONTRACT}`,
        input: { type: "object", properties: { previous_session_id: str() }, required: ["previous_session_id"], additionalProperties: false },
        async execute(args: { previous_session_id: string }, c) {
          await call("continue", c, { previous_session: args.previous_session_id })
          return { content: `Continues ${args.previous_session_id}` }
        },
      })
      editor.add({
        name: "memory_close_session",
        description: `Close this session's memory with its objective and state. ${CONTRACT}`,
        input: { type: "object", properties: { objective: str(), state: str() }, required: ["objective", "state"], additionalProperties: false },
        async execute(args: { objective: string; state: string }, c) {
          const result = await call("close", c, { objective: args.objective, state: args.state })
          touched.delete(await rootSession(c.sessionID))
          return { content: `Closed session as #${result.end_anchor_id} with ${result.entries} entries` }
        },
      })
    })

    // Exiting OpenCode is not a session end: a resumed session (--continue) must keep recording.
    return () => abort.abort()
  },
})
