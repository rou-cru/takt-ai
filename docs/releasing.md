# Releasing

Every merge to `main` that passes CI is released automatically by
`.github/workflows/release.yml`. There is no release branch and no manual tag.

## Flow

1. **CI** runs on the push to `main`. Release starts only when CI succeeds (`workflow_run`).
2. **version**: `development/release/next-version.sh` computes the next version from the commits since the last `v*` tag. If there is nothing to release, everything else is skipped.
3. **image**: the workspace image (`deploy/workspace/Dockerfile`) is built on native `amd64` and `arm64` runners and pushed **by digest only**, with BuildKit SBOM and `mode=max` SLSA provenance. A build failure stops the release before anything public exists.
4. **tag**: the annotated tag `vX.Y.Z` is created on the released commit.
5. In parallel:
   - **goreleaser** builds the binaries and packages, generates SBOMs, signs `checksums.txt` with cosign (keyless), attests build provenance, and opens a **draft** GitHub release.
   - **image-publish** tags the multi-arch manifest in Docker Hub and GHCR, signs it with cosign, scans it with grype (report only, results go to code scanning), and refreshes the Docker Hub description.
6. **chart** packages `charts/takt-ai` with `version = appVersion = X.Y.Z`, pushes and signs it at `oci://ghcr.io/rou-cru/charts`, attaches the `.tgz` to the release, and updates the Helm index on `gh-pages`.
7. **publish** makes the draft public (and `latest`) only when all of the above succeeded. `install.sh` reads `releases/latest`, so it never sees a partial release.

If a job fails after the tag exists, the release stays a draft. Re-run the failed jobs: every step is idempotent for the same version.

## Artifacts per release

| Artifact | Where |
| --- | --- |
| `takt-ai_X.Y.Z_{linux,darwin}_{amd64,arm64}.tar.gz` | GitHub release |
| `.deb`, `.rpm`, `.apk`, Arch `.pkg.tar.zst` (amd64, arm64) | GitHub release |
| Source tarball `takt-ai_X.Y.Z_source.tar.gz` | GitHub release |
| SPDX SBOM per archive and for the source (`*.sbom.json`) | GitHub release |
| `checksums.txt` + `checksums.txt.sigstore.json` (cosign bundle) | GitHub release |
| Build provenance attestations for binaries and packages | GitHub attestations |
| Homebrew cask `rou-cru/takt-ai/takt-ai` (only when `HOMEBREW_TAP_TOKEN` is set) | `rou-cru/homebrew-takt-ai` |
| Image `X.Y.Z`, `X.Y`, `X` (from 1.0), `latest`, `sha-<commit>`, signed, with SBOM and provenance | `docker.io/roucru/takt-ai`, `ghcr.io/rou-cru/takt-ai` |
| Helm chart `takt-ai` (signed OCI) | `oci://ghcr.io/rou-cru/charts/takt-ai` |
| Helm chart (classic repository) | `https://rou-cru.github.io/takt-ai` and the release `.tgz` |

## Version rules

The squash-merge subject on `main` is the PR title, which `.github/workflows/pr-title.yml` requires to be a conventional commit (`type(scope)!: summary`).

| Commits since last tag | `0.x` | `>= 1.0` |
| --- | --- | --- |
| No tag yet | `v0.0.1` | — |
| Breaking (`type!:` or `BREAKING CHANGE:`) | minor | major |
| `feat:` | patch | minor |
| Anything else | patch | patch |
| Only `docs:`, `ci:`, `test:`, `chore:` | no release | no release |

Before 1.0, features bump the patch on purpose: minor jumps such as `0.1.0` happen only when you ask for them.

Trailers in the merge commit body override the rules:

- `Release-As: v0.1.0` releases exactly that version.
- `Release: skip` releases nothing for this range. Those commits then ship with the next release.

A manual run (**Actions → Release → Run workflow**) with a `version` input releases the current `main` commit as that exact version. With an empty input it applies the rules above.

Check what the next version would be locally:

```sh
./development/release/next-version.sh
```

## Verifying a release

```sh
ID='^https://github.com/rou-cru/takt-ai/.github/workflows/release.yml@'
ISSUER=https://token.actions.githubusercontent.com

cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "$ID" --certificate-oidc-issuer "$ISSUER" checksums.txt
sha256sum --ignore-missing -c checksums.txt
gh attestation verify takt-ai_X.Y.Z_linux_amd64.tar.gz --repo rou-cru/takt-ai

cosign verify docker.io/roucru/takt-ai:X.Y.Z --certificate-identity-regexp "$ID" --certificate-oidc-issuer "$ISSUER"
cosign verify ghcr.io/rou-cru/charts/takt-ai:X.Y.Z --certificate-identity-regexp "$ID" --certificate-oidc-issuer "$ISSUER"
docker buildx imagetools inspect docker.io/roucru/takt-ai:X.Y.Z --format '{{ json .SBOM }}'
```

`install.sh` verifies the checksum always, and the cosign bundle whenever `cosign` is on `PATH`.

## One-time repository setup

- Secrets: `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN` (Read/Write/Delete). Updating the Docker Hub description also needs an admin-scoped token; the step tolerates a failure. `HOMEBREW_TAP_TOKEN` is optional: without it the cask is skipped.
- Settings → General → Pull Requests: allow squash merging and set the default commit message to **Pull request title and description**.
- Settings → Pages: deploy from the `gh-pages` branch. The first release creates the branch.
- Tag rulesets on `v*`, if any, must allow `github-actions[bot]`.
- After the first release, make the GHCR packages `takt-ai` and `charts/takt-ai` public.

## Local checks

```sh
make release-check      # goreleaser check
make release-snapshot   # full build into dist/ without publishing (needs syft)
docker buildx bake workspace --set '*.platform=linux/amd64' --load
```
