# Backstage self-service

This is the user-facing contract for creating Takt workspaces from [Backstage](https://backstage.io): each person requests a workspace for a repository, is directed to Coder under their own account, and the workspace carries that same person's Backstage credential. The current scope is self-service creation; the crew-side Backstage tooling (a Takt MCP/plugin used inside the workspace) is a later phase and is not shipped here.

Nothing in this repository installs Backstage code. The integration is Coder's official plugin plus native browser flows, and every version and registration below is operator-supplied. The workspace-template side of this contract lives in [README.md](README.md) (the `backstage_url` and `owner_secret_env` inputs); the acceptance procedure lives in [ACCEPTANCE.md](ACCEPTANCE.md).

## Packages

Two official packages, installed into your Backstage by you, at versions compatible with your Backstage release:

- `@coder/backstage-plugin-coder` — the frontend plugin (workspaces card, create link, status).
- `@coder/plugin-auth-backend-module-coder-provider` — the backend auth provider for per-user Coder sign-in.

A compatible published combination for a given Backstage release is unverified: the plugin's current documentation describes this interface from its main branch, and a main-branch source is not a tested package combination. Record the deployed Backstage version and the exact package versions you publish; that combination, not this document, is the evidence.

## Coder-side setup

- Coder's OAuth2 provider must be enabled. The Helm chart's `coder`-mode defaults set `CODER_OAUTH2_PROVIDER_ENABLE=true` (available in the Community edition). Coder's pricing page limits the Community edition to one platform integration such as Backstage; whether and how that limit is enforced is unverified (acceptance open item OI-05).
- Register a Coder OAuth application with the callback `{backstage-base-url}/api/auth/coder/handler/frame`. The application's client secret stays in Backstage's secret store; it never enters this repository, the chart, or the template.
- Ordinary users sign in to Coder through your OIDC provider. The Coder OAuth token issued by this flow authorizes Coder only; it is not a Backstage credential.
- The chart's `coder`-mode default `CODER_DISABLE_PASSWORD_AUTH=true` disables password login for ordinary users; Coder keeps its documented Owner password exception, so an Owner can still sign in with the owner password ([Required environment entries](../../charts/takt-ai/README.md#required-environment-entries)).

## Plugin configuration

The plugin consumes four settings:

| Key | Value |
| --- | --- |
| `deployment.accessUrl` | Your Coder access URL — the same absolute HTTPS URL as `CODER_ACCESS_URL` |
| `workspaces.defaultTemplateName` | The name you published the workspace template under |
| `workspaces.defaultMode` | `manual` |
| `workspaces.repoUrlParamKeys` | `["repository_url"]` |

`defaultMode: manual` keeps identity, parameters, and consent visible on Coder's create page. The configured template name resolves to the template's active version in Coder; the create link does not pin a template version — publication controls activation.

## The create flow

Selecting a repository and choosing "Open in Coder" navigates the person's browser to Coder's native create page:

```
{CODER_ACCESS_URL}/templates/{percent-encoded template name}/workspace?mode=manual&param.repository_url={percent-encoded repository URL}
```

- The workspace is created by Coder, under the browser's Coder session, as an asynchronous build shown on the person's workspace page. There is no backend create action, no service token, and no owner parameter: nobody creates a workspace as someone else through this flow.
- `repository_url` is the only human-supplied parameter — an immutable, credential-free HTTPS hint. Coder never clones it.
- Cancellation creates nothing. Invalid parameters, a missing or unpublished template, an unresolved import, denied authorization, and failed builds are native Coder outcomes: observable errors, never silent successes. A `401` means the person re-authorizes; an administrator cannot retry it for them. If a create times out or the outcome is uncertain, resolve it against the person's workspace list before creating again; there is no deduplication service. A person may own several workspaces.
- `mode=auto` exists as optional native prefill-with-consent behavior. It is not headless provisioning and is not required.

## Listing and status

The plugin lists a person's workspaces through Backstage's proxy to Coder's API with that person's Coder session. Coder's statuses (pending, starting, running, stopping, stopped, failed, canceling, canceled, deleting, deleted) are displayed as-is. Two cautions:

- A workspace in `running` means provisioning succeeded. A connected agent, a successful blocking startup check, and a normal crew session are separate evidence; running alone is not Takt readiness.
- An empty workspaces card is not proof that no workspace exists: the plugin can turn a failed search into an empty list. Cross-check the Coder dashboard or the person's workspace list before concluding.

## Identity contract

Acceptance ties four identifiers to one human: the IdP issuer and immutable subject, the Coder user UUID, the Backstage user entity reference, and the workspace `owner_id`. A shared IdP, matching display names, or matching email addresses are not proof of that binding.

- The plugin's sign-in resolver (`usernameMatchingUserEntityName`) requires an operator-controlled, unambiguous mapping between Coder usernames and Backstage user entities; it is not an immutable binding by itself.
- The plugin's resource-access login is separate from Backstage sign-in. A different Coder browser session can create a workspace for a different user; two-account mismatch testing (Backstage profile of one person with the Coder session of another) is part of acceptance, not an edge case.
- Whether your deployed Backstage/Coder/package combination actually enforces same-user linking is unverified. No Takt-side identity enforcement service exists or is planned here; a detected mismatch fails acceptance for that deployment and is fixed in your configuration.

## Per-user credential contract

For the workspace to reach Backstage as its owner, you supply two template inputs ([README.md](README.md)):

- `backstage_url` — the absolute HTTPS base URL of your Backstage. When set, every started workspace receives it as `BACKSTAGE_URL`.
- `owner_secret_env[<owner UUID>]["BACKSTAGE_TOKEN"]` — a reference to a Secret `{name, key}` in the workspace namespace.

The delivered behavior:

- When `backstage_url` is set, a starting workspace whose actual owner has no `BACKSTAGE_TOKEN` reference fails before the Pod is created. With `backstage_url` empty, the workspace carries neither `BACKSTAGE_URL` nor a token requirement, but a `BACKSTAGE_TOKEN` mapped for the actual owner is still injected.
- Another owner's reference, a shared token, or a fallback is never substituted; `BACKSTAGE_URL` cannot be injected through `owner_secret_env`; and the template never reads Secret contents.
- A missing Secret or key fails Pod startup in Kubernetes. Stopping a workspace never requires the reference.
- The credential must represent that Backstage user, with permissions equivalent to the user's own access — measured by the same allowed and denied operations against the same resources, not by decoding a subject.
- Environment is fixed at Pod creation: rotating a credential means a controlled workspace restart, and expiry never falls back to a broader credential.

**Unverified limit (recorded blocker).** A supported way to issue and renew a per-user Backstage credential with the required audience and lifetime is not established. A placeholder Secret, any token-shaped value, or a raw OIDC access token accepted as external access (a service-style principal, not the user) does not satisfy this contract. Until a supported issuance path exists and is exercised, the credential portion of this integration is uncertified: the injection boundary above is delivered, the credential path is not. This is the recorded blocker for the per-user credential scenarios in [ACCEPTANCE.md](ACCEPTANCE.md).

## What this contract does not include

- No crew Backstage MCP/plugin inside the workspace; that is a later phase.
- No custom Scaffolder action, no backend service token, no create-on-behalf flow.
- No shared credential broker, permission mirror, or Takt-side authorization layer: Backstage remains the authority on its resources.
- No deduplication service, and no plugin code changes: a plugin defect is fixed upstream, not here.

## Acceptance

Every claim above has a per-scenario procedure and an evidence ledger in [ACCEPTANCE.md](ACCEPTANCE.md): two-user self-service, create errors and cancellation, the identity tuple with a deliberate mismatch, the per-user credential and its expiry and revocation, permission equivalence, and the no-product-change proof. The environment fixtures — two non-admin human accounts, a permission matrix, and a supported credential path — are specified there.
