# CI and releasing

## CI (`.github/workflows/ci.yml`)

Every pull request and every push to `main` or `stage` runs the jobs below. `ci-required` is the single required check: it passes only when all of them succeed.

| Job | What it gates |
|---|---|
| `quality` | gofmt, build and vet, golangci-lint, plugin typecheck and lint, shellcheck, actionlint |
| `test-go-host`, `test-go-container`, `test-js`, `coverage` | Go and JS tests and the combined coverage gate |
| `e2e-container` | Setup CLI end-to-end contract; Grype report on the e2e image (informational) |
| `sast` | Semgrep (`.github/workflows/sast.yml`, filtered by `.semgrepignore`); also runs weekly |
| `secrets` | TruffleHog, verified credentials only, over the commits the run adds |
| `sca` | Syft SBOM of the source tree; Grype fails on high or critical vulnerabilities with a fix |
| `iac` | Helm lint and render, then Checkov over Dockerfiles, workflows and the rendered manifests (`.checkov.yaml`) |
| `release-check` | GoReleaser snapshot, so the release cannot break on `main` |
| `sonarqube` | SonarCloud analysis with Go and JS coverage |

Semgrep and Grype SARIF land in code scanning under the categories `semgrep`, `grype-source` and `grype-image`. CodeQL is not used: keep its "default setup" disabled in the repository settings.

## Release (`.github/workflows/release.yml`)

A release starts when CI succeeds on a push to `main`, or by hand through `workflow_dispatch` with an exact `version`.

1. `version` computes the next version with `development/release/next-version.sh`, from the squash-merge subjects (conventional commits) since the highest `v*` tag:
   - `docs`, `ci`, `test` and `chore` alone, or a `Release: skip` trailer, skip the release;
   - a `Release-As: vX.Y.Z` trailer forces a version.
2. The same job creates the annotated tag with `development/release/create-tag.sh`.
   - GitHub keeps the tag of a published-then-deleted release reserved, and git cannot see it. When GitHub refuses the tag, the script bumps the patch and retries; the version it actually tags is the one every later job uses.
   - On a skip, nothing else runs.
3. With the tag in place, three tracks publish independently:
   - `goreleaser`: archives, Linux packages, SBOMs (Syft) and the Homebrew cask, a Cosign keyless signature over `checksums.txt` (verified right after signing), and build provenance attestations. `publish` then turns the draft release into the published one.
   - `image` and `image-publish`: a multi-arch workspace image on Docker Hub and GHCR, signed and verified with Cosign, with a Grype report in code scanning.
   - `chart`: the Helm chart on GHCR, signed and verified with Cosign, and on the gh-pages index.

A failed tag stops every publishing job, so a chart or image is never published for a version without a release.
