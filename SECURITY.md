# Security Policy

## Supported Versions

Only the latest release receives security fixes.

## Reporting a Vulnerability

Please do not open a public issue for security problems.

Report privately through
[GitHub private vulnerability reporting](https://github.com/rou-cru/takt-ai/security/advisories/new).
Include the affected version, steps to reproduce, and the impact you observed.

If you cannot use GitHub, write to rc@roura.xyz.

Reports are triaged by the maintainers, who will work to resolve them as soon as
possible. No response or fix times are guaranteed. Confirmed vulnerabilities are
fixed in a new release and disclosed through a published security advisory.

## Verifying releases

Every release is built and signed by this repository's release workflow, with
keyless [Sigstore](https://www.sigstore.dev) signatures. Download the archive,
`checksums.txt` and `checksums.txt.sigstore.json` from the
[release](https://github.com/rou-cru/takt-ai/releases), then verify:

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/rou-cru/takt-ai/.github/workflows/release.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum --ignore-missing -c checksums.txt
gh attestation verify takt-ai_<version>_linux_amd64.tar.gz --repo rou-cru/takt-ai
```

The signature covers `checksums.txt`, so the checksum check extends it to every
archive listed there. `gh attestation verify` checks the build provenance of an
archive.

The Docker image on Docker Hub and the image and Helm chart on GHCR are signed
with the same identity. To verify the Docker Hub image:

```sh
cosign verify docker.io/roucru/takt-ai:<version> \
  --certificate-identity-regexp '^https://github.com/rou-cru/takt-ai/.github/workflows/release.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

`install.sh` runs the signature check on `checksums.txt` automatically when
`cosign` is installed. If the signature cannot be downloaded it stops, unless
`--insecure` is passed. Without `cosign` it still verifies the SHA-256 checksums.

## Security design

Takt AI is built to avoid creating security gaps, within these limits:

- **No telemetry.** Takt AI sends no usage data. Its only network traffic is
  to the local memory service on the same machine, and the download of the
  pinned Engram release during installation, checked against a fixed SHA-256.
- **Sensitive files blocked by default.** Agent reads and writes are denied for
  obvious secret-bearing paths such as `.env`, `.ssh/**`, `.aws/credentials`,
  `*.pem`, `*.key` and `secrets/**`.
- **Privileged operations belong to the orchestrator.** Consolidating staged
  work into the workspace and discarding it are reserved to the orchestrator,
  the agent you talk to directly and the one you can see and control most.
- **Dependencies are kept current** to pick up security patches. Dependabot
  proposes updates weekly for Go modules, npm packages, Docker images, the Helm
  chart and GitHub Actions.

## Local event record

Takt AI keeps a record of its own activity for observability, in
`.takt-ai/events.db` inside each workspace. It is a SQLite file readable and
writable only by your user, and it never leaves your machine: nothing sends it
anywhere.

- **What it holds.** Metadata about what happened, in two tables: events
  (file staging, collisions, dispatch decisions, work-unit transitions, tool
  activity, model token and cost usage, garbage-collection findings and
  problem rates) and control actions. Each row carries the session, the acting
  agent, the work unit and attempt, and a reference to the execution history.
- **What it never holds.** File contents, prompts, model responses, secrets or
  credentials. Keys such as `content`, `prompt`, `response`, `token` and
  `password` are rejected, most event types accept only a fixed list of keys,
  and nested values are refused.
- **What it can reveal.** Tool activity can include the path of the file a tool
  touched, and usage events name the provider and model. Treat the file as
  private to the workspace.
- **Who reads it.** Only the orchestrator is granted access; other agents are
  denied read and edit permission. A shell that runs natively, without the
  sandbox, is not covered by that permission.
- **Retention.** Takt AI does not delete or rotate it. Remove it by deleting
  `.takt-ai/events.db` when no session is running; it is recreated on demand.

## Limits

These are efforts, not guarantees. Takt AI cannot take responsibility for:

- the behavior of a model provider or of a model itself;
- zero-day vulnerabilities;
- vulnerabilities that reach the project indirectly through deep supply-chain
  dependencies.

Keeping every dependency and tool up to date is a best-effort commitment. When a
package or tool ships a breaking change, upgrading can take time for testing and
adaptation, and a security patch may be missing until then. The project has
limited maintainer time and cannot promise that everything is always in its
ideal security state.

## Transparency

Because the project cannot offer stronger guarantees, it publishes the evidence
so you can judge and act on your own concerns. These checks are standard
practice, and they are also there for you:

- **SBOMs.** Each release attaches an SPDX SBOM (`*.sbom.json`) for every
  archive and for the source tarball. The Docker image carries BuildKit SBOM
  and SLSA provenance attestations.
- **Signatures and provenance.** Cosign signatures and build provenance
  attestations, as described in "Verifying releases".
- **Scanning in CI.** Every pull request and every push to `main` or `trunk`
  runs Semgrep (static analysis), TruffleHog (verified secrets), Syft and Grype
  (SBOM and vulnerabilities, failing on high or critical issues with a fix) and
  Checkov (infrastructure as code). Semgrep and Grype results are reported in
  the repository's code scanning.

Use these to review a release, check a specific dependency or decide whether a
version fits your risk tolerance.
