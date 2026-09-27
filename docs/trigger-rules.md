# Agent Trigger Rules

<- [Back to README](../README.md)

---

takt-ai injects a **trigger-rules** section into every supported agent's system prompt or orchestrator configuration. This section is a set of organic recommendations that guide the AI orchestrator on when to run review and verification agents during the development workflow.

## What Are Trigger Rules?

Trigger rules are **organic recommendations, not hard gates**. takt-ai renders the rules as plain instruction text and injects them into the agent's prompt; the AI orchestrator decides when and how to act on them. takt-ai never fires, blocks, or executes any rule itself.

The injected section looks like this in your agent's system-prompt file:

```markdown
<!-- takt-ai:trigger-rules -->
## Agent Trigger Rules

These are organic recommendations, not enforced checkpoints. takt-ai only
renders this text; the AI orchestrator decides when to act on it.

- At **pre-commit**, always, consider running `review-readability`. ...
- At **pre-pr**, when the diff touches auth/update/security OR exceeds 400
  changed lines, **strongly recommend** running all four 4R lenses in parallel.
- ...
<!-- /takt-ai:trigger-rules -->
```

## Where the Section Is Injected

| Agent | Location |
|-------|----------|
| Kiro | `~/.kiro/steering/takt-ai.md` (marker section) |
| Hermes | `~/.hermes/SOUL.md` (marker section) |
| OpenCode | `opencode.json` → `agents.takt-orchestrator.system` (inline) |

## Default Rule Tiers

The built-in default set follows a three-tier cost model:

**Tier 1 — Advisory (everyday events)**

- `pre-commit` and `pre-push`: run `review-readability` as a single advisory lens.
  Cost: ~1x. Keeps the everyday loop lightweight.

**Tier 2 — Strong (hot paths / large diffs)**

- `pre-pr` on `**/auth/**`, `**/update/**`, `**/security/**`, or `**/payments/**` paths, OR when the diff exceeds 400 changed lines: run all four 4R review lenses (`review-risk`, `review-resilience`, `review-readability`, `review-reliability`) in parallel.
  Cost: ~4x. Reserved for high-risk changes.

**Tier 3 — Strong (high-stakes SDD phases)**

- `post-sdd-phase` after the `design` or `apply` phase: run `judgment-day` adversarial verification.
  Cost: ~4 + 3×findings. Reserved for the SDD phases most likely to introduce architectural debt.

**No built-in binding for `on-ci` and `on-schedule`** — the appropriate agent and cadence for CI and scheduled runs are installation-specific. Both events are part of the supported event vocabulary and can be used in a future override mechanism.

## Refreshing the Injected Section

Re-run install or sync after an update to refresh the injected section:

```bash
takt-ai install   # full install
takt-ai sync      # re-sync only (faster)
```

The injection is idempotent — running it twice replaces the existing section without duplication.
