# Native Terraform mock-provider tests for the phase-one Coder template.
#
# Blind authorship: written from the frozen contracts alone - Engram #15074
# (narrow correction to #15065: Terraform >=1.9.0,<2.0.0, coder/coder =2.0.2,
# hashicorp/kubernetes =2.16.0 and the explicit production TAKT_WORKSPACE_DIR),
# #15065 (architecture: inputs, resources, outputs, lifecycle; superseded only
# where #15074 replaces it), #15062 (approved scope), #15066 (D2 disposition),
# #15067 (acceptance CA-01..CA-08), #15068 (QA plan and traceability matrix) and
# #15069 (work unit N4). No implementation file of this work unit was read.
#
# Execution model:
#   * `terraform -chdir=deploy/coder test` discovers this file through the
#     module's `tests/` directory and runs it against the module in
#     `deploy/coder`.
#   * `mock_provider` replaces the coder and kubernetes providers, so no Coder
#     deployment, cluster, registry, kubeconfig or credential is required.
#   * every run block uses `command = plan`; nothing is ever applied.
#   * each run supplies its own `override_data` values instead of relying on
#     file-level overrides, so every run is self-contained.
#   * negative cases use `expect_failures` on the input variable whose custom
#     condition the contract requires; every other input of that run is valid.
#
# Consumed dependency and path correction (#15074):
#   * the module declares Terraform >=1.9.0,<2.0.0, coder/coder =2.0.2 and
#     hashicorp/kubernetes =2.16.0; this file consumes those declarations
#     rather than restating or widening them. The floor is load-bearing here:
#     the cross-variable persistence validation asserted below (a non-null
#     storage_class_name with persistence_allowed=false) is only available
#     from Terraform 1.9, so 1.7/1.8 engines cannot express this file's
#     negative case.
#   * the production agent environment carries the literal value
#     TAKT_WORKSPACE_DIR=/workspace, because the Coder command bypasses
#     workspace-entrypoint and that entrypoint's own default is never
#     executed. It is the existing image-owned workspace variable, not a
#     Terraform input or a per-human parameter, so it is asserted as an agent
#     environment literal and is never operator- or user-supplied.
#   * the repository hint (TAKT_REPOSITORY_URL) lives in the same agent
#     environment, which is where the contract places it.
#   * coder_agent mock_resource defaults make id and init_script deterministic
#     during plan, preserving exact app-agent and Pod-init-script binding
#     assertions without apply. Terraform test mocking is supported from 1.7;
#     no syntax beyond the module floor 1.9 is required.
#
# Observable surface: a plan exposes the values the module configures
# (resource counts, names, labels, security contexts, mounts, environment,
# secret references, storage) and the module outputs. It does not expose an
# inventory of resource types, and no apply-time, cluster, CNI or
# container-runtime behaviour is observable here. Those conditions belong to
# the Docker smoke runner and to live two-human validation.
#
# Uncovered conditions, returned to the verifier as uncovered rather than
# silently dropped:
#   * persist_workspace=true with persistence_allowed=false: #15065 and #15067
#     CA-02 fix "a resource lifecycle precondition fails before writes", and
#     the #15074 correction confirms this "remains the existing resource
#     lifecycle precondition" while still naming no checkable object for it.
#     No exact `expect_failures` reference can be written blind, so this
#     condition stays uncovered here. Its variable-level sibling - a non-null
#     storage_class_name with persistence_allowed=false - is covered below,
#     because #15074 names var.storage_class_name for it.
#   * An omitted required variable: Terraform reports it as a core "no value
#     for required variable" diagnostic rather than a custom condition on a
#     checkable object, so `expect_failures` cannot cover it. The contract's
#     "missing required value produces a variable-name diagnostic" requirement
#     is asserted here through the null, whitespace and format cases.
#   * Absence of a Service, Ingress or custom controller: a plan exposes no
#     inventory of resource kinds.
#   * Completeness of the output set: a plan exposes the declared outputs, not
#     the absence of undeclared ones.
#   * Live two-human access, isolation and denial, CNI enforcement, storage
#     reclamation and live memory behaviour: those need the T1-T6
#     prerequisites and are not observable through mocked plans.

mock_provider "coder" {
  mock_resource "coder_agent" {
    defaults = {
      id = "mock-coder-agent-id"
      init_script = "mock-coder-agent-init-script"
    }
  }
}
mock_provider "kubernetes" {}

# ---------------------------------------------------------------------------
# CA-02 / D1: per-human identity. Two owners and three workspaces (one owner
# holding two distinct workspaces) must yield exactly takt-<workspace UUID>
# names, the three frozen labels, and owner-scoped secret environment only.
# ---------------------------------------------------------------------------

run "identity_owner_a_workspace_a1" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    architecture    = "amd64"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        MODEL_API_KEY = { name = "takt-owner-a-secret", key = "model-api-key" }
      }
      "d4a7e0f1-2c69-4b83-a5de-91f0c7b2e845" = {
        MODEL_API_KEY = { name = "takt-owner-b-secret", key = "model-api-key" }
      }
    }
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
      name        = "owner-a-workspace-one"
      start_count = 1
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
      name      = "owner-a-login"
      full_name = "Owner A"
      email     = "owner-a@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "https://git.example.invalid/team/takt-ai.git" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "false" }
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
    error_message = "Pod name must be exactly takt-<workspace UUID>."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].metadata[0].namespace == "takt-phase-one-test"
    error_message = "The workspace Pod must be created in the operator-configured namespace."
  }

  assert {
    condition = kubernetes_pod_v1.workspace[0].metadata[0].labels["app.kubernetes.io/name"] == "takt-coder" && kubernetes_pod_v1.workspace[0].metadata[0].labels["takt.ai/workspace-id"] == "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_pod_v1.workspace[0].metadata[0].labels["takt.ai/owner-id"] == "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
    error_message = "Pod labels must be app.kubernetes.io/name=takt-coder, takt.ai/workspace-id=<workspace UUID>, takt.ai/owner-id=<owner UUID>."
  }

  assert {
    condition = !strcontains(kubernetes_pod_v1.workspace[0].metadata[0].name, "@") && !strcontains(kubernetes_pod_v1.workspace[0].metadata[0].name, "owner-a-login") && alltrue([for v in values(kubernetes_pod_v1.workspace[0].metadata[0].labels) : !strcontains(v, "@") && !strcontains(v, "owner-a-login")])
    error_message = "No human email or mutable username may enter resource names or label values; identity is the owner UUID only."
  }

  assert {
    condition     = length(kubernetes_pod_v1.workspace[0].spec[0].container) == 1 && kubernetes_pod_v1.workspace[0].spec[0].container[0].name == "workspace"
    error_message = "The Pod must run exactly one main container, named workspace."
  }

  assert {
    condition = toset([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : try(e.value_from[0].secret_key_ref[0].name, "") if try(e.value_from[0].secret_key_ref[0].name, "") != ""]) == toset(["takt-owner-a-secret"])
    error_message = "Only lookup(owner_secret_env, owner.id, {}) may reach this Pod: owner A gets takt-owner-a-secret and never owner B's secret, although both mappings exist."
  }

  assert {
    condition     = contains([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.name], "MODEL_API_KEY")
    error_message = "The owner-mapped environment variable MODEL_API_KEY must reach its owner's Pod."
  }

  assert {
    condition     = contains([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.name], "CODER_AGENT_TOKEN")
    error_message = "CODER_AGENT_TOKEN must be injected into the workspace Pod."
  }

  assert {
    condition     = tolist(kubernetes_pod_v1.workspace[0].spec[0].container[0].command) == tolist(["/bin/sh", "-c", coder_agent.workspace.init_script])
    error_message = "The Pod command must be exactly [\"/bin/sh\", \"-c\", coder_agent.workspace.init_script]."
  }

  assert {
    condition     = coder_agent.workspace.os == "linux" && coder_agent.workspace.arch == "amd64" && coder_agent.workspace.dir == "/workspace" && coder_agent.workspace.startup_script_behavior == "blocking" && length(trimspace(coder_agent.workspace.startup_script)) > 0
    error_message = "The agent must run on linux/amd64 in /workspace, with a blocking startup script."
  }

  assert {
    condition = length(coder_agent.workspace.display_apps) == 1 && alltrue([for app in coder_agent.workspace.display_apps : app.web_terminal && app.ssh_helper && !app.vscode && !app.vscode_insiders && !try(coalesce(app.port_forwarding_helper, false), false)])
    error_message = "display_apps must enable web_terminal and ssh_helper only; vscode, vscode_insiders and (where the pinned schema declares it) port_forwarding_helper stay disabled."
  }

  assert {
    condition     = coder_app.opencode.slug == "opencode" && coder_app.opencode.display_name == "OpenCode" && coder_app.opencode.command == "cd /workspace && exec opencode" && coder_app.opencode.agent_id == coder_agent.workspace.id
    error_message = "coder_app.opencode must be the terminal command app 'cd /workspace && exec opencode', bound to the workspace agent."
  }

  assert {
    condition     = try(coalesce(coder_app.opencode.url, null), null) == null && try(coalesce(coder_app.opencode.share, "owner"), "owner") == "owner" && try(coalesce(coder_app.opencode.subdomain, false), false) == false
    error_message = "coder_app.opencode must expose no HTTP URL and no share setting beyond the owner."
  }

  assert {
    condition = kubernetes_pod_v1.workspace[0].spec[0].security_context[0].run_as_non_root == true && tostring(kubernetes_pod_v1.workspace[0].spec[0].security_context[0].run_as_user) == "10001" && tostring(kubernetes_pod_v1.workspace[0].spec[0].security_context[0].run_as_group) == "10001" && tostring(kubernetes_pod_v1.workspace[0].spec[0].security_context[0].fs_group) == "10001" && kubernetes_pod_v1.workspace[0].spec[0].security_context[0].fs_group_change_policy == "OnRootMismatch" && kubernetes_pod_v1.workspace[0].spec[0].security_context[0].seccomp_profile[0].type == "RuntimeDefault"
    error_message = "Pod security context must be non-root UID/GID/fsGroup 10001, fsGroupChangePolicy OnRootMismatch, seccomp RuntimeDefault."
  }

  assert {
    condition = kubernetes_pod_v1.workspace[0].spec[0].container[0].security_context[0].allow_privilege_escalation == false && toset(kubernetes_pod_v1.workspace[0].spec[0].container[0].security_context[0].capabilities[0].drop) == toset(["ALL"])
    error_message = "The main container must be non-root without privilege escalation, dropping ALL capabilities."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].restart_policy == "Never" && kubernetes_pod_v1.workspace[0].spec[0].automount_service_account_token == false && length(kubernetes_pod_v1.workspace[0].spec[0].image_pull_secrets) == 0
    error_message = "restartPolicy must be Never, the service account token must not automount, and no image pull secret is configured by default."
  }

  assert {
    condition     = try([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.value if e.name == "HOME"][0], null) == "/home/takt"
    error_message = "The Pod must carry HOME=/home/takt as a plain environment value."
  }

  assert {
    condition     = try(coder_agent.workspace.env["TAKT_WORKSPACE_DIR"], "missing") == "/workspace" && try(coder_agent.workspace.env["TAKT_REPOSITORY_URL"], "missing") == "https://git.example.invalid/team/takt-ai.git"
    error_message = "The agent environment must set the literal production TAKT_WORKSPACE_DIR=/workspace and deliver the configured repository hint through TAKT_REPOSITORY_URL."
  }

  assert {
    condition = toset([for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : m.mount_path]) == toset(["/workspace", "/home/takt/.local/share", "/home/takt/.local/state", "/home/takt/.cache", "/tmp"]) && alltrue([for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : !contains(["/home/takt", "/home/takt/.config", "/home/takt/.opencode", "/home/takt/.local/bin"], m.mount_path)])
    error_message = "The main container must mount exactly /workspace, /home/takt/.local/share, /home/takt/.local/state, /home/takt/.cache and /tmp, and must never mount over baked image state."
  }

  assert {
    condition     = length(kubernetes_pod_v1.workspace[0].spec[0].volume) == 2 && alltrue([for v in kubernetes_pod_v1.workspace[0].spec[0].volume : length(v.host_path) == 0])
    error_message = "The Pod must use exactly the runtime volume plus the /tmp volume, and no host path volume."
  }

  assert {
    condition = length(kubernetes_pod_v1.workspace[0].spec[0].init_container) == 1 && kubernetes_pod_v1.workspace[0].spec[0].init_container[0].name == "prepare-runtime" && kubernetes_pod_v1.workspace[0].spec[0].init_container[0].image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90" && toset([for m in kubernetes_pod_v1.workspace[0].spec[0].init_container[0].volume_mount : m.mount_path]) == toset(["/mnt/runtime"])
    error_message = "Init container prepare-runtime must use the same workspace image and mount the runtime volume at /mnt/runtime."
  }

  assert {
    condition     = tolist(slice(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args), 0, 2)) == tolist(["/bin/bash", "-euc"]) && length(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args)) == 3
    error_message = "The init container must invoke exactly /bin/bash -euc with one constant source."
  }

  assert {
    condition = alltrue([for d in ["workspace", "data", "state", "cache"] : strcontains(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args)[2], d)]) && !strcontains(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args)[2], "takt-ai setup") && !strcontains(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args)[2], "curl") && !strcontains(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args)[2], "git clone")
    error_message = "The init source must create the workspace/data/state/cache siblings and must not install software, clone a repository or use the network."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].node_selector["kubernetes.io/arch"] == "amd64"
    error_message = "The architecture variable must drive the kubernetes.io/arch node selector."
  }

  assert {
    condition     = length(kubernetes_persistent_volume_claim_v1.workspace) == 0
    error_message = "Ephemeral mode must create no PVC (PVC count is persist_workspace ? 1 : 0)."
  }

  assert {
    condition = kubernetes_network_policy_v1.workspace.metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_network_policy_v1.workspace.metadata[0].namespace == "takt-phase-one-test" && kubernetes_network_policy_v1.workspace.spec[0].pod_selector[0].match_labels["takt.ai/workspace-id"] == "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && tolist(kubernetes_network_policy_v1.workspace.spec[0].policy_types) == tolist(["Ingress"]) && length(kubernetes_network_policy_v1.workspace.spec[0].ingress) == 0 && length(kubernetes_network_policy_v1.workspace.spec[0].egress) == 0
    error_message = "The NetworkPolicy must be takt-<workspace UUID>, select only its own workspace-id label, admit no ingress and not restrict egress."
  }

  assert {
    condition     = output.workspace_name == "owner-a-workspace-one" && output.owner_id == "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
    error_message = "Outputs must report the Coder workspace name and the owner UUID."
  }

  assert {
    condition     = output.pod_name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && output.namespace == "takt-phase-one-test" && output.image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90" && output.architecture == "amd64"
    error_message = "Outputs must report the pod name, namespace, image and architecture of the selected configuration."
  }

  assert {
    condition     = output.persistence_mode == "ephemeral" && output.pvc_name == null
    error_message = "Outputs must report persistence_mode=ephemeral and pvc_name=null while no PVC is requested."
  }
}

# The same owner instantiating a second workspace must produce a second, fully
# distinct resource set (the namespace and the owner are shared infrastructure).
run "identity_same_owner_second_workspace" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        MODEL_API_KEY = { name = "takt-owner-a-secret", key = "model-api-key" }
      }
    }
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d"
      name        = "owner-a-workspace-two"
      start_count = 1
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
      name      = "owner-a-login"
      full_name = "Owner A"
      email     = "owner-a@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "false" }
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].metadata[0].name == "takt-2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d" && kubernetes_pod_v1.workspace[0].metadata[0].labels["takt.ai/workspace-id"] == "2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d" && kubernetes_pod_v1.workspace[0].metadata[0].labels["takt.ai/owner-id"] == "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
    error_message = "A second workspace of the same owner must get its own takt-<workspace UUID> name while sharing the owner label."
  }

  assert {
    condition     = kubernetes_network_policy_v1.workspace.metadata[0].name == "takt-2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d" && kubernetes_network_policy_v1.workspace.spec[0].pod_selector[0].match_labels["takt.ai/workspace-id"] == "2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d"
    error_message = "The NetworkPolicy must follow its own workspace identity, not the owner's."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].metadata[0].name != "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
    error_message = "Two distinct workspaces of one owner must not share a resource name."
  }

  assert {
    condition     = try(coder_agent.workspace.env["TAKT_REPOSITORY_URL"], "missing") == ""
    error_message = "With no repository configured the agent environment must carry an empty TAKT_REPOSITORY_URL, not a default repository."
  }
}

# CA-02 / D1: owner B receives its own owner-scoped secret and never owner A's,
# even though the operator variable carries both mappings.
run "identity_owner_b_workspace_b1" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        MODEL_API_KEY = { name = "takt-owner-a-secret", key = "model-api-key" }
      }
      "d4a7e0f1-2c69-4b83-a5de-91f0c7b2e845" = {
        MODEL_API_KEY = { name = "takt-owner-b-secret", key = "model-api-key" }
      }
    }
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "3b4c5d6e-7f8a-4b9c-a1d2-e3f4a5b6c7d8"
      name        = "owner-b-workspace-one"
      start_count = 1
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "d4a7e0f1-2c69-4b83-a5de-91f0c7b2e845"
      name      = "owner-b-login"
      full_name = "Owner B"
      email     = "owner-b@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "false" }
  }

  assert {
    condition = toset([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : try(e.value_from[0].secret_key_ref[0].name, "") if try(e.value_from[0].secret_key_ref[0].name, "") != ""]) == toset(["takt-owner-b-secret"])
    error_message = "Owner B must receive exactly its own secret reference and never owner A's secret."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].metadata[0].name == "takt-3b4c5d6e-7f8a-4b9c-a1d2-e3f4a5b6c7d8" && kubernetes_pod_v1.workspace[0].metadata[0].namespace == "takt-phase-one-test" && kubernetes_pod_v1.workspace[0].metadata[0].labels["takt.ai/owner-id"] == "d4a7e0f1-2c69-4b83-a5de-91f0c7b2e845"
    error_message = "Owner B's objects must carry owner B's identity and never owner A's."
  }

  assert {
    condition     = output.owner_id == "d4a7e0f1-2c69-4b83-a5de-91f0c7b2e845" && output.workspace_name == "owner-b-workspace-one"
    error_message = "The outputs must describe the workspace actually being provisioned, not another owner's."
  }
}

# ---------------------------------------------------------------------------
# CA-03 / D1 / D3: lifecycle through Coder operations only. The agent, the app
# and the NetworkPolicy are not start_count gated; the Pod count follows
# start_count; the PVC count follows persist_workspace alone.
# ---------------------------------------------------------------------------

# A deliberate stop removes compute and ephemeral storage while the agent, the
# app and the workspace-scoped policy remain declared.
run "lifecycle_stopped_ephemeral_removes_pod_but_keeps_agent" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
      name        = "owner-a-workspace-one"
      start_count = 0
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
      name      = "owner-a-login"
      full_name = "Owner A"
      email     = "owner-a@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "false" }
  }

  assert {
    condition     = length(kubernetes_pod_v1.workspace) == 0
    error_message = "Pod count must equal data.coder_workspace.me.start_count: a stopped workspace has no Pod."
  }

  assert {
    condition     = length(kubernetes_persistent_volume_claim_v1.workspace) == 0
    error_message = "A stopped ephemeral workspace must have neither Pod nor PVC."
  }

  assert {
    condition     = length(trimspace(coder_agent.workspace.startup_script)) > 0 && coder_agent.workspace.dir == "/workspace" && coder_agent.workspace.startup_script_behavior == "blocking" && coder_agent.workspace.arch == "amd64" && coder_agent.workspace.os == "linux"
    error_message = "The agent must remain declared while the workspace is stopped, with the default linux/amd64 platform and a blocking startup script."
  }

  assert {
    condition     = coder_app.opencode.slug == "opencode" && coder_app.opencode.command == "cd /workspace && exec opencode"
    error_message = "coder_app.opencode must not be start_count gated."
  }

  assert {
    condition     = kubernetes_network_policy_v1.workspace.metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && tolist(kubernetes_network_policy_v1.workspace.spec[0].policy_types) == tolist(["Ingress"]) && length(kubernetes_network_policy_v1.workspace.spec[0].ingress) == 0
    error_message = "The workspace-scoped NetworkPolicy must not be start_count gated."
  }

  assert {
    condition     = output.pod_name == null
    error_message = "pod_name must be null while the workspace is stopped."
  }

  assert {
    condition     = output.namespace == "takt-phase-one-test" && output.image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90" && output.architecture == "amd64" && output.persistence_mode == "ephemeral" && output.pvc_name == null && output.workspace_name == "owner-a-workspace-one" && output.owner_id == "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
    error_message = "Configuration outputs must stay truthful while compute is stopped."
  }
}

# Opted-in persistence survives the stop: the PVC is independent of
# start_count, so it remains while no Pod exists.
run "lifecycle_stopped_persistent_keeps_pvc" {
  command = plan

  variables {
    namespace           = "takt-phase-one-test"
    workspace_image     = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    persistence_allowed = true
    storage_class_name  = "standard"
    storage_size_gib    = 20
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
      name        = "owner-a-workspace-one"
      start_count = 0
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
      name      = "owner-a-login"
      full_name = "Owner A"
      email     = "owner-a@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "true" }
  }

  assert {
    condition     = length(kubernetes_pod_v1.workspace) == 0 && length(kubernetes_persistent_volume_claim_v1.workspace) == 1
    error_message = "A stopped persistent workspace must keep exactly one PVC and no Pod."
  }

  assert {
    condition     = kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].namespace == "takt-phase-one-test" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].labels["app.kubernetes.io/name"] == "takt-coder" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].labels["takt.ai/workspace-id"] == "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].labels["takt.ai/owner-id"] == "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
    error_message = "The PVC must be named takt-<workspace UUID> with the three frozen labels, like the Pod and the NetworkPolicy."
  }

  assert {
    condition     = kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].access_modes == ["ReadWriteOnce"] && kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].storage_class_name == "standard" && tostring(kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].resources[0].requests["storage"]) == "20Gi"
    error_message = "The PVC must use ReadWriteOnce, the operator-supplied storage class and the operator-supplied size in Gi."
  }

  assert {
    condition     = output.pod_name == null && output.persistence_mode == "workspace-pvc" && output.pvc_name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
    error_message = "Outputs must report the retained PVC and an absent Pod while persistence is opted in and the workspace is stopped."
  }
}

# A running persistent workspace attaches its own PVC to the runtime volume and
# mounts the sibling subPaths from it.
run "lifecycle_running_persistent_uses_pvc" {
  command = plan

  variables {
    namespace           = "takt-phase-one-test"
    workspace_image     = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    persistence_allowed = true
    storage_class_name  = "standard"
    storage_size_gib    = 20
    image_pull_secrets  = ["takt-registry-pull"]
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
      name        = "owner-a-workspace-one"
      start_count = 1
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
      name      = "owner-a-login"
      full_name = "Owner A"
      email     = "owner-a@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "true" }
  }

  assert {
    condition     = length(kubernetes_pod_v1.workspace) == 1 && length(kubernetes_persistent_volume_claim_v1.workspace) == 1
    error_message = "A running persistent workspace must have exactly one Pod and one PVC."
  }

  assert {
    condition     = length([for v in kubernetes_pod_v1.workspace[0].spec[0].volume : v if length(v.persistent_volume_claim) > 0]) == 1 && [for v in kubernetes_pod_v1.workspace[0].spec[0].volume : v.persistent_volume_claim[0].claim_name if length(v.persistent_volume_claim) > 0][0] == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
    error_message = "Exactly one Pod volume must claim the workspace PVC: the runtime volume is the PVC when persistence is enabled."
  }

  assert {
    condition     = alltrue([for v in kubernetes_pod_v1.workspace[0].spec[0].volume : length(v.empty_dir) == 0 if length(v.persistent_volume_claim) > 0])
    error_message = "The persistent runtime volume must not also be an emptyDir."
  }

  assert {
    condition = [for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : try(m.sub_path, "") if m.mount_path == "/workspace"][0] == "workspace" && [for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : try(m.sub_path, "") if m.mount_path == "/home/takt/.local/share"][0] == "data" && [for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : try(m.sub_path, "") if m.mount_path == "/home/takt/.local/state"][0] == "state" && [for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : try(m.sub_path, "") if m.mount_path == "/home/takt/.cache"][0] == "cache"
    error_message = "The runtime volume must be mounted at /workspace (workspace), /home/takt/.local/share (data), /home/takt/.local/state (state) and /home/takt/.cache (cache) through its sibling subPaths."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].container[0].image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90" && length(kubernetes_pod_v1.workspace[0].spec[0].image_pull_secrets) == 1 && kubernetes_pod_v1.workspace[0].spec[0].image_pull_secrets[0].name == "takt-registry-pull"
    error_message = "The operator-supplied image and image pull secrets must be applied to the workspace Pod."
  }

  assert {
    condition = try(coalesce(kubernetes_pod_v1.workspace[0].spec[0].host_network, false), false) == false && length(kubernetes_pod_v1.workspace[0].spec[0].volume) == 2
    error_message = "The Pod must not use host networking and must still use exactly the runtime volume plus the /tmp volume."
  }

  assert {
    condition     = output.persistence_mode == "workspace-pvc" && output.pvc_name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && output.pod_name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
    error_message = "Outputs must report workspace-pvc persistence with both resource names while running."
  }
}

# Defaults: an omitted storage class leaves storageClassName unset for the
# cluster default, the default size renders as 10Gi, the default architecture
# is amd64, and the default resource envelope renders as documented.
run "lifecycle_running_persistent_default_storage_and_envelope" {
  command = plan

  variables {
    namespace           = "takt-phase-one-test"
    workspace_image     = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    persistence_allowed = true
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
      name        = "owner-a-workspace-one"
      start_count = 1
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
      name      = "owner-a-login"
      full_name = "Owner A"
      email     = "owner-a@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "true" }
  }

  assert {
    condition     = kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].storage_class_name == null && tostring(kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].resources[0].requests["storage"]) == "10Gi" && kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].access_modes == ["ReadWriteOnce"]
    error_message = "A null storage class must omit storageClassName for the cluster default, with the default 10Gi request and ReadWriteOnce access."
  }

  assert {
    condition     = coder_agent.workspace.arch == "amd64" && kubernetes_pod_v1.workspace[0].spec[0].node_selector["kubernetes.io/arch"] == "amd64"
    error_message = "The default architecture is amd64 and must drive both the agent and the node selector."
  }

  assert {
    condition = tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["cpu"]) == "500m" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["cpu"]) == "2000m" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["memory"]) == "1024Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["memory"]) == "4096Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["ephemeral-storage"]) == "512Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["ephemeral-storage"]) == "2048Mi"
    error_message = "The default resource envelope must render as 500m/2000m CPU, 1024Mi/4096Mi memory and 512Mi/2048Mi ephemeral storage."
  }
}

# CA-04: architecture drives the agent, the node selector and nothing else.
run "architecture_arm64_sets_agent_and_node_selector" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    architecture    = "arm64"
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
      name        = "owner-a-workspace-one"
      start_count = 1
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
      name      = "owner-a-login"
      full_name = "Owner A"
      email     = "owner-a@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "false" }
  }

  assert {
    condition     = coder_agent.workspace.arch == "arm64" && coder_agent.workspace.os == "linux"
    error_message = "The architecture variable must set the agent arch, while the OS stays fixed at linux."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].node_selector["kubernetes.io/arch"] == "arm64" && alltrue([for k, v in kubernetes_pod_v1.workspace[0].spec[0].node_selector : k == "kubernetes.io/arch" || (k == "kubernetes.io/os" && v == "linux")])
    error_message = "The node selector must carry the selected architecture (and at most the fixed linux OS), not a host-inferred value."
  }

  assert {
    condition     = output.architecture == "arm64"
    error_message = "The architecture output must report the selected value."
  }
}

# CA-01: the structured resource envelope renders for both containers, and the
# init container shares the same envelope and image.
run "resources_envelope_rendering" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    resources = {
      cpu_request_milli     = 750
      cpu_limit_milli       = 1500
      memory_request_mib    = 2048
      memory_limit_mib      = 3072
      ephemeral_request_mib = 1024
      ephemeral_limit_mib   = 4096
    }
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
      name        = "owner-a-workspace-one"
      start_count = 1
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
      name      = "owner-a-login"
      full_name = "Owner A"
      email     = "owner-a@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "false" }
  }

  assert {
    condition = tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["cpu"]) == "750m" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["cpu"]) == "1500m" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["memory"]) == "2048Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["memory"]) == "3072Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["ephemeral-storage"]) == "1024Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["ephemeral-storage"]) == "4096Mi"
    error_message = "Milli/mebibyte integers must render as Kubernetes quantity strings: CPU as Nm, memory and ephemeral storage as NMi."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].init_container[0].resources[0] == kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0]
    error_message = "The init container and the main container must consume the same resource envelope."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].init_container[0].image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    error_message = "The init container must use the same workspace image as the main container."
  }
}

# CA-01 / CA-06: with no owner mapping and no repository configured, the Pod
# carries no secret reference, no envFrom, and an empty repository hint.
run "env_defaults_carry_no_secret_reference" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
      name        = "owner-a-workspace-one"
      start_count = 1
      access_url  = "https://coder.example.invalid"
    }
  }

  override_data {
    target = data.coder_workspace_owner.me
    values = {
      id        = "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
      name      = "owner-a-login"
      full_name = "Owner A"
      email     = "owner-a@example.invalid"
    }
  }

  override_data {
    target = data.coder_parameter.repository_url
    values = { value = "" }
  }

  override_data {
    target = data.coder_parameter.persist_workspace
    values = { value = "false" }
  }

  assert {
    condition     = length([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e if length(e.value_from) > 0]) == 0
    error_message = "With owner_secret_env defaulting to {} no environment value may come from a Secret reference."
  }

  assert {
    condition     = length(kubernetes_pod_v1.workspace[0].spec[0].container[0].env_from) == 0
    error_message = "No envFrom block may exist: only explicit secretKeyRef entries are allowed."
  }

  assert {
    condition     = try(coder_agent.workspace.env["TAKT_WORKSPACE_DIR"], "missing") == "/workspace" && try(coder_agent.workspace.env["TAKT_REPOSITORY_URL"], "missing") == ""
    error_message = "The agent environment must always set the literal TAKT_WORKSPACE_DIR=/workspace and default the repository hint to an empty TAKT_REPOSITORY_URL rather than any organization value."
  }

  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].container[0].working_dir == "/workspace"
    error_message = "The main container working directory must be /workspace."
  }
}

# ---------------------------------------------------------------------------
# CA-01: negative cases. Each run supplies one invalid operator input and
# expects the custom condition on that input variable to fail; every other
# input of the run is valid, so the diagnostic can only come from the variable
# named in expect_failures.
# ---------------------------------------------------------------------------

run "reject_namespace_uppercase" {
  command  = plan
  variables {
    namespace       = "Takt-Prod"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.namespace]
}

run "reject_namespace_underscore" {
  command  = plan
  variables {
    namespace       = "takt_prod"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.namespace]
}

run "reject_namespace_leading_whitespace" {
  command  = plan
  variables {
    namespace       = " takt-prod"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.namespace]
}

run "reject_namespace_over_63_characters" {
  command  = plan
  variables {
    namespace       = "takt-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.namespace]
}

run "reject_image_tagged_not_digest_pinned" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai:2.0.28"
  }
  expect_failures = [var.workspace_image]
}

run "reject_image_with_url_scheme" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "https://registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.workspace_image]
}

run "reject_image_with_surrounding_whitespace" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = " registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.workspace_image]
}

run "reject_image_with_short_digest" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9"
  }
  expect_failures = [var.workspace_image]
}

run "reject_image_without_repository_path" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.workspace_image]
}

run "reject_architecture_not_in_enum" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    architecture    = "x86_64"
  }
  expect_failures = [var.architecture]
}

run "reject_architecture_with_whitespace" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    architecture    = "amd64 "
  }
  expect_failures = [var.architecture]
}

run "reject_resources_cpu_request_above_limit" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    resources = {
      cpu_request_milli     = 4000
      cpu_limit_milli       = 2000
      memory_request_mib    = 1024
      memory_limit_mib      = 4096
      ephemeral_request_mib = 512
      ephemeral_limit_mib   = 2048
    }
  }
  expect_failures = [var.resources]
}

run "reject_resources_non_positive_value" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    resources = {
      cpu_request_milli     = 500
      cpu_limit_milli       = 2000
      memory_request_mib    = 1024
      memory_limit_mib      = 0
      ephemeral_request_mib = 512
      ephemeral_limit_mib   = 2048
    }
  }
  expect_failures = [var.resources]
}

run "reject_resources_ephemeral_request_above_limit" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    resources = {
      cpu_request_milli     = 500
      cpu_limit_milli       = 2000
      memory_request_mib    = 1024
      memory_limit_mib      = 4096
      ephemeral_request_mib = 4096
      ephemeral_limit_mib   = 2048
    }
  }
  expect_failures = [var.resources]
}

run "reject_repository_ssh_url" {
  command  = plan
  variables {
    namespace              = "takt-phase-one-test"
    workspace_image        = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "git@git.example.invalid:team/takt-ai.git"
  }
  expect_failures = [var.repository_url_default]
}

run "reject_repository_plain_http_url" {
  command  = plan
  variables {
    namespace              = "takt-phase-one-test"
    workspace_image        = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "http://git.example.invalid/team/takt-ai.git"
  }
  expect_failures = [var.repository_url_default]
}

run "reject_repository_embedded_userinfo" {
  command  = plan
  variables {
    namespace              = "takt-phase-one-test"
    workspace_image        = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "https://user:password@git.example.invalid/team/takt-ai.git"
  }
  expect_failures = [var.repository_url_default]
}

run "reject_repository_query_string" {
  command  = plan
  variables {
    namespace              = "takt-phase-one-test"
    workspace_image        = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "https://git.example.invalid/team/takt-ai.git?ref=main"
  }
  expect_failures = [var.repository_url_default]
}

run "reject_repository_trailing_whitespace" {
  command  = plan
  variables {
    namespace              = "takt-phase-one-test"
    workspace_image        = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "https://git.example.invalid/team/takt-ai.git "
  }
  expect_failures = [var.repository_url_default]
}

# The forbidden persistence combination allowed by CA-01: a non-null storage
# class while persistence is not allowed. The contract requires a named-variable
# diagnostic but does not name the variable; this run pins storage_class_name,
# the value the combination makes illegal.
run "reject_storage_class_without_persistence_allowed" {
  command  = plan
  variables {
    namespace          = "takt-phase-one-test"
    workspace_image    = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    storage_class_name = "standard"
  }
  expect_failures = [var.storage_class_name]
}

run "reject_storage_class_empty_string" {
  command  = plan
  variables {
    namespace           = "takt-phase-one-test"
    workspace_image     = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    persistence_allowed = true
    storage_class_name  = ""
  }
  expect_failures = [var.storage_class_name]
}

run "reject_storage_size_below_range" {
  command  = plan
  variables {
    namespace           = "takt-phase-one-test"
    workspace_image     = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    persistence_allowed = true
    storage_size_gib    = 0
  }
  expect_failures = [var.storage_size_gib]
}

run "reject_storage_size_above_range" {
  command  = plan
  variables {
    namespace           = "takt-phase-one-test"
    workspace_image     = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    persistence_allowed = true
    storage_size_gib    = 1025
  }
  expect_failures = [var.storage_size_gib]
}

run "reject_image_pull_secret_invalid_dns_subdomain" {
  command  = plan
  variables {
    namespace          = "takt-phase-one-test"
    workspace_image    = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    image_pull_secrets = ["Takt_Registry_Pull"]
  }
  expect_failures = [var.image_pull_secrets]
}

# Reserved environment names must never reach an owner Pod: they would override
# bootstrap, access, OpenCode configuration or memory defaults.
run "reject_owner_env_reserved_home" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        HOME = { name = "takt-owner-a-secret", key = "home" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_coder_prefix" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        CODER_AGENT_TOKEN = { name = "takt-owner-a-secret", key = "token" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_takt_prefix" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        TAKT_REPOSITORY_URL = { name = "takt-owner-a-secret", key = "repository" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_opencode_prefix" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        OPENCODE_PASSWORD = { name = "takt-owner-a-secret", key = "password" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_engram_prefix" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        ENGRAM_DATA_DIR = { name = "takt-owner-a-secret", key = "engram-dir" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_xdg_prefix" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        XDG_DATA_HOME = { name = "takt-owner-a-secret", key = "data-home" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_lowercase_variable_name" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        model_api_key = { name = "takt-owner-a-secret", key = "model-api-key" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_uppercase_owner_key" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6B1F9C2E-8A34-4D7B-9F21-0C5E7A1D3B48" = {
        MODEL_API_KEY = { name = "takt-owner-a-secret", key = "model-api-key" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_invalid_secret_name" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        MODEL_API_KEY = { name = "Takt_Owner_Secret", key = "model-api-key" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_invalid_secret_key" {
  command  = plan
  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        MODEL_API_KEY = { name = "takt-owner-a-secret", key = "model/api/key" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}
