# Releasing

Every merge to `main` that passes CI is released automatically by
`.github/workflows/release.yml`. There is no release branch and no manual tag.

## Flow

CI on `main` computes one version and tag. Binary, image and chart publishing then run independently: binary waits only for the tag, image publication for its platform builds, and chart publication for the version. A lane's failure does not block the others. The chart archive and index are both served from `gh-pages` at `https://rou-cru.github.io/takt-ai`.

Re-run only the failed lane for the same version.

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
| Helm chart (classic repository) | `https://rou-cru.github.io/takt-ai` (index and `.tgz` on `gh-pages`) |

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
