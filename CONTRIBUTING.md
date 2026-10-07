# Contributing

Thanks for your interest in Takt AI.

## Development setup

- Go 1.27.1 (see `go.mod`) and Node 24.21.0.
- Docker, for `make dev` and `make test-containerized`.
- `golangci-lint`, for `make lint`.
- `goreleaser`, only for `make release-check` and `make release-snapshot`.

| Command | What it runs |
| --- | --- |
| `make test-host` | Go tests that do not mutate user state or run external binaries. Run it before opening a pull request. |
| `make test-containerized` | Tests that mutate the filesystem or run binaries, inside a disposable container. |
| `make test-shell` | Offline regressions for the shell scripts. |
| `make lint` | `golangci-lint` with its default linters, as in CI. |
| `make docs` | Regenerates `docs/api/` from Go doc comments. |
| `make dev` | Opens the disposable development container. |

## Branches and pull requests

`main` is the release branch: every push to it that passes CI can produce a release. `trunk` accumulates the changes pending for the next release and must always be `main` plus those changes.

- Branch from `trunk` and open your pull request against `trunk`.
- Only `trunk` is merged into `main`.

Every pull request title must be a conventional commit, such as `feat(cli): add doctor --json` or `fix!: drop legacy flag`. Allowed types are `feat`, `fix`, `perf`, `refactor`, `revert`, `docs`, `test`, `ci`, `build`, `chore` and `style`; the scope is optional and `!` marks a breaking change. The title becomes the squash-merge subject, and the release version is derived from those subjects (see `docs/releasing.md`).

The `ci-required` check must pass before a merge.

## License

Contributions are licensed under the project's license, [AGPL-3.0-or-later](LICENSE). No sign-off or contributor agreement is required.

## Engineering practices

These practices guide work in this repository when they are relevant. They do
not prescribe a universal planning or delivery workflow.

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
- **Black-box tests**: tests live in an external `_test` package and use only
  the exported API, driving the code through its real environment (`HOME`,
  `PATH`, the working directory, a local test server) rather than swapped
  package variables. Touching a test file means converting it; being already
  white-box is not a reason to add more of the same.
- **Evidence-based tests**: test user-visible behavior at the component
  boundary. For changes to installation, injection, update, state, or assets,
  assert the produced file, command result, or rendered output rather than an
  internal implementation detail.
- **Verification**: run focused package tests for the changed behavior. Run
  `go test ./...` when a supported-agent, generated-contract, CLI lifecycle,
  or other cross-cutting behavior changes; report any check that cannot run.
