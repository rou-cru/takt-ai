# Supported Agents

← [Back to README](../README.md)

---

## Agent Matrix

| Agent           | ID               | Skills       | MCP | Delegation                       | Output Styles | Slash Commands | Config Path                         |
| --------------- | ---------------- | ------------ | --- | -------------------------------- | ------------- | -------------- | ----------------------------------- |
| OpenCode        | `opencode`       | Yes          | Yes | Full (multi-mode overlay)        | No            | Yes            | `~/.config/opencode`                |

OpenCode receives the **full SDD orchestrator** policy, plus skill files written to its skills directory, through the OpenCode-compatible `opencode.json` agent overlay. The agent handles SDD automatically when the task is large enough, or when the user explicitly asks for it — no manual setup required.

`takt-ai install --scope=workspace` is supported for agent-scoped files. In workspace scope, Takt AI writes system prompts, skills, and SDD agents into the current project root when the agent supports project-local configuration. Global-only integrations, such as settings that the agent only reads from its global config, remain global by design.

---

## Delegation Models

| Model                 | How It Works                                                                                                                                                                                       | Agents                                                                                                    |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| **Full (sub-agents)** | Each SDD phase runs in an isolated context window via native sub-agent delegation, package-managed subagents, or an OpenCode-compatible overlay. The orchestrator coordinates; sub-agents execute. | OpenCode |

---

## SDD Mode Support

| Feature          | OpenCode |
| ---------------- | :------: |
| SDD orchestrator |   Yes    |
| Single-mode SDD  |   Yes    |
| Multi-mode SDD   |   Yes    |

**Multi-mode** (assigning different AI models to each SDD phase) is supported by **OpenCode** through the OpenCode-compatible multi-mode overlay. Otherwise the agent runs in **single-mode** — the orchestrator manages everything using whatever model the agent is already running.

---

## Agent Notes

### OpenCode

- Full multi-agent overlay with 11 named agents in `opencode.json` (`takt-orchestrator` plus 10 SDD phase agents)
- Slash commands for SDD phases (`/sdd-new`, `/sdd-explore`, etc.)
- Native OpenCode `subagent` delegation; experimental background execution is available when supported by the installed OpenCode V2 build
- The TUI model picker includes providers and models discovered from the local `opencode.json`, including custom providers
- Custom models from `opencode.json` must advertise `capabilities.tools:true` explicitly to appear as selectable SDD-capable options in the model picker
- Multi-mode prerequisite: connect your AI providers first via `/connect`, then verify models via `/models` or `opencode api get /api/model`
- Takt AI sets OpenCode SDD agent sharing to `disabled` by default for privacy; existing user-managed `share` values such as `manual` or `auto` are preserved.
- OpenCode Desktop SDD commands resolve the project with `git rev-parse --show-toplevel || pwd` before acting, avoiding Electron current-working-directory drift.

- Sub-agents run natively via `mode:subagent` in `opencode.json` (server/project config) and terminal preferences in the global `cli.json`; there is no V2 `settings.json`.
- **Delegation**: Full (multi-mode overlay)

---

## Appendix: OpenCode V2 Diagnostics

Read-only triage for a sick OpenCode setup. Full procedure with log queries
and config precedence lives in [Usage → OpenCode V2 Diagnostics](usage.md#opencode-v2-diagnostics).

```bash
opencode service status            # background service URL when healthy
opencode api get /api/info         # exact server version / availability probe
opencode api --standalone get /api/info  # same probe on a private server
opencode service restart           # restart the background service
grep 'role=server' ~/.local/share/opencode/log/opencode.log | tail -20
OPENCODE_LOG_LEVEL=DEBUG opencode api --standalone get /api/info
```

`cli.json` is global-only (`~/.config/opencode/cli.json`); there is no
project-local `cli.json` and no V2 `settings.json`. `OPENCODE_CLI_CONFIG_CONTENT`
overrides the CLI config inline for one-shot probes, and
`OPENCODE_DISABLE_PROJECT_CONFIG=1` isolates global from project-local causes.
