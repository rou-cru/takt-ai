# takt-ai workspace image

Disposable, ready-to-use [Takt AI](https://github.com/rou-cru/takt-ai) + OpenCode v2 workspace. It serves the OpenCode web UI and API on port `4096` and bootstraps a `Workspace` session for `/workspace`.

- Platforms: `linux/amd64`, `linux/arm64`
- Tags: `X.Y.Z`, `X.Y`, `latest`, `sha-<commit>` (mirrored at `ghcr.io/rou-cru/takt-ai`)
- Runs as the non-root user `takt` (UID/GID 10001)

## Scope of this directory

This README documents the workspace image, which is a generic product artifact.

The deployment helpers next to it are generic: `helm.sh` wraps `helm install|upgrade|uninstall charts/takt-ai` with a default namespace, and `namespace.yaml` declares a restricted Pod Security Namespace. Neither file names a cluster, account, domain, or any other environment-specific value, and no environment-specific values are added here. The generic paths do not use them: the Helm chart is documented in [charts/takt-ai/README.md](../../charts/takt-ai/README.md) and the Coder workspace template in [deploy/coder/README.md](../coder/README.md); neither consumes these files or copies them into its examples.

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

## Coder workspaces

To give each person their own managed workspace through [Coder](https://coder.com), use the [Coder workspace template](../coder/README.md). That template starts the Pod with the Coder agent's command instead of this image's `workspace-entrypoint`, so the HTTP server on port `4096` and its generated password are not used, and you run `opencode` in the workspace terminal.

Overriding the entrypoint does not change the image: OpenCode and the Takt setup baked under `/home/takt` at build time stay installed, and nothing reruns or removes them at startup.

## Verify

```sh
cosign verify roucru/takt-ai:<version> \
  --certificate-identity-regexp '^https://github.com/rou-cru/takt-ai/.github/workflows/release.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
docker buildx imagetools inspect roucru/takt-ai:<version> --format '{{ json .SBOM }}'
docker buildx imagetools inspect roucru/takt-ai:<version> --format '{{ json .Provenance }}'
```

License: AGPL-3.0-or-later.
