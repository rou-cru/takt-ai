---
name: takt-archify
description: Run headless repository architecture analysis (coupling, cycles, hotspots, diagram drift) or author interactive Archify diagrams and on-demand one-page explainers when Archify is installed.
license: AGPL-3.0
metadata:
  author: takt
  version: "1.0"
  component: archify
---

# Takt Archify

Always invoke the installed Archify CLI with update notices suppressed:

```sh
ARCHIFY_UPDATE_CHECK_DISABLED=1 node ~/.opencode/skills/archify/bin/archify.mjs <command>
```

## 1. Read-only codebase analysis (`code-analysis`)

Use headless `code-analysis` for coupling, cycles, hotspots, layer boundaries, and diagram drift without mutating the workspace:

```sh
ARCHIFY_UPDATE_CHECK_DISABLED=1 node ~/.opencode/skills/archify/bin/archify.mjs code-analysis check <repo-root> --out /tmp/takt-archify-<slug> --json
```

- Always write `--out` under `/tmp/...`, never inside the repository workspace.
- Inspect `investigation.leads`, `investigation.architecture`, `ruleCounts`, and `coverage` in the JSON receipt, and read the cited source files before stating findings. Use `code-analysis task <out-dir> --finding <id> --root <repo-root> --format json` for finding excerpts, or `code-analysis drift` when comparing two commits against an authored architecture IR.
- Static findings are structural signals, never proof of runtime failure or a verified fix.
- Never pass `--update-baseline`, `--accept`, or `--revoke` without explicit user approval. Report missing language toolchains or parse gaps honestly.

## 2. Diagrams, workbenches, and explainers (authoring roles only)

For interactive HTML diagrams, `code-analysis prepare`, or explainers, read `~/.opencode/skills/archify/SKILL.md` for schemas, `finalize`, and delivery gates.

- **Enforce `[archify explainer: on-demand]`:** never generate unsolicited explainer pages for routine answers, status updates, or internal agent-to-agent handoffs; answer in prose or a Takt memory entry instead.
- Write authored artifacts under `.archify/<type>-<slug>-<timestamp>/` (or the user's requested path), never inside an active VFS claim or secret-bearing path.
- Report exact output paths and state clearly if Chrome/Chromium was unavailable so `browser-check` did not run.
