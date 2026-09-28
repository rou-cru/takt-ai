// Takt VFS — managed by takt-ai; edits are overwritten on sync.
// The harness, not the model, supplies author, session and directory. Every
// write goes through the durable VFS core: staging first, consolidation only
// after an independent verifier's verdict. One private store per workspace.
import { createHash, randomUUID } from "node:crypto"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { Plugin } from "@opencode/plugin"

const TAKT_AI = "__TAKT_AI_BINARY__"
const ORCHESTRATOR_ID = "__TAKT_ORCHESTRATOR_ID__"
const IPC_VERSION = 4

type ToolInput = Record<string, unknown>
function isToolInput(value: unknown): value is ToolInput {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}
function toolInput<T extends ToolInput = ToolInput>(value: unknown): T {
  if (!isToolInput(value)) throw new Error("tool input must be an object")
  return value as T
}

type VFSShellResponse = { decision: "allow" | "ask" | "deny"; reason: string; writable?: string[]; scratch?: string; protected?: string[]; private?: string[]; capture?: boolean; cwd?: string; confirm?: string }
type VFSResponse = {
  ok: boolean; key?: string; content?: string; revision?: number; delta_hash?: string
  seq?: number; error?: string; attempt_id?: string; invariants_version?: string
  shell?: VFSShellResponse; claims?: OwnershipClaim[]; collision?: { path: string; owner: string; attempted_by: string }
}
type GCPlan = Record<string, unknown> & { session_id: string; cycle_id: string; mandate_class: string; delta: unknown[]; closure: string[]; reachability: string }
type GCCycle = Record<string, unknown> & { phase: string; plan: GCPlan; scope: string[]; sessions: Record<string, string>; report: unknown; started: string; author_key?: string }
type StagedView = { revision: number; delta_hash: string; files: Record<string, string | null> }
type CoordinatorResponse = Record<string, unknown> & {
  version: number; units: number; mutations: number; cursor: number; deferrals: number
  next_mandate: number; requested: boolean; draining: boolean; cycle?: GCCycle
  history?: GCCycle[]
}
type HandoffEnvelope = { result: string; additional_context: string; extra_artifacts: string[]; memory: number[] }
type DispatchResponse = GCPlan | GCCycle | StagedView | CoordinatorResponse | HandoffEnvelope | null
function isCoordinatorResponse(value: DispatchResponse): value is CoordinatorResponse {
  return value !== null && "version" in value && "units" in value
}
function isGCCycleResponse(value: DispatchResponse): value is GCCycle {
  return value !== null && isResponseObject(value) && isGCCycle(value)
}
function isHandoffEnvelope(value: Record<string, unknown>): value is HandoffEnvelope {
  return typeof value.result === "string" && typeof value.additional_context === "string" &&
    Array.isArray(value.extra_artifacts) && value.extra_artifacts.every(item => typeof item === "string") &&
    Array.isArray(value.memory) && value.memory.every(item => typeof item === "number")
}
type OwnershipClaim = { key?: string; author_key?: string; root_session_id: string; work_unit_id: string; agent_id: string; target_instance: string; pending?: boolean; active?: boolean; scope?: string[] }
type CollisionError = { path: string; owner: string; attempted_by: string }
function isResponseObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}
function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every(item => typeof item === "string")
}
// isOptional accepts an absent field or one that passes check.
function isOptional<T>(value: unknown, check: (value: unknown) => value is T): value is T | undefined {
  return value === undefined || check(value)
}
const isString = (value: unknown): value is string => typeof value === "string"
const isBoolean = (value: unknown): value is boolean => typeof value === "boolean"
function parseVFSShell(value: unknown): VFSShellResponse | undefined {
  if (!isResponseObject(value) || (value.decision !== "allow" && value.decision !== "ask" && value.decision !== "deny") ||
      !isString(value.reason) || !isOptional(value.cwd, isString) || !isOptional(value.scratch, isString) ||
      !isOptional(value.confirm, isString) || !isOptional(value.capture, isBoolean) ||
      !isOptional(value.writable, isStringArray) || !isOptional(value.protected, isStringArray) || !isOptional(value.private, isStringArray)) return undefined
  const { decision, reason, cwd, scratch, confirm, capture, writable, protected: protectedPaths, private: privatePaths } = value
  return { decision, reason, ...(isString(cwd) ? { cwd } : {}),
    ...(isString(scratch) ? { scratch } : {}), ...(isString(confirm) ? { confirm } : {}),
    ...(isBoolean(capture) ? { capture } : {}),
    ...(isStringArray(writable) ? { writable } : {}),
    ...(isStringArray(protectedPaths) ? { protected: protectedPaths } : {}), ...(isStringArray(privatePaths) ? { private: privatePaths } : {}) }
}
function responseObject(value: unknown, command: string): Record<string, unknown> {
  if (!isResponseObject(value)) throw new Error(`takt-ai ${command} returned a non-object response`)
  return value
}
// parseClaim keeps a well-formed ownership claim and drops anything else.
function parseClaim(item: unknown): OwnershipClaim[] {
  if (!isResponseObject(item)) return []
  const claim = item
  if (typeof claim.key !== "string" || typeof claim.root_session_id !== "string" || typeof claim.work_unit_id !== "string" || typeof claim.agent_id !== "string" || typeof claim.target_instance !== "string") return []
  return [{ key: claim.key, root_session_id: claim.root_session_id, work_unit_id: claim.work_unit_id, agent_id: claim.agent_id, target_instance: claim.target_instance, ...(claim.pending === true ? { pending: true } : {}), ...(claim.active === true ? { active: true } : {}), ...(Array.isArray(claim.scope) ? { scope: claim.scope.filter((path): path is string => typeof path === "string") } : {}), ...(typeof claim.author_key === "string" ? { author_key: claim.author_key } : {}) }]
}
// vfsResponse keeps the fields of a `takt-ai vfs` answer the plugin knows, each only when well typed.
function vfsResponse(parsed: Record<string, unknown>, ok: boolean): VFSResponse {
  const shell = parseVFSShell(parsed.shell)
  return {
    ok,
    ...(typeof parsed.key === "string" ? { key: parsed.key } : {}),
    ...(typeof parsed.content === "string" ? { content: parsed.content } : {}),
    ...(typeof parsed.revision === "number" ? { revision: parsed.revision } : {}),
    ...(typeof parsed.delta_hash === "string" ? { delta_hash: parsed.delta_hash } : {}),
    ...(typeof parsed.seq === "number" ? { seq: parsed.seq } : {}),
    ...(typeof parsed.error === "string" ? { error: parsed.error } : {}),
    ...(typeof parsed.attempt_id === "string" ? { attempt_id: parsed.attempt_id } : {}),
    ...(typeof parsed.invariants_version === "string" ? { invariants_version: parsed.invariants_version } : {}),
    ...(Array.isArray(parsed.claims) ? { claims: parsed.claims.flatMap(parseClaim) } : {}),
    ...(shell ? { shell } : {}),
  }
}
function isGCPlan(value: Record<string, unknown>): value is GCPlan {
  return typeof value.session_id === "string" && typeof value.cycle_id === "string" && typeof value.mandate_class === "string" &&
    Array.isArray(value.delta) && Array.isArray(value.closure) && value.closure.every((path): path is string => typeof path === "string") && typeof value.reachability === "string"
}
function isGCCycle(value: Record<string, unknown>): value is GCCycle {
  return typeof value.phase === "string" && isResponseObject(value.plan) && isGCPlan(value.plan) &&
    Array.isArray(value.scope) && value.scope.every((path): path is string => typeof path === "string") &&
    isResponseObject(value.sessions) && Object.values(value.sessions).every((session): session is string => typeof session === "string") &&
    Object.hasOwn(value, "report") && typeof value.started === "string" &&
    (value.author_key === undefined || typeof value.author_key === "string")
}
function isStagedView(value: Record<string, unknown>): value is StagedView {
  return typeof value.revision === "number" && typeof value.delta_hash === "string" && isResponseObject(value.files) &&
    Object.values(value.files).every(file => file === null || typeof file === "string")
}
function isCoordinator(value: Record<string, unknown>): value is CoordinatorResponse {
  return typeof value.version === "number" && typeof value.units === "number" && typeof value.mutations === "number" &&
    typeof value.cursor === "number" && typeof value.deferrals === "number" && typeof value.next_mandate === "number" &&
    typeof value.requested === "boolean" && typeof value.draining === "boolean" &&
    (value.cycle === undefined || isResponseObject(value.cycle) && isGCCycle(value.cycle)) &&
    (value.history === undefined || Array.isArray(value.history) && value.history.every(item => isResponseObject(item) && isGCCycle(item)))
}
// gcCoordinateResponse returns result when it is the shape `gc coordinate` answers action with.
function gcCoordinateResponse(result: Record<string, unknown>, action: unknown): DispatchResponse | undefined {
  switch (action) {
    case "prepare": return isGCPlan(result) ? result : undefined
    case "findings": case "investigate": case "authorize": return isGCCycle(result) ? result : undefined
    case "collected": case "delta": return isStagedView(result) ? result : undefined
    default: return isCoordinator(result) ? result : undefined
  }
}
function parseCoordinationResponse(value: unknown, verb: string[], action: unknown): DispatchResponse {
  if (value === null) return null
  const result = responseObject(value, verb.join(" "))
  if (isHandoffEnvelope(result)) return result
  const coordinated = verb[0] === "gc" && verb[1] === "coordinate" ? gcCoordinateResponse(result, action) : undefined
  if (coordinated !== undefined) return coordinated
  if (verb[0] === "dispatch") {
    const acknowledgement = action === "switch" || action === "validate_results"
    if (acknowledgement && (Object.keys(result).length === 0 || result.ok === true)) return null
    if (!acknowledgement && isCoordinator(result)) return result
  }
  throw new Error(`${verb.join(" ")} ${asString(action, "unknown")} returned an unexpected response shape`)
}

// orchestratorOnly refuses a claim operation from any agent but the orchestrator.
const orchestratorOnly = (c: { agent?: string }, operation: string) => {
  if (c.agent !== ORCHESTRATOR_ID) throw new Error(`${operation} belongs to the orchestrator`)
}

// Notices the orchestrator receives around a maintenance cycle: conduct only.
const MAINTENANCE_DUE =
  "Maintenance is due. Delegate nothing new until it concludes. Work already running continues normally and is verified and consolidated as usual. A cleanup cycle then runs alone; the changes it makes were not requested by the user and are not a regression."
const MAINTENANCE_DONE = "Maintenance concluded. Delegation and normal work resume."

// SANDBOX_ADAPTER is the sibling module takt-ai deploys next to this plugin. It
// wraps a command with @anthropic-ai/sandbox-runtime; without it no shell
// command runs at all, because running one unwrapped would be a way around the
// VFS (PR-HAR-15).
const SANDBOX_ADAPTER = "./takt-sandbox.mjs"

// QUOTED_QUOTE closes a single-quoted shell word, emits a literal quote and reopens it.
const QUOTED_QUOTE = String.raw`'\''`

// SHELL_ACTIONS are the permission actions shell execution resolves under; the
// command itself is the resource the rules and the approval match.
const SHELL_ACTIONS = new Set(["shell", "bash"])

// SHELL_NOT_ADMITTED is the status a command exits with when no admitted plan
// covers it. create.before cannot abort, so the command is replaced by its own
// refusal instead of reaching the workspace unwrapped.
const SHELL_NOT_ADMITTED = 122

// VFS_SHELL_ENFORCED decides whether specialist shell commands are admitted
// through the VFS and run in the sandbox. When false every shell command runs
// natively under its OpenCode permission profile; file edits stay governed.
const VFS_SHELL_ENFORCED: boolean = "__TAKT_VFS_SHELL_ENFORCED__" as unknown as boolean

// VFS_AGENTS are the catalog instances that stage their own work; only their
// launch needs the exact scope reserved first.
const VFS_AGENTS: string[] = "__TAKT_VFS_AGENTS__" as unknown as string[]

// RESULT_AGENTS are the catalog producers that carry the takt-result-handoff
// skill: their delegation must call deliver_result before it ends.
const RESULT_AGENTS: string[] = "__TAKT_RESULT_AGENTS__" as unknown as string[]

// INVARIANT_DOCUMENTS are the governing documents a bind declares, in the
// precedence order PR-VFS-CSL-3 fixes. AGENTS.md carries the goal and directives
// the user set for this workspace; the harness pins each document by its content
// and answers with the resulting set version. Dispatches carry no structured
// declaration of SDD planning artifacts, so only user directives are declared.
const INVARIANT_DOCUMENTS = ["AGENTS.md"]

// BINDINGS_KEY holds the plugin's binding correlation across a runtime restart.
// The attempt identity is not here: the harness issues it and keeps it durable.
const BINDINGS_KEY = "takt/vfs/bindings"

// EXCEPTION_BOUNDS are the limits a user exception may raise, by the names the
// execution history records them under.
const EXCEPTION_BOUNDS = [
  "ceiling/concurrent-specialists", "budget/unplanned-units", "budget/contests",
  "budget/recovery-failures", "budget/recovery-actions", "budget/recovery-attempts",
]

// DELEGATIONS_KEY holds which work unit each in-flight host delegation executes.
const DELEGATIONS_KEY = "takt/vfs/delegations"
const VFS_TOOL_NAMES = ["vfs_bind", "vfs_write", "vfs_read", "vfs_delete", "vfs_discard", "vfs_verify", "vfs_consolidate"]

// str and obj keep the JSON Schema inputs readable; V2 takes plain JSON Schema
// rather than the V1 zod-shaped tool.schema helper.
const str = (description?: string) => (description ? { type: "string", description } : { type: "string" })
const obj = (properties: Record<string, unknown>, required: string[]) =>
  ({ type: "object", properties, required, additionalProperties: false })

// asString narrows an unknown protocol field to a string, falling back
// instead of letting a non-string value stringify to the useless
// "[object Object]" (dispatch/shell fields are caller-supplied and only
// typed unknown because they pass through a Record<string, unknown>).
const asString = (v: unknown, fallback: string): string => (typeof v === "string" ? v : fallback)

// SENSITIVE_READ_GLOBS are the secret-bearing paths no agent reads, injected
// from the same list the global read rules deny.
const SENSITIVE_READ_GLOBS: string[] = "__TAKT_SENSITIVE_READ_GLOBS__" as unknown as string[]

// GC_LANE_TOOLS are the tools each GC lane keeps besides its gc_* tools: both
// read the workspace natively to investigate or judge; only the collector
// stages its authorized change.
const GC_LANE_TOOLS: Record<string, string[]> = {
  collector: ["read", "glob", "grep", "vfs_read", "vfs_write", "vfs_delete", "memory_record", "memory_continue_session", "memory_close_session"],
  verifier: ["read", "glob", "grep", "memory_record", "memory_continue_session", "memory_close_session"],
}

// gcSessionPermissions is the ruleset a GC child session is created with: only
// its lane's tools survive. A tool whose last matching rule is a blanket deny
// is removed from the session's tool snapshot entirely.
const gcSessionPermissions = (role: string) => [
  { action: "*", resource: "*", effect: "deny" as const },
  { action: "gc_*", resource: "*", effect: "allow" as const },
  ...GC_LANE_TOOLS[role].map(action => ({ action, resource: "*", effect: "allow" as const })),
  ...SENSITIVE_READ_GLOBS.map(glob => ({ action: "read", resource: `**/${glob}`, effect: "deny" as const })),
]

export default Plugin.define({
  id: "takt.vfs",
  async setup(ctx) {
    const workspace = ctx.location.directory
    // MCP tools are catalogued during OpenCode startup. Prepare the index
    // synchronously here rather than from a later session event, so CodeGraph
    // is active by the time OpenCode asks its MCP server for tools.
    const codegraph = Bun.spawn([TAKT_AI, "codegraph", "ensure-index"], {
      cwd: workspace, stdout: "pipe", stderr: "pipe",
    })
    const [codegraphOut, codegraphErr, codegraphExit] = await Promise.all([
      new Response(codegraph.stdout).text(), new Response(codegraph.stderr).text(), codegraph.exited,
    ])
    if (codegraphExit !== 0) {
      const reason = codegraphErr.trim() || codegraphOut.trim() || `exit ${codegraphExit}`
      throw new Error(`CodeGraph workspace preparation failed: ${reason}`)
    }
    // Everything a binding needs to speak for its dispatch. `dispatch` is the
    // session that ran the bind (the child session the subagent tool created for
    // this dispatch, or the root session when there is none); `unit` is the
    // dispatch that authored the delta, which a verifier joins through the
    // author's key. Keyed by core key.
    // `judges` is the author key a verifier's binding judges; it reads that
    // author's staged view.
    type Binding = { key: string; agent: string; session: string; dispatch: string; unit: string; revision: number; deltaHash: string; judges?: string }
    const bindings = new Map<string, Binding>()
    // OpenCode restarts the plugin's process freely, and a binding outlives that:
    // the map is reloaded here and written back on every change, so a rehydrated
    // dispatch keeps speaking for the attempt the harness issued it.
    for (const b of ((await ctx.storage.get(BINDINGS_KEY)) as Binding[] | undefined) ?? []) bindings.set(b.key, b)
    const persist = () => ctx.storage.set(BINDINGS_KEY, [...bindings.values()])
    // delegations maps each in-flight host subagent call to the work unit it
    // executes, so its end is recorded against the unit it was admitted as. It
    // is durable for the same reason bindings are, and carries its root session
    // so a restart can end it without asking a host that no longer runs it.
    type Delegation = { unit: string; root: string; agent?: string }
    const delegations = new Map<string, Delegation>(
      Object.entries(((await ctx.storage.get(DELEGATIONS_KEY)) as Record<string, Delegation> | undefined) ?? {}))
    const persistDelegations = () => ctx.storage.set(DELEGATIONS_KEY, Object.fromEntries(delegations))
    // deliveries and childOf track a producer's result delivery for the life
    // of one delegation only; neither is durable, unlike bindings/delegations
    // above, because a producer whose process died must redeliver anyway.
    const deliveries = new Map<string, true>()
    const childOf = new Map<string, string>()
    const unitKey = (root: string, unit: string) => `${root}\0${unit}`
    // record keeps the binding, and its durable copy, in step with the harness's
    // answer to an operation.
    async function record(b: Binding, res: VFSResponse) {
      if (typeof res.revision !== "number" || typeof res.delta_hash !== "string") throw new Error("takt-ai VFS mutation response omitted revision or delta_hash")
      b.revision = res.revision
      b.deltaHash = res.delta_hash
      await persist()
    }

    // vfs.Open binds one store to exactly one workspace, so the state directory
    // is derived from the absolute workspace path: slug + 8 hex chars of its
    // SHA-256, kept outside the workspace itself.
    function stateDir(): string {
      const abs = workspace.startsWith("/") ? workspace : `${process.env.HOME}/${workspace}`
      const slug = abs.split("/").findLast(Boolean) ?? "workspace"
      const hash = createHash("sha256").update(abs).digest("hex").slice(0, 8)
      return `${process.env.HOME}/.local/share/takt-ai/vfs/${slug}-${hash}`
    }

    // delegatedUnit is the work unit a dispatch session executes. The host
    // titles a delegated child session with the delegation's description, which
    // is the unit identity; maintenance and root sessions stand for themselves.
    async function delegatedUnit(sessionID: string): Promise<string> {
      if (gcChildren.has(sessionID)) return sessionID
      const info = await ctx.session.get({ sessionID })
      if (!info.parentID) return sessionID
      const unit = String(info.title ?? "").trim()
      if (!unit) throw new Error(`session ${sessionID} carries no work unit identity`)
      return unit
    }

    async function rootSession(id: string): Promise<string> {
      const seen = new Set<string>()
      let current = id
      while (!seen.has(current)) {
        seen.add(current)
        const info = await ctx.session.get({ sessionID: current })
        // A switch-created session has no parentID; its root is found through
        // the takt_switch metadata the switch itself recorded instead.
        const metadataParent = info.metadata?.takt_switch
        const next = info.parentID ?? (typeof metadataParent === "string" ? metadataParent : undefined)
        if (!next) return current
        current = next
      }
      throw new Error(`session ${id} has a parent cycle`)
    }

    // The workspace lock is exclusive and non-blocking, so this process must never
    // run two takt-ai invocations at once: hooks, tools and the GC pump interleave.
    let ipcTail: Promise<unknown> = Promise.resolve()
    function exclusive<T>(run: () => Promise<T>): Promise<T> {
      const next = ipcTail.then(run, run)
      ipcTail = next.catch(() => undefined)
      return next
    }

    const takt = (command: string, request: Record<string, unknown>) => exclusive(() => taktUnlocked(command, request))
    // findClaim returns the claim held for exactly this root session, work unit and
    // agent instance while it is in one of the given states.
    const findClaim = async (root: string, unit: string, agent: string, states: ("pending" | "active")[]) =>
      ((await takt("claims", { session_id: root })).claims ?? []).find((c: OwnershipClaim) =>
        c.root_session_id === root && c.work_unit_id === unit && c.agent_id === agent && c.target_instance === agent && states.some(state => c[state] === true))

    // Observability is deliberately a side channel: telemetry failure must never
    // change the outcome of the governed operation. The Go CLI validates the
    // envelope and persists it in the workspace-local event store.
    function observe(eventClass: string, source: string, agent: string, sessionID: string, attributes: Record<string, unknown>, workUnitID = "", attemptID = "") {
      const payload = JSON.stringify({
        ipc_version: 1, session_id: sessionID, work_unit_id: workUnitID,
        attempt_id: attemptID, agent, source, event_class: eventClass, attributes,
      })
      // Defer the subprocess until the governed hook has returned. This keeps
      // telemetry strictly non-blocking even when OpenCode is under load.
      setTimeout(() => { void (async () => {
        try {
          const proc = Bun.spawn([TAKT_AI, "obs", "ingest", "--workspace", workspace], {
            stdin: "pipe", stdout: "pipe", stderr: "pipe", cwd: workspace,
          })
          proc.stdin.write(payload)
          proc.stdin.end()
          // Drain both streams so a telemetry process cannot remain blocked on
          // pipe backpressure. Its exit status is intentionally non-fatal.
          await Promise.all([new Response(proc.stdout).text(), new Response(proc.stderr).text(), proc.exited])
        } catch (error) {
          console.error("Takt observability unavailable", error)
        }
      })() }, 0)
    }

    // refused records a governed refusal before it reaches the caller: the
    // denials are the signal of whether conduct and controls agree, so they are
    // observed like any other dispatch decision, then thrown unchanged.
    function refused(reasonCode: string, agent: string, sessionID: string, error: Error, workUnitID = ""): Error {
      observe("dispatch", "takt.orchestration", agent || "harness", sessionID, {
        decision: "refused", specialist: agent, reason_code: reasonCode, dispatch_id: workUnitID,
      }, workUnitID)
      return error
    }

    async function taktUnlocked(command: string, request: Record<string, unknown>): Promise<VFSResponse> {
      const proc = Bun.spawn([TAKT_AI, "vfs", command, "--workspace", workspace, "--state", stateDir()], {
        stdin: "pipe", stdout: "pipe", stderr: "pipe", cwd: workspace,
      })
      proc.stdin.write(JSON.stringify({ ipc_version: IPC_VERSION, ...request }))
      proc.stdin.end()
      // takt-ai reports its failures on stderr, so a refusal is read from there.
      const [out, err, code] = await Promise.all([
        new Response(proc.stdout).text(), new Response(proc.stderr).text(), proc.exited,
      ])
      try {
        const output: unknown = JSON.parse(out)
        const parsed = responseObject(output, `vfs ${command}`)
        if (code !== 0 || parsed.ok === false) throw new Error(typeof parsed.error === "string" ? parsed.error : `takt-ai vfs ${command} exited ${code}: ${err}`)
        if (typeof parsed.ok !== "boolean") throw new Error(`takt-ai vfs ${command} returned a response without ok`)
        return vfsResponse(parsed, parsed.ok)
      } catch (e) {
        if (e instanceof SyntaxError) throw new Error(`takt-ai vfs ${command} exited ${code}: ${err || out}`, { cause: e })
        throw e
      }
    }

    // The binding identity is the harness-delivered agent name: opencode.json
    // keys every Takt agent by its catalog instance id, which is what the core
    // resolves role from. The model never supplies it.
    // The attempt and the applicable invariant set are the harness's: it issues
    // them at bind time and resolves them from the binding key afterwards, so no
    // later request restates them.
    function identity(b: Binding) {
      return {
        session_id: b.session,
        work_unit_id: b.unit,
        agent_id: b.agent,
        specialist: b.agent,
        ...(gcChildren.has(b.dispatch) ? { cycle_id: b.unit } : {}),
      }
    }

    // own is the caller's latest binding: this dispatch session under this agent.
    function own(c: { sessionID: string; agent: string }): Binding {
      let found: Binding | undefined
      for (const b of bindings.values()) {
        if (b.dispatch === c.sessionID && b.agent === c.agent) found = b
      }
      if (!found) throw new Error("no VFS binding in this delegation; call vfs_bind first")
      return found
    }

    // binding resolves a tool's target: the author_key it names within this
    // root session AND owned by the calling agent in this very delegation,
    // else the caller's own binding. A key surviving in shared context (e.g. the vfs_bind
    // confirmation message) names a binding, not a grant: another specialist
    // that has merely seen it must not be able to operate under it.
    async function binding(c: { sessionID: string; agent: string }, authorKey?: string): Promise<Binding> {
      if (gcChildren.has(c.sessionID)) return own(c)
      const named = authorKey ? bindings.get(authorKey) : undefined
      if (named?.agent === c.agent && named.dispatch === c.sessionID && named.session === await rootSession(c.sessionID)) return named
      return own(c)
    }

    // release drops a settled binding; the harness opens the next attempt on the
    // next bind, because the settled one no longer holds its scope.
    async function release(b: Binding) {
      bindings.delete(b.key)
      await persist()
    }

    // ─── shell capture ────────────────────────────────────────────────────────
    // Specialist shell execution is governed by VFS: the harness resolves a
    // sandbox plan from the binding's own scope, then imports workspace mutations
    // as a staged delta. The orchestrator instead uses OpenCode's native shell
    // permissions and is never required to bind or route through VFS.
    //
    // OpenCode runs a shell tool call as: tool execute.before (session and
    // agent known) → shell create.before (command only) → permission evaluate
    // over the command as create.before left it. So admission happens in
    // execute.before, create.before swaps the shell executable for a sandboxed
    // one without touching the command text, and evaluate only applies the
    // decision admission already took.
    type ShellPlan = { decision: "allow" | "ask" | "deny"; reason: string; cwd: string; scratch: string; confirm: string; writable: string[]; protected: string[]; private: string[]; capture: boolean }
    type AdmittedShell = {
      plan: ShellPlan; callID: string; session: string; key?: string
      started: boolean; evaluated: boolean; wrapper?: string
    }
    const admittedShell = new Map<string, AdmittedShell>()
    // create.before has no session/agent context, so remember the orchestrator's
    // native-permission admission by exact command until create.
    const directShell = new Map<string, number>()

    if (VFS_SHELL_ENFORCED) {
      await ctx.tool.hook("execute.before", async (event) => {
        if (!SHELL_ACTIONS.has(event.tool)) return
        const command = asString((event.input as { command?: unknown })?.command, "")
        // Correlation into create.before is by command text alone, so a command
        // already pending for another caller would be ambiguous: it could run
        // under the wrong binding's sandbox, or take the orchestrator's direct path.
        const pending = admittedShell.get(command)
        const direct = directShell.has(command)
        if (event.agent === ORCHESTRATOR_ID) {
          if (pending) throw new Error("Takt refused this shell command: an identical specialist command is pending")
          directShell.set(command, (directShell.get(command) ?? 0) + 1)
          return
        }
        if (direct || (pending && pending.session !== event.sessionID)) {
          throw new Error("Takt refused this shell command: an identical command from another session is pending")
        }
        const callID = randomUUID()
        let b: Binding | undefined
        try {
          b = own({ sessionID: event.sessionID, agent: event.agent ?? "" })
        } catch {
          b = undefined // an unbound caller may still inspect; it may not mutate
        }
        const res = await takt("shell-prepare", {
          ...(b
            ? { ...identity(b), author_key: b.key, expected_revision: b.revision }
            : { session_id: await rootSession(event.sessionID), work_unit_id: event.sessionID,
                agent_id: event.agent, specialist: event.agent }),
          call_id: callID, command,
        })
        if (!res.shell) throw new Error("takt-ai vfs shell-prepare response omitted shell plan")
        if (res.shell.decision === "deny") throw refused("shell_denied", event.agent ?? "", event.sessionID, new Error(`Takt refused this shell command: ${res.shell.reason}`))
        if (typeof res.shell.cwd !== "string" || typeof res.shell.scratch !== "string" || typeof res.shell.confirm !== "string" || typeof res.shell.capture !== "boolean" || !res.shell.writable || !res.shell.protected || !res.shell.private) throw new Error("takt-ai vfs shell-prepare response omitted an allowed shell plan field")
        const plan: ShellPlan = { decision: res.shell.decision, reason: res.shell.reason, cwd: res.shell.cwd, scratch: res.shell.scratch, confirm: res.shell.confirm, capture: res.shell.capture, writable: res.shell.writable, protected: res.shell.protected, private: res.shell.private }
        admittedShell.set(command, {
          plan, callID, session: event.sessionID, key: b?.key, started: false, evaluated: false,
        })
      })

      await ctx.shell.hook("create.before", async (input) => {
        const refuse = (reason: string) => {
          const message = JSON.stringify(`Takt: ${reason}`)
          input.command = `echo ${message} >&2; exit ${SHELL_NOT_ADMITTED}`
        }
        const directCount = directShell.get(input.command) ?? 0
        if (directCount > 0) {
          if (directCount === 1) directShell.delete(input.command)
          else directShell.set(input.command, directCount - 1)
          return // The orchestrator uses OpenCode's native shell permission, not VFS.
        }
        // create.before cannot abort, so an unadmitted command is replaced by its
        // own refusal. The hook carries no session or agent, so a shell the user
        // opens outside a dispatch is refused too.
        const admitted = admittedShell.get(input.command)
        if (!admitted || admitted.started) return refuse("this shell command was not admitted")
        const { plan, callID } = admitted
        try {
          const { wrap, supervise } = await import(SANDBOX_ADAPTER)
          const run = supervise(await wrap(input.command, {
            writable: plan.writable, scratch: plan.scratch,
            protectedPaths: plan.protected, privatePaths: plan.private, callID,
          }), plan.confirm)
          // The command text stays as admitted: OpenCode parses it for its own
          // permission check after this hook. The sandbox lives in the shell
          // executable instead, which ignores the arguments it is handed and runs
          // the supervised command against its projection. A command that
          // mutates gets the sandbox scratch as home and temporary directory;
          // inspection runs in the workspace and keeps the real home.
          const quote = (s: string) => `'${s.replaceAll("'", QUOTED_QUOTE)}'`
          const dir = await mkdtemp(join(tmpdir(), "takt-shell-"))
          const wrapper = join(dir, "sh")
          await writeFile(wrapper, [
            "#!/bin/sh",
            `cd ${quote(plan.cwd)} || exit ${SHELL_NOT_ADMITTED}`,
            `export TMPDIR=${quote(plan.scratch)}`,
            ...(plan.cwd === workspace ? [] : [`export HOME=${quote(plan.scratch)}`]),
            `exec /bin/sh -c ${quote(run)}`,
            "",
          ].join("\n"), { mode: 0o700 })
          input.shell = wrapper
          admitted.wrapper = dir
          admitted.started = true
        } catch (e) {
          refuse(`sandbox unavailable, shell denied: ${e}`)
        }
      })

      await ctx.permission.hook("evaluate", async (event) => {
        if (!SHELL_ACTIONS.has(event.action)) return
        // A configured denial is never weakened: the harness may only tighten what
        // the ruleset already resolved, and a denied command is never admitted.
        if (event.effect === "deny" || event.agent === ORCHESTRATOR_ID) return
        // OpenCode asks over the commands it parsed out of the call, not the call
        // itself, so the admission is found by session. Parallel calls in one
        // session resolve to the strictest decision among them. The decision is
        // resolved again for every execution, so a persistent approval cannot
        // silently broaden the command, the resource or the role: an
        // approval-gated command is asked once per run (PR-HAR-9).
        const mine = [...admittedShell.values()].filter(a => a.started && !a.evaluated && a.session === event.sessionID)
        if (mine.length === 0) {
          event.effect = "deny"
          event.message = "Takt refused this shell command: it was not admitted"
          return
        }
        const strict = mine.find(a => a.plan.decision === "ask") ?? mine[0]
        for (const a of mine) a.evaluated = true
        event.effect = strict.plan.decision
        event.message = strict.plan.reason
      })

      // The command's result is admitted as one VFS transaction once it has ended.
      await ctx.tool.hook("execute.after", async (event) => {
        for (const [command, admitted] of admittedShell) {
          if (!admitted.started || admitted.session !== event.sessionID) continue
          admittedShell.delete(command)
          if (admitted.wrapper) await rm(admitted.wrapper, { recursive: true, force: true })
          const b = admitted.key ? bindings.get(admitted.key) : undefined
          if (!admitted.plan.capture || !b) continue
          // A failing command keeps its delta: it is imported, retained, and left
          // unverified for the gate to judge.
          await record(b, await takt("shell-import", {
            ...identity(b), author_key: b.key, call_id: admitted.callID, expected_revision: b.revision,
          }))
        }
      })
    }

    // GC policy and lifecycle live in Go. This queue only transports host events
    // and API-created child identities; no event handler waits for model work.
    let gcInitialized = false
    let gcQueue: Promise<void> = Promise.resolve()
    const prompted = new Set<string>()
    const gcChildren = new Set<string>()
    const gcLaneRole = new Map<string, string>()
    const gcBusy = new Set<string>()
    // interlocutorChild/interlocutorRoot are the temporary interlocutor a
    // dispatch_switch opened, kept locally so a session that dies mid-turn can
    // be aborted without first asking Go who currently holds the role.
    let interlocutorChild: string | undefined
    let interlocutorRoot: string | undefined
    // spawnCoordination runs one of the two harness surfaces that share the
    // same coordinator state: `gc coordinate` for GC's own maintenance-cycle
    // phases and trigger bookkeeping, `dispatch` for ordinary crew admission
    // and lifecycle. Neither is the model's to choose; each call site below
    // names the one its action belongs to.
    async function spawnCoordination(verb: string[], request: Record<string, unknown>): Promise<DispatchResponse> {
      const proc = Bun.spawn([TAKT_AI, ...verb, "--workspace", workspace,
        "--state", stateDir(), "--request", JSON.stringify(request)], {
        stdin: "ignore", stdout: "pipe", stderr: "pipe", cwd: workspace,
      })
      const [out, err, code] = await Promise.all([
        new Response(proc.stdout).text(), new Response(proc.stderr).text(), proc.exited,
      ])
      if (code !== 0) throw new Error(`${verb.join(" ")} ${asString(request.action, "unknown")}: ${err || out}`)
      const parsed: unknown = JSON.parse(out)
      return parseCoordinationResponse(parsed, verb, request.action)
    }
    const coordinate = (request: Record<string, unknown>): Promise<DispatchResponse> =>
      exclusive(() => spawnCoordination(["gc", "coordinate"], request))
    // dispatchAction admits and drives ordinary crew work: the same execution
    // history and admission protocol `gc coordinate` uses to gate its own
    // cycle, reached under its own name rather than under GC's.
    const dispatchAction = (request: Record<string, unknown>): Promise<DispatchResponse> =>
      exclusive(async () => {
        const result = await spawnCoordination(["dispatch"], request)
        const action = asString(request.action, "unknown")
        const session = asString(request.session, "")
        const event = asString(request.event, "")
        const agent = asString(request.agent, "harness")
        observe("dispatch", "takt.orchestration", agent, session, {
          decision: action, dispatch_id: event, reason_code: "dispatch_completed", parallelism: 0,
        }, event)
        if (["admit", "launch", "finish"].includes(action)) {
          observe("unit_lifecycle", "takt.orchestration", agent, session, {
            transition: action, from_state: "", to_state: action, specialist: agent, reason_code: "dispatch_completed",
          }, event)
        }
        return result
      })
    // stopChild interrupts a GC child and reports whether it still exists, so a
    // session removed behind the plugin's back is not mistaken for a live turn.
    async function stopChild(id: string): Promise<boolean> {
      try {
        await ctx.session.get({ sessionID: id })
      } catch {
        return false
      }
      await ctx.session.interrupt({ sessionID: id })
      return true
    }
    // createChildSession and promptChild are the generic child-session trio:
    // GC's own pump and the interlocutor switch (below) both spin up a lane
    // session and seed it the same way, so neither owns this pair.
    async function createChildSession(opts: Parameters<typeof ctx.session.create>[0]): Promise<string> {
      const created = await ctx.session.create(opts)
      return created.id
    }
    async function promptChild(sessionID: string, text: string): Promise<void> {
      await ctx.session.prompt({ sessionID, text })
    }
    // returnInterface hands the interlocutor envelope to the root orchestrator,
    // which holds the interface again; the lent session gets nothing back.
    async function returnInterface(root: string, from: string, envelope: unknown): Promise<void> {
      await ctx.session.synthetic({ sessionID: root, text: `The interface is back from ${from}. Envelope:\n${JSON.stringify(envelope, null, 2)}` })
    }
    async function initializeGC() {
      if (gcInitialized) return
      // A process restart cannot prove that an old model turn has stopped. Abort
      // known children first, then ask the core to restore its own cycle only.
      const state = await coordinate({ action: "status" })
      if (isCoordinatorResponse(state) && state.cycle) {
        for (const id of Object.values(state.cycle.sessions ?? {})) await stopChild(id)
        await coordinate({ action: "recover", evidence: "plugin restart; previous turn cannot be safely resumed" })
      }
      // A host subagent call lives inside the process that started it, so one
      // still recorded here died with that process: its liveness is uncertain,
      // and it is reconciled as not running, which releases its slot (PR-HAR-18).
      for (const [delegation, d] of delegations) {
        await dispatchAction({ action: "uncertain", event: d.unit, session: d.root })
        await dispatchAction({ action: "reconcile", event: d.unit, session: d.root, pass: false })
        delegations.delete(delegation)
        await persistDelegations()
      }
      gcInitialized = true
    }
    function scheduleGC() {
      // setTimeout releases tool/event hooks before the SDK is asked to prompt.
      setTimeout(() => {
        gcQueue = gcQueue.then(pumpGC).catch(async error => {
          try {
            const state = await coordinate({ action: "status" })
            if (isCoordinatorResponse(state) && state.cycle) {
              for (const id of Object.values(state.cycle.sessions ?? {})) await stopChild(id)
              await coordinate({ action: "abort", evidence: String(error) })
            }
          } catch (recoveryError) {
            // No blind unlock on failed reconciliation or external divergence.
            console.error("Takt GC held for recovery", recoveryError)
          }
          console.error("Takt GC", error)
        })
      }, 0)
    }
    async function pumpGC() {
      await initializeGC()
      const state = await coordinate({ action: "status" })
      const cycle = isCoordinatorResponse(state) ? state.cycle : undefined
      if (!cycle?.plan) return
      let role: "verifier" | "collector" | undefined
      if (cycle.phase === "baseline" || cycle.phase === "verify" || cycle.phase === "acceptance") role = "verifier"
      else if (cycle.phase === "investigate") role = "collector"
      if (!role) return
      const stamp = `${cycle.plan.cycle_id}:${cycle.phase}`
      if (prompted.has(stamp)) return
      let child = cycle.sessions?.[role]
      const agent = role === "collector" ? "simplify" : "verify"
      if (!child) {
        // V2 sessions have no settable parent, so a GC lane is its own session,
        // marked and permission-bounded at creation. The cycle's ordinary root
        // stays in cycle.plan.session_id, which is what every binding records.
        child = await createChildSession({
          title: `Takt GC ${cycle.plan.cycle_id} ${role}`,
          agent,
          metadata: { takt_gc: cycle.plan.cycle_id },
          permissions: gcSessionPermissions(role),
        })
        await coordinate({ action: "attach", role, child })
      }
      if (gcBusy.has(child)) return
      gcChildren.add(child)
      gcLaneRole.set(child, role)
      const instructions: Record<string, string> = {
        baseline: "Execute gc_baseline to record the acceptance baseline before any mutation. Then stop.",
        investigate: "Use gc_findings. Investigate each finding and record evidence with gc_investigate. Absence of refutation is not authorization. Public contracts, framework entries, new symbols and semantic documentation are protected. If only proposals/no changes, call gc_no_change. Otherwise call gc_authorize; change only its authorized files through vfs_write/vfs_delete and call gc_collected. Never consolidate yourself.",
        verify: "Use gc_delta to inspect the exact staged delta. Independently verify it preserves behavior and stays within the declared mandate/scope. Call gc_verdict with your explicit evidence. Do not author changes or verify your own work.",
        acceptance: "Call gc_acceptance to execute the same prepared checks on the materialized workspace. Then stop.",
      }
      prompted.add(stamp)
      gcBusy.add(child)
      // Agent catalog model assignments remain authoritative: no model override.
      await promptChild(child, `Harness cycle ${cycle.plan.cycle_id}; mandate ${cycle.plan.mandate_class}. ${instructions[cycle.phase]}`)
    }
    async function gcTool(action: string, c: { sessionID: string; agent: string }, fields: Record<string, unknown> = {}) {
      const role = ["baseline", "delta", "verdict", "acceptance"].includes(action) ? "verifier" : "collector"
      const expected = role === "verifier" ? "verify" : "simplify"
      if (c.agent !== expected || gcLaneRole.get(c.sessionID) !== role || !gcChildren.has(c.sessionID))
        throw new Error(`GC ${action} requires the attached ${role} session`)
      const result = await coordinate({ ...fields, action, session: c.sessionID })
      if (action === "authorize") {
        if (!isGCCycleResponse(result) || typeof result.author_key !== "string") throw new Error("GC authorization response omitted its binding plan")
        const authorKey = result.author_key
        const plan = result.plan
        const b: Binding = { key: authorKey, agent: c.agent, session: plan.session_id,
          dispatch: c.sessionID, unit: plan.cycle_id, revision: 0, deltaHash: "" }
        bindings.set(b.key, b)
        await persist()
      }
      scheduleGC()
      return { content: JSON.stringify(result) }
    }

    const toolStartedAt = new Map<string, number>()
    await ctx.tool.hook("execute.before", async (event) => {
      toolStartedAt.set(`${event.sessionID}:${event.id}`, Date.now())
    })

    await ctx.tool.hook("execute.after", async (event) => {
      const key = `${event.sessionID}:${event.id}`
      const started = toolStartedAt.get(key)
      toolStartedAt.delete(key)
      const root = await rootSession(event.sessionID).catch(() => event.sessionID)
      observe("tool_activity", "takt.platform", event.agent ?? "harness", root, {
        platform: "opencode", tool: event.tool, phase: "after",
        outcome: String(event.status ?? "completed"),
        ...(started === undefined ? {} : { duration_ms: Date.now() - started }),
      })
    })

    await ctx.tool.hook("execute.before", async (event) => {
      if (gcChildren.has(event.sessionID)) {
        // The session's own permission ruleset already removes everything else;
        // this is the second lock, for a tool that reaches execution anyway.
        const allowed = event.tool.startsWith("gc_") || (GC_LANE_TOOLS[gcLaneRole.get(event.sessionID) ?? ""] ?? []).includes(event.tool)
        if (!allowed) throw refused("gc_lane_tool", event.agent ?? "", event.sessionID, new Error("This maintenance cycle does not use that tool"))
        return
      }
      if (event.tool !== "subagent") return
      await initializeGC()
      const root = await rootSession(event.sessionID)
      const input = event.input as { agent?: string; description?: string }
      // The delegation's description names the work unit it executes: a planned
      // unit by its committed identity, and a retry by the same name again. The
      // host call identity tells a transport repetition apart from a retry.
      const unit = (input.description ?? "").trim()
      const specialist = String(input.agent ?? "")
      if (!unit) throw refused("missing_unit", specialist, root, new Error("Name the delegation after the work unit it executes: set its description to the unit identity"))
      if (!specialist) throw refused("missing_specialist", "", root, new Error("Takt VFS refused launch: the requested specialist identity is missing"), unit)
      const delegation = `${event.sessionID}:${event.id}`
      if (VFS_AGENTS.includes(specialist) && !await findClaim(root, unit, specialist, ["pending"])) throw refused("missing_claim", specialist, root, new Error(`Takt VFS refused launch: no matching pending clean claim for session ${root}, work unit ${unit}, agent ${specialist}; assign the exact scope before launching`), unit)
      try {
        await dispatchAction({ action: "admit", event: unit, dispatch: delegation, session: root, agent: specialist })
      } catch (error) {
        throw refused("admission_denied", specialist, root, error as Error, unit)
      }
      // Only an admitted delegation is remembered: a denied one never ran.
      delegations.set(delegation, { unit, root, agent: specialist })
      await persistDelegations()
      // The subagent tool runs synchronously right after this hook returns, so
      // its execution is observed running immediately — this is a recorded
      // fact of this architecture, not an inferred one (PR-HAR-16).
      await dispatchAction({ action: "launch", event: unit, session: root })
      scheduleGC()
    })

    await ctx.tool.hook("execute.after", async (event) => {
      if (event.tool === "subagent" && !gcChildren.has(event.sessionID)) {
        const delegation = `${event.sessionID}:${event.id}`
        const d = delegations.get(delegation)
        if (d === undefined) return
        delegations.delete(delegation)
        await persistDelegations()
        const key = unitKey(d.root, d.unit)
        if (event.status === "completed" && RESULT_AGENTS.includes(d.agent ?? "") && !deliveries.has(key)) {
          const child = childOf.get(key)
          // Bounded to one retry: a producer that forgot deliver_result gets a
          // single nudge, never an unbounded prompt loop.
          if (child) await promptChild(child, "Call deliver_result with this delegation's completed Engram entry IDs before ending your turn.")
          if (!deliveries.has(key)) {
            childOf.delete(key)
            throw new Error("delegation ended without delivering a result via deliver_result")
          }
        }
        deliveries.delete(key)
        childOf.delete(key)
        await dispatchAction({ action: "finish", event: d.unit, dispatch: delegation, session: d.root })
        scheduleGC()
      } else if (event.tool.startsWith("vfs_") && !gcChildren.has(event.sessionID)) {
        await initializeGC()
        await coordinate({ action: "tick", session: await rootSession(event.sessionID) })
        scheduleGC()
      }
    })

    // A native edit never lands on a path a VFS claim holds: that path belongs
    // to the staged work of its unit until it is consolidated or discarded, so
    // editing it natively would race the gate. Only ever tightens a decision.
    const workspaceRelative = (resource: string) => {
      const clean = resource.startsWith(workspace + "/") ? resource.slice(workspace.length + 1) : resource
      return clean.replace(/^\.\//, "")
    }
    await ctx.permission.hook("evaluate", async (event) => {
      if (event.action !== "edit" || event.effect === "deny") return
      const relativeResources = event.resources
        .filter(resource => resource.length > 0)
        .map(workspaceRelative)
      if (relativeResources.length === 0) return
      const session = await rootSession(event.sessionID).catch(() => event.sessionID)
      const claims = await takt("claims", { session_id: session }).then(r => r.claims ?? [], () => undefined)
      if (claims === undefined) {
        // Ownership that cannot be read is not free to take.
        event.effect = "deny"
        event.message = "Takt refused this edit: path ownership could not be read"
        return
      }
      for (const c of claims) {
        if (c.pending !== true && c.active !== true) continue
        const held = relativeResources.find(r => (c.scope ?? []).includes(r))
        if (!held) continue
        event.effect = "deny"
        event.message = `Takt refused this edit: ${held} is held by work unit ${c.work_unit_id} (agent ${c.agent_id}); wait for its consolidation or discard it`
        refused("claimed_path", event.agent ?? "", session, new Error(event.message), c.work_unit_id)
        return
      }
    })

    // The orchestrator learns about maintenance only while it matters: a small
    // notice while a cycle is due or running, and one when it has concluded.
    // Outside maintenance nothing is added to its context.
    let maintenanceAnnounced = false
    await ctx.session.hook("context", async (event) => {
      if (event.agent !== ORCHESTRATOR_ID) return
      // A failed status read must never break the orchestrator's own request.
      const state = await coordinate({ action: "status" }).catch(() => undefined)
      if (state === undefined) return
      const active = isCoordinatorResponse(state) && (state.draining === true || state.cycle != null)
      if (active) {
        maintenanceAnnounced = true
        event.system.push({ type: "text", text: MAINTENANCE_DUE })
      } else if (maintenanceAnnounced) {
        maintenanceAnnounced = false
        event.system.push({ type: "text", text: MAINTENANCE_DONE })
      }
    })

    // OpenCode reports provider usage on message updates. Keep only numeric
    // accounting fields; message text, prompts and tool output never cross into
    // Takt telemetry. A message update is emitted once usage is available, so
    // this avoids counting the same assistant turn on every token delta.
    const observedUsageMessages = new Set<string>()
    function observeModelUsage(event: unknown) {
      if (!isResponseObject(event) || event.type !== "message.updated") return
      const data = isResponseObject(event.data) ? event.data : isResponseObject(event.properties) ? event.properties : {}
      const info = isResponseObject(data.info) ? data.info : isResponseObject(data.message) ? data.message : data
      const tokens = info.tokens
      if (!isResponseObject(tokens)) return
      const numeric = (value: unknown) => typeof value === "number" && Number.isFinite(value) ? value : 0
      const sessionID = String(info.sessionID ?? data.sessionID ?? "")
      if (!sessionID || info.role !== "assistant") return
      const messageID = String(info.id ?? data.messageID ?? "")
      if (messageID && observedUsageMessages.has(messageID)) return
      const binding = [...bindings.values()].find((candidate) => candidate.session === sessionID)
      const cache = isResponseObject(tokens.cache) ? tokens.cache : {}
      const attributes = {
        provider: String(info.providerID ?? ""), model: String(info.modelID ?? ""),
        message_id: messageID,
        input_tokens: numeric(tokens.input), output_tokens: numeric(tokens.output),
        reasoning_tokens: numeric(tokens.reasoning), cache_read_tokens: numeric(cache.read),
        cache_write_tokens: numeric(cache.write), cost_usd: numeric(info.cost),
      }
      observe("model_usage", "takt.platform", String(info.agent ?? "harness"), sessionID,
        attributes, binding?.unit ?? "")
      if (messageID) observedUsageMessages.add(messageID)
    }

    const abort = new AbortController()
    void (async () => {
      for await (const event of ctx.event.subscribe({ signal: abort.signal })) {
        observeModelUsage(event)
        if (event.type === "session.idle") {
          gcBusy.delete(event.data.sessionID)
          scheduleGC()
          continue
        }
        // The temporary interlocutor died without handing off or being
        // explicitly aborted; the harness itself reports the abort so the
        // stack is never left pointing at a session that no longer exists.
        if (event.type === "session.deleted" && event.data.sessionID === interlocutorChild) {
          const child = interlocutorChild
          const root = interlocutorRoot
          interlocutorChild = undefined
          interlocutorRoot = undefined
          if (root) {
            await dispatchAction({ action: "abort_switch", session: root, child, evidence: "session ended", origin: "harness" })
              .then(envelope => returnInterface(root, "a lent session that ended without a handoff", envelope))
              .catch(error => console.error("Takt interlocutor abort_switch", error))
          }
        }
      }
    })().catch(() => undefined) // an aborted subscription is an ordinary shutdown

    await ctx.tool.transform((editor) => {
      const gc = (name: string, description: string, action: string, input = obj({}, [])) =>
        editor.add({ name, description, input, execute: (value: unknown, c) => gcTool(action, c, toolInput(value)) })

      gc("gc_baseline", "Verifier: run and record prepared acceptance baseline before mutation.", "baseline")
      gc("gc_findings", "Collector: read persisted findings and cycle declaration.", "findings")
      gc("gc_investigate", "Collector: record refuted, confirmed or proposal with explicit investigation evidence.", "investigate",
        obj({ finding: str(), outcome: str(), evidence: str() }, ["finding", "outcome", "evidence"]))
      gc("gc_authorize", "Collector: request a bounded VFS scope after investigation.", "authorize")
      gc("gc_collected", "Collector: submit the staged delta for independent verification.", "collected")
      gc("gc_delta", "Verifier: read the exact staged candidate delta.", "delta")
      gc("gc_verdict", "Verifier: deliver your independent verdict; an approval consolidates the delta.", "verdict",
        obj({ pass: { type: "boolean" }, evidence: str() }, ["pass", "evidence"]))
      gc("gc_acceptance", "Verifier: run prepared materialized acceptance; regression discards the entire cycle.", "acceptance")
      gc("gc_no_change", "Collector: explicitly close without changes, including proposals/refutations, without inventing acceptance.", "no-change")

      editor.add({
        name: "gc_prepare",
        description: "Ordinary preparation: validate reviewed project GC analyzer dependencies and acceptance commands. Never installs during GC.",
        input: obj({}, []),
        async execute(_args, c) {
          if (c.sessionID !== await rootSession(c.sessionID)) throw new Error("GC preparation belongs to the root ordinary workflow")
          return { content: JSON.stringify(await coordinate({ action: "prepare", session: c.sessionID })) }
        },
      })
      editor.add({
        name: "gc_request",
        description: "Request a GC cycle on the user's behalf.",
        input: obj({}, []),
        async execute(_args, c) {
          if (c.sessionID !== await rootSession(c.sessionID)) throw new Error("GC requests belong to the root interlocutor")
          await initializeGC()
          const result = await coordinate({ action: "request", session: c.sessionID })
          scheduleGC()
          return { content: JSON.stringify(result) }
        },
      })

      // dispatch is (name, description, action, input) for the ordinary crew
      // dispatch tools below: each is the orchestrator's own declaration, never
      // a specialist's, so every one requires the calling session to be the
      // root interlocutor.
      const dispatch = <Args extends ToolInput>(name: string, description: string, action: string, input: ReturnType<typeof obj>, fields: (args: Args) => Record<string, unknown>) =>
        editor.add({
          name, description, input,
          async execute(value: unknown, c) {
            const args = toolInput<Args>(value)
            const session = await rootSession(c.sessionID)
            if (c.sessionID !== session) throw new Error(`${name} belongs to the root orchestrator`)
            const result = await dispatchAction({ action, session, ...fields(args) })
            return { content: JSON.stringify(result) }
          },
        })

      dispatch("dispatch_commit", "Commit a plan baseline: every listed unit becomes planned, with its contract and explicit prerequisites. A planned unit is executed by delegating it with the unit identity as the subagent description; delegating that name again retries the unit. Commit before delegating more work than may run without a plan, or when the active workflow calls for one. Once a plan stands, change it by naming its current version as base_version: listed units are added or replaced, withdrawn units leave the plan.",
        "commit",
        obj({
          version: str("Identifier of this baseline version"),
          base_version: str("The standing plan version this declaration revises; omit only for the first commitment"),
          plan: { type: "array", items: obj({
            unit: str("Work unit identity"), contract: str("Frozen contract this unit is dispatched against"),
            prerequisites: { type: "array", items: { type: "string" }, description: "Prerequisite work unit identities" },
          }, ["unit", "contract"]), description: "Every unit this baseline covers, or that a revision adds or replaces" },
          withdraw: { type: "array", items: { type: "string" }, description: "Still-planned unit identities a revision removes" },
        }, ["version", "plan"]),
        (args: { version: string; base_version?: string; withdraw?: string[]; plan: { unit: string; contract: string; prerequisites?: string[] }[] }) =>
          ({
            version: args.version,
            ...(args.base_version ? { base_version: args.base_version } : {}),
            plan: args.plan.map(u => ({ unit: u.unit, contract: u.contract, prerequisites: u.prerequisites ?? [] })),
            ...(args.withdraw?.length ? { withdrawals: args.withdraw } : {}),
          }))

      editor.add({ name: "claim_list", description: "List ownership claims for the current root session.", input: obj({}, []), async execute(value: unknown, c) {
        toolInput(value)
        orchestratorOnly(c, "VFS claims")
        return { content: JSON.stringify(await takt("claims", { session_id: await rootSession(c.sessionID) })) }
      } })
      // The target instance is named once and serves as both the binding agent
      // and the catalog specialist, exactly as the launched subagent binds.
      editor.add({ name: "claim_assign", description: "Before delegating implementation, reserve the unit's exact file set for the specialist that will stage it. The returned key is that work's author_key. Never infer or expand scope.", input: obj({
        work_unit_id: str("The unit the delegation will be named after"),
        agent: str("Instance id of the specialist you will launch"),
        scope: { type: "array", items: str(), description: "Exact workspace-relative file paths, existing or to be created; no directories or globs" },
      }, ["work_unit_id", "agent", "scope"]), async execute(input: unknown, c) {
        const args = toolInput(input)
        orchestratorOnly(c, "VFS claim assignment")
        return { content: JSON.stringify(await takt("assign", { session_id: await rootSession(c.sessionID), work_unit_id: args.work_unit_id, agent_id: args.agent, specialist: args.agent, invariants: INVARIANT_DOCUMENTS, scope: args.scope })) }
      } })
      editor.add({ name: "claim_assign_verifier", description: "Before delegating a gate, preassign the verifier to the staged work it judges.", input: obj({
        work_unit_id: str("The verifier's own unit, the one its delegation will be named after"),
        agent: str("Instance id of the verifier you will launch"),
        author_key: str("The author_key of the staged work to judge"),
      }, ["work_unit_id", "agent", "author_key"]), async execute(input: unknown, c) {
        const args = toolInput(input)
        orchestratorOnly(c, "VFS verifier assignment")
        return { content: JSON.stringify(await takt("assign-verifier", { session_id: await rootSession(c.sessionID), work_unit_id: args.work_unit_id, agent_id: args.agent, specialist: args.agent, invariants: INVARIANT_DOCUMENTS, author_key: args.author_key })) }
      } })
      editor.add({ name: "claim_release", description: "Use claim_list to get the exact claim_key. Release a prior-session claim directly. For a claim in the current root session, first obtain actual user confirmation through the orchestrator's native question mechanism, then set confirmed true only after yes.", input: obj({ claim_key: str("Exact key returned by claim_list; never infer this key"), confirmed: { type: "boolean", description: "Set true only after actual user confirmation for a claim in this root session" } }, ["claim_key"]), async execute(value: unknown, c) {
        const args = toolInput<{ claim_key: string; confirmed?: boolean }>(value)
        orchestratorOnly(c, "VFS claim release")
        const session = await rootSession(c.sessionID)
        const claims = await takt("claims", { session_id: session })
        const claim = (claims.claims ?? []).find((x: OwnershipClaim) => x.key === args.claim_key)
        if (!claim) throw new Error(`unknown claim_key ${args.claim_key}; call claim_list and use an exact listed key`)
        if (claim.root_session_id === session && args.confirmed !== true) {
          let status = "neither active nor pending"
          if (claim.pending === true) status = "pending"
          else if (claim.active === true) status = "active"
          throw new Error(`WARNING: agent ${claim.agent_id} (instance ${claim.target_instance}, ${status}) owns [${(claim.scope ?? []).join(", ")}]. Ask the user via the orchestrator's native question mechanism whether to release this exact claim; retry with confirmed:true only after an explicit yes. This server plugin cannot present dialogs or verify confirmation. Ownership was not released; staged delta is preserved.`)
        }
        return { content: JSON.stringify(await takt("release", { session_id: session, claim_key: claim.key })) }
      } })

      dispatch("dispatch_activity_start", "Record direct orchestrator work activity (not a delegated unit).", "activity_start", obj({ activity_id: str(), node_kind: { type: "string", enum: ["orchestrator"] } }, ["activity_id", "node_kind"]), (args: { activity_id: string; node_kind: string }) => args)
      dispatch("dispatch_activity_finish", "Finish direct orchestrator work activity with its outcome.", "activity_finish", obj({ activity_id: str(), node_kind: { type: "string", enum: ["orchestrator"] }, outcome: { type: "string", enum: ["completed", "failed", "interrupted"] } }, ["activity_id", "node_kind", "outcome"]), (args: { activity_id: string; node_kind: string; outcome: string }) => args)

      dispatch("dispatch_declare_recovery", "Declare bounded autonomous recovery of an objective before uncertain work begins: a binary expected result, a prior recoverable point, explicit scope, and both budgets. Present evidence and alternatives to the user and await their decision before a second recovery of the same objective.",
        "recovery",
        obj({
          objective: str("Identity of the objective being recovered"),
          result: str("The binary expected result that would demonstrate recovery"),
          point: str("Reference to the prior recoverable point"),
          scope: { type: "array", items: { type: "string" }, description: "Work unit identities this recovery's scope covers" },
          actions: { type: "number", description: "Action budget this recovery may consume" },
          attempts: { type: "number", description: "Attempt budget: how many times this recovery may be retried" },
        }, ["objective", "result", "point", "scope", "actions", "attempts"]),
        (args: { objective: string; result: string; point: string; scope: string[]; actions: number; attempts: number }) => args)

      dispatch("dispatch_close_recovery", "Close a declared recovery: record whether its result was demonstrated and link the evidence. Only a demonstrated recovery breaks the objective's failure streak.",
        "recovered",
        obj({
          objective: str("Identity of the recovered objective"),
          evidence: str("Evidence supporting the claimed result"),
          demonstrated: { type: "boolean", description: "Whether the declared result was actually demonstrated" },
        }, ["objective", "evidence", "demonstrated"]),
        (args: { objective: string; evidence: string; demonstrated: boolean }) => ({ objective: args.objective, evidence: args.evidence, pass: args.demonstrated }))

      dispatch("dispatch_restore", "Confirm restoration of an abandoned recovery scope's virtual state after forced backtracking, by discarding the staged work of that scope.",
        "restore",
        obj({
          objective: str("Identity of the abandoned recovery's objective"),
          author_key: str("The author_key of the staged work inside the abandoned scope"),
        }, ["objective", "author_key"]),
        (args: { objective: string; author_key: string }) => ({ objective: args.objective, key: args.author_key }))

      dispatch("dispatch_exception", "Record a scoped user exception raising one bound by a finite additional allowance, after the user has explicitly granted it. Never grant this yourself from inference; it must reflect a decision the user actually made.",
        "exception",
        obj({
          event: str("Work unit the exception applies to"),
          bound: { type: "string", enum: EXCEPTION_BOUNDS, description: "The bound being raised, named exactly as a denial reports it: ceiling/concurrent-specialists (concurrent delegations), budget/unplanned-units (units delegated without a committed plan), budget/contests, budget/recovery-failures, budget/recovery-actions, budget/recovery-attempts" },
          objective: str("Recovery objective the exception applies to, if the bound is recovery-scoped"),
          allowance: { type: "number", description: "Finite additional allowance in that bound's own unit" },
        }, ["event", "bound", "allowance"]),
        (args: { event: string; bound: string; objective?: string; allowance: number }) => args)

      dispatch("dispatch_contest", "Contest a specifically identified terminal failure by requesting independent verification against the invariants that already applied to it. You request it; you do not choose or re-dispatch the verifier, and a rejected contest leaves the failure standing.",
        "contest",
        obj({
          event: str("The work unit whose recorded terminal failure is contested"),
          attempt: str("The attempt identity the failure belongs to; defaults to the unit's current attempt"),
        }, ["event"]),
        (args: { event: string; attempt?: string }) => ({ event: args.event, attempt: args.attempt ?? "" }))

      // dispatch_switch and dispatch_abort_switch cannot ride the dispatch()
      // factory above unmodified: both need to create or stop the temporary
      // interlocutor's own session around the call to Go, and fields() only
      // ever contributes a plain object into that one call. Each repeats
      // dispatch()'s own root-only guard instead (IR-19/IR-24: a switch or
      // its abort is only ever originated by the base or the user, never by
      // a temporary holder).
      //
      // OpenCode's plugin context has no programmatic way to raise the native
      // `question` confirmation (no ctx.question domain, and unlike
      // ctx.permission, there is no question.create on the local service's
      // own client either — verified against the installed @opencode/plugin
      // 2.0.16 and @opencode/client packages). `question` is a tool only the
      // model itself can invoke, so these descriptions instruct the model to
      // call it first and only proceed here after an accepted confirmation.
      editor.add({
        name: "dispatch_switch",
        description: "Switch the active interlocutor role to another specialist for the rest of this conversation. Before calling this tool, invoke the native `question` tool to confirm the target specialist and objective with the user. Call this tool only after an accepted confirmation; on rejection or a freeform answer, do not call this tool — reevaluate instead.",
        input: obj({
          target_agent: str("Catalog id of the specialist to switch the interlocutor role to"),
          objective: str("Objective for the incoming interlocutor's session"),
          requirements: str("Requirements context for the incoming interlocutor"),
          client: str("Client-facing context for the incoming interlocutor"),
          expected_artifact: str("Optional filesystem copy; the result's Engram ID is required at handoff"),
        }, ["target_agent", "objective"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ target_agent: string; objective: string; requirements?: string; client?: string; expected_artifact?: string }>(value)
          const session = await rootSession(c.sessionID)
          if (c.sessionID !== session) throw new Error("dispatch_switch belongs to the root orchestrator")
          const text = [
            "You are taking over as the active interlocutor for this workspace.",
            `Objective: ${args.objective}`,
            args.requirements ? `Requirements: ${args.requirements}` : undefined,
            args.client ? `Client: ${args.client}` : undefined,
          ].filter(Boolean).join("\n")
          const child = await createChildSession({
            title: `Takt switch: ${args.target_agent}`,
            agent: args.target_agent,
            metadata: { takt_switch: session },
          })
          let result: DispatchResponse
          try {
            result = await dispatchAction({ action: "switch", session, child, agent: args.target_agent, artifact: args.expected_artifact })
          } catch (error) {
            // The switch never registered, so the child is stopped rather than
            // left dangling unprompted.
            await stopChild(child)
            throw error
          }
          await promptChild(child, text)
          interlocutorChild = child
          interlocutorRoot = session
          return { content: JSON.stringify(result) }
        },
      })
      editor.add({
        name: "dispatch_abort_switch",
        description: "Abort the current interlocutor switch and return the interlocutor role to the base. No confirmation required.",
        input: obj({ reason: str("Evidence for aborting the switch") }, ["reason"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ reason: string }>(value)
          const session = await rootSession(c.sessionID)
          if (c.sessionID !== session) throw new Error("dispatch_abort_switch belongs to the root orchestrator")
          if (!interlocutorChild) throw new Error("no active interlocutor switch to abort")
          const child = interlocutorChild
          const result = await dispatchAction({ action: "abort_switch", session, child, evidence: args.reason, origin: "user" })
          await stopChild(child)
          interlocutorChild = undefined
          interlocutorRoot = undefined
          return { content: JSON.stringify(result) }
        },
      })
      editor.add({
        name: "dispatch_handoff",
        description: "Hand the interlocutor role back to the base after a switch. Only the temporary holder that a switch created may call this. Before calling this tool, invoke the native `question` tool to confirm with the user what is being handed back (result, additional context, extra artifacts). Call this tool only after an accepted confirmation.",
        input: obj({
          result: { type: "string", enum: ["Standard", "EarlyHandoff", "TechFault", "Outraged"], description: "Outcome of this temporary interlocutor's turn" },
          additional_context: str("Context to carry back to the base"),
          extra_artifacts: { type: "array", items: { type: "string" }, description: "Optional filesystem copies" },
          result_ids: { type: "array", items: { type: "integer" }, description: "One existing Engram entry ID per delivered result; required for Standard handoff" },
        }, ["result", "additional_context", "extra_artifacts", "result_ids"]),
        // Not built on dispatch(): the caller here is the temporary holder,
        // never the root, so dispatch()'s root-only guard does not apply.
        async execute(value: unknown, c) {
          const args = toolInput<{ result: string; additional_context: string; extra_artifacts: string[]; result_ids: number[] }>(value)
          const session = await rootSession(c.sessionID)
          // A refused handoff throws here, and its reason is all the holder
          // receives: it corrects and retries.
          const envelope = await dispatchAction({
            action: "handoff", session, child: c.sessionID, agent: c.agent,
            result: args.result, additional_context: args.additional_context, extra_artifacts: args.extra_artifacts, result_ids: args.result_ids,
          })
          interlocutorChild = undefined
          interlocutorRoot = undefined
          await returnInterface(session, c.agent, envelope)
          // The holder has no further active work; its turn ends once this call returns.
          const holder = c.sessionID
          setTimeout(() => void stopChild(holder).catch(() => undefined), 0)
          return { content: "" }
        },
      })

      editor.add({
        name: "vfs_bind",
        description: "Bind to the assignment named in your context, before any other VFS tool.",
        input: obj({
          scope: { type: "array", items: { type: "string" }, description: "The exact paths your assignment names" },
          author_key: str("The author_key your assignment names, when it names one"),
        }, ["scope"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ scope: string[]; author_key?: string }>(input)
          const session = await rootSession(c.sessionID)
          const authorKey = typeof args.author_key === "string" ? args.author_key : undefined
          const author = authorKey ? bindings.get(authorKey) : undefined
          if (authorKey && author?.session !== session) throw new Error("author_key does not name an active binding of this session")
          // A verifier runs as its own unit; the author key alone links it to
          // the staged work it judges.
          const unit = await delegatedUnit(c.sessionID)
          const b: Binding = { key: "", agent: c.agent, session, dispatch: c.sessionID, unit, revision: 0, deltaHash: "", ...(author && authorKey ? { judges: authorKey } : {}) }
          const res = await takt("bind", { ...identity(b), invariants: INVARIANT_DOCUMENTS, scope: args.scope, ...(author && authorKey ? { author_key: authorKey } : {}) })
          b.key = res.key ?? (() => { throw new Error("vfs bind response omitted key") })()
          bindings.set(b.key, b)
          await persist()
          return { content: `Bound as ${b.agent} to scope ${JSON.stringify(args.scope)}; unit ${unit}, attempt ${res.attempt_id}; invariants ${INVARIANT_DOCUMENTS.join(", ")} at version ${res.invariants_version}; author_key ${b.key}` }
        },
      })
      editor.add({
        name: "vfs_write",
        description: "Create or update a file inside your declared scope (create if absent, patch if present). Staged virtually; not written to disk until verified and consolidated.",
        input: obj({
          path: str(),
          content: str(),
          call_id: str("Unique id for this operation"),
          author_key: str("Binding key from vfs_bind; defaults to your own latest binding"),
        }, ["path", "content", "call_id"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; content: string; call_id: string; author_key?: string }>(input)
          const b = await binding(c, typeof args.author_key === "string" ? args.author_key : undefined)
          const res = await takt("op", {
            ...identity(b), author_key: b.key, call_id: args.call_id,
            expected_revision: b.revision, action: "create", path: args.path, content: args.content,
          })
          await record(b, res)
          return { content: `Staged ${args.path} at revision ${b.revision} (delta_hash ${b.deltaHash})` }
        },
      })
      editor.add({
        name: "vfs_read",
        description: "Read a file of your assignment as staged: your own staged view, or the staged work you judge.",
        input: obj({ path: str("Workspace-relative path within your assignment"), call_id: str("Unique id for this operation") }, ["path", "call_id"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; call_id: string }>(input)
          const b = own(c)
          const judged = b.judges ? bindings.get(b.judges) : undefined
          if (judged) {
            // A gate reads exactly the author's staged state it will judge.
            const res = await takt("op", {
              ...identity(b), author_key: b.key, view_key: judged.key, call_id: args.call_id,
              expected_revision: judged.revision, action: "read", path: args.path,
            })
            return { content: res.content ?? "" }
          }
          const res = await takt("op", {
            ...identity(b), author_key: b.key, call_id: args.call_id,
            expected_revision: b.revision, action: "read", path: args.path,
          })
          await record(b, res)
          return { content: res.content ?? "" }
        },
      })
      editor.add({
        name: "vfs_delete",
        description: "Stage the deletion of a file inside your declared scope. Staged virtually; the workspace copy is untouched until verified and consolidated.",
        input: obj({
          path: str(),
          call_id: str("Unique id for this operation"),
          author_key: str("Binding key from vfs_bind; defaults to your own latest binding"),
        }, ["path", "call_id"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; call_id: string; author_key?: string }>(input)
          const b = await binding(c, typeof args.author_key === "string" ? args.author_key : undefined)
          const res = await takt("op", {
            ...identity(b), author_key: b.key, call_id: args.call_id,
            expected_revision: b.revision, action: "delete", path: args.path,
          })
          await record(b, res)
          return { content: `Staged deletion of ${args.path} at revision ${b.revision} (delta_hash ${b.deltaHash})` }
        },
      })
      editor.add({
        name: "vfs_discard",
        description: "Discard an author's entire staged work without touching the workspace, releasing the paths it owns. Name the author by its author_key.",
        input: obj({
          author_key: str("The author_key of the staged work to discard"),
          call_id: str("Unique id for this operation"),
        }, ["author_key", "call_id"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ author_key: string; call_id: string }>(input)
          // Discarding is the orchestrator's backtracking decision (PR-VFS-CSL-5).
          orchestratorOnly(c, "vfs_discard")
          if (typeof args.author_key !== "string") throw new Error("author_key must be a string")
          const b = bindings.get(args.author_key)
          if (b?.session !== await rootSession(c.sessionID)) throw new Error("author_key does not name staged work of this session")
          await takt("op", {
            ...identity(b), author_key: b.key, call_id: args.call_id,
            expected_revision: b.revision, action: "rollback",
          })
          await release(b)
          return { content: "Discarded staged delta" }
        },
      })
      editor.add({
        name: "vfs_verify",
        description: "As an independent verifier: attach a pass/fail verdict to the author's staged delta, bound first with that author_key. The verdict covers exactly the revision staged when you judged it.",
        input: obj({
          author_key: str("The author_key the author reported"),
          pass: { type: "boolean" },
          finding: str("What the verdict rests on; required when pass is false"),
        }, ["author_key", "pass", "finding"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ author_key: string; pass: boolean; finding: string }>(input)
          if (typeof args.author_key !== "string") throw new Error("author_key must be a string")
          const author = bindings.get(args.author_key)
          if (!author) throw new Error("author_key does not name an active binding")
          // The verifier is the caller's own gate over exactly this author.
          const verifier = [...bindings.values()].find(b => b.dispatch === c.sessionID && b.agent === c.agent && b.judges === args.author_key)
          if (!verifier) throw new Error("bind to this author_key before attaching a verdict")
          // The judged revision and its hash are the author's current staged
          // state; the store rejects the verdict if that state moved.
          await takt("verify", {
            ...identity(verifier), verifier_key: verifier.key, author_key: args.author_key, call_id: randomUUID(),
            expected_revision: author.revision, delta_hash: author.deltaHash,
            pass: args.pass, finding: args.finding,
          })
          return { content: args.pass ? "Verdict: passed" : `Verdict: rejected — ${args.finding}` }
        },
      })
      editor.add({
        name: "vfs_consolidate",
        description: "Move an author's verified staged work into the workspace once its verdict passed. Name the author by the author_key it reported; a missing or failed verdict leaves the work staged.",
        input: obj({
          author_key: str("The author_key of the staged work to consolidate"),
          checkpoint: str("Short consolidation label hashed into the journal"),
        }, ["author_key", "checkpoint"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ author_key: string; checkpoint: string }>(input)
          if (typeof args.author_key !== "string") throw new Error("author_key must be a string")
          // Consolidation is the orchestrator's act over work its own delegations
          // staged (PR-DAG-REP-5); the author's binding speaks for the delta.
          if (c.agent !== ORCHESTRATOR_ID) throw new Error("vfs_consolidate belongs to the orchestrator")
          const b = bindings.get(args.author_key)
          if (b?.session !== await rootSession(c.sessionID)) throw new Error("author_key does not name staged work of this session")
          await takt("consolidate", {
            ...identity(b), author_key: b.key, checkpoint: args.checkpoint, expected_revision: b.revision,
          })
          await release(b)
          return { content: "Consolidated to workspace" }
        },
      })
      editor.add({
        name: "deliver_result",
        description: "Deliver this delegation's completed result: the Engram entry IDs already recorded for it. Call this before your turn ends.",
        input: obj({
          result_ids: { type: "array", items: { type: "integer" }, minItems: 1, description: "Existing Engram entry IDs this delegation delivers" },
        }, ["result_ids"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ result_ids: number[] }>(input)
          const root = await rootSession(c.sessionID)
          const unit = await delegatedUnit(c.sessionID)
          await dispatchAction({ action: "validate_results", session: root, agent: c.agent, result_ids: args.result_ids })
          deliveries.set(unitKey(root, unit), true)
          const count = Array.isArray(args.result_ids) ? args.result_ids.length : 0
          return { content: `Delivered ${count} result id(s)` }
        },
      })
    })

    // Delegated VFS context is granted per exact agent instance and only while
    // its exact root/unit/identity claim remains pending or active. Permission
    // rules are read from that instance; no role defaults are inferred here.
    await ctx.session.hook("context", async (event) => {
      if (!event.tools || !event.sessionID || !event.agent) return
      const removeVFS = () => { for (const name of VFS_TOOL_NAMES) delete event.tools[name] }
      try {
        const info = await ctx.session.get({ sessionID: event.sessionID })
        if (!info.parentID) return
        const agent = await ctx.agent.get({ agentID: event.agent })
        const rules = agent.data.permissions
        const explicitlyAllows = (toolName: string) => {
          if (!Array.isArray(rules)) return false
          let decision: string | undefined
          for (const rule of rules) {
            if (rule && (rule.resource === toolName || rule.resource === "*") && ["*", toolName].includes(rule.action)) decision = rule.effect
          }
          return decision === "allow"
        }
        const allowed = VFS_TOOL_NAMES.filter(name => explicitlyAllows(name) && event.tools[name])
        const root = await rootSession(event.sessionID)
        const unit = await delegatedUnit(event.sessionID)
        if (RESULT_AGENTS.includes(event.agent)) childOf.set(unitKey(root, unit), event.sessionID)
        const claim = await findClaim(root, unit, event.agent, ["pending", "active"])
        if (!claim || allowed.length === 0) { removeVFS(); return }
        for (const name of VFS_TOOL_NAMES) if (!allowed.includes(name)) delete event.tools[name]
        event.system.push({ type: "text", text: claim.author_key
          ? `You judge the staged work of author_key ${claim.author_key} for work unit ${unit}. Bind with vfs_bind using an empty scope and that author_key, read the staged files with vfs_read, and attach your pass or fail verdict with vfs_verify, naming the finding it rests on.`
          : `Work unit ${unit} owns exactly these workspace-relative paths: ${(claim.scope ?? []).join(", ")}. Bind with vfs_bind using exactly this scope, stage changes with vfs_write and vfs_delete, read your staged view with vfs_read, and return the author_key in your handoff.` })
      } catch { removeVFS() }
    })

    return () => abort.abort()
  },
})
