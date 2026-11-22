# Takt harness prompt registry

Single canonical source (`STR-1`) for every runtime prompt, tool description,
output directive, and agent-facing refusal guidance emitted by the Takt harness.

## Relationship to `takt/catalog/assets/`

- `takt/catalog/assets/` holds static Markdown agent definitions and skills
  deployed verbatim to `~/.config/opencode/takt/`.
- `takt/prompts/assets/` holds structured YAML templates compiled into the Go
  binary and injected into OpenCode plugins (`takt-vfs.ts`, `takt-memory.ts`,
  `takt-dag.tsx`) via `"__TAKT_PROMPTS__"`.

Every entry and tool spec declares:

- `surface`: runtime channel (`system_clause`, `context_injection`,
  `interlocutor_turn`, `tool_contract`, `tool_output_directive`,
  `refusal_guidance`).
- `owner`: component that emits it (`opencode/layout`, `cli/dispatch`,
  `memory/mcp`, `plugin/takt-vfs`, `plugin/takt-memory`, `plugin/takt-dag`).
- `audience`: catalog audience selector resolved against `takt/catalog`.
- `correlates_with`: paths under `takt/catalog/assets/` defining the standing
  procedure or rule this prompt binds or enforces.
- `vars`: exact `{{var}}` placeholders allowed in the template.

## Files

| File | Domain |
| :--- | :--- |
| `assets/context.yaml` | Static `MemoryClause` and dynamic `event.system` context bindings |
| `assets/interlocutor.yaml` | Interlocutor switch opening, handoff return, and abort notices |
| `assets/directives.yaml` | Tool output rewrites and operational addenda |
| `assets/tools_vfs.yaml` | VFS/dispatch tool descriptions and `subagent` parameter overrides |
| `assets/tools_memory.yaml` | Memory MCP tool descriptions |
| `assets/refusals_vfs.yaml` | Agent-facing refusal guidance (`STR-14`) from `takt-vfs.ts` |
| `assets/refusals_memory.yaml` | Agent-facing refusal guidance (`STR-14`) from `takt-memory.ts` |

Automated tests in `takt/prompts/prompts_test.go` and
`takt/agents/opencode/alignment_test.go` verify placeholder integrity,
`correlates_with` target existence, tool-permission alignment per `audience`,
and zero orphan prompts.
