# Native Terraform mock-provider tests: runtime workspace behaviour of the
# Coder workspace template (deploy/coder).
#
# Blind authorship from the frozen contracts: #15112 (CW-T1-05 workspace RBAC
# portion, CW-T1-07, CW-T1-08), #15116 (WS check location), #15110 B1-B5 (image,
# identity, command, app, persistent runtime environment and mounts), C1/C7
# (credential reference and BACKSTAGE_URL), D4 (automount and RBAC). The
# implementation was not read.
#
# Scope: identity, labels and naming; Pod runtime security, environment, mounts
# and terminal app; the actual-owner Backstage credential reference; the
# workspace-scoped NetworkPolicy. Stop/start, PVC retention and output states are
# in lifecycle.tftest.hcl. Input acquisition is in inputs.tftest.hcl.
#
# Persistence is unconditional: each workspace always retains its PVC at
# kubernetes_persistent_volume_claim_v1.workspace[0] (#15110 B4). No persistence
# input or persist_workspace parameter exists and none is overridden here.
#
# Execution: terraform -chdir=deploy/coder test, plan only, no provider contact.

mock_provider "coder" {
  mock_resource "coder_agent" {
    defaults = {
      id          = "mock-coder-agent-id"
      init_script = "mock-coder-agent-init-script"
    }
  }
}
mock_provider "kubernetes" {}

run "identity_owner_a_workspace_a1" {
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

  override_resource {
    target          = coder_agent.workspace
    override_during = plan
    values = {
      id          = "mock-coder-agent-id"
      init_script = "mock-coder-agent-init-script"
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

  assert {
    condition     = kubernetes_pod_v1.workspace[0].metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_pod_v1.workspace[0].metadata[0].namespace == "takt-phase-one-test"
    error_message = "Pod name must be exactly takt-<workspace UUID> in the operator-configured namespace."
  }
  assert {
    condition     = kubernetes_pod_v1.workspace[0].metadata[0].labels["app.kubernetes.io/name"] == "takt-coder" && kubernetes_pod_v1.workspace[0].metadata[0].labels["takt.ai/workspace-id"] == "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_pod_v1.workspace[0].metadata[0].labels["takt.ai/owner-id"] == "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
    error_message = "Pod labels must be app.kubernetes.io/name=takt-coder, takt.ai/workspace-id=<workspace UUID>, takt.ai/owner-id=<owner UUID>."
  }
  assert {
    condition     = !strcontains(kubernetes_pod_v1.workspace[0].metadata[0].name, "@") && !strcontains(kubernetes_pod_v1.workspace[0].metadata[0].name, "owner-a-login") && alltrue([for v in values(kubernetes_pod_v1.workspace[0].metadata[0].labels) : !strcontains(v, "@") && !strcontains(v, "owner-a-login")])
    error_message = "No human email or mutable username may enter resource names or label values; identity is the owner UUID only."
  }
  assert {
    condition     = length(kubernetes_pod_v1.workspace[0].spec[0].container) == 1 && kubernetes_pod_v1.workspace[0].spec[0].container[0].name == "workspace"
    error_message = "The Pod must run exactly one main container, named workspace."
  }
  assert {
    condition     = toset([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : try(e.value_from[0].secret_key_ref[0].name, "") if try(e.value_from[0].secret_key_ref[0].name, "") != ""]) == toset(["takt-owner-a-secret"])
    error_message = "Only lookup(owner_secret_env, owner.id, {}) may reach this Pod: owner A gets takt-owner-a-secret and never owner B's secret, although both mappings exist."
  }
  assert {
    condition     = contains([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.name], "MODEL_API_KEY") && contains([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.name], "CODER_AGENT_TOKEN")
    error_message = "The owner-mapped MODEL_API_KEY and the CODER_AGENT_TOKEN must reach the workspace Pod."
  }
  assert {
    condition     = tolist(kubernetes_pod_v1.workspace[0].spec[0].container[0].command) == tolist(["/bin/sh", "-c", coder_agent.workspace.init_script])
    error_message = "The Pod command must be the shell invocation of coder_agent.workspace.init_script, bypassing the entrypoint."
  }
  assert {
    condition     = coder_agent.workspace.os == "linux" && coder_agent.workspace.arch == "amd64" && coder_agent.workspace.dir == "/workspace" && coder_agent.workspace.startup_script_behavior == "blocking" && length(trimspace(coder_agent.workspace.startup_script)) > 0
    error_message = "The agent must run on linux/amd64 in /workspace, with a blocking startup script."
  }
  assert {
    condition     = length(coder_agent.workspace.display_apps) == 1 && alltrue([for app in coder_agent.workspace.display_apps : app.web_terminal && app.ssh_helper && !app.vscode && !app.vscode_insiders && !try(coalesce(app.port_forwarding_helper, false), false)])
    error_message = "display_apps must enable web_terminal and ssh_helper only; vscode, vscode_insiders and port_forwarding_helper stay disabled."
  }
  assert {
    condition     = coder_app.opencode.slug == "opencode" && coder_app.opencode.display_name == "OpenCode" && coder_app.opencode.command == "cd /workspace && exec opencode" && coder_app.opencode.agent_id == coder_agent.workspace.id
    error_message = "coder_app.opencode must be the terminal command app 'cd /workspace && exec opencode', bound to the workspace agent."
  }
  assert {
    condition     = try(coder_app.opencode.url, null) == null && coder_app.opencode.share == "owner" && try(coalesce(coder_app.opencode.subdomain, false), false) == false
    error_message = "coder_app.opencode must expose no HTTP URL, must set share=owner, and must not use a subdomain."
  }
  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].security_context[0].run_as_non_root == true && tostring(kubernetes_pod_v1.workspace[0].spec[0].security_context[0].run_as_user) == "10001" && tostring(kubernetes_pod_v1.workspace[0].spec[0].security_context[0].run_as_group) == "10001" && tostring(kubernetes_pod_v1.workspace[0].spec[0].security_context[0].fs_group) == "10001" && kubernetes_pod_v1.workspace[0].spec[0].security_context[0].fs_group_change_policy == "OnRootMismatch" && kubernetes_pod_v1.workspace[0].spec[0].security_context[0].seccomp_profile[0].type == "RuntimeDefault"
    error_message = "Pod security context must be non-root UID/GID/fsGroup 10001, fsGroupChangePolicy OnRootMismatch, seccomp RuntimeDefault."
  }
  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].container[0].security_context[0].allow_privilege_escalation == false && toset(kubernetes_pod_v1.workspace[0].spec[0].container[0].security_context[0].capabilities[0].drop) == toset(["ALL"])
    error_message = "The main container must not allow privilege escalation and must drop ALL capabilities."
  }
  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].restart_policy == "Never" && kubernetes_pod_v1.workspace[0].spec[0].automount_service_account_token == false && length(kubernetes_pod_v1.workspace[0].spec[0].image_pull_secrets) == 0
    error_message = "restartPolicy must be Never, the service account token must not automount, and no image pull secret is configured by default."
  }
  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].node_selector["kubernetes.io/arch"] == "amd64"
    error_message = "The architecture variable must drive the kubernetes.io/arch node selector."
  }
  assert {
    condition     = kubernetes_network_policy_v1.workspace.metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_network_policy_v1.workspace.metadata[0].namespace == "takt-phase-one-test" && kubernetes_network_policy_v1.workspace.spec[0].pod_selector[0].match_labels["takt.ai/workspace-id"] == "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && tolist(kubernetes_network_policy_v1.workspace.spec[0].policy_types) == tolist(["Ingress"]) && length(kubernetes_network_policy_v1.workspace.spec[0].ingress) == 0 && length(kubernetes_network_policy_v1.workspace.spec[0].egress) == 0
    error_message = "The NetworkPolicy must be takt-<workspace UUID>, select only its own workspace-id label, admit no ingress and not restrict egress."
  }
  assert {
    condition     = output.workspace_name == "owner-a-workspace-one" && output.owner_id == "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" && output.pod_name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && output.namespace == "takt-phase-one-test" && output.architecture == "amd64"
    error_message = "Outputs must report the workspace name, owner UUID, pod name, namespace and architecture of the selected configuration."
  }
}

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

  assert {
    condition     = toset([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : try(e.value_from[0].secret_key_ref[0].name, "") if try(e.value_from[0].secret_key_ref[0].name, "") != ""]) == toset(["takt-owner-b-secret"])
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

run "runtime_mounts_env_and_persistent_runtime_volume" {
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

  assert {
    condition     = length(kubernetes_pod_v1.workspace[0].spec[0].volume) == 2 && length([for v in kubernetes_pod_v1.workspace[0].spec[0].volume : v if length(v.persistent_volume_claim) > 0]) == 1 && length([for v in kubernetes_pod_v1.workspace[0].spec[0].volume : v if length(v.host_path) > 0]) == 0
    error_message = "The Pod must use exactly one PVC-backed runtime volume plus one /tmp volume, and no host path."
  }
  assert {
    condition     = [for v in kubernetes_pod_v1.workspace[0].spec[0].volume : v.persistent_volume_claim[0].claim_name if length(v.persistent_volume_claim) > 0][0] == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
    error_message = "The runtime volume must claim the workspace PVC takt-<workspace UUID>."
  }
  assert {
    condition     = toset([for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : m.mount_path]) == toset(["/workspace", "/home/takt/.local/share", "/home/takt/.local/state", "/home/takt/.cache", "/tmp"]) && alltrue([for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : !contains(["/home/takt", "/home/takt/.config", "/home/takt/.opencode", "/home/takt/.local/bin"], m.mount_path)])
    error_message = "The main container must mount exactly /workspace, the XDG data/state/cache homes and /tmp, and never mount over baked image state."
  }
  assert {
    condition     = [for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : try(m.sub_path, "") if m.mount_path == "/workspace"][0] == "workspace" && [for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : try(m.sub_path, "") if m.mount_path == "/home/takt/.local/share"][0] == "data" && [for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : try(m.sub_path, "") if m.mount_path == "/home/takt/.local/state"][0] == "state" && [for m in kubernetes_pod_v1.workspace[0].spec[0].container[0].volume_mount : try(m.sub_path, "") if m.mount_path == "/home/takt/.cache"][0] == "cache"
    error_message = "The runtime volume must be mounted through the sibling subPaths workspace, data, state and cache."
  }
  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].container[0].image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90" && kubernetes_pod_v1.workspace[0].spec[0].container[0].working_dir == "/workspace"
    error_message = "The main container must run the operator-supplied image with working directory /workspace."
  }
  assert {
    condition     = tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["cpu"]) == "500m" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["cpu"]) == "2000m" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["memory"]) == "1024Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["memory"]) == "4096Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["ephemeral-storage"]) == "512Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["ephemeral-storage"]) == "2048Mi"
    error_message = "The default resource envelope must render as 500m/2000m CPU, 1024Mi/4096Mi memory and 512Mi/2048Mi ephemeral storage."
  }
  assert {
    condition     = length(kubernetes_pod_v1.workspace[0].spec[0].init_container) == 1 && kubernetes_pod_v1.workspace[0].spec[0].init_container[0].name == "prepare-runtime" && kubernetes_pod_v1.workspace[0].spec[0].init_container[0].image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90" && toset([for m in kubernetes_pod_v1.workspace[0].spec[0].init_container[0].volume_mount : m.mount_path]) == toset(["/mnt/runtime"])
    error_message = "Init container prepare-runtime must use the same workspace image and mount the runtime volume at /mnt/runtime."
  }
  assert {
    condition     = tolist(slice(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args), 0, 2)) == tolist(["/bin/bash", "-euc"]) && length(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args)) == 3
    error_message = "The init container must invoke exactly /bin/bash -euc with one constant source."
  }
  assert {
    condition     = alltrue([for d in ["workspace", "data", "state", "cache"] : strcontains(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args)[2], d)]) && !strcontains(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args)[2], "git clone") && !strcontains(concat(kubernetes_pod_v1.workspace[0].spec[0].init_container[0].command, kubernetes_pod_v1.workspace[0].spec[0].init_container[0].args)[2], "curl")
    error_message = "The init source must create the workspace/data/state/cache siblings and must not clone a repository or use the network."
  }
  assert {
    condition     = try([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.value if e.name == "HOME"][0], null) == "/home/takt" && try([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.value if e.name == "XDG_DATA_HOME"][0], null) == "/home/takt/.local/share" && try([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.value if e.name == "XDG_STATE_HOME"][0], null) == "/home/takt/.local/state" && try([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.value if e.name == "XDG_CACHE_HOME"][0], null) == "/home/takt/.cache" && try([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.value if e.name == "ENGRAM_DATA_DIR"][0], null) == "/home/takt/.local/share/engram"
    error_message = "The Pod must set HOME=/home/takt and the XDG and ENGRAM_DATA_DIR paths as plain values, so Engram data lands on a persisted mount."
  }
  assert {
    condition     = try(coder_agent.workspace.env["TAKT_WORKSPACE_DIR"], "missing") == "/workspace" && try(coder_agent.workspace.env["TAKT_REPOSITORY_URL"], "missing") == ""
    error_message = "The agent environment must set the literal TAKT_WORKSPACE_DIR=/workspace and default the repository hint to an empty TAKT_REPOSITORY_URL."
  }
  assert {
    condition     = length([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e if length(e.value_from) > 0]) == 0 && length(kubernetes_pod_v1.workspace[0].spec[0].container[0].env_from) == 0
    error_message = "With no owner mapping no environment value may come from a Secret reference, and no envFrom block may exist."
  }
}

run "backstage_reference_uses_actual_owner" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    backstage_url = "https://backstage.example.invalid"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        BACKSTAGE_TOKEN = { name = "takt-owner-a-secret", key = "backstage-token" }
      }
      "d4a7e0f1-2c69-4b83-a5de-91f0c7b2e845" = {
        BACKSTAGE_TOKEN = { name = "takt-owner-b-secret", key = "backstage-token" }
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

  assert {
    condition     = try([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e.value if e.name == "BACKSTAGE_URL"][0], "missing") == "https://backstage.example.invalid"
    error_message = "BACKSTAGE_URL must carry the operator's backstage_url as a plain value."
  }
  assert {
    condition     = toset([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : try(e.value_from[0].secret_key_ref[0].name, "") if e.name == "BACKSTAGE_TOKEN"]) == toset(["takt-owner-b-secret"]) && toset([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : try(e.value_from[0].secret_key_ref[0].key, "") if e.name == "BACKSTAGE_TOKEN"]) == toset(["backstage-token"])
    error_message = "BACKSTAGE_TOKEN must reference only owner B's own secret, never owner A's entry."
  }
}

run "backstage_reject_owner_without_own_token_reference" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    backstage_url = "https://backstage.example.invalid"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        BACKSTAGE_TOKEN = { name = "takt-owner-a-secret", key = "backstage-token" }
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

  expect_failures = [kubernetes_pod_v1.workspace]
}

run "backstage_absent_when_url_unset" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        BACKSTAGE_TOKEN = { name = "takt-owner-a-secret", key = "backstage-token" }
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
    values = { value = "" }
  }

  assert {
    condition     = length([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e if e.name == "BACKSTAGE_URL"]) == 0
    error_message = "With backstage_url empty BACKSTAGE_URL must not reach the Pod."
  }

  assert {
    condition     = length([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e if e.name == "BACKSTAGE_TOKEN"]) == 1 && toset([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : try(e.value_from[0].secret_key_ref[0].name, "") if e.name == "BACKSTAGE_TOKEN"]) == toset(["takt-owner-a-secret"]) && toset([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : try(e.value_from[0].secret_key_ref[0].key, "") if e.name == "BACKSTAGE_TOKEN"]) == toset(["backstage-token"])
    error_message = "With backstage_url empty the actual owner's explicitly mapped BACKSTAGE_TOKEN reference must still be injected exactly once, from that owner's own Secret."
  }
}

# With backstage_url empty and no BACKSTAGE_TOKEN mapping for the owner, neither
# the URL nor a token reference may reach the Pod.
run "backstage_absent_without_url_or_mapping" {
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
    condition     = length([for e in kubernetes_pod_v1.workspace[0].spec[0].container[0].env : e if e.name == "BACKSTAGE_URL" || e.name == "BACKSTAGE_TOKEN"]) == 0
    error_message = "With backstage_url empty and no BACKSTAGE_TOKEN mapping, neither BACKSTAGE_URL nor BACKSTAGE_TOKEN may reach the Pod."
  }
}

run "pull_secrets_and_arm64_scheduling" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    image_pull_secrets = ["takt-registry-pull"]
    architecture       = "arm64"
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

  assert {
    condition     = length(kubernetes_pod_v1.workspace[0].spec[0].image_pull_secrets) == 1 && kubernetes_pod_v1.workspace[0].spec[0].image_pull_secrets[0].name == "takt-registry-pull" && kubernetes_pod_v1.workspace[0].spec[0].container[0].image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    error_message = "The operator-supplied image and image pull secret must be applied to the workspace Pod."
  }
  assert {
    condition     = try(coalesce(kubernetes_pod_v1.workspace[0].spec[0].host_network, false), false) == false && coder_agent.workspace.arch == "arm64" && kubernetes_pod_v1.workspace[0].spec[0].node_selector["kubernetes.io/arch"] == "arm64"
    error_message = "The Pod must not use host networking, and architecture must drive both the agent and the node selector."
  }
}

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

  assert {
    condition     = tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["cpu"]) == "750m" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["cpu"]) == "1500m" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["memory"]) == "2048Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["memory"]) == "3072Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].requests["ephemeral-storage"]) == "1024Mi" && tostring(kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0].limits["ephemeral-storage"]) == "4096Mi"
    error_message = "Milli and mebibyte integers must render as Kubernetes quantities: CPU as Nm, memory and ephemeral storage as NMi."
  }
  assert {
    condition     = kubernetes_pod_v1.workspace[0].spec[0].init_container[0].resources[0] == kubernetes_pod_v1.workspace[0].spec[0].container[0].resources[0] && kubernetes_pod_v1.workspace[0].spec[0].init_container[0].image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    error_message = "The init container and the main container must share the resource envelope and the workspace image."
  }
}

run "architecture_arm64_sets_agent_and_output" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    architecture = "arm64"
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

  assert {
    condition     = coder_agent.workspace.arch == "arm64" && coder_agent.workspace.os == "linux" && kubernetes_pod_v1.workspace[0].spec[0].node_selector["kubernetes.io/arch"] == "arm64"
    error_message = "The architecture variable must set the agent arch and the node selector, while the OS stays linux."
  }
  assert {
    condition     = output.architecture == "arm64"
    error_message = "The architecture output must report the selected value."
  }
}

run "workspace_pod_mounts_no_host_path_or_token" {
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

  assert {
    condition     = length([for v in kubernetes_pod_v1.workspace[0].spec[0].volume : v if length(v.host_path) > 0]) == 0 && kubernetes_pod_v1.workspace[0].spec[0].automount_service_account_token == false
    error_message = "Workspace Pods must not mount host paths or any service account token."
  }
}
