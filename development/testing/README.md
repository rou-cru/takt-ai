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
