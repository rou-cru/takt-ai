# Coder workspaces

This directory is the product's [Coder](https://coder.com) workspace template: a Terraform root module that Coder's provisioner applies to give each person one isolated, persistent Kubernetes workspace running the preinstalled [Takt AI workspace image](../workspace/README.md). Coder owns workspace create, start, stop, and delete. People work in a terminal inside their own workspace.

Coder is optional. Takt AI is complete without it: the [local install](../../README.md), the Docker image, and the [Helm chart](../../charts/takt-ai/README.md) are separate paths that depend on nothing here. The chart can install Coder for you (`execution.mode=coder`); this template is published to Coder separately and works with any Coder deployment you administer.

`variables.tf` is the sole definition of the operator inputs; this guide explains how to set them and what people see. The Backstage self-service contract lives in [BACKSTAGE.md](BACKSTAGE.md); the acceptance procedure and evidence ledger live in [ACCEPTANCE.md](ACCEPTANCE.md).

## Operator path

1. Meet the [prerequisites](#prerequisites).
2. Run a Coder deployment you administer — install the [chart](../../charts/takt-ai/README.md) in `coder` mode or use an existing one. The chart's optional dependency pins Coder 2.38.0; record the exact server version and edition you test against.
3. Create the first user with Coder's documented first-user workflow (the dashboard's first-login setup or `coder login` with its first-user flags). The chart and this template ship no bootstrap password. First-user bootstrap on an OIDC-only deployment with the exact release is unverified; record the path you used.
4. Authenticate the CLI: `coder login <your-coder-deployment-url>`.
5. [Publish the template](#publish-the-template) with your values.
6. Ask each person to create a workspace from the template — in Coder's dashboard or through Backstage self-service ([BACKSTAGE.md](BACKSTAGE.md)) — open a terminal, and run `opencode` in `/workspace`.
7. Record evidence per [ACCEPTANCE.md](ACCEPTANCE.md).

## What it provisions

Each workspace contains one Coder agent and the resources below. A person may own more than one workspace; two people never share one.

| Resource | Terraform address | Created when |
| --- | --- | --- |
| Workspace agent | `coder_agent.workspace` | The workspace exists |
| OpenCode app | `coder_app.opencode` | The workspace exists; runs `cd /workspace && exec opencode`, owner-only (`share = "owner"`) |
| Pod | `kubernetes_pod_v1.workspace[0]` | The workspace is started (`start_count` is 1) |
| Persistent volume claim | `kubernetes_persistent_volume_claim_v1.workspace[0]` | Always, including while stopped |
| Network policy | `kubernetes_network_policy_v1.workspace` | The workspace exists; denies ingress to the Pod |

Names are `takt-<workspace UUID>` for the Pod, the claim, and the network policy. Labels are `app.kubernetes.io/name=takt-coder`, `takt.ai/workspace-id=<workspace UUID>`, and `takt.ai/owner-id=<owner UUID>`. Nothing derived from a display name, username, or email enters a name, so renaming an account does not rename resources.

Every supported workspace has exactly one per-workspace RWO claim. Only compute is gated on start: stopping removes the Pod and keeps the claim; starting creates a fresh Pod and agent that reuse it. The claim does not wait for binding (`wait_until_bound=false`), so storage classes with delayed binding do not block the Pod.

The Pod runs as UID and GID 10001 with `/workspace` as the working directory and `/home/takt` as home, and mounts the claim through sibling subPaths: `workspace` at `/workspace`, and `data`, `state`, and `cache` at `/home/takt/.local/share`, `/home/takt/.local/state`, and `/home/takt/.cache`. `/tmp` is an emptyDir. The Pod sets `XDG_DATA_HOME`, `XDG_STATE_HOME`, and `XDG_CACHE_HOME` to those directories and `ENGRAM_DATA_DIR=/home/takt/.local/share/engram`, so tool data and per-workspace memory (Engram) persist on the claim. Files outside the declared mounts — an ad-hoc `~/.ssh`, edits to the baked configuration under `/home/takt` — are not durable; durable files belong in `/workspace` or the persisted XDG directories. The claim is storage, not a backup.

OpenCode and the Takt setup are baked into the image at build time; no workspace runs an installer or a second OpenCode installation. Only the Coder web terminal and SSH are enabled. The template creates no Service, Ingress, or route and exposes no port: the terminal is the access path, and OpenCode's own HTTP authentication is not involved on it. Workspace Pods run with the service-account token unmounted; the provisioner's identity never enters a workspace.

## Prerequisites

Everything below is yours to supply. The template ships no organization-specific endpoint, repository, identity, cluster value, or secret, and it creates no namespace, cluster role, or Coder deployment.

- A Coder deployment you run and administer. The chart's optional dependency pins Coder 2.38.0; the pinned `coder/coder` Terraform provider is 2.0.2. Record the exact server version and edition you test against.
- A working provisioner with an existing Kubernetes namespace. The provisioner's service account needs Pods, claims, and network policies in that namespace only — the chart's `coder`-mode defaults grant exactly that profile, including the NetworkPolicy permissions; Pods-and-PVCs alone are insufficient. The workspace Pod never receives that identity.
- A CNI that enforces network policy. The template's policy denies ingress to the Pod; enforcement is a property of your cluster, not of this schema.
- The workspace image pushed to a registry your cluster can reach, referenced by digest: build the `workspace` target of `deploy/images/Dockerfile` or use a published release digest. A mutable tag is not accepted, because a tag lets one template provision different workspaces over time.
- Cluster nodes matching `architecture`, and a StorageClass (the cluster default or `storage_class_name`) that provisions `storage_size_gib`.
- The Coder CLI authenticated to your deployment for publication.
- For Backstage self-service, the operator-side setup in [BACKSTAGE.md](BACKSTAGE.md).

Terraform 1.11.0 up to, but not including, 2.0.0 is needed only for the local validation below. Coder's provisioner runs Terraform itself when it imports the template; you never run Terraform against your cluster to deploy workspaces.

## Publish the template

Validate the module first; no Coder account is needed:

```sh
terraform -chdir=deploy/coder fmt -check
terraform -chdir=deploy/coder init -backend=false -input=false -lockfile=readonly
terraform -chdir=deploy/coder validate
```

`init` downloads the two pinned providers and needs network access. `-lockfile=readonly` keeps the checked-in `.terraform.lock.hcl` from being rewritten: it carries checksums for `linux_amd64` and `linux_arm64` only, and without the flag an `init` on another platform can rewrite the file. The three commands report formatting, schema, and validation results only; none contacts Coder or Kubernetes.

Then push with your values. Keep real values in a variables file outside the repository, or pass them individually:

```sh
coder login <your-coder-deployment-url>

coder templates push <your-template-name> --directory deploy/coder --yes \
  --var namespace=<your-existing-namespace> \
  --var workspace_image=<your-registry>/<your-path>@sha256:<64-lowercase-hex>
```

`--name <version>` names the new template version, and `--variables-file <file>` replaces the `--var` flags. `--activate` defaults to true in Coder 2.38.0, so a push activates the new version: it becomes the template's active version, the one new workspaces — including Backstage's create flow — are created from. `--activate=false` matters when you want to publish a version without switching the template to it yet, for example to review or test the new version first; the previous active version stays in place until you activate the new one in Coder.

What a push does and does not prove:

- The push uploads the module to Coder, which imports it and runs Terraform internally. The result is a template version and an import job. Successful import and a selected active version are still not a working workspace; the first real proof is a started workspace with a connected agent ([ACCEPTANCE.md](ACCEPTANCE.md)).
- Authentication failures, invalid variables, provider resolution failures, and import failures are publication failures. A value that fails validation is named in the error before any resource is created; a missing required value is named the same way. No fallback to a repaired default exists.
- Publication is an explicit operator action, not a Helm side effect. Repeating a push creates another template version; no exactly-once publication is claimed. The chart stores no publisher token, and no publishing job or hook exists.

## Operator inputs

`variables.tf` is the sole executable definition of the inputs — exact types, patterns, and validation rules, evaluated at plan time before any resource is created. This table is explanatory; when it and `variables.tf` differ, `variables.tf` wins.

| Input | Required | Default | Purpose |
| --- | --- | --- | --- |
| `namespace` | Yes | none | Existing namespace for the workspace resources |
| `workspace_image` | Yes | none | Digest-pinned reference to the workspace image (`registry/path@sha256:<64 lowercase hex>`) |
| `architecture` | No | `amd64` | `amd64` or `arm64`; sets the agent architecture and the `kubernetes.io/arch` node selector |
| `image_pull_secrets` | No | `[]` | Existing Secret names for pulling the image |
| `repository_url_default` | No | empty | Optional credential-free HTTPS repository hint offered to each person |
| `resources` | No | CPU 500m/2000m, memory 1024/4096 MiB, ephemeral storage 512/2048 MiB (requests/limits) | Request and limit pairs for the init and workspace containers; requests must not exceed limits |
| `storage_class_name` | No | null (cluster default) | StorageClass for the claim |
| `storage_size_gib` | No | `10` | Claim size in GiB, 1 to 1024 |
| `owner_secret_env` | No | `{}` | Environment entries drawn from existing Secrets, keyed by owner UUID |
| `backstage_url` | No | empty | Absolute HTTPS base URL without credentials, query, or fragment; when set, workspaces receive it as `BACKSTAGE_URL` and every starting owner needs a `BACKSTAGE_TOKEN` reference. Empty omits `BACKSTAGE_URL` and the token requirement, but a token mapped for the owner is still injected |

`owner_secret_env` is the structured input. Its outer key is a Coder owner UUID; its inner keys are environment variable names mapped to Secret references:

```hcl
owner_secret_env = {
  "<owner-uuid>" = {
    MODEL_API_KEY = { name = "workspace-credentials", key = "model-api-key" }
  }
}
```

The template reads only the entry whose key matches the workspace's actual owner, so an owner never receives another owner's variables. Reserved names (`HOME`, `PATH`, `USER`, `LOGNAME`, `SHELL`, `ENV`, `BASH_ENV`, `LD_PRELOAD`, `LD_LIBRARY_PATH`, `BACKSTAGE_URL`, and every `CODER_*`, `TAKT_*`, `OPENCODE_*`, `XDG_*`, `ENGRAM_*` name) are rejected, because they would override bootstrap, access, or memory defaults, or duplicate the template's own `BACKSTAGE_URL` input. The template never reads Secret contents, and no output carries a credential.

The template declares these non-secret outputs: `workspace_name`, `owner_id`, `pod_name` (null while stopped), `namespace`, `image`, `architecture`, `persistence_mode` (`workspace-pvc`), and `pvc_name`.

Earlier published versions exposed `persist_workspace` and `persistence_allowed`. Those inputs are gone from the current module, and existing workspaces are not silently migrated: moving an older workspace to the persistent layout is an explicit transition you plan and confirm, and existing data at the previous `~/.engram` path is neither deleted nor silently relocated.

## What each person sees

Each person creates a workspace from the published template under their own Coder identity. Identity comes from the Coder workspace and owner, never from a text field, so nobody can provision a workspace as someone else.

One parameter appears at creation:

| Parameter | Default | Notes |
| --- | --- | --- |
| Repository URL | The operator's `repository_url_default`, or empty | A visible, immutable, credential-free HTTPS hint. Provisioning never clones, fetches, or resets a repository, and the URL never carries credentials. Empty means none is configured — clone or initialize one yourself in `/workspace`. |

Open the workspace terminal in the Coder dashboard, or connect from a terminal:

```sh
coder ssh <your-workspace>
```

Then start OpenCode in the project directory:

```sh
cd /workspace
opencode
```

The OpenCode app in the Coder dashboard runs that same command; only the workspace owner can open it.

## Startup and readiness

Coder's agent startup runs `scripts/check-workspace.sh` as a blocking startup script. It verifies that `/workspace` and the runtime paths under `/home/takt` are writable, that `opencode`, `takt-ai`, `engram`, and `codegraph` are present, and that the installed configuration is readable; it reports the versions of `opencode` and `takt-ai`, and prints stage names and failures only — never environment values or tokens. Exit 0 means the local bootstrap prerequisites are met. It does not prove that a model, an MCP server, or a Backstage credential is ready, and a running workspace with a connected agent means provisioning succeeded, not that a crew session works. Both are acceptance evidence to record ([ACCEPTANCE.md](ACCEPTANCE.md)).

The agent sets `TAKT_WORKSPACE_DIR=/workspace` and `TAKT_REPOSITORY_URL=<repository_url>`. The repository URL remains a hint the person chooses to use, not an instruction the workspace executes.

## Lifecycle and data

Coder's own operations drive the lifecycle; this template adds no controller, scheduler, or timer.

| State | Meaning |
| --- | --- |
| Validation | A plan rejected a value before creating anything; the error names the variable |
| Provisioning | Coder is applying the template; the init container prepares the runtime directories |
| Startup check | `scripts/check-workspace.sh` runs as the blocking startup script |
| Running | Provisioning succeeded. A connected agent, the startup check exiting 0, and readiness to work are separate evidence |
| Failed | A prerequisite is missing or a step failed; the check prints the stage that failed |
| Stopped | The Pod is gone; the claim and the workspace record remain, and the next start reuses them |
| Deleting | You confirmed deletion; Coder removes the workspace and its resources, including the claim |

Stop is a deliberate pause, not a data operation. Delete is Coder's destructive operation: it removes every resource the template created for that workspace, including the claim and everything on it. Confirm the workspace name and what is lost before confirming deletion. The storage backend's reclaim policy decides the backing volume, and no secure erasure is guaranteed. Push work you want to keep to the remote repository before deleting.

The template claims no automatic stop: no idle detection, dormancy, autostop, quota, or sweeper. If a long job is running and you disconnect, nothing stops the workspace. Coder's native scheduling, if you configure it, is your explicit choice with its own consequences; a disconnected background job is not idleness. Per-workspace project memory (Engram under `ENGRAM_DATA_DIR`) persists with the claim; shared team memory across workspaces is a separate, later concern this template does not provide.

## Backstage

Backstage self-service — each person creating their own workspace from the portal, with their own Backstage credential available inside it — is a separate contract documented in [BACKSTAGE.md](BACKSTAGE.md). It reuses two inputs of this template: `backstage_url` and the `BACKSTAGE_TOKEN` entry of the actual owner in `owner_secret_env`. When `backstage_url` is empty the workspace receives neither `BACKSTAGE_URL` nor a token requirement, but a `BACKSTAGE_TOKEN` mapped for its owner is still injected. The issuance and rotation of a real per-user Backstage credential is unverified; this template delivers the injection boundary, not the credential path.

## Verification

Static and mock checks run without a cluster; the runner map — what each script and suite covers, and its tier boundary — is in [development/testing/README.md](../../development/testing/README.md):

```sh
terraform -chdir=deploy/coder fmt -check
terraform -chdir=deploy/coder init -backend=false -input=false -lockfile=readonly
terraform -chdir=deploy/coder validate
terraform -chdir=deploy/coder test
```

`terraform test` runs the mock-provider suites (`inputs`, `workspace`, `lifecycle`) and asserts the planned shape — input validation, per-owner naming and secret selection, lifecycle structure — without applying anything. A mock success is not a live proof.

No live scenario is certified by documentation. Bootstrap, publication, workspace start, terminal access, two-owner isolation, stop/start memory survival, mode switching, and Backstage self-service are acceptance procedures with per-scenario evidence fields in [ACCEPTANCE.md](ACCEPTANCE.md); their current status lives there.

## Limits

- No lifecycle platform: no controller, reconciler, TTL sweeper, quota engine, or idle detector.
- No Coder deployment, database, namespace, or cluster RBAC provisioning; this template creates Pods, claims, and network policies in one existing namespace.
- No Backstage code; [BACKSTAGE.md](BACKSTAGE.md) is the contract, and the per-user credential path is unverified.
- No auto-clone: the repository URL is a hint; cloning and pushing stay the person's own authorized workflow.
- The deny-ingress network policy separates runtime and data; it is not a claim of hostile-tenant kernel isolation.
- No paid-edition capability is used or promised: per-template access lists, groups, autodelete, enforced idle stop, headless service accounts, and external provisioners are all absent.

## Related files

| Path | Purpose |
| --- | --- |
| `versions.tf` | Read-only pins: Terraform >= 1.11.0 and < 2.0.0, `coder/coder` 2.0.2, `hashicorp/kubernetes` 2.16.0 |
| `variables.tf` | The sole operator-input definition: types, defaults, validation |
| `main.tf` | Data sources, the person-facing parameter, and the resources |
| `outputs.tf` | The non-secret summary outputs |
| `.terraform.lock.hcl` | Provider checksums for `linux_amd64` and `linux_arm64` |
| `scripts/check-workspace.sh` | The blocking startup diagnostic |
| `tests/inputs.tftest.hcl`, `tests/workspace.tftest.hcl`, `tests/lifecycle.tftest.hcl` | Mock-provider plan suites |
| `ACCEPTANCE.md` | Per-scenario acceptance procedure and evidence ledger |
| `BACKSTAGE.md` | The Backstage self-service and per-user credential contract |
| `../workspace/README.md` | The workspace image |
| `../../charts/takt-ai/README.md` | The Helm chart, including `coder` mode |
| `../../development/testing/README.md` | The test-runner map and tier boundaries |
