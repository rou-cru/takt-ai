# Supported Agents

← [Back to README](../README.md)

---

## Agent Matrix

| Agent    | ID         | Skills | MCP | Delegation            | Config Path          |
| -------- | ---------- | ------ | --- | --------------------- | -------------------- |
| OpenCode | `opencode` | Yes    | Yes | Full (multi-mode overlay) | `~/.config/opencode` |

OpenCode is the only supported agent. It receives the Takt crew as native
agents in `opencode.json`: the `takt` orchestrator (primary, default agent)
plus the specialist instances declared in `takt/catalog/assets/agents/`.
Skills deploy to `~/.opencode/skills/` and load on demand.

The crew roster, role classes, VFS grants and what each role may change are
defined once in [the catalog README](../takt/catalog/assets/README.md); this
page does not restate them.

---

## OpenCode Notes

- One native agent per catalog instance; OpenCode's built-in `build`, `plan`,
  `general` and `explore` agents are disabled so delegation always reaches a
  Takt specialist.
- Native OpenCode `subagent` delegation, admitted and bounded by the
  `takt-vfs` plugin (concurrency ceiling, plan budgets, VFS claims before
  implementation).
- The TUI model picker includes providers and models discovered from the
  local `opencode.json`, including custom providers.
- Custom models from `opencode.json` must advertise `capabilities.tools:true`
  explicitly to appear as selectable options in the model picker.
- Multi-model prerequisite: connect your AI providers first via `/connect`,
  then verify models via `/models` or `opencode api get /api/model`.
- Sub-agents run natively via `mode:subagent` in `opencode.json` and terminal
  preferences live in the global `cli.json`; there is no V2 `settings.json`.

Diagnostics and log queries live in
[Usage → OpenCode V2 Diagnostics](usage.md#opencode-v2-diagnostics).
