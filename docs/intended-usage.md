# Intended Usage

← [Back to README](../README.md)

---

This page explains how takt-ai is meant to be used. Not the flags, not the architecture -- just the mental model. If you read one page besides the README, make it this one.

---

## After Installing -- You're Done

Once you run `takt-ai` and complete the install flow, everything is configured. There is nothing else to do. No commands to memorize, no workflows to learn, no config files to edit.

Open your AI agent and start working. That's it.

---

## Engram (Memory) -- Automatic

Engram is persistent memory for your AI agent. It saves decisions, discoveries, bug fixes, and context across sessions -- automatically. The agent manages all of it via MCP tools (`mem_save`, `mem_search`, etc.).

**Day-to-day: you don't need to do anything.** The agent handles memory automatically.

For full documentation: [github.com/Takt-Programming/engram](https://github.com/Takt-Programming/engram)

---

## The Crew -- Specialists, Not One Monolithic Agent

Takt configures OpenCode with a team of specialist agents: an orchestrator
(`takt`) plus specialists for planning, specification, architecture,
implementation, fixing, verification, judging, and simplification. The crew
roster and what each role may change are defined once in [the catalog
README](../takt/catalog/assets/README.md).

- **Small request?** Just ask. The orchestrator handles it directly.
- **Substantial work?** The orchestrator delegates to the right specialist,
  with staged file operations and verification gates handled by the harness.
- You review and approve at key decision points. The orchestrator keeps the
  direction; specialists do the focused work.

Every specialist persists its artifacts through Engram before returning, so
the next one can pick up exactly where the previous one left off, even across
sessions.

---

## Skills -- Deployed, Then Loaded on Demand

Takt deploys **SDD skills** (bounded planning, handoffs, workflow selection,
recovery) and **memory skills** for each role directly from its catalog. They
install to `~/.opencode/skills/` and OpenCode discovers them natively: skills
appear as lightweight descriptors and load on demand via the `skill` tool.
There is no refresh step, no index file, and nothing for you to activate.

For **coding skills** (React, Angular, TypeScript, Go testing, etc.), the
community maintains a separate repository:
[Takt-Programming/Takt-Skills](https://github.com/Takt-Programming/Takt-Skills).
You install those manually by cloning the repo and copying the skills you
want into `~/.opencode/skills/` — OpenCode picks them up the same way:

```bash
git clone https://github.com/Takt-Programming/Takt-Skills.git
cp -r Takt-Skills/curated/react-19 ~/.opencode/skills/
```

You can also add your own skills there. OpenCode discovers them natively;
never register or index skills manually.

---

## Maintenance Cycles -- GC, Not You

Takt's harness can run workspace GC cycles: a bounded collector pass that
proposes removals (dead code, duplication, documentation debt) as
**candidates, not mutations**. An independent verification gate judges the
delta before anything is accepted, and findings can be refuted with evidence.
You never run this by hand; the crew coordinates it, and `takt-ai gc` is its
machine interface.

---

## The Golden Rule

Takt AI is an ecosystem **configurator**. It sets up your AI agent with
memory, skills, a specialist crew, and deterministic guardrails -- then gets
out of the way.

The less you think about takt-ai after installing, the better it's working.

---

## Quick Reference

| Do                                                         | Don't                                                                             |
| ---------------------------------------------------------- | --------------------------------------------------------------------------------- |
| Run the installer and follow the flow                      | Manually edit the generated config files                                          |
| Just start coding with your AI agent                       | Memorize commands or agent rosters                                                |
| Let the orchestrator delegate when work is substantial     | Force ceremony on every small task                                                |
| Trust that Engram is saving context for you                | Dig into Engram's storage                                                         |
| Add your own skills under `~/.opencode/skills`             | Register or index skills manually                                                 |
| Re-run the installer (or `setup sync`) after an upgrade    | Manually patch skill files or agent instructions                                  |
