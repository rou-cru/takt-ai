# Usage

← [Back to README](../README.md)

---

## Persona Modes

| Persona   | ID          | Description                                                                       |
| --------- | ----------- | --------------------------------------------------------------------------------- |
| Takt | `takt` | Teaching-oriented mentor persona — pushes back on bad practices, explains the why |
| Neutral   | `neutral`   | Same teacher, same philosophy, no regional language — warm and professional       |
| Custom    | `custom`    | Keep your existing persona/config unmanaged — takt-ai does not inject a persona |

`custom` is a compatibility/ownership choice, not a persona editor. Use it when you already have your own persona instructions and want takt-ai to leave them alone.

---

## Interactive TUI

Just run it — the Bubbletea TUI guides you through agent selection, components, skills, presets, and managed uninstall flows:

```bash
takt-ai
```

The uninstall flow is also available from the TUI menu. It lets you:

- select one or more configured agents
- select which managed components to remove (for example `sdd`, `persona`, or `context7`)
- confirm the exact uninstall scope before applying changes

Before any managed file is modified, `takt-ai` creates a backup snapshot so the configuration can be restored later if needed.

---

## CLI Commands

### install

First-time setup — detects your tools, configures agents, injects all components. When installing a single agent with `--agent X`, takt-ai **merges** the new agent into the existing `installed_agents` list in `state.json` and **preserves** any existing `model_assignments` — it does not overwrite the full state.

```bash
# Full ecosystem
takt-ai install \
  --agent opencode \
  --preset full-takt

# Minimal setup for Cursor
takt-ai install \
  --agent cursor \
  --preset minimal

# Pick specific components and skills
takt-ai install \
  --agent opencode \
  --component engram,sdd,skills,context7,persona,permissions \
  --skill go-testing,skill-creator,branch-pr,issue-creation \
  --persona takt

# Dry-run first (preview plan without applying changes)
takt-ai install --dry-run \
  --agent opencode \
  --preset full-takt
```

### sync

Refresh managed assets to the current version. Use after `takt-ai upgrade` or when you want your local configs aligned with the latest release. Does NOT reinstall binaries (engram, GGA) — only updates prompt content, skills, MCP configs, and SDD orchestrators.

> **Important:** `takt-ai sync` updates the agents recorded as installed by Takt AI, not every AI agent config directory on your machine.
>
> Takt AI stores your selected install targets in `~/.takt-ai/state.json`. Future `sync` runs use that stored selection so Takt AI does not accidentally write into tools you did not choose to manage. If you rerun install and select only one agent, that new selection becomes the default sync scope.
>
> Before syncing, you can preview the active scope with `takt-ai sync --dry-run`. If you want to sync agents outside the stored selection, pass them explicitly with `--agent`.

```bash
# Preview which agents sync will update
takt-ai sync --dry-run

# Sync the agents currently registered in ~/.takt-ai/state.json
takt-ai sync

# Sync a specific agent explicitly
takt-ai sync --agent opencode
```

Sync is safe and idempotent — running it twice produces no changes the second time. When files change, the summary reports the changed file count and lists the changed file paths.

`sync` refreshes the managed component set for the selected agents. It does not support `--component`; use `--include-permissions` or `--include-theme` for the opt-in components that are excluded from the default sync scope.

For Hermes, takt-ai is detect-only: it cannot install Hermes. Install Hermes manually first. Detection is driven by the `~/.hermes` config directory (the binary being on `PATH` is reported separately). Once Hermes is detected, `takt-ai install --agent hermes` injects context7 and Engram MCP blocks into `~/.hermes/config.yaml`, writes the SDD orchestrator and persona into `~/.hermes/SOUL.md`, and copies skills to `~/.hermes/skills/`. Use `takt-ai sync --agent hermes` to update the managed configuration after upgrades.

### uninstall

Remove only the `takt-ai` managed configuration from one or more agents. This does not uninstall external packages or binaries — it removes managed prompt sections, MCP entries, skills/config fragments, and other managed files, then updates `state.json` accordingly.

Before any change is applied, `takt-ai` creates a backup snapshot of the affected files.

```bash
# Partial uninstall for a specific agent
takt-ai uninstall \
  --agent opencode

# Partial uninstall for specific components only
takt-ai uninstall \
  --agent opencode \
  --component sdd,persona,context7

# Complete uninstall of managed config from all supported agents
takt-ai uninstall --all

# Skip confirmation prompt
takt-ai uninstall --agent cursor --component skills --yes
```

If no `--component` flag is provided for a partial uninstall, `takt-ai` removes all managed uninstallable components for the selected agent set.

### update / upgrade

Check for and install new versions of `takt-ai` itself. The pre-upgrade backup snapshot covers only the agents recorded in `state.InstalledAgents` (`~/.takt-ai/state.json`) — not every agent config directory that exists on your machine.

```bash
# Check if a newer version is available
takt-ai update

# Upgrade to the latest release (downloads new binary, replaces current)
takt-ai upgrade
```

After upgrading, run `takt-ai sync` to refresh all managed assets to the new version's content.

If GitHub rate-limits update checks, export `GITHUB_TOKEN` or `GH_TOKEN` before running `takt-ai update`/`upgrade`.

**Self-update prompt behavior** (changed in v1.x slice 5 — `TAKT_AI_CONFIRM_UPDATE` removed):

| Situation | Behavior |
|-----------|----------|
| Interactive terminal (TTY) | Always prompts `Apply now? [Y/n]`. Empty Enter accepts. |
| Non-TTY (CI, pipe, script) | Auto-declines — never hangs. |
| `TAKT_AI_YES=1` | Auto-accepts without prompting (for scripted upgrades). This variable is inherited by subprocesses, so scope it to a single invocation when needed (e.g. `TAKT_AI_YES=1 takt-ai …`). |
| `TAKT_AI_NO_SELF_UPDATE=1` | Skips the self-update check entirely. |

`TAKT_AI_CONFIRM_UPDATE` was removed in slice 5. It is now ignored if set.

`TAKT_AI_SELF_UPDATE_DONE` is an internal loop guard and should not be set manually.

### model assignment

The TUI **Configure Models** screen can assign different models to SDD phases, `sdd-onboard`, and Judgment Day agents (`jd-judge-a`, `jd-judge-b`, `jd-fix-agent`) when the selected agent supports those slots. This lets you keep review or apply phases on stronger models while routing cheaper phases to faster models.

### doctor

Read-only ecosystem health diagnostics — no changes made to your configuration:

```bash
takt-ai doctor
```

Checks performed:

| Check | What it verifies |
|-------|-----------------|
| Tool binaries | Required tools present on `PATH`; shadow detection (wrong binary resolves first) |
| `state.json` validity | Parses `~/.takt-ai/state.json` and reports any schema/corruption issues |
| Engram MCP reachability | Confirms the Engram MCP server responds |
| Disk space | Warns when available space is critically low |

Each check reports **pass**, **warn**, or **fail** with an optional remedy hint. Run `doctor` first when troubleshooting an unexpected install or sync result.

### version

```bash
takt-ai version
takt-ai --version
takt-ai -v
```

---

## CLI Flags (install)

| Flag                          | Description                                                                                                       |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `--agent`, `--agents`         | Agents to configure (comma-separated)                                                                             |
| `--component`, `--components` | Components to install (comma-separated)                                                                           |
| `--skill`, `--skills`         | Skills to install (comma-separated)                                                                               |
| `--persona`                   | Persona mode: `takt`, `neutral`, `custom` (`custom` keeps your existing persona unmanaged)                   |
| `--preset`                    | Preset: `full-takt`, `ecosystem-only`, `minimal`, `custom` (`custom` means manual component/skill selection) |
| `--sdd-mode`                  | SDD orchestrator mode: `single` or `multi`                                                                        |
| `--scope`                     | Install scope for agent-scoped files: `global` (default, writes to each selected agent's global config directory) or `workspace` (writes to the current project root). Also settable via `TAKT_AI_INSTALL_SCOPE` env var for CI/non-interactive use. |
| `--dry-run`                   | Preview the install plan without applying changes                                                                 |

## CLI Flags (sync)

| Flag                     | Description                                                                                          |
| ------------------------ | ---------------------------------------------------------------------------------------------------- |
| `--agent`, `--agents`    | Agents to sync (defaults to all installed agents)                                                    |
| `--skill`, `--skills`    | Skills to sync (comma-separated; defaults to selected preset skills)                                  |
| `--sdd-mode`             | SDD orchestrator mode: `single` or `multi`                                                           |
| `--strict-tdd`           | Enable Strict TDD Mode for SDD agents                                                                |
| `--profile`              | Create or update an SDD profile: `name:provider/model` (sets the default model for all phases)       |
| `--profile-phase`        | Override a specific phase in a profile: `name:phase:provider/model`                                  |
| `--sdd-profile-strategy` | OpenCode profile sync strategy: `generated-multi` or `external-single-active`                        |
| `--include-permissions`  | Include permissions sync (opt-in)                                                                    |
| `--include-theme`        | Include theme sync (opt-in)                                                                          |
| `--dry-run`              | Preview the sync plan without applying changes                                                       |

**Profile examples:**

```bash
# Create a "cheap" profile using a free model for all phases
takt-ai sync --profile cheap:openrouter/qwen/qwen3-30b-a3b:free

# Override the design phase to use a stronger model
takt-ai sync --profile-phase cheap:sdd-design:anthropic/claude-sonnet-4-20250514

# Create multiple profiles in one command
takt-ai sync \
  --profile cheap:openrouter/qwen/qwen3-30b-a3b:free \
  --profile premium:anthropic/claude-sonnet-4-20250514

# Use compatibility mode with an external OpenCode profile manager
takt-ai sync --agent opencode --sdd-profile-strategy external-single-active
```

## CLI Flags (uninstall)

| Flag                          | Description                                                             |
| ----------------------------- | ----------------------------------------------------------------------- |
| `--agent`, `--agents`         | Agents to uninstall managed config from (required unless using `--all`) |
| `--component`, `--components` | Managed components to remove only from the selected agents              |
| `--all`                       | Remove managed configuration from all supported agents                  |
| `--yes`, `-y`                 | Skip the confirmation prompt                                            |

---

## Typical Workflow

```bash
# First time: install everything
takt-ai install --agent opencode --preset full-takt

# After a new release: upgrade + sync
takt-ai upgrade
takt-ai sync

# Remove only managed SDD + persona config
takt-ai uninstall --agent opencode --component sdd,persona

# Re-apply the full preset later
takt-ai install --agent opencode --preset full-takt
```

---

## Dependency Management

`takt-ai` auto-detects prerequisites before installation and provides platform-specific guidance:

- **Detected tools**: git, curl, node, npm, brew, go
- **Version checks**: validates minimum versions where applicable
- **Platform-aware hints**: suggests `brew install`, `apt install`, `pacman -S`, `dnf install`, or `winget install` depending on your OS
- **Node LTS alignment**: on apt/dnf systems, Node.js hints use NodeSource LTS bootstrap before package install
- **Dependency-first approach**: detects what's installed, calculates what's needed, shows the full dependency tree before installing anything, then verifies each dependency after installation

---

## OpenCode V2 Diagnostics

When an `install` or `sync` result looks wrong, check what the running
OpenCode V2 server actually sees — never infer health from files on disk.
Every command below is read-only.

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
