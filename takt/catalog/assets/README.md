# Takt declarative catalog

This is the single editable source for agent and skill content. Go loaders live
outside this directory. `agent.yaml` declares identity, instances, role, native
settings, context files, and on-demand skill links; it must not contain prompt prose.

OpenCode installs this tree once at `~/.config/opencode/takt/`. Its native JSON
registers each instance with direct file references in this order:
BASELINE → PERSONA → SOUL → OPERATIONS. Only undeclared optional files are omitted.
This order does not introduce an authority hierarchy. Both judge instances share
one package. Skills are discovered from the absolute installed `skills` directory
and are not injected into initial prompts. Resource bytes are copied unchanged.

Skill directories match their frontmatter `name`. References between skills are
relative to the containing skill directory. Declare executable scripts as a list
under frontmatter `metadata.executables` (for example `scripts/check.sh`); these
install as 0755. All other files install as 0644. Paths must stay inside packages.

The native registry is published after package dependencies. Editing an installed
Markdown file takes effect on a new OpenCode load, not necessarily an active
session. Missing declared context is an installation/diagnostic error. No global
Takt AGENTS.md or second compiled prompt is generated.

## VFS by role

| Role | VFS tools | How it changes the workspace |
| --- | --- | --- |
| orchestrator | claims, `vfs_discard`, `vfs_consolidate` | native edit outside claimed paths; assigns, discards and consolidates staged work |
| planning_author, direct_interlocutor | none | native edit, delivering documents to the paths named |
| execution (dev, fix, simplify) | bind, write, read, delete | staged, never native; shell only under a bound identity; simplify also collects inside a GC cycle's authorized scope when attached |
| verification (verify) | bind, read, verify | none; judges staged work when assigned a gate, including GC deltas |
| judge (judge-a, judge-b) | none | none |

The permission rules live in `takt/agents/opencode/renderer.go`; this table and the
agents' `OPERATIONS.md` and skills must agree with them.
