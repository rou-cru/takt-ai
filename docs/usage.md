# Usage

← [Back to README](../README.md)

---

`takt-ai` has two faces: an interactive TUI when run with no arguments in a
terminal, and a small command set for scripts. It configures one supported
agent: **OpenCode**. There are no other agent targets, personas, or presets.

## Interactive TUI

Run it in a terminal:

```bash
takt-ai
```

The main menu offers, depending on whether Takt is already installed:

| Screen | What it does |
| --- | --- |
| Install / Configure installation | Full first install, or re-apply and adjust the managed setup |
| Assign models | Reassign the models OpenCode sub-agents use |
| Check for drift | Read-only inspection of managed files against the ownership manifest |
| Uninstall | Remove Takt's managed configuration (with preservation choices) |
| Diagnostics | Reusable functional checks |

A cancelled operation stops at a stable point: either nothing was applied, or
the applied changes stay in place and are reported. Replaced files that already
existed are backed up under `<root>/.takt-backups/`.

---

## CLI Commands

```
takt-ai version | doctor | setup install|sync|uninstall|image
       | setup default-request | memory record|continue|close
       | codegraph ensure-index | restore | dispatch | dag status
       | vfs journal|recover|claims|assign|assign-verifier|release|bind|op|verify|consolidate|resolve|shell-prepare|shell-import
       | obs ingest | gc plan|findings|refute|acceptance|coordinate
```

`takt-ai` with no arguments and no terminal prints this usage and exits.

### version

```bash
takt-ai version    # also: --version, -v
```

### doctor

Read-only ecosystem health check — no changes made to your configuration:

```bash
takt-ai doctor
```

| Check | What it verifies |
|-------|------------------|
| Tool binaries | Required tools present on `PATH`; shadow detection (wrong binary resolves first) |
| Deployment state | Ownership-manifest state of Takt's managed files |
| Engram reachability | The Engram memory server responds |
| Disk space | Warns when available space is critically low |

Each check reports **pass**, **warn**, or **fail** with an optional remedy hint. Run `doctor` first when troubleshooting an unexpected install or sync result.

### setup install / sync / uninstall / image

First-time setup, refresh, and removal of Takt's managed configuration for OpenCode. The request is a strict JSON document on stdin or `--input`:

```bash
# Preview the plan without applying changes
takt-ai setup default-request | takt-ai setup install --plan-only

# Full install (requires explicit --yes to change anything)
takt-ai setup default-request | takt-ai setup install --yes

# Custom component selection (see "Components" below)
echo '{"components":["context7","theme"]}' | takt-ai setup install --yes

# Refresh managed assets to the current version
takt-ai setup sync --yes

# Remove only Takt's managed configuration
takt-ai setup uninstall --yes
```

Flags:

| Flag | Applies to | Description |
| --- | --- | --- |
| `--root <dir>` | all | Install root (default: your home directory) |
| `--input <file-or--->` | setup | JSON request file, or `-` for stdin (default) |
| `--plan-only` | setup | Preview the plan without touching the environment |
| `--yes` | setup | Required to apply any change; refuses otherwise |
| `--json` | setup | One JSON value on stdout; diagnostics move to stderr |

Behavior:

- **install** acquires prerequisites first (Engram, CodeGraph), then deploys
  the OpenCode configuration, catalog agents, skills, and plugins, records the
  installation, and attempts an OpenCode reload. A reload failure is reported
  as evidence; files are already valid on disk.
- **sync** redeploys the same managed set idempotently — re-running it twice
  produces no change the second time.
- **uninstall** removes managed files, restores pre-existing content it had
  taken over, and forgets the installation record. The Engram database lives
  outside Takt's footprint and is never removed.
- **image** is the install variant used when building the container image; it
  skips probing or reloading a live OpenCode process.
- A previously interrupted operation prints a warning notice on the next run;
  every command re-checks the actual files before acting.
- User-modified managed files are preserved (never silently overwritten on
  uninstall); conflicts with pre-existing content must be resolved through the
  TUI or by choosing what to preserve.

### Components

The installable pieces (see `takt/catalog/capabilities.yaml`):

| Component | Core | Selectable | Purpose |
| --- | --- | --- | --- |
| `engram` | yes | no | Memory/context graph runtime wiring (MCP) |
| `codegraph` | yes | no | Tree-sitter codebase exploration for agents (MCP) |
| `skills` | yes | no | Skill system deployed by every install and sync |
| `specialists` | yes | no | Specialist sub-agent roster rendered from the catalog |
| `context7` | no | yes | Up-to-date library docs for agents (MCP) |
| `theme` | no | yes | Takt terminal theme for the harness |
| `opencode-takt-logo` | no | yes | OpenCode logo plugin (artifacts install regardless of selection) |

`takt-ai setup default-request` prints the request selecting every component, so scripts can pipe it into `setup install --input`.

### memory

Machine interface for the OpenCode memory plugin: one strict JSON request on
stdin, one JSON line on stdout. Not meant for interactive use — your agent
manages memory automatically through Engram's MCP tools.

```bash
takt-ai memory record   < request.json   # record observations in a session
takt-ai memory continue < request.json   # resume with prior session context
takt-ai memory close    < request.json   # close a session
```

A malformed request exits with status 2 and a validation-error line; other
failures exit 1.

### codegraph ensure-index

Prepares the current directory's CodeGraph index before the MCP server can
expose its tools. Run from the workspace root:

```bash
takt-ai codegraph ensure-index
```

### restore

Restores pre-existing content Takt had taken over, so a deployment can be
undone safely:

```bash
takt-ai restore [--root <dir>]
```

### dispatch / gc coordinate

Harness IPC that admits and drives crew work through the execution history.
`dispatch` serves the ordinary orchestrator actions (admit, finish, tick,
launch, commit, switch, handoff, abort_switch, contest, recovery, exception,
…); maintenance-cycle actions belong to `gc coordinate`. Both read a JSON
request and answer JSON:

```bash
takt-ai dispatch --workspace <dir> --state <private-dir> --request '{"action":"admit",...}'
takt-ai gc coordinate --workspace <dir> --state <private-dir> --request '...'
```

### dag status

Read-only DAG snapshot of the execution history — it never selects, admits, or
mutates work. Consumed by the OpenCode TUI DAG plugin, which parses stdout as
JSON: on a control-plane read failure it emits `{"capture":"unavailable"}` and
exits 0 rather than rendering a fake empty graph.

```bash
takt-ai dag status --workspace <dir> --state <private-dir> [--session <root-session>] --format json
```

### vfs

Transactional file operations and evidence for the OpenCode `takt-vfs`
plugin. Offline subcommands inspect or repair; the rest consume one strict
JSON request on stdin (the `ipc_version` wire contract is checked and
reported on mismatch):

```bash
# Journal pagination and explicit recovery (requires --restore)
takt-ai vfs journal  --workspace <dir> --state <private-dir> [--after N] [--limit N] [--session id] [--unit id]
takt-ai vfs recover  --workspace <dir> --state <private-dir> --restore

# Claim control (orchestrator capability)
takt-ai vfs claims   --workspace <dir> --state <private-dir> < request.json
takt-ai vfs assign   --workspace <dir> --state <private-dir> < request.json
takt-ai vfs assign-verifier --workspace <dir> --state <private-dir> < request.json
takt-ai vfs release  --workspace <dir> --state <private-dir> < request.json

# Bound mutation IPC
takt-ai vfs bind          --workspace <dir> --state <private-dir> < request.json
takt-ai vfs op            --workspace <dir> --state <private-dir> < request.json   # read|create|patch|delete|rollback
takt-ai vfs verify        --workspace <dir> --state <private-dir> < request.json
takt-ai vfs consolidate   --workspace <dir> --state <private-dir> < request.json
takt-ai vfs resolve       --workspace <dir> --state <private-dir> < request.json   # collision resolution
takt-ai vfs shell-prepare --workspace <dir> --state <private-dir> < request.json   # sandbox plan for one command
takt-ai vfs shell-import  --workspace <dir> --state <private-dir> < request.json   # admit the captured result
```

### obs ingest

Appends externally observed, content-free events to the workspace event store.
Only the four plugin-visible classes are accepted (`dispatch`,
`unit_lifecycle`, `tool_activity`, `model_usage`), each from its declared
source plane; everything else is produced inside Takt and rejected here:

```bash
takt-ai obs ingest --workspace <dir> < event.json
```

### gc

Workspace GC cycle declaration, findings, and acceptance. A cycle declares
exactly one mandate: `dead-code`, `complexity`, `duplication`, `documentation`,
or `analyzer-integrity`.

```bash
takt-ai gc plan       --workspace <dir> --state <private-dir> --session <id> --cycle <id> --mandate <class>
takt-ai gc findings   --workspace <dir> --state <private-dir> --session <id> --cycle <id> --mandate <class>
takt-ai gc refute     ... --finding <id> --class <evidence-class> --evidence <text> --instance <id>
takt-ai gc acceptance --workspace <dir> --state <private-dir> --cycle <id> --result pass|regress
```

Findings are candidates, not mutation authority: independent investigation and
explicit authorization still apply, and refutations attach evidence classes
for what the analysis could not see.

---

## What Takt installs

Managed files live under your install root (home by default):

| Path | Content |
| --- | --- |
| `~/.config/opencode/opencode.json` | Agents, permissions, MCP servers (Engram, CodeGraph, context7), session-start hooks |
| `~/.config/opencode/cli.json` | Terminal preferences: Takt theme selection |
| `~/.config/opencode/AGENTS.md` | Short "Takt Memory" section (Engram wiring) |
| `~/.config/opencode/plugins/` | `takt-vfs.ts`, memory plugin, `takt-sandbox.mjs`, `takt-dag/tui.tsx`, `package.json` |
| `~/.config/opencode/takt/` | Catalog agent context files (BASELINE, PERSONA, SOUL, OPERATIONS) |
| `~/.opencode/skills/` | Takt skills, discovered by OpenCode on demand |
| `~/.takt-manifest.json` | Ownership manifest: every managed file with hashes and owners |
| `~/.takt-backups/` | Preserved copies of pre-existing content taken over by an operation |

Editing an installed Markdown file takes effect on the next OpenCode load, not
necessarily in an active session. To refresh managed content after upgrading
the binary, re-run `takt-ai setup sync --yes` (or re-run the TUI).

---

## OpenCode V2 Diagnostics

When an install or sync result looks wrong, check what the running OpenCode V2
server actually sees — never infer health from files on disk. Every command
below is read-only.

```bash
# 1. Is the background service alive? Prints its URL when healthy.
opencode service status

# 2. Exact server version (also the cheapest availability probe).
opencode api get /api/info

# 3. Same probe against a private server instead of the background service.
#    Use it to rule out a stale or wedged background service.
opencode api --standalone get /api/info

# 4. Restart the background service after config changes that a reload
#    did not pick up.
opencode service restart
```

Server state lives behind the same API (`GET /api/mcp`, `GET /api/agent`,
`GET /api/model/default`, `GET /api/skill`, `GET /api/plugin`) — `takt-ai`
verifies through those routes instead of parsing config files.

### Logs

The server appends to `~/.local/share/opencode/log/opencode.log`. Each line
carries a `role` field telling you which side wrote it:

```bash
# CLI-side entries (resolution, service lifecycle).
grep 'role=cli' ~/.local/share/opencode/log/opencode.log | tail -20

# Server-side entries (permissions, sessions, tools).
grep 'role=server' ~/.local/share/opencode/log/opencode.log | tail -20
```

For verbose output on a single invocation without touching global config:

```bash
OPENCODE_LOG_LEVEL=DEBUG opencode api --standalone get /api/info
```

### Config precedence

- `cli.json` is **global only** (`~/.config/opencode/cli.json`) — terminal
  preferences such as `theme` and TUI plugin registrations live there. There
  is no project-local `cli.json`, and there is no V2 `settings.json`:
  server/project config lives in `opencode.json`.
- `OPENCODE_CLI_CONFIG_CONTENT` overrides the CLI config inline (same
  mechanism as `OPENCODE_CONFIG_CONTENT` for the server config). Prefer it
  for one-shot probes over editing files:
  `OPENCODE_CLI_CONFIG_CONTENT='{"plugins":[]}' opencode api get /api/info`.
- `OPENCODE_DISABLE_PROJECT_CONFIG=1` ignores project-local server config
  when you need to isolate whether a problem comes from the project or the
  global setup. `opencode debug config` and `opencode debug paths` show the
  effective sources and global directories.
