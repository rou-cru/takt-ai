---
name: takt-archify
description: Analyze a repository's architecture headlessly with Archify when you need coupling, cycles, hotspots, layer boundaries, or new imports between two commits measured against an authored diagram.
license: AGPL-3.0
metadata:
  author: takt
  version: "1.1"
  component: archify
---

# Takt Archify

Run Archify through Takt's launcher. It keeps analysis output in a private scratch, keeps the installed version fixed, and prints every follow-up command ready to run:

```sh
node ~/.opencode/skills/takt-archify/scripts/archify.mjs code-analysis check <repo-root>
```

| Need | Command after the launcher |
| --- | --- |
| First structural look at a repository | `code-analysis check <repo-root>` |
| Evidence and excerpts for one lead or signal | the `task` command printed with it |
| New imports between two commits, against an authored diagram | `code-analysis drift <repo-root> --base <sha> --head <sha> --ir <architecture.json>` |

- **Read the cited source before stating a finding.** Start from `investigation.leads`, `investigation.architecture`, and `ruleCounts` in the JSON.
- **A finding is a structural signal**, not proof of a runtime failure or of a fix.
- **Report what the analysis could not see.** Name the files or languages `coverage` lists as unparsed or missing a toolchain.
- **Baseline changes are the user's decision.** The launcher refuses `--update-baseline`, `--accept`, and `--revoke` and prints the command to run once the user approves.
