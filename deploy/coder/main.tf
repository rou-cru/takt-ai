# Single provisioning graph for one human's Coder workspace on Kubernetes.
#
# Every resource derives from the Coder workspace and owner identity, never from
# free-text input. One workspace maps to one Pod, one persistent per-workspace
# PersistentVolumeClaim, one workspace-scoped NetworkPolicy and one Coder agent.
# Only compute is gated on start_count; the claim survives stop and resume. The
# workspace Pod receives no cluster identity and mounts no host path.

data "coder_workspace" "me" {}

data "coder_workspace_owner" "me" {}

data "coder_parameter" "repository_url" {
  name         = "repository_url"
  display_name = "Repository URL"
  description  = "Optional credential-free HTTPS repository exposed to the agent as TAKT_REPOSITORY_URL. Empty means no repository is configured; provisioning never clones or mutates Git."
  type         = "string"
  mutable      = false
  default      = var.repository_url_default

  validation {
    regex = "^$|^https://[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?/[^[:space:][:cntrl:]#?@]+$"
    error = "Enter an empty value or a credential-free https://host/path URL with no userinfo, query, fragment or whitespace."
  }
}

locals {
  # Full workspace and owner UUIDs keep names collision-free without exposing an
  # email or a mutable username. Names, labels and identity live here only.
  resource_name = "takt-${data.coder_workspace.me.id}"

  labels = {
    "app.kubernetes.io/name" = "takt-coder"
    "takt.ai/workspace-id"   = data.coder_workspace.me.id
    "takt.ai/owner-id"       = data.coder_workspace_owner.me.id
  }

  # Constant init-container source: no setup, network access or interpolation.
  prepare_runtime_script = "mkdir -p /mnt/runtime/workspace /mnt/runtime/data /mnt/runtime/state /mnt/runtime/cache"

  # The same compute envelope serves the prepare-runtime init container and the
  # workspace container.
  container_resources = {
    limits = {
      cpu                 = "${var.resources.cpu_limit_milli}m"
      memory              = "${var.resources.memory_limit_mib}Mi"
      "ephemeral-storage" = "${var.resources.ephemeral_limit_mib}Mi"
    }
    requests = {
      cpu                 = "${var.resources.cpu_request_milli}m"
      memory              = "${var.resources.memory_request_mib}Mi"
      "ephemeral-storage" = "${var.resources.ephemeral_request_mib}Mi"
    }
  }

  # Only the matching owner's existing Secret references reach this workspace.
  owner_secret_env = lookup(var.owner_secret_env, data.coder_workspace_owner.me.id, {})

  # The owner's BACKSTAGE_TOKEN Secret reference, present only when the operator
  # mapped it to this exact owner. It is the sole BACKSTAGE_TOKEN source and the
  # value checked by the workspace Pod precondition.
  backstage_token_ref = lookup(local.owner_secret_env, "BACKSTAGE_TOKEN", null)
}

# The agent, its terminal app and the ingress policy exist for the whole
# workspace lifetime; compute alone is gated on start_count.
resource "coder_agent" "workspace" {
  os   = "linux"
  arch = var.architecture
  dir  = "/workspace"

  # TAKT_WORKSPACE_DIR is the existing image-owned workspace variable. The Coder
  # command bypasses workspace-entrypoint.sh, so production supplies it here.
  env = {
    TAKT_REPOSITORY_URL = data.coder_parameter.repository_url.value
    TAKT_WORKSPACE_DIR  = "/workspace"
  }

  display_apps {
    vscode                 = false
    vscode_insiders        = false
    web_terminal           = true
    ssh_helper             = true
    port_forwarding_helper = false
  }

  startup_script          = file("${path.module}/scripts/check-workspace.sh")
  startup_script_behavior = "blocking"
}

resource "coder_app" "opencode" {
  agent_id     = coder_agent.workspace.id
  slug         = "opencode"
  display_name = "OpenCode"
  command      = "cd /workspace && exec opencode"
  share        = "owner"
}

# Ingress-only policy for this one workspace Pod; egress stays available for
# Coder, DNS, Git and model providers. No cluster-wide policy is managed here.
resource "kubernetes_network_policy_v1" "workspace" {
  metadata {
    name      = local.resource_name
    namespace = var.namespace
    labels    = local.labels
  }

  spec {
    policy_types = ["Ingress"]

    pod_selector {
      match_labels = {
        "takt.ai/workspace-id" = data.coder_workspace.me.id
      }
    }
  }
}

# One persistent claim per workspace, kept for the workspace's whole lifetime:
# stop removes compute only and start reuses the same claim. The explicit count
# keeps the historical workspace[0] resource address stable across template
# versions. wait_until_bound follows the upstream Kubernetes example so
# WaitForFirstConsumer storage does not block creation of the consuming Pod.
resource "kubernetes_persistent_volume_claim_v1" "workspace" {
  count = 1

  metadata {
    name      = local.resource_name
    namespace = var.namespace
    labels    = local.labels
  }

  wait_until_bound = false

  spec {
    access_modes       = ["ReadWriteOnce"]
    storage_class_name = var.storage_class_name

    resources {
      requests = {
        storage = "${var.storage_size_gib}Gi"
      }
    }
  }
}

resource "kubernetes_pod_v1" "workspace" {
  count = data.coder_workspace.me.start_count

  metadata {
    name      = local.resource_name
    namespace = var.namespace
    labels    = local.labels
  }

  spec {
    restart_policy                  = "Never"
    automount_service_account_token = false

    node_selector = {
      "kubernetes.io/arch" = var.architecture
    }

    dynamic "image_pull_secrets" {
      for_each = var.image_pull_secrets

      content {
        name = image_pull_secrets.value
      }
    }

    security_context {
      run_as_non_root        = true
      run_as_user            = "10001"
      run_as_group           = "10001"
      fs_group               = "10001"
      fs_group_change_policy = "OnRootMismatch"

      seccomp_profile {
        type = "RuntimeDefault"
      }
    }

    init_container {
      name    = "prepare-runtime"
      image   = var.workspace_image
      command = ["/bin/bash", "-euc"]
      args    = [local.prepare_runtime_script]

      security_context {
        allow_privilege_escalation = false

        capabilities {
          drop = ["ALL"]
        }
      }

      resources {
        limits   = local.container_resources.limits
        requests = local.container_resources.requests
      }

      volume_mount {
        name       = "runtime"
        mount_path = "/mnt/runtime"
      }
    }

    container {
      name        = "workspace"
      image       = var.workspace_image
      command     = ["/bin/sh", "-c", coder_agent.workspace.init_script]
      working_dir = "/workspace"

      env {
        name  = "CODER_AGENT_TOKEN"
        value = coder_agent.workspace.token
      }

      env {
        name  = "HOME"
        value = "/home/takt"
      }

      # Explicit runtime environment selects the persistent subpaths of the
      # workspace claim; agent sessions and tools inherit the Pod environment.
      # ENGRAM_DATA_DIR keeps Engram data on the persisted share subpath.
      env {
        name  = "XDG_DATA_HOME"
        value = "/home/takt/.local/share"
      }

      env {
        name  = "XDG_STATE_HOME"
        value = "/home/takt/.local/state"
      }

      env {
        name  = "XDG_CACHE_HOME"
        value = "/home/takt/.cache"
      }

      env {
        name  = "ENGRAM_DATA_DIR"
        value = "/home/takt/.local/share/engram"
      }

      # The Backstage endpoint comes solely from the operator variable. The
      # owner's BACKSTAGE_TOKEN Secret reference is injected from
      # owner_secret_env by the dynamic block below and required by the
      # precondition on this resource.
      dynamic "env" {
        for_each = var.backstage_url != "" ? [1] : []

        content {
          name  = "BACKSTAGE_URL"
          value = var.backstage_url
        }
      }

      dynamic "env" {
        for_each = local.owner_secret_env

        content {
          name = env.key

          value_from {
            secret_key_ref {
              name = env.value.name
              key  = env.value.key
            }
          }
        }
      }

      security_context {
        allow_privilege_escalation = false

        capabilities {
          drop = ["ALL"]
        }
      }

      resources {
        limits   = local.container_resources.limits
        requests = local.container_resources.requests
      }

      volume_mount {
        name       = "runtime"
        mount_path = "/workspace"
        sub_path   = "workspace"
      }

      volume_mount {
        name       = "runtime"
        mount_path = "/home/takt/.local/share"
        sub_path   = "data"
      }

      volume_mount {
        name       = "runtime"
        mount_path = "/home/takt/.local/state"
        sub_path   = "state"
      }

      volume_mount {
        name       = "runtime"
        mount_path = "/home/takt/.cache"
        sub_path   = "cache"
      }

      volume_mount {
        name       = "tmp"
        mount_path = "/tmp"
      }
    }

    # One runtime volume: the per-workspace claim, mounted at every persisted
    # path. /tmp stays an emptyDir for the life of this Pod.
    volume {
      name = "runtime"

      persistent_volume_claim {
        claim_name = kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].name
      }
    }

    volume {
      name = "tmp"

      empty_dir {}
    }
  }

  # Backstage credential seam: a starting workspace whose operator configured a
  # Backstage endpoint must carry that exact owner's BACKSTAGE_TOKEN Secret
  # reference. Stopping is never blocked by the credential: with start_count 0
  # no Pod instance is planned and the precondition has nothing to evaluate.
  lifecycle {
    precondition {
      condition     = var.backstage_url == "" || local.backstage_token_ref != null
      error_message = "backstage_url is set but owner_secret_env has no BACKSTAGE_TOKEN Secret reference for this workspace owner; add owner_secret_env[<owner UUID>].BACKSTAGE_TOKEN or leave backstage_url empty."
    }
  }
}
