# Repository Guidelines

## Project structure & module organization

Takt AI is a Go integration layer for OpenCode. `takt/cli/` contains the executable, `takt/tui/` the Bubble Tea interface, and other `takt/` packages own setup, dispatch, memory, and runtime behavior. Embedded agent and skill content lives in `takt/catalog/assets/`; OpenCode plugins live in `takt/agents/opencode/assets/`. Tests sit beside source files, with fixtures in `testdata/`. `development/` holds tooling, `docs/` documentation and artwork, `product/` specifications and governance, and `deploy/` and `charts/` deployment resources.

## Mental model for harness and agent changes

Apply this model **every time** you change agents, prompts, skills, permissions, or harness behavior. Start from the owning contracts in [product/INDEX.md](product/INDEX.md), not assumptions about current code.

- The user defines value and retains decision authority. Agents contribute specialized judgment; the orchestrator coordinates that judgment rather than replacing available specialists. Preserve scoped user overrides and avoid unsolicited restrictions ([Constitution](product/governance/CONSTITUTION.md)).
- Instructions guide normal behavior; deterministic harness controls back critical limits. Neither prose grants a blocked capability nor a runtime block determines normative authority. Make the intended path fluent, not repeated trial-and-error against denials ([harness contract](product/meta-harness/harness/PRD_HARNESS.md)).
- Model crew execution as feedback control: agents do the work, sensors record evidence, the control plane evaluates policy, and actuators apply the minimum sufficient correction. Control constrains execution without substituting for semantic judgment. Measure enforcement windows and budgets in recorded work, not elapsed time; distinguish missing telemetry from agent failure ([observability contract](product/meta-harness/observability/PRD_OBSERVABILITY.md)).
- Keep the installation application's TUI/navigation separate from the installed crew's execution model. Similar FSM patterns do not imply shared controllers, clocks, or state.
- Design context through Relevance, Recency, Ranking, and Retrieval. Keep role-appropriate static context small, retrieve skills on demand, resolve instruction conflicts, and preserve judgment and safety rather than optimize token counts alone ([context architecture](product/meta-harness/context/ARCH_CONTEXT.md)).
- Edit the [canonical catalog](takt/catalog/assets/README.md), not independently maintained installed prompts. Preserve identity, permissions, and skills through projection. `agent.yaml` is metadata, not prose; context composes BASELINE → PERSONA → SOUL → OPERATIONS without implying an authority hierarchy.
- Use OpenCode's adequate native mechanisms first; add Takt-owned extensions only for missing capabilities. Verify observable end-to-end behavior, not identical model outputs or merely registered artifacts ([platform contract](product/meta-harness/platforms/PRD_PLATFORMS.md)).

## Build, test, and development commands

Use Go as pinned in `go.mod` and Node as specified in `CONTRIBUTING.md`.

- `go build ./...`: compile all Go packages.
- `go run ./takt/cli`: launch the interactive TUI in a terminal.
- `make dev`: open a disposable Docker development environment.
- `make test-host`: run classified host-safe tests with race detection and coverage.
- `make test-containerized`: run filesystem-mutating and external-binary tests inside Docker.
- `make test-shell`: run offline shell regressions.
- `make lint`: run `golangci-lint`.
- `make docs`: regenerate `docs/api/` from Go comments.

Always use `pnpm`, not `npm`, for JavaScript development. Use `bun` for Bun-specific tasks, including plugin tests. From the repository root:

```sh
pnpm --dir takt/agents/opencode/assets install --ignore-scripts
pnpm --dir takt/agents/opencode/assets run typecheck
pnpm --dir takt/agents/opencode/assets run lint
```

## Coding style & naming conventions

Follow [CONTRIBUTING.md](CONTRIBUTING.md) and [DOCTRINE.md](product/governance/DOCTRINE.md). The rules below summarize those sources; product requirements govern required behavior, while the doctrine governs implementation structure.

### Formatting and language conventions

- Format Go with `gofmt`, using tabs. Use lowercase package names, exported `PascalCase` symbols, and unexported `camelCase` symbols.
- Match existing TypeScript's two-space indentation. Keep strict typechecking enabled and run ESLint. Text files use LF endings through `.gitattributes`.

### Structure, contracts, and scope

- Give each fact, rule, and structure one definitional home. Reference that home instead of duplicating definitions or adding reconciliation layers (`STR-1`).
- Reject invalid states where they first become representable. Validate and normalize external, user-supplied, or inferred data when acquired (`STR-2`, `STR-4`).
- Producers deliver the promised data shape, ready for use. Consumers trust the contract instead of re-validating or re-normalizing its guarantees (`STR-3`).
- Reuse or extend the existing adapter, injector, resolver, asset, or update owner before creating an equivalent (`STR-6`).
- Implement present requirements only. Do not add speculative interfaces, single-product factories, fixed-value configuration, unnecessary fallbacks, or unrelated cleanup. Make atomic, scoped edits (`STR-5`).

### State and concurrency

- Every decision reads its fact's sole owner store when acting. Caches, mirrors, projections, and second writers cannot authorize, refuse, or block work; remove competing authority rather than synchronize it (`STR-15`, no exceptions).
- Put decision reads and their authorized action inside the same lock, queue, or transaction. Avoid check-then-act across calls or asynchronous gaps. Bound contention waits, then refuse with the retry call rather than proceed unserialized (`STR-16`).

### Configuration and observable output

- Preserve user-owned content through established managed blocks or merges. Keep writes idempotent and each integration within its own format and directory.
- Keep support claims consistent across detection, installation, injection, documentation, and doctor output.
- Report actual installation, download, update, state, and cleanup outcomes. Verify required executables and assets before reporting success; surface partial failures and data-loss risks.
- Treat embedded assets, generated files, and golden fixtures as output contracts. Change their source or generator and verify deployed or rendered output.

### Skill authoring

- Follow [catalog conventions](takt/catalog/CONVENTIONS.md). Give each skill one responsibility: routing, flow, or exception. An exception pairs with one flow and stays shorter (`STR-11`).
- Reference only whole, more-specific skills in the permitted downward direction. Avoid reciprocal references or prescribing another role's procedure (`STR-12`).
- Declare a real consumer for every skill (`STR-13`). Every prohibition states its replacement or how to choose a valid alternative (`STR-14`).
- Use `-recovery` for retrying or backtracking committed work, and `-exceptions` for live-protocol deviations. Write situation-led descriptions under about 120 words, without a `Trigger:` prefix. Prefer lists or tables for enumerable content.

## Testing guidelines

Use Go's `testing` package, `*_test.go` files, and `TestName` functions. New or touched tests must use external `<package>_test` packages and exported APIs. Assert observable output; do not change golden fixtures merely to silence failures. Classify new packages in `development/testing/test-packages.sh`. CI requires at least 80% combined Go statement coverage. Run plugin tests with `bun test` from the assets directory.

## Commit & pull request guidelines

Follow history's Conventional Commit style, such as `fix(tui): handle empty selection`. Branch from and target `trunk`; `main` is the release branch. Use a conventional PR title, explain what changed and why, and list executed checks and checks that could not run. Follow `.github/PULL_REQUEST_TEMPLATE.md`; `ci-required` must pass.

## Agent-specific instructions

When `.codegraph/` exists, use `codegraph explore "<symbol or question>"` before searching or reading code. Keep changes scoped and preserve unrelated worktree edits. Read `CONTRIBUTING.md` for the full engineering practices.
