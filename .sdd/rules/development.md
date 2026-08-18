# Development rules

These requirements are non-negotiable. Every code change must satisfy them,
even when the test suite passes.

They apply to planning, implementation, tests, generated assets, and review.

- **Product contract first**: make supported agents, platforms, commands, and
  installation behavior agree across detection, installation, injection,
  documentation, and doctor output. Do not claim support from a binary name or
  a registration entry alone.
- **Single owner**: extend the existing adapter, injector, asset, resolver, or
  update owner. Do not add parallel paths for the same agent configuration or
  installation concern.
- **Managed configuration**: preserve user-owned content. Use the established
  managed block or merge strategy, keep writes idempotent, and never write one
  agent's format or directory from another agent's integration.
- **Observable lifecycle**: installation, download, update, state, and cleanup
  paths must report their actual outcome. Do not claim success before the
  executable, configuration, or required assets are available; do not hide
  partial failure or data-loss risk with fallback behavior.
- **Asset contract**: embedded assets, generated files, and golden fixtures are
  output contracts. Change their source or generator deliberately and verify
  the resulting installed, injected, or rendered output. Never update a golden
  fixture merely to silence a failure.
- **Minimal change**: make atomic edits for the requested behavior only. Do not
  disturb unrelated worktree changes, perform bulk rewrites, or add cleanup,
  normalization, fallback logic, configuration switches, or abstractions that
  the task does not require.
- **Evidence-based tests**: test user-visible behavior at the component
  boundary. For changes to installation, injection, update, state, or assets,
  assert the produced file, command result, or rendered output rather than an
  internal implementation detail.
- **Verification**: run focused package tests for the changed behavior. Run
  `go test ./...` when a supported-agent, generated-contract, CLI lifecycle,
  or other cross-cutting behavior changes; report any check that cannot run.
