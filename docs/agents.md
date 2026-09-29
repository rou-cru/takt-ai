# Supported Agents

← [Back to README](../README.md)

---

## Agent Matrix

| Agent           | ID               | Skills       | MCP | Delegation                       | Output Styles | Slash Commands | Config Path                         |
| --------------- | ---------------- | ------------ | --- | -------------------------------- | ------------- | -------------- | ----------------------------------- |
| OpenCode        | `opencode`       | Yes          | Yes | Full (multi-mode overlay)        | No            | Yes            | `~/.config/opencode`                |

OpenCode receives the Takt crew as native agents in `opencode.json`: the `takt`
orchestrator (primary, default agent) plus the specialist instances declared in
`takt/catalog/assets/agents/`. Skills are deployed to `~/.opencode/skills/` and load
on demand. The orchestrator loads the SDD workflow skill when a delivery needs it.

The crew roster, role classes, VFS grants and what each role may change are defined
once in [the catalog README](../takt/catalog/assets/README.md); this page does not
restate them.

---

## Agent Notes

### OpenCode

- One native agent per catalog instance; OpenCode's built-in `build`, `plan`,
  `general` and `explore` agents are disabled so delegation always reaches a Takt
  specialist.
- Native OpenCode `subagent` delegation, admitted and bounded by the `takt-vfs`
  plugin (concurrency ceiling, plan budgets, VFS claims before implementation).
- The TUI model picker includes providers and models discovered from the local `opencode.json`, including custom providers
- Custom models from `opencode.json` must advertise `capabilities.tools:true` explicitly to appear as selectable options in the model picker
- Multi-model prerequisite: connect your AI providers first via `/connect`, then verify models via `/models` or `opencode api get /api/model`
- Sub-agents run natively via `mode:subagent` in `opencode.json` and terminal preferences in the global `cli.json`; there is no V2 `settings.json`.

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
