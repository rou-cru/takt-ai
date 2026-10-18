# Native Terraform mock-provider tests: lifecycle of the Coder workspace
# template (deploy/coder): compute follows start_count, storage does not.
#
# Blind authorship from the frozen contracts: #15112 (CW-T1-07 lifecycle and
# output states, CW-T1-08 stop exception), #15116 (LC check location), #15110 B4
# (always-retained PVC at kubernetes_persistent_volume_claim_v1.workspace[0],
# takt-<workspace UUID> identity, wait_until_bound=false, only the Pod gated by
# start_count), B6 (stop keeps the PVC; start reuses it), B7 (storage inputs).
# The implementation was not read.
#
# Scope: stopped and running Pod and PVC counts; PVC identity, labels, access
# mode, storage class and size; the agent, app and NetworkPolicy remaining
# declared while stopped; output values in each state; and the actual-owner
# Backstage reference rule (required only while the Pod exists).
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

run "stopped_removes_pod_keeps_pvc" {
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

  assert {
    condition     = length(kubernetes_pod_v1.workspace) == 0
    error_message = "Pod count must equal data.coder_workspace.me.start_count: a stopped workspace has no Pod."
  }
  assert {
    condition     = length(kubernetes_persistent_volume_claim_v1.workspace) == 1
    error_message = "The PVC must be retained while the workspace is stopped: exactly one claim at workspace[0] regardless of start_count."
  }
  assert {
    condition     = kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].namespace == "takt-phase-one-test" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].labels["app.kubernetes.io/name"] == "takt-coder" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].labels["takt.ai/workspace-id"] == "1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].labels["takt.ai/owner-id"] == "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
    error_message = "The retained PVC must be named takt-<workspace UUID> with the three frozen labels."
  }
  assert {
    condition     = kubernetes_persistent_volume_claim_v1.workspace[0].wait_until_bound == false
    error_message = "The PVC must set wait_until_bound=false so WaitForFirstConsumer storage does not block the consuming Pod."
  }
  assert {
    condition     = toset(kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].access_modes) == toset(["ReadWriteOnce"]) && var.storage_class_name == null && tostring(kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].resources[0].requests["storage"]) == "10Gi"
    error_message = "With defaults the PVC must be ReadWriteOnce, omit storageClassName for the cluster default, and request 10Gi."
  }
  assert {
    condition     = output.pod_name == null && output.persistence_mode == "workspace-pvc" && output.pvc_name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
    error_message = "While stopped, pod_name must be null and persistence_mode and pvc_name must still report the retained claim."
  }
  assert {
    condition     = output.namespace == "takt-phase-one-test" && output.image == "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90" && output.architecture == "amd64" && output.workspace_name == "owner-a-workspace-one" && output.owner_id == "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48"
    error_message = "Configuration outputs must stay truthful while compute is stopped."
  }
}

run "stopped_keeps_agent_app_and_policy_declared" {
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

  assert {
    condition     = length(trimspace(coder_agent.workspace.startup_script)) > 0 && coder_agent.workspace.dir == "/workspace" && coder_agent.workspace.startup_script_behavior == "blocking" && coder_agent.workspace.arch == "amd64" && coder_agent.workspace.os == "linux"
    error_message = "The agent must remain declared while the workspace is stopped, with the linux/amd64 platform and a blocking startup script."
  }
  assert {
    condition     = coder_app.opencode.slug == "opencode" && coder_app.opencode.command == "cd /workspace && exec opencode"
    error_message = "coder_app.opencode must not be start_count gated."
  }
  assert {
    condition     = kubernetes_network_policy_v1.workspace.metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && tolist(kubernetes_network_policy_v1.workspace.spec[0].policy_types) == tolist(["Ingress"]) && length(kubernetes_network_policy_v1.workspace.spec[0].ingress) == 0
    error_message = "The workspace-scoped NetworkPolicy must not be start_count gated."
  }
}

run "stopped_allows_missing_backstage_reference" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    backstage_url   = "https://backstage.example.invalid"
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

  assert {
    condition     = length(kubernetes_pod_v1.workspace) == 0
    error_message = "A stopped workspace must plan without its Pod, so no actual-owner Backstage reference is required to stop."
  }
}

run "running_requires_actual_owner_backstage_reference" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    backstage_url   = "https://backstage.example.invalid"
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

run "running_pod_and_pvc_both_present" {
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
    condition     = length(kubernetes_pod_v1.workspace) == 1 && length(kubernetes_persistent_volume_claim_v1.workspace) == 1
    error_message = "A running workspace must have exactly one Pod and one retained PVC."
  }
  assert {
    condition     = kubernetes_pod_v1.workspace[0].metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
    error_message = "The Pod and the PVC must share the takt-<workspace UUID> identity."
  }
  assert {
    condition     = output.pod_name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && output.pvc_name == "takt-1f2c3d4e-5a6b-4c7d-8e9f-0a1b2c3d4e5f" && output.persistence_mode == "workspace-pvc"
    error_message = "Outputs must report the Pod name, the PVC name and workspace-pvc persistence while running."
  }
}

run "stopped_second_owner_pvc_identity" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }

  override_data {
    target = data.coder_workspace.me
    values = {
      id          = "3b4c5d6e-7f8a-4b9c-a1d2-e3f4a5b6c7d8"
      name        = "owner-b-workspace-one"
      start_count = 0
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
    condition     = kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].name == "takt-3b4c5d6e-7f8a-4b9c-a1d2-e3f4a5b6c7d8" && kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].labels["takt.ai/owner-id"] == "d4a7e0f1-2c69-4b83-a5de-91f0c7b2e845" && length(kubernetes_pod_v1.workspace) == 0
    error_message = "Owner B's stopped workspace must keep its own PVC with owner B's identity and no Pod."
  }
}

run "default_storage_omits_storage_class" {
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
    condition     = var.storage_class_name == null && tostring(kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].resources[0].requests["storage"]) == "10Gi" && toset(kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].access_modes) == toset(["ReadWriteOnce"])
    error_message = "A null storage class must omit storageClassName for the cluster default, with the default 10Gi request and ReadWriteOnce access."
  }
}

run "explicit_storage_class_and_size" {
  command = plan

  variables {
    namespace          = "takt-phase-one-test"
    workspace_image    = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    storage_class_name = "standard"
    storage_size_gib   = 20
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
    condition     = kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].storage_class_name == "standard" && tostring(kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].resources[0].requests["storage"]) == "20Gi" && toset(kubernetes_persistent_volume_claim_v1.workspace[0].spec[0].access_modes) == toset(["ReadWriteOnce"])
    error_message = "The PVC must use the operator-supplied storage class and size in Gi with ReadWriteOnce access."
  }
  assert {
    condition     = kubernetes_persistent_volume_claim_v1.workspace[0].wait_until_bound == false
    error_message = "Explicit storage must keep wait_until_bound=false."
  }
}
