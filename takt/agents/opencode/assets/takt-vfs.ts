// Takt VFS — managed by takt-ai; edits are overwritten on sync.
// The harness, not the model, supplies author, session and directory. Every
// write goes through the durable VFS core: staging first, consolidation only
// after an independent verifier's verdict. One private store per workspace.
import { randomUUID } from "node:crypto"
import { mkdtemp, realpath, rm, writeFile } from "node:fs/promises"
import { homedir, tmpdir } from "node:os"
import type { OpenCodeEvent } from "@opencode/client"
import { join, resolve } from "node:path"
import { Plugin } from "@opencode/plugin"

const TAKT_AI = "__TAKT_AI_BINARY__"
const ORCHESTRATOR_ID = "__TAKT_ORCHESTRATOR_ID__"
const IPC_VERSION = 4

async function sameDirectory(left: string, right: string): Promise<boolean> {
  const canonical = (path: string) => realpath(path).catch(() => resolve(path))
  const [leftPath, rightPath] = await Promise.all([canonical(left), canonical(right)])
  return leftPath === rightPath
}

type ToolInput = Record<string, unknown>
export function isToolInput(value: unknown): value is ToolInput {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}
export function toolInput<T extends ToolInput = ToolInput>(value: unknown): T {
  if (!isToolInput(value)) throw new Error("tool input must be an object")
  return value as T
}

type VFSShellResponse = { decision: "allow" | "ask" | "deny"; reason: string; writable?: string[]; scratch?: string; protected?: string[]; private?: string[]; capture?: boolean; cwd?: string; confirm?: string }
type VFSResponse = {
  ok: boolean; key?: string; content?: string; revision?: number; delta_hash?: string
  error?: string; attempt_id?: string; invariants_version?: string
  shell?: VFSShellResponse; claims?: OwnershipClaim[]
}
type GCPlan = Record<string, unknown> & { session_id: string; cycle_id: string; mandate_class: string; delta: unknown[]; closure: string[]; reachability: string }
type GCCycle = Record<string, unknown> & { phase: string; plan: GCPlan; scope: string[]; sessions: Record<string, string>; report: unknown; started: string; author_key?: string }
type StagedView = { revision: number; delta_hash: string; files: Record<string, string | null> }
type CoordinatorResponse = Record<string, unknown> & {
  version: number; units: number; mutations: number; cursor: number; deferrals: number
  next_mandate: number; requested: boolean; cycle?: GCCycle
  history?: GCCycle[]
}
type HandoffEnvelope = { result: string; additional_context: string; extra_artifacts: string[]; memory: number[] }
type DispatchResponse = GCPlan | GCCycle | StagedView | CoordinatorResponse | HandoffEnvelope | null
export function isCoordinatorResponse(value: DispatchResponse): value is CoordinatorResponse {
  return value !== null && "version" in value && "units" in value
}
export function isGCCycleResponse(value: DispatchResponse): value is GCCycle {
  return value !== null && isResponseObject(value) && isGCCycle(value)
}
export function isHandoffEnvelope(value: Record<string, unknown>): value is HandoffEnvelope {
  return typeof value.result === "string" && typeof value.additional_context === "string" &&
    Array.isArray(value.extra_artifacts) && value.extra_artifacts.every(item => typeof item === "string") &&
    Array.isArray(value.memory) && value.memory.every(item => typeof item === "number")
}
type OwnershipClaim = { key?: string; author_key?: string; root_session_id: string; work_unit_id: string; agent_id: string; target_instance: string; pending?: boolean; active?: boolean; staged?: boolean; scope?: string[] }
export function isResponseObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}
export function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every(item => typeof item === "string")
}
// isOptional accepts an absent field or one that passes check.
export function isOptional<T>(value: unknown, check: (value: unknown) => value is T): value is T | undefined {
  return value === undefined || check(value)
}
export const isString = (value: unknown): value is string => typeof value === "string"
export const isBoolean = (value: unknown): value is boolean => typeof value === "boolean"
export function parseVFSShell(value: unknown): VFSShellResponse | undefined {
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
export function responseObject(value: unknown, command: string): Record<string, unknown> {
  if (!isResponseObject(value)) throw new Error(`takt-ai ${command} returned a non-object response`)
  return value
}
// parseClaim keeps a well-formed ownership claim and drops anything else.
export function parseClaim(item: unknown): OwnershipClaim[] {
  if (!isResponseObject(item)) return []
  const claim = item
  if (typeof claim.key !== "string" || typeof claim.root_session_id !== "string" || typeof claim.work_unit_id !== "string" || typeof claim.agent_id !== "string" || typeof claim.target_instance !== "string") return []
  return [{ key: claim.key, root_session_id: claim.root_session_id, work_unit_id: claim.work_unit_id, agent_id: claim.agent_id, target_instance: claim.target_instance, ...(claim.pending === true ? { pending: true } : {}), ...(claim.active === true ? { active: true } : {}), ...(claim.staged === true ? { staged: true } : {}), ...(Array.isArray(claim.scope) ? { scope: claim.scope.filter((path): path is string => typeof path === "string") } : {}), ...(typeof claim.author_key === "string" ? { author_key: claim.author_key } : {}) }]
}
// vfsResponse keeps the fields of a `takt-ai vfs` answer the plugin knows, each only when well typed.
export function vfsResponse(parsed: Record<string, unknown>, ok: boolean): VFSResponse {
  const shell = parseVFSShell(parsed.shell)
  return {
    ok,
    ...(typeof parsed.key === "string" ? { key: parsed.key } : {}),
    ...(typeof parsed.content === "string" ? { content: parsed.content } : {}),
    ...(typeof parsed.revision === "number" ? { revision: parsed.revision } : {}),
    ...(typeof parsed.delta_hash === "string" ? { delta_hash: parsed.delta_hash } : {}),
    ...(typeof parsed.error === "string" ? { error: parsed.error } : {}),
    ...(typeof parsed.attempt_id === "string" ? { attempt_id: parsed.attempt_id } : {}),
    ...(typeof parsed.invariants_version === "string" ? { invariants_version: parsed.invariants_version } : {}),
    ...(Array.isArray(parsed.claims) ? { claims: parsed.claims.flatMap(parseClaim) } : {}),
    ...(shell ? { shell } : {}),
  }
}
export function isGCPlan(value: Record<string, unknown>): value is GCPlan {
  return typeof value.session_id === "string" && typeof value.cycle_id === "string" && typeof value.mandate_class === "string" &&
    Array.isArray(value.delta) && Array.isArray(value.closure) && value.closure.every((path): path is string => typeof path === "string") && typeof value.reachability === "string"
}
export function isGCCycle(value: Record<string, unknown>): value is GCCycle {
  return typeof value.phase === "string" && isResponseObject(value.plan) && isGCPlan(value.plan) &&
    Array.isArray(value.scope) && value.scope.every((path): path is string => typeof path === "string") &&
    isResponseObject(value.sessions) && Object.values(value.sessions).every((session): session is string => typeof session === "string") &&
    Object.hasOwn(value, "report") && typeof value.started === "string" &&
    (value.author_key === undefined || typeof value.author_key === "string")
}
export function isStagedView(value: Record<string, unknown>): value is StagedView {
  return typeof value.revision === "number" && typeof value.delta_hash === "string" && isResponseObject(value.files) &&
    Object.values(value.files).every(file => file === null || typeof file === "string")
}
export function isCoordinator(value: Record<string, unknown>): value is CoordinatorResponse {
  return typeof value.version === "number" && typeof value.units === "number" && typeof value.mutations === "number" &&
    typeof value.cursor === "number" && typeof value.deferrals === "number" && typeof value.next_mandate === "number" &&
    typeof value.requested === "boolean" &&
    (value.cycle === undefined || isResponseObject(value.cycle) && isGCCycle(value.cycle)) &&
    (value.history === undefined || Array.isArray(value.history) && value.history.every(item => isResponseObject(item) && isGCCycle(item)))
}
// gcCoordinateResponse returns result when it is the shape `gc coordinate` answers action with.
export function gcCoordinateResponse(result: Record<string, unknown>, action: unknown): DispatchResponse | undefined {
  switch (action) {
    case "prepare": return isGCPlan(result) ? result : undefined
    case "findings": case "investigate": case "authorize": return isGCCycle(result) ? result : undefined
    case "collected": case "delta": return isStagedView(result) ? result : undefined
    default: return isCoordinator(result) ? result : undefined
  }
}
export function parseCoordinationResponse(value: unknown, verb: string[], action: unknown): DispatchResponse {
  if (value === null) return null
  const result = responseObject(value, verb.join(" "))
  if (isHandoffEnvelope(result)) return result
  const coordinated = verb[0] === "gc" && verb[1] === "coordinate" ? gcCoordinateResponse(result, action) : undefined
  if (coordinated !== undefined) return coordinated
  if (verb[0] === "dispatch") {
    const acknowledgement = action === "switch" || action === "validate_results" || action === "validate_inputs"
    if (acknowledgement && (Object.keys(result).length === 0 || result.ok === true)) return null
    if (!acknowledgement && isCoordinator(result)) return result
  }
  throw new Error(`${verb.join(" ")} ${asString(action, "unknown")} returned an unexpected response shape`)
}

// orchestratorOnly refuses a claim operation from any agent but the orchestrator.
export const orchestratorOnly = (c: { agent?: string }, operation: string) => {
  if (c.agent !== ORCHESTRATOR_ID) throw new Error(`${operation} belongs to the orchestrator`)
}

// Notices the orchestrator receives around a maintenance cycle: conduct only.
const MAINTENANCE_DUE =
  "A cleanup cycle is running while you are idle. It ends the moment you delegate, so delegate whenever the work calls for it; changes it already consolidated were not requested by the user and are not a regression."
const MAINTENANCE_DONE = "The cleanup cycle concluded. Delegation and normal work continue."

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
// INPUTS_KEY holds the invariants each root/unit was declared to consume.
const INPUTS_KEY = "takt/vfs/inputs"
const VFS_TOOL_NAMES = ["vfs_bind", "vfs_write", "vfs_read", "vfs_delete", "vfs_discard", "vfs_verify", "vfs_consolidate"]

// str and obj keep the JSON Schema inputs readable; V2 takes plain JSON Schema
// rather than the V1 zod-shaped tool.schema helper.
// withText appends one text block to a tool result's content, in either of its shapes.
function withText<C>(content: string | ReadonlyArray<C> | undefined, text: string): string | ReadonlyArray<C | { readonly type: "text"; readonly text: string }> {
  if (content === undefined) return text
  if (typeof content === "string") return `${content}\n\n${text}`
  return [...content, { type: "text" as const, text }]
}
// engramRefs renders Engram entry IDs the way agents cite them.
const engramRefs = (ids: number[]) => ids.map(id => `Engram #${id}`).join(", ")
// engramInvariants names the invariants a unit consumes the way the harness
// pins them: each Engram entry is immutable, so its ID is its own pin.
const engramInvariants = (ids: number[]) => ids.map(id => `engram:${id}`)
const str = (description?: string) => (description ? { type: "string", description } : { type: "string" })
const obj = (properties: Record<string, unknown>, required: string[]) =>
  ({ type: "object", properties, required, additionalProperties: false })

// asString narrows an unknown protocol field to a string, falling back
// instead of letting a non-string value stringify to the useless
// "[object Object]" (dispatch/shell fields are caller-supplied and only
// typed unknown because they pass through a Record<string, unknown>).
export const asString = (v: unknown, fallback: string): string => (typeof v === "string" ? v : fallback)

// once runs work until it succeeds a single time. Concurrent callers share the
// run in flight, and a failed run is attempted again by the next caller.
function once(work: () => Promise<void>): () => Promise<void> {
  let done = false
  let running: Promise<void> | undefined
  return async () => {
    if (done) return
    running ??= work().then(() => { done = true }).finally(() => { running = undefined })
    return running
  }
}

// SENSITIVE_READ_GLOBS are the secret-bearing paths no agent reads, injected
// from the same list the global read rules deny.
const SENSITIVE_READ_GLOBS: string[] = "__TAKT_SENSITIVE_READ_GLOBS__" as unknown as string[]

// GC_LANE_TOOLS are the tools each GC lane keeps besides its gc_* tools: both
// read the workspace natively to investigate or judge, and the verifier keeps
// everything an ordinary verification uses (shell, web, Engram reads); only the
// collector stages its authorized change.
const GC_LANE_TOOLS: Record<string, string[]> = {
  collector: ["read", "glob", "grep", "vfs_read", "vfs_write", "vfs_delete", "memory_record", "memory_continue_session", "memory_close_session"],
  verifier: ["read", "glob", "grep", "shell", "bash", "webfetch", "websearch", "engram_mem_current_project", "engram_mem_search", "engram_mem_get_observation", "engram_mem_context", "memory_record", "memory_continue_session", "memory_close_session"],
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
    const home = homedir()
    const codegraphReady = !(await sameDirectory(workspace, home))
    if (!codegraphReady) {
      console.warn(`Takt: CodeGraph was not initialized because OpenCode is open in the home directory (${home}). Open a specific project to enable CodeGraph tools.`)
    } else {
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
    }
    // Everything a binding needs to speak for its dispatch. `dispatch` is the
    // session that ran the bind (the child session the subagent tool created for
    // this dispatch, or the root session when there is none); `unit` is the
    // dispatch that authored the delta, which a verifier joins through the
    // author's key. Keyed by core key.
    // `judges` is the author key a verifier's binding judges; it reads that
    // author's staged view.
    type Binding = { key: string; agent: string; session: string; dispatch: string; unit: string; revision: number; deltaHash: string; judges?: string; judgedRevision?: number; judgedHash?: string; judgedPaths?: string[] }
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
    // A host subagent call lives inside the process that started it, so a call
    // recorded by an earlier process died with it: its liveness is uncertain, and
    // it is reconciled as not running, which releases its slot (PR-HAR-18). Only
    // those inherited records are reconciled; a call this process admitted is
    // alive. This is bookkeeping about the past and never stands in the way of a
    // new delegation: a record that fails to reconcile is kept for the next
    // attempt, and the unit's staged work and claims stay as they are. A unit
    // that is no longer in flight has nothing to reconcile and succeeds at once.
    const inherited = new Map(delegations)
    let reconciling: Promise<void> | undefined
    const reconcileStoredDelegations = () => reconciling ??= (async () => {
      for (const [delegation, d] of inherited) {
        try {
          await dispatchAction({ action: "uncertain", event: d.unit, session: d.root })
          await dispatchAction({ action: "reconcile", event: d.unit, session: d.root, pass: false })
          inherited.delete(delegation)
          delegations.delete(delegation)
          await persistDelegations()
        } catch (error) {
          console.error("Takt could not reconcile a stored delegation yet", delegation, error)
        }
      }
    })().finally(() => { reconciling = undefined })
    // deliveries and childOf track a producer's result delivery for the life
    // of one delegation only; neither is durable, unlike bindings/delegations
    // above, because a producer whose process died must redeliver anyway.
    const deliveries = new Map<string, number[]>()
    // inputs holds, per root/unit, the Engram IDs of the invariants the
    // orchestrator declared that unit consumes; an empty list is the declared
    // absence. It outlives retries of the unit, and a restart, and is replaced
    // by a redeclaration.
    const inputs = new Map<string, number[]>(
      Object.entries(((await ctx.storage.get(INPUTS_KEY)) as Record<string, number[]> | undefined) ?? {}))
    const persistInputs = () => ctx.storage.set(INPUTS_KEY, Object.fromEntries(inputs))
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

    function stateDir(): string {
      return resolve(workspace, ".takt-ai", "vfs")
    }

    // delegatedUnit is the work unit a dispatch session executes. The host
    // titles a delegated child session with the delegation's description, which
    // is the unit identity; maintenance and root sessions stand for themselves.
    async function delegatedUnit(sessionID: string): Promise<string> {
      if (gcChildren.has(sessionID)) return sessionID
      const info = await ctx.session.get({ sessionID })
      if (!info.parentID) return sessionID
      const unit = (info.title ?? "").trim()
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

    // A request that reads state a queued call can change (a binding's revision)
    // is passed as a function, so it is built when its turn in the queue comes.
    type Request = Record<string, unknown>
    const takt = (command: string, request: Request | (() => Request)) =>
      exclusive(() => taktUnlocked(command, typeof request === "function" ? request() : request))
    // stage runs one operation on b's own staged work. The revision it expects is
    // read, and the answer recorded, inside the queue: calls an agent issues
    // together each start from the revision the previous one left.
    const stage = (b: Binding, command: string, fields: Request) => exclusive(async () => {
      const res = await taktUnlocked(command, {
        ...identity(b), author_key: b.key, call_id: randomUUID(), expected_revision: b.revision, ...fields,
      })
      await record(b, res)
      return res
    })
    // findClaim returns the claim held for exactly this root session, work unit and
    // agent instance while it is in one of the given states; a verifier unit holds
    // one gate per judged author, so authorKey narrows the match to that gate.
    const findClaims = async (root: string, unit: string, agent: string, states: ("pending" | "active")[], authorKey?: string): Promise<OwnershipClaim[]> =>
      ((await takt("claims", { session_id: root })).claims ?? []).filter((c: OwnershipClaim) =>
        c.root_session_id === root && c.work_unit_id === unit && c.agent_id === agent && c.target_instance === agent && states.some(state => c[state] === true) && (!authorKey || c.author_key === authorKey))
    const findClaim = async (root: string, unit: string, agent: string, states: ("pending" | "active")[], authorKey?: string) =>
      (await findClaims(root, unit, agent, states, authorKey))[0]

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

    // verifierGrant reports whether the specialist's last matching rule for
    // vfs_verify (or the wildcard) allows it: a later rule overrides an earlier one.
    function verifierGrant(rules: unknown): boolean {
      if (!Array.isArray(rules)) return false
      const matches = (value: unknown) => value === "*" || value === "vfs_verify"
      return rules.reduce((allowed: boolean, rule) => rule && matches(rule.action) && matches(rule.resource) ? rule.effect === "allow" : allowed, false)
    }

    // validatedAuthorKeys checks the author_keys a delegation declares: unique,
    // nonempty staged author keys, handed to a specialist holding a verification
    // grant. Undefined means the delegation is not a gate.
    async function validatedAuthorKeys(value: unknown, specialist: string, root: string, unit: string): Promise<string[] | undefined> {
      if (value === undefined) return undefined
      if (!isStringArray(value) || value.length === 0 || value.some(key => !key.trim()) || new Set(value).size !== value.length) {
        throw refused("invalid_authors", specialist, root, new Error("author_keys must list unique, nonempty staged author keys"), unit)
      }
      const target = await ctx.agent.get({ agentID: specialist })
      if (!verifierGrant(target.data.permissions)) throw refused("invalid_verifier", specialist, root, new Error("The delegated specialist has no verification grant"), unit)
      return value
    }

    // prepareVerifierGates assigns one gate per author key, recording each key in
    // prepared as it is issued so a failure can release exactly those, then drops
    // the verifier's earlier judging bindings: the new gates replace them.
    async function prepareVerifierGates(authorKeys: string[] | undefined, root: string, unit: string, specialist: string, prepared: string[]): Promise<void> {
      if (authorKeys === undefined) return
      for (const authorKey of authorKeys) {
        // Sequential on purpose: each assignment supersedes the previous
        // attempt's gate for its author and prepared must keep issue order.
        const gate = await takt("assign-verifier", { session_id: root, work_unit_id: unit, agent_id: specialist, // NOSONAR
          specialist, author_key: authorKey })
        if (gate.key) prepared.push(gate.key)
      }
      for (const [key, binding] of bindings) {
        if (binding.session === root && binding.unit === unit && binding.agent === specialist && binding.judges) bindings.delete(key)
      }
      await persist()
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
        // A failed command reports on stderr and leaves stdout empty; only
        // unparseable output from a successful exit is a parse failure.
        if (e instanceof SyntaxError) throw new Error(`takt-ai vfs ${command} exited ${code}: ${err || out}`, code === 0 ? { cause: e } : undefined)
        throw e
      }
    }

    // bindAs binds the caller to its assignment: an author to its scope, a
    // verifier gate to an empty scope linked to the one author it judges.
    async function bindAs(c: { sessionID: string; agent: string }, session: string, unit: string, scope: string[], authorKey?: string) {
      const author = authorKey ? bindings.get(authorKey) : undefined
      if (authorKey && author?.session !== session) throw new Error("author_key does not name an active binding of this session")
      // A verifier runs as its own unit; the author key alone links it to
      // the staged work it judges.
      const b: Binding = { key: "", agent: c.agent, session, dispatch: c.sessionID, unit, revision: 0, deltaHash: "", ...(author && authorKey ? { judges: authorKey, judgedRevision: author.revision, judgedHash: author.deltaHash, judgedPaths: [] } : {}) }
      const res = await takt("bind", { ...identity(b), ...(author ? {} : { invariants: engramInvariants(inputs.get(unitKey(session, unit)) ?? []) }), scope, ...(author && authorKey ? { author_key: authorKey } : {}) })
      b.key = res.key ?? (() => { throw new Error("vfs bind response omitted key") })()
      // Adopting work already staged under the key continues from its revision.
      if (typeof res.revision === "number" && typeof res.delta_hash === "string") { b.revision = res.revision; b.deltaHash = res.delta_hash }
      bindings.set(b.key, b)
      await persist()
      return { b, res }
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

    // gateOwning picks, among a verifier's gates, the one judging the author whose
    // scope holds path; a path no author owns is plain workspace content, which any gate reads.
    async function gateOwning(gates: Binding[], path: string, root: string): Promise<Binding | undefined> {
      if (gates.length < 2) return gates[0]
      const owned = ((await takt("claims", { session_id: root })).claims ?? []) as OwnershipClaim[]
      return gates.find(g => owned.find(x => x.key === g.judges)?.scope?.includes(path)) ?? gates[0]
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
    // and RESULT_AGENTS' native-permission admission by exact command until create.
    const directShell = new Map<string, number>()
    // admitDirect lets the orchestrator or a result agent run a command on the
    // native shell, counting it for create.before.
    const admitDirect = async (agent: string | undefined, sessionID: string, command: string, pending: AdmittedShell | undefined) => {
      if (pending) throw new Error("Takt refused this shell command: an identical specialist command is pending")
      // A result agent's native shell has no sandbox; its Git mutation is
      // refused by the same classifier the shell plan applies (PR-CRW-5).
      if (agent !== ORCHESTRATOR_ID) {
        try {
          await takt("shell-guard", { session_id: await rootSession(sessionID), command })
        } catch (e) {
          throw refused("git_mutation_denied", agent ?? "", sessionID, new Error(`Takt refused this shell command: ${e instanceof Error ? e.message : e}`))
        }
      }
      directShell.set(command, (directShell.get(command) ?? 0) + 1)
    }
    // shellPlanOf reads the plan shell-prepare admitted, refusing a denial or
    // a plan missing a field.
    const shellPlanOf = (shell: VFSShellResponse | undefined, agent: string | undefined, sessionID: string): ShellPlan => {
      if (!shell) throw new Error("takt-ai vfs shell-prepare response omitted shell plan")
      if (shell.decision === "deny") throw refused("shell_denied", agent ?? "", sessionID, new Error(`Takt refused this shell command: ${shell.reason}`))
      if (typeof shell.cwd !== "string" || typeof shell.scratch !== "string" || typeof shell.confirm !== "string" || typeof shell.capture !== "boolean" || !shell.writable || !shell.protected || !shell.private) throw new Error("takt-ai vfs shell-prepare response omitted an allowed shell plan field")
      return { decision: shell.decision, reason: shell.reason, cwd: shell.cwd, scratch: shell.scratch, confirm: shell.confirm, capture: shell.capture, writable: shell.writable, protected: shell.protected, private: shell.private }
    }

    if (VFS_SHELL_ENFORCED) {
      await ctx.tool.hook("execute.before", async (event) => {
        if (!SHELL_ACTIONS.has(event.tool)) return
        const input = isResponseObject(event.input) ? event.input : {}
        const command = asString(input.command, "")
        // Correlation into create.before is by command text alone, so a command
        // already pending for another caller would be ambiguous: it could run
        // under the wrong binding's sandbox, or take the orchestrator's direct path.
        const pending = admittedShell.get(command)
        const direct = directShell.has(command)
        if (event.agent === ORCHESTRATOR_ID || RESULT_AGENTS.includes(event.agent ?? "")) {
          await admitDirect(event.agent, event.sessionID, command, pending)
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
        const unbound = b ? undefined : { session_id: await rootSession(event.sessionID), work_unit_id: event.sessionID,
          agent_id: event.agent, specialist: event.agent }
        const res = await takt("shell-prepare", () => ({
          ...(b ? { ...identity(b), author_key: b.key, expected_revision: b.revision } : unbound),
          call_id: callID, command,
        }))
        const plan = shellPlanOf(res.shell, event.agent, event.sessionID)
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

      await ctx.permission.hook("evaluate", (event) => {
        if (!SHELL_ACTIONS.has(event.action)) return
        // A configured denial is never weakened: the harness may only tighten what
        // the ruleset already resolved, and a denied command is never admitted.
        if (event.effect === "deny" || event.agent === ORCHESTRATOR_ID || RESULT_AGENTS.includes(event.agent ?? "")) return
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

      async function finishShell(command: string, admitted: AdmittedShell, sessionID: string): Promise<void> {
        if (!admitted.started || admitted.session !== sessionID) return
        admittedShell.delete(command)
        if (admitted.wrapper) await rm(admitted.wrapper, { recursive: true, force: true })
        const b = admitted.key ? bindings.get(admitted.key) : undefined
        if (!admitted.plan.capture || !b) return
        // A failing command keeps its delta: it is imported, retained, and left
        // unverified for the gate to judge.
        await stage(b, "shell-import", { call_id: admitted.callID })
      }

      // Consume the live iterator sequentially: each import can advance the
      // binding's revision, and an error must leave later admissions untouched.
      async function finishShells(entries: IterableIterator<[string, AdmittedShell]>, sessionID: string): Promise<void> {
        const next = entries.next()
        if (next.done) return
        await finishShell(next.value[0], next.value[1], sessionID)
        return finishShells(entries, sessionID)
      }

      // The command's result is admitted as one VFS transaction once it has ended.
      await ctx.tool.hook("execute.after", (event) => finishShells(admittedShell.entries(), event.sessionID))
    }

    // GC policy and lifecycle live in Go. This queue only transports host events
    // and API-created child identities; no event handler waits for model work.
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
    const coordinate = (request: Record<string, unknown>): Promise<DispatchResponse> => {
      if (!codegraphReady) {
        return Promise.reject(new Error(`CodeGraph is unavailable because ${workspace} is the home directory; open a specific project directory first.`))
      }
      return exclusive(() => spawnCoordination(["gc", "coordinate"], request))
    }
    // dispatchAction admits and drives ordinary crew work: the same execution
    // history and admission protocol `gc coordinate` uses to gate its own
    // cycle, reached under its own name rather than under GC's.
    const dispatchAction = (request: Record<string, unknown>): Promise<DispatchResponse> =>
      exclusive(async () => {
        const action = asString(request.action, "unknown")
        const session = asString(request.session, "")
        const event = asString(request.event, "")
        const agent = asString(request.agent, "harness")
        let result: DispatchResponse
        try {
          result = await spawnCoordination(["dispatch"], request)
        } catch (e) {
          throw refused("dispatch_refused", agent, session, e instanceof Error ? e : new Error(String(e)), event)
        }
        // Only a unit transition carries the slots held and the states it moved
        // between; other actions report none rather than an invented value.
        const transition: Record<string, unknown> = result !== null && isResponseObject(result) ? result : {}
        observe("dispatch", "takt.orchestration", agent, session, {
          decision: action, dispatch_id: event, reason_code: "dispatch_completed",
          ...(typeof transition.in_flight === "number" ? { parallelism: transition.in_flight } : {}),
        }, event)
        if (["admit", "launch", "finish"].includes(action)) {
          observe("unit_lifecycle", "takt.orchestration", agent, session, {
            transition: action, from_state: asString(transition.from_state, ""), to_state: asString(transition.to_state, ""), specialist: agent, reason_code: "dispatch_completed",
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
    // stopLanes interrupts a cycle's lane sessions together; one that fails
    // does not keep the others running.
    async function stopLanes(ids: readonly string[]): Promise<void> {
      const results = await Promise.allSettled(ids.map(stopChild))
      const failed = results.flatMap(r => r.status === "rejected" ? [r.reason] : [])
      if (failed.length > 0) throw new AggregateError(failed, "GC lane interruption failed")
    }
    // cycleLanes lists the lane sessions of the cycle in flight, if any.
    async function cycleLanes(session: string): Promise<string[]> {
      const state = await coordinate({ action: "status", session })
      return isCoordinatorResponse(state) && state.cycle ? Object.values(state.cycle.sessions ?? {}) : []
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
    // A process restart cannot prove that an old model turn has stopped. Abort
    // known children first, then ask the core to restore its own cycle only.
    const restoreGCCycle = once(async () => {
      const state = await coordinate({ action: "status" })
      if (isCoordinatorResponse(state) && state.cycle) {
        await stopLanes(Object.values(state.cycle.sessions ?? {}))
        await coordinate({ action: "recover", evidence: "plugin restart; previous turn cannot be safely resumed" })
      }
    })
    function scheduleGC() {
      // setTimeout releases tool/event hooks before the SDK is asked to prompt.
      setTimeout(() => {
        gcQueue = gcQueue.then(pumpGC).catch(async error => {
          try {
            const state = await coordinate({ action: "status" })
            if (isCoordinatorResponse(state) && state.cycle) {
              await stopLanes(Object.values(state.cycle.sessions ?? {}))
              await coordinate({ action: "abort", evidence: error instanceof Error ? error.message : "unknown error" })
            }
          } catch (recoveryError) {
            // No blind unlock on failed reconciliation or external divergence.
            console.error("Takt GC held for recovery", recoveryError)
          }
          console.error("Takt GC", error)
        })
      }, 0)
    }
    // Whether a cycle is due is only asked when the root orchestrator goes idle:
    // never mid-turn, never from a delegated specialist's or a lane's own idle.
    async function startCycleWhenIdle(sessionID: string) {
      try {
        if (gcChildren.has(sessionID) || await rootSession(sessionID) !== sessionID) return
        await restoreGCCycle()
        await coordinate({ action: "tick", session: sessionID })
        scheduleGC()
      } catch (error) {
        console.error("Takt GC", error)
      }
    }
    async function pumpGC() {
      await restoreGCCycle()
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
      await promptChild(child, `Maintenance cycle ${cycle.plan.cycle_id}; mandate ${cycle.plan.mandate_class}. ${instructions[cycle.phase]}`)
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
    await ctx.tool.hook("execute.before", (event) => {
      toolStartedAt.set(`${event.sessionID}:${event.id}`, Date.now())
    })

    await ctx.tool.hook("execute.after", async (event) => {
      const key = `${event.sessionID}:${event.id}`
      const started = toolStartedAt.get(key)
      toolStartedAt.delete(key)
      const root = await rootSession(event.sessionID).catch(() => event.sessionID)
      observe("tool_activity", "takt.platform", event.agent ?? "harness", root, {
        platform: "opencode", tool: event.tool, phase: "after",
        outcome: asString(event.status, "completed"),
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
      await reconcileStoredDelegations()
      const root = await rootSession(event.sessionID)
      const input = isResponseObject(event.input) ? event.input : {}
      // The delegation's description names the work unit it executes: a planned
      // unit by its committed identity, and a retry by the same name again. The
      // host call identity tells a transport repetition apart from a retry.
      const unit = asString(input.description, "").trim()
      const specialist = asString(input.agent, "")
      if (!unit) throw refused("missing_unit", specialist, root, new Error("Name the delegation after the work unit it executes: set its description to the unit identity"))
      if (!specialist) throw refused("missing_specialist", "", root, new Error("Takt VFS refused launch: the requested specialist identity is missing"), unit)
      if (!inputs.has(unitKey(root, unit))) throw refused("missing_inputs", specialist, root, new Error(`Declare the invariants work unit ${unit} consumes with dispatch_inputs before delegating it, or declare that none exists yet`), unit)
      const authorKeys = await validatedAuthorKeys(input.author_keys, specialist, root, unit)
      const delegation = `${event.sessionID}:${event.id}`
      if (VFS_AGENTS.includes(specialist) && !await findClaim(root, unit, specialist, ["pending"])) throw refused("missing_claim", specialist, root, new Error(`Takt VFS refused launch: no matching pending clean claim for session ${root}, work unit ${unit}, agent ${specialist}; assign the exact scope before launching`), unit)
      // A cleanup cycle never holds a delegation: an admitted one ends the
      // cycle and discards its delta, then its lanes stop. A denied one leaves
      // the cycle and its lanes running.
      const lanes = await cycleLanes(root)
      try {
        await dispatchAction({ action: "admit", event: unit, dispatch: delegation, session: root, agent: specialist })
      } catch (error) {
        const cause = error instanceof Error ? error : new Error("dispatch admission failed")
        throw refused("admission_denied", specialist, root, cause, unit)
      }
      // Admission issues the attempt before its gates are prepared, so a retry
      // receives fresh gates for that admitted attempt rather than a stale preassignment.
      const preparedGates: string[] = []
      try {
        await prepareVerifierGates(authorKeys, root, unit, specialist, preparedGates)
      } catch (error) {
        // Cleanup is best effort: a failure here must never replace the
        // assignment error, and one gate that cannot be released must not
        // keep the others or the admitted dispatch from being closed.
        await Promise.all([
          ...preparedGates.map(key => takt("release", { session_id: root, key })
            .catch(e => console.error("Takt gate release", key, e))),
          dispatchAction({ action: "finish", event: unit, dispatch: delegation, session: root })
            .catch(e => console.error("Takt dispatch finish", e)),
        ])
        throw refused("verifier_assignment", specialist, root, error instanceof Error ? error : new Error(String(error)), unit)
      }
      // The cycle is already closed: a lane that keeps running only meets
      // refusals, so a failed interruption never refuses the delegation.
      await stopLanes(lanes).catch(error => console.error("Takt GC lane interruption", error))
      // Only an admitted delegation is remembered: a denied one never ran.
      delegations.set(delegation, { unit, root, agent: specialist })
      await persistDelegations()
      // The subagent tool runs synchronously right after this hook returns, so
      // its execution is observed running immediately — this is a recorded
      // fact of this architecture, not an inferred one (PR-HAR-16).
      await dispatchAction({ action: "launch", event: unit, session: root })
      scheduleGC()
    })

    // collectDelivery returns the Engram IDs a completed producer delivered for
    // its unit. Bounded to one retry: a producer that forgot deliver_result gets
    // a single nudge, never an unbounded prompt loop.
    async function collectDelivery(key: string): Promise<number[]> {
      const child = childOf.get(key)
      if (!deliveries.has(key) && child) {
        await promptChild(child, "Call deliver_result with this delegation's completed Engram entry IDs before ending your turn.")
        await ctx.session.wait({ sessionID: child })
      }
      const delivered = deliveries.get(key)
      if (!delivered) throw new Error("the specialist ended without delivering its result; delegating the same unit again retries it")
      return delivered
    }

    /**
     * Settles tracked delegations and advances GC after ordinary VFS tool calls.
     * A completed result producer with no delivery gets one reminder if its child
     * is known; the hook waits for that turn before rejecting a missing result.
     * Delivery checks clear the child and result tracking and attempt to finish
     * the delegation even on failure. Storage, session, and coordination errors
     * propagate; a failed finish can replace a delivery or session error.
     * GC children and untracked delegations are ignored.
     */
    await ctx.tool.hook("execute.after", async (event) => {
      if (event.tool === "subagent" && !gcChildren.has(event.sessionID)) {
        const delegation = `${event.sessionID}:${event.id}`
        const d = delegations.get(delegation)
        if (d === undefined) return
        delegations.delete(delegation)
        await persistDelegations()
        const key = unitKey(d.root, d.unit)
        try {
          if (event.status === "completed" && RESULT_AGENTS.includes(d.agent ?? "")) {
            const delivered = await collectDelivery(key)
            // The orchestrator receives the delivered IDs with the result itself.
            event.result = { ...event.result, content: withText(event.result.content, `Delivered results: ${engramRefs(delivered)}`) }
          }
        } finally {
          deliveries.delete(key)
          childOf.delete(key)
          await dispatchAction({ action: "finish", event: d.unit, dispatch: delegation, session: d.root })
          scheduleGC()
        }
      } else if (event.tool.startsWith("vfs_") && !gcChildren.has(event.sessionID)) {
        await restoreGCCycle()
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

    // Before delegation, surface prior-root ownership alongside maintenance
    // notices. Neither context read introduces a startup gate.
    let maintenanceAnnounced = false
    await ctx.session.hook("context", async (event) => {
      if (event.agent !== ORCHESTRATOR_ID) return
      const root = await rootSession(event.sessionID)
      const claims = await takt("claims", { session_id: root }).then(r => r.claims ?? [], () => undefined)
      if (claims !== undefined) {
        const prior = claims.filter(claim => claim.root_session_id !== root)
        if (prior.length) event.system.push({ type: "text", text: `Previous-session VFS claims (keys, files and staged status): ${JSON.stringify(prior)}. Before delegating: claim_assign without author_key replaces overlapping claims from other roots completely, discarding their staged work; with author_key it continues the existing work. Same-root collisions are refused.` })
      }
      // A failed status read must never break the orchestrator's own request.
      const state = await coordinate({ action: "status" }).catch(() => undefined)
      if (state === undefined) return
      const active = isCoordinatorResponse(state) && state.cycle != null
      if (active) {
        maintenanceAnnounced = true
        event.system.push({ type: "text", text: MAINTENANCE_DUE })
      } else if (maintenanceAnnounced) {
        maintenanceAnnounced = false
        event.system.push({ type: "text", text: MAINTENANCE_DONE })
      }
    })

    // OpenCode reports provider usage on session.usage.updated. Keep only numeric
    // accounting fields; message text, prompts and tool output never cross into
    // Takt telemetry. Event IDs prevent duplicate delivery from counting twice.
    const observedUsageEvents = new Set<string>()
    function observeModelUsage(event: OpenCodeEvent) {
      if (event.type !== "session.usage.updated") return
      if (observedUsageEvents.has(event.id)) return
      const { sessionID, cost, tokens } = event.data
      const binding = [...bindings.values()].find((candidate) => candidate.session === sessionID || candidate.dispatch === sessionID)
      const attributes = {
        usage_event_id: event.id,
        input_tokens: tokens.input, output_tokens: tokens.output,
        reasoning_tokens: tokens.reasoning, cache_read_tokens: tokens.cache.read,
        cache_write_tokens: tokens.cache.write, cost_usd: cost,
      }
      observe("model_usage", "takt.platform", binding?.agent ?? "harness", sessionID,
        attributes, binding?.unit ?? "")
      observedUsageEvents.add(event.id)
    }

    const abort = new AbortController()
    void (async () => {
      for await (const event of ctx.event.subscribe({ signal: abort.signal })) {
        observeModelUsage(event)
        if (event.type === "session.idle") {
          gcBusy.delete(event.data.sessionID)
          void startCycleWhenIdle(event.data.sessionID)
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

    await ctx.tool.transform(async (editor) => {
      const subagent = editor.get("subagent")
      if (subagent) {
        // Native tools use Effect schemas; plugin tools may use plain JSON Schema.
        // Extend the native declaration without replacing its execution.
        let schema: ToolInput
        if (typeof subagent.input === "function") {
          const { Schema } = await import("effect")
          if (!Schema.isSchema(subagent.input)) throw new Error("subagent input is not a schema")
          const document = Schema.toJsonSchemaDocument(subagent.input)
          schema = { ...document.schema, $defs: document.definitions }
        } else {
          schema = toolInput(subagent.input)
        }
        editor.update("subagent", (tool) => {
          tool.input = { ...schema, properties: {
            ...toolInput(schema.properties),
            author_keys: { type: "array", items: str(), minItems: 1, uniqueItems: true,
              description: "Staged author keys to review in this delegation; access is ready automatically, including retries." },
          } }
        })
      }
      const gc = (name: string, description: string, action: string, input = obj({}, [])) =>
        editor.add({ name, description, input, execute: (value: unknown, c) => {
          if (!codegraphReady) throw new Error(`CodeGraph is unavailable because ${workspace} is the home directory; open a specific project directory first.`)
          return gcTool(action, c, toolInput(value))
        } })

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
          await restoreGCCycle()
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

      dispatch("dispatch_commit", "Commit a plan baseline: every listed unit becomes planned, with its contract and explicit prerequisites. A planned unit is executed by delegating it with the unit identity as the subagent description; delegating that name again retries the unit. Commit before delegating more work than may run without a plan, or when the active workflow calls for one. Once a plan stands, change it by naming its current version as base_version: list only the units the revision adds or replaces, withdrawn units leave the plan, and units already delegated stand as committed.",
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

      editor.add({
        name: "dispatch_inputs",
        description: "Before delegating a unit, declare the Engram IDs of the invariants it consumes, or declare that none exists yet. The specialist receives them with its delegation; redeclaring replaces the declaration.",
        input: obj({
          work_unit_id: str("The unit the delegation will be named after"),
          result_ids: { type: "array", items: { type: "integer" }, description: "Existing Engram entry IDs of the invariants this unit consumes" },
          none: { type: "boolean", description: "True only when no invariant this unit consumes has been recorded yet" },
        }, ["work_unit_id"]),
        async execute(value: unknown, c) {
          const args = toolInput<{ work_unit_id: string; result_ids?: number[]; none?: boolean }>(value)
          const session = await rootSession(c.sessionID)
          orchestratorOnly(c, "dispatch_inputs")
          if (c.sessionID !== session) throw new Error("dispatch_inputs belongs to the root orchestrator")
          const unit = asString(args.work_unit_id, "").trim()
          const ids = Array.isArray(args.result_ids) ? args.result_ids : []
          if (!unit) throw new Error("Name the unit the delegation will be named after")
          if ((ids.length > 0) === (args.none === true)) throw new Error("Declare either the consumed result_ids or none: true, exactly one of them")
          if (ids.length > 0) await dispatchAction({ action: "validate_inputs", session, result_ids: ids })
          inputs.set(unitKey(session, unit), ids)
          await persistInputs()
          return { content: ids.length > 0 ? `Declared ${ids.length} consumed invariant(s) for ${unit}` : `Declared that ${unit} consumes no recorded invariant yet` }
        },
      })

      editor.add({ name: "claim_list", description: "List ownership claims for the current root session.", input: obj({}, []), async execute(value: unknown, c) {
        toolInput(value)
        orchestratorOnly(c, "VFS claims")
        return { content: JSON.stringify(await takt("claims", { session_id: await rootSession(c.sessionID) })) }
      } })
      // The target instance is named once and serves as both the binding agent
      // and the catalog specialist, exactly as the launched subagent binds.
      editor.add({ name: "claim_assign", description: "Before delegating implementation, reserve the unit's exact file set for the specialist that will stage it. The returned key is that work's author_key. Never infer or expand scope. Without author_key, overlapping claims from other root sessions are discarded completely, including staged work; same-root collisions are refused. With author_key, continue existing work. To retry or correct work that already has staged changes, pass its author_key with the new exact file set: the staged work is kept, only added paths are checked for collisions, and the set must include every path already staged.", input: obj({
        work_unit_id: str("The unit the delegation will be named after"),
        agent: str("Catalog instance id of the specialist you will launch (for example `dev`); parallel lanes of one specialty share it and are told apart by `work_unit_id`"),
        scope: { type: "array", items: str(), description: "Exact workspace-relative file paths, existing or to be created; no directories or globs" },
        author_key: str("The author_key of this unit's existing staged work to keep; omit for new work"),
      }, ["work_unit_id", "agent", "scope"]), async execute(input: unknown, c) {
        const args = toolInput(input)
        orchestratorOnly(c, "VFS claim assignment")
        const session = await rootSession(c.sessionID)
        const unit = asString(args.work_unit_id, "").trim()
        const consumed = inputs.get(unitKey(session, unit))
        if (consumed === undefined) throw new Error(`Declare the invariants work unit ${unit} consumes with dispatch_inputs before assigning its claim, or declare that none exists yet`)
        return { content: JSON.stringify(await takt("assign", { session_id: session, work_unit_id: unit, agent_id: args.agent, specialist: args.agent, invariants: engramInvariants(consumed), scope: args.scope, author_key: args.author_key })) }
      } })
      editor.add({ name: "claim_release", description: "Use claim_list to get the exact claim_key. A claim that holds staged work is not released: consolidate it, reassign it with claim_assign and its author_key, or discard it. A claim with no staged work is released directly, except one an agent is actively working under in the current root session: ask the user through the orchestrator's native question mechanism first and set confirmed true once they agree.", input: obj({ claim_key: str("Exact key returned by claim_list; never infer this key"), confirmed: { type: "boolean", description: "Set true once the user agreed to release a claim in this root session" } }, ["claim_key"]), async execute(value: unknown, c) {
        const args = toolInput<{ claim_key: string; confirmed?: boolean }>(value)
        orchestratorOnly(c, "VFS claim release")
        const session = await rootSession(c.sessionID)
        const claims = await takt("claims", { session_id: session })
        const claim = (claims.claims ?? []).find((x: OwnershipClaim) => x.key === args.claim_key)
        if (!claim) throw new Error(`unknown claim_key ${args.claim_key}; call claim_list and use an exact listed key`)
        if (claim.staged === true) throw new Error(`Claim ${claim.key} holds staged work for [${(claim.scope ?? []).join(", ")}]. Consolidate it with vfs_consolidate, reassign it with claim_assign and author_key ${claim.key}, or discard it with vfs_discard. Discard only when a spot fix cannot reach the work. Nothing was released.`)
        if (claim.root_session_id === session && claim.active === true && args.confirmed !== true) {
          throw new Error(`WARNING: agent ${claim.agent_id} (instance ${claim.target_instance}, active) owns [${(claim.scope ?? []).join(", ")}]. Ask the user via the orchestrator's native question mechanism whether to release this exact claim; retry with confirmed:true only after an explicit yes. This server plugin cannot present dialogs or verify confirmation. Ownership was not released.`)
        }
        return { content: JSON.stringify(await takt("release", { session_id: session, key: claim.key })) }
      } })

      dispatch("dispatch_activity_start", "Record direct orchestrator work activity (not a delegated unit).", "activity_start", obj({ activity_id: str(), node_kind: { type: "string", enum: ["orchestrator"] } }, ["activity_id", "node_kind"]), (args: { activity_id: string; node_kind: string }) => args)
      dispatch("dispatch_activity_finish", "Finish direct orchestrator work activity with its outcome.", "activity_finish", obj({ activity_id: str(), node_kind: { type: "string", enum: ["orchestrator"] }, outcome: { type: "string", enum: ["completed", "failed", "interrupted"] } }, ["activity_id", "node_kind", "outcome"]), (args: { activity_id: string; node_kind: string; outcome: string }) => args)

      dispatch("dispatch_declare_recovery", "Declare bounded autonomous recovery of an objective before uncertain work begins: a binary expected result, a prior recoverable point, explicit scope, and both budgets. The scope cannot overlap a recovery that is still open or abandoned and not restored. After repeated failed recoveries of the same objective, present evidence and alternatives to the user and await their decision.",
        "recovery",
        obj({
          objective: str("Identity of the objective being recovered"),
          result: str("The binary expected result that would demonstrate recovery"),
          point: str("Reference to the prior recoverable point"),
          scope: { type: "array", items: { type: "string" }, description: "Work unit identities this recovery's scope covers" },
          actions: { type: "number", description: "Action budget: the governed calls, reads included, that the scope's units may make" },
          attempts: { type: "number", description: "Attempt budget shared by every unit of the scope: each attempt admitted for any of them after this declaration spends one" },
        }, ["objective", "result", "point", "scope", "actions", "attempts"]),
        /** Copies the recovery declaration, using its objective as the history event's work unit. */
        (args: { objective: string; result: string; point: string; scope: string[]; actions: number; attempts: number }) => ({ ...args, event: args.objective }))

      dispatch("dispatch_close_recovery", "Close an open declared recovery: record whether its result was demonstrated and link the evidence. A scope whose budget ran out is already abandoned: continue it with dispatch_exception or drop it with dispatch_restore.",
        "recovered",
        obj({
          objective: str("Identity of the recovered objective"),
          evidence: str("Evidence supporting the claimed result"),
          demonstrated: { type: "boolean", description: "Whether the declared result was actually demonstrated" },
        }, ["objective", "evidence", "demonstrated"]),
        /** Maps the recovery result to history fields, using the objective as the work unit and demonstrated as pass. */
        (args: { objective: string; evidence: string; demonstrated: boolean }) => ({ event: args.objective, objective: args.objective, evidence: args.evidence, pass: args.demonstrated }))

      dispatch("dispatch_restore", "Drop an abandoned recovery scope: reverts all the staged work of its units, after which a new recovery can be declared. It waits until the scope's delegations have returned; call it again then.",
        "restore",
        obj({
          objective: str("Identity of the abandoned recovery's objective"),
        }, ["objective"]),
        (args: { objective: string }) => ({ objective: args.objective }))

      dispatch("dispatch_exception", "Raise one bound by the finite allowance the user explicitly granted. A recovery bound needs the objective. Granting attempts or actions of an abandoned recovery reopens it with its staged work intact.",
        "exception",
        obj({
          event: str("Optional label for the record"),
          bound: { type: "string", enum: EXCEPTION_BOUNDS, description: "The bound being raised, one of: ceiling/concurrent-specialists (concurrent delegations), budget/unplanned-units (units delegated without a committed plan), budget/contests, budget/recovery-failures, budget/recovery-actions, budget/recovery-attempts" },
          objective: str("Recovery objective the exception applies to; required for the recovery bounds"),
          allowance: { type: "number", description: "Finite additional allowance in that bound's own unit" },
        }, ["bound", "allowance"]),
        (args: { event?: string; bound: string; objective?: string; allowance: number }) => args)

      dispatch("dispatch_contest", "Contest the recorded terminal failure of a unit attempt, one that ended without a result: it records the contest and spends one of the contests. Judging the contested work is a Verify delegation of yours.",
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
        }, ["scope"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ scope: string[] }>(input)
          const session = await rootSession(c.sessionID)
          const unit = await delegatedUnit(c.sessionID)
          if (isResponseObject(input) && input.author_key !== undefined) throw new Error("vfs_bind only binds implementation scope; verifier access is automatic")
          const { b, res } = await bindAs(c, session, unit, args.scope)
          return { content: `Bound as ${b.agent} to scope ${JSON.stringify(args.scope)}; unit ${unit}, attempt ${res.attempt_id}; invariants ${engramRefs(inputs.get(unitKey(session, unit)) ?? []) || "none"} at version ${res.invariants_version}; author_key ${b.key}` }
        },
      })
      editor.add({
        name: "vfs_write",
        description: "Create or update a file inside your declared scope (create if absent, patch if present). Staged virtually; not written to disk until verified and consolidated.",
        input: obj({
          path: str(),
          content: str(),
          author_key: str("Binding key from vfs_bind; defaults to your own latest binding"),
        }, ["path", "content"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; content: string; author_key?: string }>(input)
          const b = await binding(c, typeof args.author_key === "string" ? args.author_key : undefined)
          const res = await stage(b, "op", { action: "create", path: args.path, content: args.content })
          return { content: `Staged ${args.path} at revision ${res.revision} (delta_hash ${res.delta_hash})` }
        },
      })
      editor.add({
        name: "vfs_read",
        description: "Read a file of your assignment as staged: your own staged view, or the staged work you judge.",
        input: obj({ path: str("Workspace-relative path within your assignment"), author_key: str("Verifier only, optional: the author_key of the staged work whose files you read; omitted, the path's owner is used") }, ["path"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; author_key?: string }>(input)
          const gates = [...bindings.values()].filter(x => x.dispatch === c.sessionID && x.agent === c.agent && x.judges !== undefined)
          // A path has one owner, so a gate that names no author_key reads the author whose scope holds it.
          const gate = args.author_key ? gates.find(x => x.judges === args.author_key) : await gateOwning(gates, args.path, await rootSession(c.sessionID))
          // A gate reads only the authors it judges: an unknown key never falls back to another gate.
          if (args.author_key && gates.length > 0 && !gate) throw new Error(`author_key ${args.author_key} is not one of your gates: ${gates.map(x => x.judges).join(", ")}`)
          const b = gate ?? own(c)
          const judged = b.judges ? bindings.get(b.judges) : undefined
          if (judged) {
            // The author's state is read and the gate's judged state recorded in
            // one turn of the queue, so the verdict covers exactly what was read.
            return exclusive(async () => {
              const { revision, deltaHash } = judged
              // A gate reads exactly the author's staged state it will judge.
              const res = await taktUnlocked("op", {
                ...identity(b), author_key: b.key, view_key: judged.key, call_id: randomUUID(),
                expected_revision: revision, action: "read", path: args.path,
              })
              // The verdict covers the state of the last read. Files read under an
              // earlier state may be outdated, so the read that moved it names them.
              const moved = b.judgedHash !== deltaHash || b.judgedRevision !== revision
              const outdated = moved ? (b.judgedPaths ?? []).filter(path => path !== args.path) : []
              b.judgedRevision = revision
              b.judgedHash = deltaHash
              b.judgedPaths = moved ? [args.path] : [...new Set([...(b.judgedPaths ?? []), args.path])]
              await persist()
              const note = outdated.length > 0
                ? `The author's staged work changed after you read: ${outdated.join(", ")}. What you read there may be outdated; read those files again before attaching the verdict.\n\n`
                : ""
              return { content: `${note}${res.content ?? ""}` }
            })
          }
          const res = await stage(b, "op", { action: "read", path: args.path })
          return { content: res.content ?? "" }
        },
      })
      editor.add({
        name: "vfs_delete",
        description: "Stage the deletion of a file inside your declared scope. Staged virtually; the workspace copy is untouched until verified and consolidated.",
        input: obj({
          path: str(),
          author_key: str("Binding key from vfs_bind; defaults to your own latest binding"),
        }, ["path"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ path: string; author_key?: string }>(input)
          const b = await binding(c, typeof args.author_key === "string" ? args.author_key : undefined)
          const res = await stage(b, "op", { action: "delete", path: args.path })
          return { content: `Staged deletion of ${args.path} at revision ${res.revision} (delta_hash ${res.delta_hash})` }
        },
      })
      editor.add({
        name: "vfs_discard",
        description: "Discard an author's entire staged work without touching the workspace, releasing the paths it owns. Name the author by its author_key.",
        input: obj({
          author_key: str("The author_key of the staged work to discard"),
        }, ["author_key"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ author_key: string }>(input)
          // Discarding is the orchestrator's backtracking decision (PR-VFS-CSL-5).
          orchestratorOnly(c, "vfs_discard")
          if (typeof args.author_key !== "string") throw new Error("author_key must be a string")
          const b = bindings.get(args.author_key)
          if (b?.session !== await rootSession(c.sessionID)) throw new Error("author_key does not name staged work of this session")
          await takt("op", () => ({
            ...identity(b), author_key: b.key, call_id: randomUUID(),
            expected_revision: b.revision, action: "rollback",
          }))
          await release(b)
          return { content: "Discarded staged delta" }
        },
      })
      editor.add({
        name: "vfs_verify",
        description: "As an independent verifier: attach a pass/fail verdict to the author's staged delta, assigned by your delegation to that author_key. The verdict covers exactly the revision staged when you judged it.",
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
          if (!verifier) throw new Error("author_key is not assigned to this verifier delegation")
          // The verdict covers the last staged state delivered/read by this gate;
          // the store rejects it if the author has since changed that state.
          await takt("verify", () => ({
            ...identity(verifier), verifier_key: verifier.key, author_key: args.author_key, call_id: randomUUID(),
            expected_revision: verifier.judgedRevision, delta_hash: verifier.judgedHash,
            pass: args.pass, finding: args.finding,
          })).catch(error => {
            const reason = error instanceof Error ? error.message : String(error)
            throw new Error(`${reason}; if the author's staged work changed since you read it, read it again with vfs_read and attach the verdict again`)
          })
          return { content: args.pass ? "Verdict: passed" : `Verdict: rejected — ${args.finding}` }
        },
      })
      editor.add({
        name: "vfs_consolidate",
        description: "Move authorized staged work into the workspace by author_key. When a verdict applies, it must pass and match the staged revision; ownership and physical-base checks always apply.",
        input: obj({
          author_key: str("The author_key of the staged work to consolidate"),
          checkpoint: str("Optional short label hashed into the journal; defaults to the author key and revision"),
        }, ["author_key"]),
        async execute(input: unknown, c) {
          const args = toolInput<{ author_key: string; checkpoint?: string }>(input)
          if (typeof args.author_key !== "string") throw new Error("author_key must be a string")
          // Consolidation is the orchestrator's act over work its own delegations
          // staged (PR-DAG-REP-5); the author's binding speaks for the delta.
          if (c.agent !== ORCHESTRATOR_ID) throw new Error("vfs_consolidate belongs to the orchestrator")
          const b = bindings.get(args.author_key)
          if (b?.session !== await rootSession(c.sessionID)) throw new Error("author_key does not name staged work of this session")
          try {
            await takt("consolidate", () => ({
              ...identity(b), author_key: b.key, checkpoint: args.checkpoint, expected_revision: b.revision,
            }))
          } catch (error) {
            if (error instanceof Error && /physical base changed|recovery required/.test(error.message))
              error.message += "; freeze this path and escalate with the report"
            throw error
          }
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
          const key = unitKey(root, unit)
          deliveries.set(key, [...(deliveries.get(key) ?? []), ...args.result_ids])
          const count = Array.isArray(args.result_ids) ? args.result_ids.length : 0
          return { content: `Delivered ${count} result id(s)` }
        },
      })
    })

    // Delegated VFS context is granted per exact agent instance and only while
    // its exact root/unit/identity claim remains pending or active. Permission
    // rules are read from that instance; no role defaults are inferred here.
    await ctx.session.hook("context", async (event) => {
      if (!event.sessionID || !event.agent) return
      const removeVFS = () => { for (const name of VFS_TOOL_NAMES) delete event.tools?.[name] }
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
        const allowed = VFS_TOOL_NAMES.filter(name => explicitlyAllows(name))
        const root = await rootSession(event.sessionID)
        const unit = await delegatedUnit(event.sessionID)
        if (RESULT_AGENTS.includes(event.agent)) childOf.set(unitKey(root, unit), event.sessionID)
        const consumed = inputs.get(unitKey(root, unit))
        if (consumed) event.system.push({ type: "text", text: consumed.length > 0
          ? `Work unit ${unit} consumes these invariants: ${engramRefs(consumed)}. Read each with mem_get_observation; they bind as read-only and prevail over any restatement in the brief.`
          : `Work unit ${unit} consumes no recorded invariant yet; author it from the brief.` })
        const claims = await findClaims(root, unit, event.agent, ["pending", "active"])
        const claim = claims[0]
        if (!claim || allowed.length === 0) { removeVFS(); return }
        for (const name of VFS_TOOL_NAMES) if (!allowed.includes(name)) delete event.tools?.[name]
        if (claim.author_key) {
          // A gate arrives bound: adopting each preassigned gate and naming the
          // author's files is the delivery of the assignment, never the verifier's task.
          delete event.tools?.vfs_bind
          const owned = ((await takt("claims", { session_id: root })).claims ?? []) as OwnershipClaim[]
          const lines: string[] = []
          const currentGates = claims.filter(gate => gate.pending === true ||
            [...bindings.values()].some(b => b.key === gate.key && b.dispatch === event.sessionID && b.agent === event.agent))
          for (const gate of currentGates) {
            const key = gate.author_key ?? ""
            let note = ""
            const adopted = [...bindings.values()].some(x => x.dispatch === event.sessionID && x.agent === event.agent && x.key === gate.key)
            if (!adopted) {
              try { await bindAs({ sessionID: event.sessionID, agent: event.agent }, root, unit, [], key) }
              catch (error) { note = ` (gate not adopted: ${error instanceof Error ? error.message : String(error)}; report it as unverified)` }
            }
            lines.push(`author_key ${key}: ${(owned.find(x => x.key === key)?.scope ?? []).join(", ")}${note}`)
          }
          event.system.push({ type: "text", text: `You judge the staged work of these authors for work unit ${unit}; every gate is already bound. Read staged files with vfs_read naming their author_key; read project context with native read tools and run validation checks without editing the project, and attach a pass or fail verdict per author_key with vfs_verify, naming the finding it rests on.\n${lines.join("\n")}` })
          return
        }
        event.system.push({ type: "text", text: `Work unit ${unit} owns exactly these workspace-relative paths: ${(claim.scope ?? []).join(", ")}. Bind with vfs_bind using exactly this scope, stage changes with vfs_write and vfs_delete, read your staged view with vfs_read, and return the author_key in your handoff.` })
      } catch { removeVFS() }
    })

    return () => abort.abort()
  },
})
