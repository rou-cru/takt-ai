# Maintenance

Checking health, updating, uninstalling and recovering files. Commands and flags are in the [CLI reference](cli.md).

## Check health

`takt-ai doctor` runs these checks against your home directory and prints one line per check with a remedy when something is wrong:

| Area | What it checks |
| --- | --- |
| Tools | `takt-ai`, `opencode` and `engram` resolve in `PATH`, and duplicates that shadow each other are flagged. |
| Control plane | The event store of the current workspace (`.takt-ai/events.db`): it opens, has its tables and passes SQLite's integrity check. A workspace without a store yet passes. A store readable by other users warns; an unusable one fails. Nothing is created or changed. |
| Deployment | The files Takt manages are present. |
| Engram | A compatible binary, the native plugin, project diagnostics and whether memories need review. |
| CodeGraph | A compatible binary. |
| OpenCode | Version 2 or newer with a working API, and the VFS plugin, memory plugin and sandbox adapter deployed. |
| Disk | Free space where `~/.takt-ai` lives: a warning under 100 MB, a failure under 10 MB. |

Each check ends as pass, warn or fail. The summary is healthy, degraded (warnings only) or unhealthy (at least one failure). Only a failure makes the command exit with status 1.

## Update

`takt-ai setup sync` re-applies Takt's managed files. Files you edited are kept; files you deleted are restored. After upgrading OpenCode, run it again so the plugins match. Use `--plan-only` first to see what would change.

## Backups

Before Takt overwrites a file, it keeps a copy under `.takt-backups` in the install root: a file that existed before Takt, or a file Takt manages that you edited since. Uninstall and `restore` read from there.

## Uninstall

`takt-ai setup uninstall` removes the files Takt installed and the Engram and CodeGraph entries it added to OpenCode's configuration. For each file:

- A file that existed before Takt, and has not changed since Takt wrote it, goes back to its original content.
- A file you edited after Takt installed it is preserved, and the command stops until you decide to keep or remove it. That decision is made in the TUI; the command line cannot make it.

Kept files are moved to `takt-retained/<UTC timestamp>/` in the install root, readable only by your user. The TUI can also hand off a snapshot of your Engram database to the same place.

Uninstall never removes the Engram database. It lives in `~/.engram`, or in the directory named by `ENGRAM_DATA_DIR`, and stays where it is.

## Restore

`takt-ai restore` puts back the pre-existing content of every file Takt replaced, from `.takt-backups`. Entries without a backup are skipped, and nothing is written outside the install root. It does not uninstall anything: Takt stays installed. The result is a JSON list of the restored paths.
