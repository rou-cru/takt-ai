# Test runners

- `make test-packages` compares `go list ./...` with the exact host/container
  package lists in `development/testing/test-packages.sh`. New, removed, or duplicated
  packages fail the check. Classify new packages explicitly; do not add wildcard
  patterns that would silently put future integration tests on the host.
- `make test-host` checks classification before running host-safe tests. This is
  also the default `make` target. Tests that execute external binaries or mutate
  real user state belong in the container list. OpenCode's Node integration test
  is container-only; the API adapter's injected-runner tests and logo transform
  tests are host-safe.
- `make test-containerized` copies source through stdin into a disposable Go +
  Node 24 container. No host bind mounts or Docker volumes are mounted; all
  module and build caches are disposable. Classification is checked inside the
  container (no host Go required).
  Images use the Docker daemon's native architecture rather than forcing amd64.
- `development/testing/test-containerized.sh [go test flags and packages...]` preserves
  argument boundaries. Flags-only invocations retain the container package list;
  explicit packages replace it. Values after `-args` are forwarded to the test
  binary, not interpreted as packages. Common value-taking Go flags support both
  `-flag value` and `-flag=value`; use the latter for newly added Go flags.
  A relative `-coverprofile 'coverage file.out'` or `-coverprofile=file.out` is
  copied back to the repository only after success, then the container is removed.
- `make test-shell` runs offline regression fixtures with fake Docker/downloads,
  real archive/checksum validation, and temporary install/home directories. It
  does not invoke a Docker daemon, contact GitHub, or install into a user path.

The installer accepts any archive size. Checksum verification, successful
extraction, and presence of `takt-ai` determine validity, not a byte threshold.

## Coder workspace acceptance runners

The optional Coder mode of `charts/takt-ai` and the `deploy/coder` template are
checked by location key. Each key names one runner. Tier boundaries follow
the frozen acceptance specification (T1 static/render mock, T2 disposable
conformant cluster, T3 the operator's real cluster and Backstage). A T1 pass
never certifies T2 or T3.

| Key | Location | Runner | Scope |
|---|---|---|---|
| CH | `development/testing/test-coder-chart.sh` | `development/testing/test-coder-chart.sh` | Offline Helm regression: dependency, mode coupling, native env, RBAC, direct gating, retained PVC claim. Exit 0 pass, 1 fail, 3 UNVERIFIED (Coder dependency not pre-resolved). |
| IN | `deploy/coder/tests/inputs.tftest.hcl` | `terraform test -filter=...` in `deploy/coder` | Template input boundaries and rejected values, mock providers only. |
| WS | `deploy/coder/tests/workspace.tftest.hcl` | `terraform test -filter=...` in `deploy/coder` | Workspace identity, runtime mounts, env, output set, Backstage credential reference, NetworkPolicy, automount. Mock providers only. |
| LC | `deploy/coder/tests/lifecycle.tftest.hcl` | `terraform test -filter=...` in `deploy/coder` | Start/stop compute gating; PVC always retained at its address. Mock providers only. |
| BS | `development/testing/test-coder-bootstrap.sh` | `development/testing/test-coder-bootstrap.sh` | Offline fixture for the workspace bootstrap diagnostic in an isolated temporary tree. |
| SM | `development/testing/test-coder-smoke.sh` | `development/testing/test-coder-smoke.sh --image <local-tag> --name <unique-name>` | Local Docker smoke of the workspace image only; no live Coder, agent, Kubernetes or crew proof. |
| AC | `deploy/coder/ACCEPTANCE.md` | Manual procedure, no shell automation | Live, contract-review and exploratory checks for T1-06..10, T2-01..09 and T3-01..07. Each execution is recorded with its evidence fields. |

Rules for these runners:

- No runner in this list is wired into CI or `make` by this document.
- `terraform test` uses mock providers, so no cluster, registry or credential is
  contacted. Run it from `deploy/coder` after `terraform init` has completed.
- A missing prerequisite is reported as UNVERIFIED, never as a pass.
- Every T2 and T3 scenario needs its AC procedure; the offline runners cannot
  substitute for it.
