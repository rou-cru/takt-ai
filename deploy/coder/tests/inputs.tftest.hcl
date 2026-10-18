# Native Terraform mock-provider tests: operator input acquisition for the
# Coder workspace template (deploy/coder).
#
# Blind authorship from the frozen contracts: #15112 (CW-T1-06, and the input
# portion of CW-T1-08), #15116 (IN check location), #15110 B7 and C7 (variables.tf
# is the single executable input schema) and #15110 C2 (repository_url is the
# only human rich parameter). The implementation was not read.
#
# Scope: valid boundary inputs are accepted; each invalid operator input fails
# acquisition with the offending variable named in expect_failures. Missing
# required values are Terraform core diagnostics, not custom conditions, so they
# are not expressible here. The retired persistence inputs and the persist_workspace
# rich parameter are absent: the template always retains one PVC (#15110 B4).
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

run "accepts_minimal_valid_inputs" {
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


  assert {
    condition     = var.architecture == "amd64" && var.storage_size_gib == 10 && var.storage_class_name == null && length(var.image_pull_secrets) == 0 && var.repository_url_default == "" && var.backstage_url == "" && length(var.owner_secret_env) == 0
    error_message = "Defaults must be architecture amd64, storage_size_gib 10, storage_class_name null, image_pull_secrets [], repository_url_default empty, backstage_url empty and owner_secret_env {}."
  }
}

run "accepts_arm64_and_storage_upper_bound" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    architecture     = "arm64"
    storage_size_gib = 1024
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


  assert {
    condition     = var.architecture == "arm64" && var.storage_size_gib == 1024
    error_message = "arm64 and the upper storage bound 1024 GiB must be accepted."
  }
}

run "accepts_storage_lower_bound" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    storage_size_gib = 1
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


  assert {
    condition     = var.storage_size_gib == 1
    error_message = "The lower storage bound 1 GiB must be accepted."
  }
}

run "accepts_backstage_url_and_owner_token_reference" {
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


  assert {
    condition     = var.backstage_url == "https://backstage.example.invalid" && length(var.owner_secret_env) == 1
    error_message = "An absolute credential-free HTTPS backstage_url and BACKSTAGE_TOKEN as an owner-scoped reference must be accepted."
  }
}

run "accepts_repository_url_hint" {
  command = plan

  variables {
    namespace       = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "https://git.example.invalid/team/takt-ai.git"
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


  assert {
    condition     = var.repository_url_default == "https://git.example.invalid/team/takt-ai.git"
    error_message = "A credential-free HTTPS repository hint must be accepted as the default."
  }
}

run "reject_namespace_uppercase" {
  command = plan
  variables {
    namespace = "Takt-Prod"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.namespace]
}

run "reject_namespace_underscore" {
  command = plan
  variables {
    namespace = "takt_prod"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.namespace]
}

run "reject_namespace_leading_whitespace" {
  command = plan
  variables {
    namespace = " takt-prod"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.namespace]
}

run "reject_namespace_over_63_characters" {
  command = plan
  variables {
    namespace = "takt-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.namespace]
}

run "reject_image_tagged_not_digest_pinned" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai:2.0.28"
  }
  expect_failures = [var.workspace_image]
}

run "reject_image_with_url_scheme" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "https://registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.workspace_image]
}

run "reject_image_with_surrounding_whitespace" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = " registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.workspace_image]
}

run "reject_image_with_short_digest" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9"
  }
  expect_failures = [var.workspace_image]
}

run "reject_image_without_repository_path" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
  }
  expect_failures = [var.workspace_image]
}

run "reject_architecture_not_in_enum" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    architecture = "x86_64"
  }
  expect_failures = [var.architecture]
}

run "reject_architecture_with_whitespace" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    architecture = "amd64 "
  }
  expect_failures = [var.architecture]
}

run "reject_resources_cpu_request_above_limit" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    resources = { cpu_request_milli = 4000, cpu_limit_milli = 2000, memory_request_mib = 1024, memory_limit_mib = 4096, ephemeral_request_mib = 512, ephemeral_limit_mib = 2048 }
  }
  expect_failures = [var.resources]
}

run "reject_resources_non_positive_value" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    resources = { cpu_request_milli = 500, cpu_limit_milli = 2000, memory_request_mib = 1024, memory_limit_mib = 0, ephemeral_request_mib = 512, ephemeral_limit_mib = 2048 }
  }
  expect_failures = [var.resources]
}

run "reject_resources_ephemeral_request_above_limit" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    resources = { cpu_request_milli = 500, cpu_limit_milli = 2000, memory_request_mib = 1024, memory_limit_mib = 4096, ephemeral_request_mib = 4096, ephemeral_limit_mib = 2048 }
  }
  expect_failures = [var.resources]
}

run "reject_storage_class_empty_string" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    storage_class_name = ""
  }
  expect_failures = [var.storage_class_name]
}

run "reject_storage_size_below_range" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    storage_size_gib = 0
  }
  expect_failures = [var.storage_size_gib]
}

run "reject_storage_size_above_range" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    storage_size_gib = 1025
  }
  expect_failures = [var.storage_size_gib]
}

run "reject_image_pull_secret_invalid_dns_subdomain" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    image_pull_secrets = ["Takt_Registry_Pull"]
  }
  expect_failures = [var.image_pull_secrets]
}

run "reject_repository_ssh_url" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "git@git.example.invalid:team/takt-ai.git"
  }
  expect_failures = [var.repository_url_default]
}

run "reject_repository_plain_http_url" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "http://git.example.invalid/team/takt-ai.git"
  }
  expect_failures = [var.repository_url_default]
}

run "reject_repository_embedded_userinfo" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "https://user:password@git.example.invalid/team/takt-ai.git"
  }
  expect_failures = [var.repository_url_default]
}

run "reject_repository_query_string" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "https://git.example.invalid/team/takt-ai.git?ref=main"
  }
  expect_failures = [var.repository_url_default]
}

run "reject_repository_trailing_whitespace" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    repository_url_default = "https://git.example.invalid/team/takt-ai.git "
  }
  expect_failures = [var.repository_url_default]
}

run "reject_backstage_url_plain_http" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    backstage_url = "http://backstage.example.invalid"
  }
  expect_failures = [var.backstage_url]
}

run "reject_backstage_url_embedded_userinfo" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    backstage_url = "https://user:password@backstage.example.invalid"
  }
  expect_failures = [var.backstage_url]
}

run "reject_backstage_url_query_string" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    backstage_url = "https://backstage.example.invalid/?tenant=team"
  }
  expect_failures = [var.backstage_url]
}

run "reject_backstage_url_fragment" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    backstage_url = "https://backstage.example.invalid/#catalog"
  }
  expect_failures = [var.backstage_url]
}

run "reject_owner_env_reserved_home" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
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
  command = plan
  variables {
    namespace = "takt-phase-one-test"
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
  command = plan
  variables {
    namespace = "takt-phase-one-test"
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
  command = plan
  variables {
    namespace = "takt-phase-one-test"
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
  command = plan
  variables {
    namespace = "takt-phase-one-test"
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
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        XDG_DATA_HOME = { name = "takt-owner-a-secret", key = "data-home" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_backstage_url_name" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        BACKSTAGE_URL = { name = "takt-owner-a-secret", key = "backstage-url" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}

run "reject_owner_env_lowercase_variable_name" {
  command = plan
  variables {
    namespace = "takt-phase-one-test"
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
  command = plan
  variables {
    namespace = "takt-phase-one-test"
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
  command = plan
  variables {
    namespace = "takt-phase-one-test"
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
  command = plan
  variables {
    namespace = "takt-phase-one-test"
    workspace_image = "registry.example.invalid/team/takt-ai@sha256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
    owner_secret_env = {
      "6b1f9c2e-8a34-4d7b-9f21-0c5e7a1d3b48" = {
        MODEL_API_KEY = { name = "takt-owner-a-secret", key = "model/api/key" }
      }
    }
  }
  expect_failures = [var.owner_secret_env]
}
