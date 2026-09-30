# takt-ai workspace image

Disposable, ready-to-use [Takt AI](https://github.com/rou-cru/takt-ai) + OpenCode v2 workspace. It serves the OpenCode web UI and API on port `4096` and bootstraps a `Workspace` session for `/workspace`.

- Platforms: `linux/amd64`, `linux/arm64`
- Tags: `X.Y.Z`, `X.Y`, `latest`, `sha-<commit>` (mirrored at `ghcr.io/rou-cru/takt-ai`)
- Runs as the non-root user `takt` (UID/GID 10001)

## Run

```sh
docker run --rm -p 4096:4096 \
  -e OPENCODE_PASSWORD=change-me \
  -e ANTHROPIC_API_KEY \
  -v "$PWD:/workspace" \
  roucru/takt-ai:latest
```

OpenCode v2 always requires Basic Auth as user `opencode`. Without `OPENCODE_PASSWORD`, the entrypoint generates a password and prints it once to the log.

| Variable | Default | Purpose |
| --- | --- | --- |
| `OPENCODE_PASSWORD` | random | Basic Auth password for the server and web terminals |
| `TAKT_WORKSPACE_DIR` | `/workspace` | Project directory the session opens |
| `TAKT_OPENCODE_PORT` | `4096` | Server port |

On Kubernetes, use the Helm chart: `helm install demo oci://ghcr.io/rou-cru/charts/takt-ai`.

## Verify

```sh
cosign verify roucru/takt-ai:<version> \
  --certificate-identity-regexp '^https://github.com/rou-cru/takt-ai/.github/workflows/release.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
docker buildx imagetools inspect roucru/takt-ai:<version> --format '{{ json .SBOM }}'
docker buildx imagetools inspect roucru/takt-ai:<version> --format '{{ json .Provenance }}'
```

License: AGPL-3.0-or-later.
