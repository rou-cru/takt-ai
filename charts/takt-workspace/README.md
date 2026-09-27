# Takt/OpenCode workspace chart

This chart deploys a disposable OpenCode v2 web workspace with Takt AI preinstalled. Each Helm release receives its own PVC; the optional repository checkout and OpenCode/Takt writable data live on that claim. Use a distinct release name for each workspace. The default is one replica; `replicaCount` remains configurable.

## Prerequisites

- Kubernetes nodes with ARM64 (`linux/arm64`) capacity. The workspace image and chart default node selector target ARM64 only.
- A default StorageClass or `persistence.storageClassName` that provisions the requested `persistence.size`; choose a reclaim policy appropriate for disposable work.
- Gateway API `HTTPRoute` CRD and controller only when `httpRoute.enabled=true`.
- External Secrets Operator CRDs/controller and a SecretStore or ClusterSecretStore only when `externalSecret.enabled=true`.
- VPA CRD/controller only when `vpa.enabled=true`.
- Optionally, a Kubernetes Secret for OpenCode server authentication. The Secret name/key are references only; secret bytes are not chart values.

## Build the prepared ARM64 image

The workspace image is built by the dedicated `docker/Dockerfile.workspace`; it does not use or modify `docker/Dockerfile.dev`. The Bake target pins OpenCode v2 and targets `linux/arm64` only. It compiles Takt AI and installs/configures OpenCode and its Takt integration during image build.

```sh
# local ARM64 image (for a matching local cluster)
docker buildx bake -f docker/docker-bake.hcl workspace --load

# publish to the registry configured for your cluster
WORKSPACE_TAG=099814429435.dkr.ecr.mx-central-1.amazonaws.com/kaf:<tag> \
  docker buildx bake -f docker/docker-bake.hcl workspace --push
```

Set `image.repository`, `image.tag`, `image.pullPolicy`, and optionally `image.pullSecrets` for your registry. The chart defaults to `099814429435.dkr.ecr.mx-central-1.amazonaws.com/kaf:kaf`.

## Install, upgrade, and remove

`scripts/helm-workspace.sh` creates/uses the shared namespace but never places Namespace in an individual Helm release. The default is `takt-workspaces`; override it with `--namespace` or `WORKSPACE_DEFAULT_NAMESPACE`.

```sh
scripts/helm-workspace.sh install demo \
  --set server.passwordSecret.name=demo-opencode-auth \
  --set httpRoute.enabled=true \
  --set-string 'httpRoute.parentRefs[0].name=public-gateway' \
  --set-string 'httpRoute.hostnames[0]=workspace.example.test'

scripts/helm-workspace.sh upgrade demo -f workspace-values.yaml
scripts/helm-workspace.sh uninstall demo

# Explicit namespace takes precedence over the default.
scripts/helm-workspace.sh install demo --namespace takt-workspaces-test -f workspace-values.yaml
```

The equivalent default namespace bootstrap manifest is `deploy/workspace-namespace.yaml`. For an explicit namespace, the helper creates it on install/upgrade and leaves it in place on uninstall. Plain `helm install` also works if the namespace already exists; pass `--namespace` explicitly.

## Repository checkout

When `workspace.repository.url` is omitted, the clone init container is omitted and OpenCode starts against the PVC. At startup, the image registers that directory as a project and creates one visible `Workspace` session if no unarchived session exists there; Pod restarts reuse the existing session. To clone a repository, configure URL and optional branch, tag, or commit revision:

```yaml
workspace:
  repository:
    url: https://github.com/example/project.git
    revision: main
    authSecret:
      name: private-git-credentials
      usernameKey: username
      passwordKey: token
```

The referenced Secret must exist in the release namespace and contain those keys. Do not put credentials in the repository URL or Helm values. The init container uses a temporary `GIT_ASKPASS` helper and removes it on exit; clone failure prevents the OpenCode container from starting. A restart with an already initialized `.git` checkout does not reclone or reset the user's changes. No-clone mode does not create a Git repository.

## Secrets

Create a Secret out-of-band, or set `externalSecret.enabled=true` and configure the External Secrets resource:

```yaml
externalSecret:
  enabled: true
  secretStoreRef:
    kind: ClusterSecretStore
    name: workspace-secrets
  target:
    name: demo-workspace-secrets
  data:
    - secretKey: password
      remoteRef:
        key: workspaces/demo/opencode
        property: password
server:
  passwordSecret:
    name: demo-workspace-secrets
    key: password
```

Alternatively omit chart ExternalSecret generation and reference a pre-existing Secret. Repository clone authentication and server password references are independent. Use `runtime.env` only for non-sensitive environment values; pass provider credentials with `runtime.envFrom` Secret references or other Kubernetes secret references. Rendered Helm resources contain references and remote SecretStore keys, never remote secret payloads. OpenCode v2 always requires Basic Auth with the fixed user `opencode`. `server.passwordSecret` supplies the password as `OPENCODE_PASSWORD`; without it, the entrypoint generates one and prints it once to the container log as `server password <value>`. Either way the server and every web-terminal PTY inherit it, so `opencode --server http://localhost:4096` works from the web terminal without extra input. The HTTPRoute itself does not require a password Secret.

## Networking and sizing

`httpRoute` is disabled by default and renders only Gateway API HTTPRoute (never Ingress). Set `parentRefs`, `hostnames`, `rules`, labels, and annotations as needed by the Gateway controller. The default route rule matches `/` and forwards to the workspace Service. OpenCode v2 serves its browser UI and API from the root path; path-prefix hosting is not guaranteed by this PoC.

`vpa` is disabled by default. If enabled, configure `updatePolicy` and `resourcePolicy` for the installed VPA version. Its default update mode is `Off` to avoid replacing an active interactive workspace automatically. `priorityClassName` optionally assigns a cluster PriorityClass to the workspace Pod. CPU/memory requests and limits, pod/container security contexts, probes, ServiceAccount behavior, image, command/args, env, and node placement are configurable.

Defaults follow the target GitOps conventions reviewed for this cluster: restricted Pod Security Admission, non-root UID/GID 10001, `RuntimeDefault` seccomp, dropped Linux capabilities, `sa-<release>` ServiceAccount names, Gateway API v1 HTTPRoute, and External Secrets Operator v1 with a namespaced SecretStore by default. Override Gateway parent references, store kind/name, labels, annotations, sync metadata, and VPA policy through values. Liveness/readiness/startup probes use TCP by default so HTTP Basic Auth does not make an HTTP probe fail; setting a probe's `path` switches that probe to HTTP GET.

## Ephemeral data and cleanup

The PVC stores the checkout and all runtime writes and is unique to each Helm release. It is mounted through sibling subPaths: `workspace` at the workspace mount, `data`, `state`, and `cache` at the Takt user's XDG directories (OpenCode data, the private VFS store, Engram memory, tool caches), and `tmp` at `/tmp`. Private state therefore persists with the workspace but never lives inside the workspace tree, which the VFS requires. Uninstall deletes the PVC by default. Set `persistence.retain=true` to preserve the PVC object intentionally. Even when a claim is deleted, actual backing PV deletion follows that StorageClass's reclaim policy (`Delete` or `Retain`); verify it before deploying user data. Git changes intended to survive should be pushed to the repository before deleting the workspace. S3/artifact export is outside this chart's scope.
