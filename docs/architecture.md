# Architecture & Development

← [Back to README](../README.md)

---

## Architecture

```
takt/
  cli/                CLI entrypoint: install/sync, doctor, vfs, dispatch, gc, memory, obs, dag
  catalog/            Declarative crew: agents, shared BASELINE, skills (single editable source)
  agents/opencode/    OpenCode projection: opencode.json renderer, permissions, plugins
  vfs/                Governed staging: claims, bindings, verdict gate, shell sandbox plans
  dispatch/           Admission, plan commitments and revisions, interlocutor stack
  gc/                 Maintenance (graph-cleaner) cycle coordinator
  history/            Execution history and its projection
  memory/ engram/     Engram-backed memory tools and wiring
  obs/                Content-free observability events
  setup/ lifecycle/   Install, sync, ownership, drift, uninstall
  runtime/sandbox/    Sandbox adapter (@anthropic-ai/sandbox-runtime)
  tui/                Bubbletea TUI
development/
  environment/        Local development container
  testing/            Test runners, fixtures, and disposable-container E2E
  quality/            Pinned Go developer tools
  generate-logo/      Branding asset generator
deploy/workspace/     Workspace image, entrypoint, namespace and Helm helper
charts/               Publishable Kubernetes chart (kept at repository root)
```

---

## Testing

```bash
# Host-safe tests
make test-host

# Filesystem-mutating and external-binary tests (requires Docker)
make test-containerized

# Disposable-host lifecycle E2E (requires Docker)
docker build -f development/testing/e2e/Dockerfile -t takt-e2e .
docker run --rm takt-e2e
```

---

## License

AGPL-3.0
