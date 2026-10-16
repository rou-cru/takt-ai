# Coder workspaces

Optional, reusable [Coder](https://coder.com) template that provisions one isolated Kubernetes workspace per person, each running the preinstalled [Takt AI workspace image](../workspace/README.md). Coder owns workspace create, start, stop, and delete. People work in a terminal inside their own workspace.

Coder is optional. Takt AI is complete without it: the [local install](../../README.md), the Docker image, and the [Helm chart](../../charts/takt-ai/README.md) are separate paths that depend on nothing here.

This directory is a Terraform root module that Coder runs as a template. The Coder provisioner applies it when a person creates a workspace. `variables.tf` is the only definition of the operator inputs; this guide explains how to set them and what people see.

## Quick path

1. Supply the [prerequisites](#prerequisites); the template ships none of them.
2. Push the template with your values.
3. Review the values you pass to the push.
4. Ask each person to create a workspace from the template, open a terminal, and run `opencode` in `/workspace`.
5. Run the [first proof](#first-proof) and record every outcome as verified, not verified, or not verifiable.

## What it provisions

Each workspace contains one Coder agent and the resources below. A person may own more than one workspace; two people never share one.

| Resource | Terraform address | Created when |
| --- | --- | --- |
| Workspace agent | `coder_agent.workspace` | The workspace exists |
| OpenCode app | `coder_app.opencode` | The workspace exists; it runs `cd /workspace && exec opencode` |
| Pod | `kubernetes_pod_v1.workspace[0]` | The workspace is started (`start_count` is 1) |
| Persistent volume claim | `kubernetes_persistent_volume_claim_v1.workspace[0]` | `persist_workspace` is true; it survives a stop |
| Network policy | `kubernetes_network_policy_v1.workspace` | The workspace exists; it denies ingress to the Pod |

Names are `takt-<workspace UUID>` for the Pod, the claim, and the network policy. Labels are `app.kubernetes.io/name=takt-coder`, `takt.ai/workspace-id=<workspace UUID>`, and `takt.ai/owner-id=<owner UUID>`. Nothing derived from a display name, username, or email enters a name, so renaming an account does not rename resources.

The Pod runs as UID and GID 10001 with `/workspace` as the working directory and `/home/takt` as home. OpenCode and the Takt setup are baked into the image at build time, so a new workspace starts with them installed. Only the Coder web terminal and SSH helper apps are enabled; there is no port forwarding or editor integration, and the template creates no Service or Ingress.

## Prerequisites

Everything below is yours to supply. The template ships no organization-specific endpoint, repository, identity, cluster value, or secret, and it creates no namespace, cluster role, or Coder deployment.

- A Coder deployment you run and administer, at version 2.18.0 or later. The v2 upgrade guide of the `coder/coder` provider 2.0.2 documents that minimum; it is a documented prerequisite, not a verified deployment. Record the exact server version and edition you test against.
- A working provisioner with an existing Kubernetes namespace. The template creates Pods, claims, and network policies in that namespace only; grant the provisioner's service account permissions for exactly those objects. The workspace Pod never receives that identity.
- A CNI that enforces network policy. The template's policy denies ingress to the Pod; enforcement is a property of your cluster, not of this schema.
- The workspace image pushed to a registry your cluster can reach, referenced by digest. Build it from the `workspace` target of `deploy/images/Dockerfile`, or pull a published `roucru/takt-ai` digest. A tag is not accepted, because a mutable tag lets one template produce different workspaces.
- Cluster nodes that match `architecture`, and a default StorageClass unless you set `storage_class_name`. Persistence also needs a StorageClass that provisions the requested size.
- The Coder CLI authenticated to your deployment (`coder login <your-coder-deployment-url>`) if you publish from a terminal.
- Terraform 1.9.0 up to, but not including, 2.0.0, for local validation only. The 1.9.0 floor is the release that supports the cross-variable validation these persistence inputs use.

## Publish the template

Set the deployment-specific values and push:

```sh
coder login <your-coder-deployment-url>

coder templates push takt-ai --directory deploy/coder --yes \
  --var namespace=<your-existing-namespace> \
  --var workspace_image=<your-registry>/<your-path>@sha256:<64-lowercase-hex>
```

A `terraform.tfvars` file inside `deploy/coder` also works and is read on push, so the CLI then needs no `--var` flags. Take the values from the [operator inputs](#operator-inputs) table and keep real values out of the repository.

Before pushing, validate the module without a Coder account:

```sh
terraform -chdir=deploy/coder fmt -check
terraform -chdir=deploy/coder init -backend=false -input=false
terraform -chdir=deploy/coder validate
```

`init` downloads the two providers and needs network access; allow about 5 minutes. `validate` takes about 2 minutes. Both report schema and validation results only; neither contacts Coder or Kubernetes.

Review the values you pass. A value that fails validation is named in the error before any resource is created. A missing required value is named the same way, and no fallback to an organization default exists.

The template declares these non-secret outputs, which summarize what was provisioned:

| Output | Meaning |
| --- | --- |
| `workspace_name` | The Coder workspace name |
| `owner_id` | The Coder owner UUID |
| `namespace` | The Kubernetes namespace |
| `image` | The workspace image reference |
| `architecture` | The selected architecture |
| `persistence_mode` | `ephemeral` or `workspace-pvc` |
| `pod_name` | The Pod name; null while the workspace is stopped |
| `pvc_name` | The claim name; null in `ephemeral` mode |

## Operator inputs

`variables.tf` owns the exact types, patterns, and validation rules, and it validates at plan time before any resource is created. This table explains what each input is for.

| Input | Required | Default | Purpose |
| --- | --- | --- | --- |
| `namespace` | Yes | none | Existing namespace for the workspace resources |
| `workspace_image` | Yes | none | Digest-pinned image reference to the `workspace` target image, which has OpenCode and Takt installed |
| `architecture` | No | `amd64` | `amd64` or `arm64`; sets both the agent architecture and the `kubernetes.io/arch` node selector |
| `image_pull_secrets` | No | `[]` | Existing Secret names for pulling the image, when your nodes need them |
| `repository_url_default` | No | empty | Optional credential-free HTTPS repository hint shown to each person; empty means none |
| `resources` | No | 500 and 2000 mCPU, 1024 and 4096 MiB memory, 512 and 2048 MiB ephemeral storage | Request and limit pairs for CPU, memory, and ephemeral storage on the init and workspace containers |
| `persistence_allowed` | No | `false` | Whether a person may keep a claim across a stop |
| `storage_class_name` | No | null, the cluster default | StorageClass for the claim; valid only with persistence |
| `storage_size_gib` | No | `10` | Claim size in GiB, 1 to 1024; valid only with persistence |
| `owner_secret_env` | No | `{}` | Environment variables drawn from existing Secrets, keyed by owner UUID |

`owner_secret_env` is the one input with structure. Its outer key is a Coder owner UUID; its inner keys are environment variable names:

```hcl
owner_secret_env = {
  "<owner-uuid>" = {
    MODEL_API_KEY = { name = "workspace-credentials", key = "model-api-key" }
  }
}
```

The template reads only the entry whose key matches the workspace owner, so an owner receives no other owner's variables. Names such as `HOME`, `PATH`, `CODER_*`, `TAKT_*`, `OPENCODE_*`, `XDG_*`, and `ENGRAM_*` are rejected, because they would override bootstrap, access, or memory defaults. The template never reads Secret contents, and no secret value appears in an output or a log.

The template is not a deployment of Coder itself: it creates no namespace, cluster roles, Coder server, or database.

## What each person sees

Each person creates a workspace from the published template under their own Coder identity. Identity comes from the Coder workspace and owner, never from a text field, so nobody can provision a workspace as someone else.

Two parameters appear at creation:

| Parameter | Default | Notes |
| --- | --- | --- |
| Repository URL | The operator's `repository_url_default`, or empty | A visible hint. Provisioning never clones, fetches, or resets a repository. Empty means none is configured; clone or initialize one yourself in `/workspace`. |
| Persist workspace | `false` | Whether the workspace keeps a claim across stops. It is immutable: change it by creating a new workspace. Selecting it when the operator disallows persistence fails before anything is created. |

Both parameters are immutable, so an ordinary edit cannot silently remove data.

### Terminal access

Open the workspace terminal in the Coder dashboard, or connect from a terminal:

```sh
coder ssh <your-workspace>
```

Then start OpenCode in the project directory:

```sh
cd /workspace
opencode
```

The OpenCode app in the Coder dashboard runs that same command for you.

Compatibility of the OpenCode and Takt interface with this access path is unverified. A running workspace with a connected agent means provisioning succeeded, not that OpenCode and Takt are ready to work. Exercise the terminal and record the result before relying on it.

## Lifecycle and data

Coder's own operations drive the lifecycle; this template adds no controller, scheduler, or timer.

| State | What it means |
| --- | --- |
| Validation | A plan rejected a value before creating anything; the error names the variable |
| Provisioning | Coder is applying the template; the init container prepares the runtime directories |
| Startup check | `scripts/check-workspace.sh` runs as the agent's blocking startup script |
| Running | The agent is connected and the startup check exited 0; readiness to work is still unverified |
| Failed | A prerequisite is missing or a step failed; the check prints the stage that failed and exits non-zero |
| Stopped | You or another operator stopped the workspace; the Pod is gone |
| Resuming | Coder starts a fresh Pod and agent, using the claim if persistence is on and an empty directory if it is off |
| Deleting | You confirmed deletion; Coder removes the workspace and its resources |

The template sets `TAKT_WORKSPACE_DIR=/workspace` for the agent, because the Coder agent command bypasses the image entrypoint that would otherwise supply it. The startup check verifies that `/workspace` and the runtime paths under `/home/takt` (`.local/share`, `.local/state`, and `.cache`) are writable, that the `opencode`, `takt-ai`, `engram`, and `codegraph` executables are present, and that the installed configuration under `/home/takt` is readable. It reports the versions of `opencode` and `takt-ai`, and prints stage names and failures only, never environment values or tokens. Exit 0 means bootstrap prerequisites are met, not that a model, an MCP server, or a tool is ready.

A failure reports the step and the resource it concerns. Fix the input or the image and start again; nothing is silently substituted, and a failed workspace is never ready.

Stop is a deliberate pause. In `ephemeral` mode it discards the Pod and everything in it; in `workspace-pvc` mode it keeps the claim, so the next start resumes with your files. The template claims no automatic stop: no idle detection, dormancy, autostop, or quota. If a long job is running and you disconnect, nothing stops the workspace.

## Git and data loss

Provisioning does not clone, commit, or push. Work in `/workspace` like any checkout and commit locally through Takt's normal authorized path. Pushing stays a separate, user-gated step in your own workflow.

Data lives in one of two places:

- In `ephemeral` mode, everything is on the Pod. A stop loses local commits, uncommitted files, and runtime data.
- In `workspace-pvc` mode, the claim survives a stop, so the next start resumes. Deleting the workspace still removes the claim.

Uncommitted work may exist only inside the workspace and never in the remote repository. Storage and stop and resume are not a substitute for committing or backing up.

## Delete a workspace

Deleting a workspace is Coder's operation, and it removes every resource the template created for that workspace, including its claim. That loses local commits, uncommitted files, and runtime data. The storage backend's reclaim policy decides what happens to the underlying volume, and no secure erasure is guaranteed.

Before you confirm deletion, check the workspace name and what you lose. This template ships no automatic cleanup: no retention sweep, no timer, and no destructive default.

## First proof

Run every command from the repository root, in order. Each tier narrows what stays unverified; none substitutes for the next.

### Tier 1: static and mock checks

No cluster, Coder account, or credential is needed.

```sh
terraform -chdir=deploy/coder fmt -check
terraform -chdir=deploy/coder init -backend=false -input=false
terraform -chdir=deploy/coder validate
terraform -chdir=deploy/coder test
bash development/testing/test-coder-bootstrap.sh
```

`init` needs network access; allow about 5 minutes. `validate` and `test` take about 2 minutes each, and the shell test about 60 seconds. `terraform test` uses mock Coder and Kubernetes providers, fixes two owner identities and two workspace identities, and asserts the planned shape without applying anything. It covers the input schema, per-owner naming and secret selection, and the lifecycle shape, including the absence of a Service, an Ingress, and a controller. The shell test loads the startup check with fake executables in an isolated home.

A mock or fixture success is not access, policy, or Kubernetes proof, and it is not a two-human result.

### Tier 2: image smoke

Needs a Docker daemon; no Coder account or cluster.

```sh
docker buildx build -f deploy/images/Dockerfile --target workspace --load -t takt-ai-workspace:local .

bash development/testing/test-coder-smoke.sh \
  --image takt-ai-workspace:local \
  --name takt-ai-smoke-<confirmed-unique-name>
```

The build has no cached layers on a first run and downloads its toolchain and pinned installers; allow about 45 minutes. The runner requires an existing image, never pulls or builds one, and never mounts your home directory, repositories, credentials, or the Docker socket. Within a 5-minute bound it runs the startup check, asserts UID 10001 and the baked installation, writes a non-secret marker into `/workspace`, starts the ordinary HTTP entrypoint with a synthetic test-only password on loopback, and polls the authenticated API for up to 90 seconds. The container stays for inspection.

This tier proves the image builds, the default HTTP entrypoint still works, the baked setup survives an explicit terminal command, and `/workspace` is writable by UID 10001. It does not prove a real Coder agent, startup-script connectivity, Kubernetes scheduling or storage, terminal transport, isolation, or memory recording.

Clean up only after you confirm what goes: `docker rm -f <name>` removes the throwaway container, and `docker rmi takt-ai-workspace:local` removes the local image.

### Tier 3: live two-human validation

Needs your infrastructure and two distinct human accounts: a reachable Coder 2.18.0 or later, a working database and provisioner, a namespace with RBAC and a policy-enforcing CNI, a digest-pinned image in a reachable registry, matching nodes, a StorageClass when you test persistence, and working DNS, TLS, and outbound routes.

1. Each account creates a workspace from the template, independently. Record both workspace names, owner IDs, and resource names; they differ.
2. Each person opens their terminal and starts OpenCode. Confirm that neither account can open the other's terminal or read the other's storage or secret-backed environment.
3. Record a memory entry, including `memory_record`, and read it back. The write path is unchanged by this template.
4. In a persistent workspace, write a marker and a local commit, stop, resume, and confirm the marker survives. Repeat with an ephemeral workspace and confirm the marker is gone after the stop.
5. Start a long job, disconnect, and confirm the workspace keeps running. Nothing autostops it.
6. Delete the named workspace and confirm the Pod and the claim are gone. Record the backend's retention behavior separately.

## Verification status

Runtime behavior of everything in this guide is unverified: the template has not run against a Coder deployment, and no two-human exercise has happened. Treat each row below as work to do, not as a result. The dependency interface is the exception: the `coder/coder` 2.0.2 artifact and the Terraform 1.9.0 feature floor are verified against primary sources. Provider initialization and executed schema compatibility stay unverified until tier 1 runs.

| Check | Tier | How | Status |
| --- | --- | --- | --- |
| Operator input schema and validation | 1 | `terraform validate`, `terraform test` | Not verified |
| Per-human naming, labels, and owner-scoped secret references | 1 | `terraform test` with two owners | Not verified |
| Lifecycle shape, and no controller, Service, or Ingress | 1 | `terraform test` | Not verified |
| Startup diagnostic | 1 | `development/testing/test-coder-bootstrap.sh` | Not verified |
| Image build and baked setup under an overridden entrypoint | 2 | Build, then `development/testing/test-coder-smoke.sh` | Not verified |
| Two people isolated, each with terminal access | 3 | Two Coder accounts | Not verifiable without your Coder deployment and two accounts |
| Stop and resume: persistent keeps, ephemeral loses | 3 | A marker in each mode | Not verifiable without your infrastructure |
| Delete removes the named Pod and claim | 3 | Delete a named workspace | Not verifiable without your infrastructure |
| OpenCode and Takt over the terminal, memory recording | 3 | `opencode` in the workspace terminal | Not verifiable without your infrastructure |
| Dormancy, enforced autostop, quotas, idle-job awareness | None | Not tested | Not verifiable; documented as paid-edition capabilities, and this template requires none of them |

A not-verified or not-verifiable row is missing evidence, not a failed template. Do not present a partial result as ready.

## Limits

- No custom lifecycle platform: no controller, reconciler, TTL sweeper, quota engine, or idle detector.
- No setup-catalog entry, TUI flow, or setup schema change. This is a deployment path documented from [README.md](../../README.md), not a capability the TUI offers.
- No Coder deployment, database, or namespace provisioning, and no cluster RBAC management.
- Coder only: this template contains no Backstage content.
- No credential or account entry is required for Takt's own operations.
- The network policy denies ingress to the Pod. It separates runtime and data; it is not a claim of hostile-tenant kernel isolation.

## Related files

| Path | Purpose |
| --- | --- |
| `versions.tf` | Terraform 1.9.0 up to, but not including, 2.0.0, `coder/coder` 2.0.2, and `hashicorp/kubernetes` 2.16.0 |
| `variables.tf` | The operator input schema, validation, and descriptions |
| `main.tf` | Data sources, per-person parameters, and resources |
| `outputs.tf` | The non-secret summary outputs |
| `.terraform.lock.hcl` | Provider checksums for `linux_amd64` and `linux_arm64` |
| `scripts/check-workspace.sh` | The blocking startup diagnostic |
| `tests/workspace.tftest.hcl` | Mock-provider plan tests |
| `development/testing/test-coder-bootstrap.sh` | Startup-check fixture tests |
| `development/testing/test-coder-smoke.sh` | Bounded container smoke runner |
| `../workspace/README.md` | The workspace image |
| `../../charts/takt-ai/README.md` | The separate Helm deployment |
