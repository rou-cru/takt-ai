# Coder workspace acceptance procedures

This document defines the manual acceptance procedure for the optional Takt AI
Coder workspace feature. It covers the scenarios whose check location is AC in
the traceability matrix (#15116): CW-T1-06 through CW-T1-10, CW-T2-01 through
CW-T2-09, and CW-T3-01 through CW-T3-07. The scenario requirements are defined by
the frozen acceptance specification (#15112). This document defines procedures
and evidence fields only; it does not restate the requirements.

## Status of every execution

Every execution recorded against this document is UNVERIFIED until an independent
verifier records its outcome. No scenario is certified by documentation, by an
offline T1 run, or by a Helm or Terraform success alone.

## Evidence record

Record one entry per scenario and execution with these fields:

- ID: the scenario identifier, for example CW-T2-04.
- Tier: T1, T2, or T3.
- Status: verified, failed, or unverified.
- Evidence reference: the location of the sanitized evidence.
- Release, chart, and template active version.
- Image digest.
- Relevant nonsecret configuration.
- Actual outcome.
- Blocker, if any.

Evidence never includes token bytes, credentials, or Secret payloads. A confirmed
finding is a failed requirement with its evidence, ranked critical, warning, or
suggestion separately from the scenario's must-not-fail rank.

## Procedures

Each section lists the tier, the procedure, and the evidence expected.

### CW-T1-06 — Template input boundary

- Tier: T1
- Check location: IN, WS, and contract review of deploy/coder/variables.tf as the executable definition
- Procedure: Run the IN and WS mock plans. Review the publication-input documentation against variables.tf. Confirm that persist_workspace and persistence_allowed are absent.
- Evidence: Named invalid variable for each rejected input; parameter inventory showing repository_url as the only human rich parameter.

### CW-T1-07 — Persistent runtime, terminal, and safe outputs

- Tier: T1
- Check location: WS, LC, BS; SM for local image corroboration only
- Procedure: Run the WS and LC plans for two owners and a start/stop sequence. Run BS and SM against the built workspace image.
- Evidence: Mock plan output for the seven-value output set; SM and BS logs without credential values.

### CW-T1-08 — Actual-owner credential reference selection

- Tier: T1
- Check location: IN, WS, LC
- Procedure: Run the two-owner positive and negative mock plans, the stop case, and the reserved-name cases.
- Evidence: Plan output showing the actual-owner secretKeyRef; expected-failure messages naming the missing reference.

### CW-T1-09 — Generic product and honest operator documentation

- Tier: T1
- Check location: CH whole-repository scan, plus AC contract and document review
- Procedure: Run the CH scan over the repository's tracked source files. Review the operator documents against #15110 and #15111, listing every prerequisite.
- Evidence: Scan output; reviewer checklist of prerequisites.

### CW-T1-10 — Authentication, scope, and assurance claims

- Tier: T1
- Check location: AC cross-review of guides and reported evidence against the scenarios
- Procedure: Read the user and operator guides against each scenario and the binding scope decisions. Record each claim as supported or unsupported.
- Evidence: Claim review table with the source of each claim. Unsupported readiness or equivalence claims fail the review.

### CW-T2-01 — Native publication and bootstrap

- Tier: T2
- Check location: AC operator procedure on a disposable conformant cluster
- Procedure: Bootstrap the first user, authenticate a Template Admin or Owner CLI session, and run native templates push with the directory, version name, variables file, and activation. Then run the failure cases: bad authentication, invalid variables, provider resolution failure, and import failure.
- Evidence: Import job and version records; the active version and its provider versions; Coder and provider versions; the bootstrap path used; failure outputs; absence of chart publisher credentials.

### CW-T2-02 — Takt-ready terminal and credential failures

- Tier: T2
- Check location: AC runtime procedure with the BS and SM diagnostics as prerequisites
- Procedure: Start a workspace and observe storage binding, image pull, agent connection, and the blocking diagnostic. Open the owner terminal and run a normal Takt/OpenCode crew task. Inject a missing Secret key and the DB, OIDC, route, provider, image, and agent faults.
- Evidence: Pod, agent, and build observations; authenticated terminal session; diagnostic outcome; crew task result; fault records without payloads. Model and runtime prerequisites are recorded separately.

### CW-T2-03 — Authenticated endpoint boundary

- Tier: T2
- Check location: AC route inventory and unauthenticated requests
- Procedure: Inventory every exposed user-facing route. Request each protected route without credentials. Authenticate an ordinary user through OIDC and confirm the owner session.
- Evidence: Route inventory; unauthenticated responses or redirects per route; OIDC session record; TLS and routing configuration. Any unexplained exposed route is a failure.

### CW-T2-04 — Concurrent owners cannot cross workspace boundaries

- Tier: T2
- Check location: AC two-session procedure with harmless per-workspace markers
- Procedure: Run two users, each with a running workspace and owner-specific references, plus a second workspace for one user. Attempt cross-owner file, process, terminal, app, and credential access, and cross-workspace network access.
- Evidence: Session records; denial results per attempt; CNI enforcement evidence; check that no provisioner token or kubeconfig is mounted. Token bytes are not recorded.

### CW-T2-05 — Provisioner namespace confinement

- Tier: T2
- Check location: AC Kubernetes authorization reviews on both RBAC profiles
- Procedure: Run access reviews and controlled operations as the provisioner identity in the allowed and denied namespaces, for both profiles. Check Secret read, Service, ServiceAccount, deployment, and exec denials.
- Evidence: Authorization review results for permitted and denied operations; resource inventory per namespace. No Secret payload is read.

### CW-T2-06 — Project and Engram memory survive stop/start

- Tier: T2
- Check location: AC normal runtime readback across stop and start
- Procedure: Write a project marker and an Engram project entry in a running workspace. Stop and restart it. Read both back through normal tools under ENGRAM_DATA_DIR, and check the XDG data, state, and cache directories.
- Evidence: Before and after PVC and workspace identity; file content and Engram readback. A newly created empty database does not count as survival. Data outside declared mounts is not asserted (OI-02).

### CW-T2-07 — Reversible mode switch without silent data loss

- Tier: T2
- Check location: AC live transition procedure with an external retained database
- Procedure: Run direct, then Coder, then direct, then Coder. Quiesce Coder workspaces before disabling it. Restart explicitly after reactivation. Read back project and memory state in each mode.
- Evidence: Recorded snapshots and identities for each mode; retained database and workspace records; absence of unintended deletion. Database or volume deletion invalidates the preconditions.

### CW-T2-08 — Operator can execute the generic procedure

- Tier: T2
- Check location: AC documentation-only rehearsal by an independent operator
- Procedure: Follow only the published procedure on a disposable cluster through installation, publication, runtime checks, and switching. Compare the product revision before and after.
- Evidence: Rehearsal log; list of every external value supplied; product content comparison; documentation corrections recorded as failures, not silently applied.

### CW-T2-09 — Credential seam and rotation mechanics

- Tier: T2
- Check location: AC synthetic principal before and after controlled replacement
- Procedure: Replace the backing credential of an actual-owner reference with a synthetic one, then perform a controlled restart. Record the principal identifier and the endpoint before and after.
- Evidence: secretKeyRef and resource metadata; principal identifiers without values. Only injection mechanics are tested. No real Backstage identity or refresh is claimed (OI-03).

### CW-T3-01 — Native portal self-service under each user

- Tier: T3
- Check location: AC two-user procedure in the operator's real cluster and Backstage
- Procedure: Each of two users selects Open in Coder for a repository. Confirm the navigation URL, native consent, and the resulting workspace page. Run a normal crew session in each workspace.
- Evidence: Sanitized navigation and configuration evidence (accessUrl, defaultTemplateName, defaultMode, repoUrlParamKeys); actual owner and build IDs; active template version and image digest.

### CW-T3-02 — Native create errors, cancellation, and status

- Tier: T3
- Check location: AC error and cancellation cases against native dashboard and API
- Procedure: Exercise cancellation, invalid parameters, inaccessible template, import failure, denied authorization, and build failure. Resolve an uncertain result against the user's workspace list before any new create.
- Evidence: Native outcomes per case; GET /api/v2/workspaces results for the user; dashboard cross-check of any empty plugin card. A running build without a connected agent is not readiness.

### CW-T3-03 — Identity tuple and two-account mismatch

- Tier: T3
- Check location: AC identity mapping evidence for each user and a deliberate mismatch
- Procedure: Record the immutable subject, Coder user UUID, Backstage entity reference, and workspace owner_id for each user. Exercise one case where the Backstage portal user and the Coder browser session differ.
- Evidence: Sanitized mapping evidence; mismatch observation. Display-name or email equality is insufficient.

### CW-T3-04 — Per-user Backstage credential reaches the right workspace

- Tier: T3
- Check location: AC supported issuance and runtime principal correlation
- Procedure: Confirm that a supported mechanism issues each user's Backstage credential. Start each owner's workspace and correlate the authenticated principal, audience, and lifetime with the actual-owner reference.
- Evidence: Issuance evidence; runtime principal, audience, and lifetime results. No token bytes. A Secret's existence or token shape does not satisfy this scenario. Premise is UNVERIFIED (OI-03).

### CW-T3-05 — Allowed and denied permissions match the user's own

- Tier: T3
- Check location: AC paired read-only probe matrix
- Procedure: Run the same authorized read-only probe matrix as each user through Backstage and from each workspace. Include at least one operation allowed to one user and denied to the other, and one permitted operation for each.
- Evidence: Paired operation, resource, identity, and outcome evidence; coverage statement and residual uncertainty. Unprobed permissions remain unverified.

### CW-T3-06 — Expired or revoked credentials fail without elevation

- Tier: T3
- Check location: AC isolated test-user expiry and revocation procedure
- Procedure: Expire or revoke an isolated test user's credential and run protected probes. Restart with a newly authorized credential only if the supported mechanism supplies one.
- Evidence: Redacted before and after authorization outcomes; principal mapping; restart and reference evidence. A synthetic T2 rejection does not certify this case.

### CW-T3-07 — Real integration needs no product changes

- Tier: T3
- Check location: AC completion review across CW-T3-01..06
- Procedure: Confirm that CW-T3-01..06 have evidence. Compare the product content and revision with the released baseline. Record the external configuration locations without secret contents.
- Evidence: Before and after content comparison showing no product change; evidence links for CW-T3-01..06. Any product edit to make the cluster work fails this scenario.

## Prerequisites outside the product

Operator-owned prerequisites are external to this repository: an existing
namespace, storage class, CNI, DNS, TLS and routing, a reachable external
PostgreSQL, the OIDC registration and callback, image and provider access,
and the Backstage configuration. These are supplied as external values and
secret references only. OI-01 is resolved by removal: the repository no longer
contains the environment-specific legacy values file, and the CH scan covers
the repository's tracked source files. Open items OI-02 through OI-06 remain
recorded in the acceptance specification and are not closed by this document.
