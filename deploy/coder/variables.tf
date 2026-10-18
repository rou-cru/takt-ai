# Sole operator-input schema for the phase-one Coder Kubernetes template.
#
# Every rule below is evaluated at the Terraform acquisition/plan boundary,
# before any resource is written. Invalid values are rejected, never repaired:
# leading or trailing whitespace, empty strings and forbidden combinations fail
# with the variable name in the diagnostic. Required values carry no default.

variable "namespace" {
  description = "Existing Kubernetes namespace that receives every workspace resource for every human."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$", var.namespace)) && length(var.namespace) <= 63
    error_message = "namespace must be an existing-namespace DNS label of 1 to 63 characters matching [a-z0-9]([-a-z0-9]*[a-z0-9])?."
  }
}

variable "workspace_image" {
  description = "Digest-pinned workspace image reference, for example registry.example.com/team/takt-ai@sha256:<64 lowercase hex>. The operator attests that this is the repository workspace target with OpenCode pinned and Takt installed."
  type        = string

  validation {
    condition     = length(var.workspace_image) <= 512 && can(regex("^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?(?:\\.[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?)*(?::[0-9]+)?(?:/[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?)+@sha256:[0-9a-f]{64}$", var.workspace_image))
    error_message = "workspace_image must be a digest-pinned reference of the form registry/path@sha256:<64 lowercase hex>, at most 512 characters, with no scheme, userinfo, tag, query or whitespace."
  }
}

variable "architecture" {
  description = "Target CPU architecture for the workspace agent, node selector and image manifest. Operating system is fixed to linux."
  type        = string
  default     = "amd64"

  validation {
    condition     = contains(["amd64", "arm64"], var.architecture)
    error_message = "architecture must be amd64 or arm64."
  }
}

variable "image_pull_secrets" {
  description = "Names of existing Kubernetes Secrets used to pull the workspace image. Operator-controlled only."
  type        = list(string)
  default     = []

  validation {
    condition     = length(var.image_pull_secrets) == length(distinct(var.image_pull_secrets))
    error_message = "image_pull_secrets entries must be unique."
  }

  validation {
    condition     = alltrue([for secret in var.image_pull_secrets : length(secret) <= 253 && can(regex("^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$", secret))])
    error_message = "each image_pull_secrets entry must be an existing Secret DNS-subdomain name of 1 to 253 characters."
  }
}

variable "repository_url_default" {
  description = "Optional credential-free HTTPS repository URL pre-filled for each human. Empty means no repository is configured; provisioning never clones or mutates Git."
  type        = string
  default     = ""

  validation {
    condition     = var.repository_url_default == "" || (length(var.repository_url_default) <= 2048 && can(regex("^https://[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?/[^[:space:][:cntrl:]#?@]+$", var.repository_url_default)))
    error_message = "repository_url_default must be empty or a credential-free https://host/path URL of at most 2048 characters with no userinfo, query, fragment, scheme other than https, or whitespace."
  }
}

variable "resources" {
  description = "Compute envelope applied to both the prepare-runtime init container and the workspace container. CPU is milli-cores; memory and ephemeral storage are MiB."
  type = object({
    cpu_request_milli     = number
    cpu_limit_milli       = number
    memory_request_mib    = number
    memory_limit_mib      = number
    ephemeral_request_mib = number
    ephemeral_limit_mib   = number
  })
  default = {
    cpu_request_milli     = 500
    cpu_limit_milli       = 2000
    memory_request_mib    = 1024
    memory_limit_mib      = 4096
    ephemeral_request_mib = 512
    ephemeral_limit_mib   = 2048
  }

  validation {
    condition     = var.resources.cpu_request_milli == floor(var.resources.cpu_request_milli) && var.resources.cpu_limit_milli == floor(var.resources.cpu_limit_milli) && var.resources.memory_request_mib == floor(var.resources.memory_request_mib) && var.resources.memory_limit_mib == floor(var.resources.memory_limit_mib) && var.resources.ephemeral_request_mib == floor(var.resources.ephemeral_request_mib) && var.resources.ephemeral_limit_mib == floor(var.resources.ephemeral_limit_mib)
    error_message = "every resources field must be a whole number of milli-cores or MiB."
  }

  validation {
    condition     = var.resources.cpu_request_milli > 0 && var.resources.cpu_limit_milli > 0 && var.resources.memory_request_mib > 0 && var.resources.memory_limit_mib > 0 && var.resources.ephemeral_request_mib > 0 && var.resources.ephemeral_limit_mib > 0
    error_message = "every resources field must be greater than zero."
  }

  validation {
    condition     = var.resources.cpu_request_milli <= var.resources.cpu_limit_milli && var.resources.memory_request_mib <= var.resources.memory_limit_mib && var.resources.ephemeral_request_mib <= var.resources.ephemeral_limit_mib
    error_message = "each resources request must be less than or equal to its paired limit: cpu, memory and ephemeral storage."
  }
}

variable "storage_class_name" {
  description = "StorageClass for the per-workspace persistent claim. Null uses the cluster default."
  type        = string
  default     = null

  validation {
    condition     = var.storage_class_name == null || (try(length(var.storage_class_name) <= 253, false) && can(regex("^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$", var.storage_class_name)))
    error_message = "storage_class_name must be null or an existing StorageClass DNS-subdomain name of 1 to 253 characters."
  }
}

variable "storage_size_gib" {
  description = "Requested size of the persistent workspace volume, in GiB. Access mode is fixed to ReadWriteOnce."
  type        = number
  default     = 10

  validation {
    condition     = var.storage_size_gib == floor(var.storage_size_gib) && var.storage_size_gib >= 1 && var.storage_size_gib <= 1024
    error_message = "storage_size_gib must be a whole number between 1 and 1024."
  }
}

variable "owner_secret_env" {
  description = "Optional existing-Secret environment references keyed by owner UUID. Only the matching owner's workspace receives them, and unmapped owners receive none. The BACKSTAGE_TOKEN entry is the per-owner Backstage credential seam: it is the only source of the workspace BACKSTAGE_TOKEN value and is required whenever backstage_url is set."
  type = map(map(object({
    name = string
    key  = string
  })))
  default = {}

  validation {
    condition     = alltrue([for owner_id in keys(var.owner_secret_env) : can(regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", owner_id))])
    error_message = "owner_secret_env outer keys must be lowercase owner UUIDs."
  }

  validation {
    condition     = try(alltrue([for vars in values(var.owner_secret_env) : alltrue([for env_name in keys(vars) : can(regex("^[A-Z_][A-Z0-9_]*$", env_name))])]), false)
    error_message = "owner_secret_env environment variable names must match [A-Z_][A-Z0-9_]*."
  }

  validation {
    condition     = try(alltrue([for vars in values(var.owner_secret_env) : alltrue([for env_name in keys(vars) : !contains(["HOME", "PATH", "USER", "LOGNAME", "SHELL", "ENV", "BASH_ENV", "LD_PRELOAD", "LD_LIBRARY_PATH", "BACKSTAGE_URL"], env_name) && !anytrue([for prefix in ["CODER_", "TAKT_", "OPENCODE_", "XDG_", "ENGRAM_"] : startswith(env_name, prefix)])])]), false)
    error_message = "owner_secret_env must not override reserved names: HOME, PATH, USER, LOGNAME, SHELL, ENV, BASH_ENV, LD_PRELOAD, LD_LIBRARY_PATH, BACKSTAGE_URL, or any CODER_, TAKT_, OPENCODE_, XDG_ or ENGRAM_ name."
  }

  validation {
    condition     = try(alltrue([for vars in values(var.owner_secret_env) : alltrue([for ref in values(vars) : can(regex("^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$", ref.name)) && try(length(ref.name) <= 253, false) && can(regex("^[A-Za-z0-9._-]+$", ref.key)) && try(length(ref.key) <= 253, false)])]), false)
    error_message = "owner_secret_env entries must reference an existing Secret DNS-subdomain name and a key matching [A-Za-z0-9._-]+, each of 1 to 253 characters."
  }
}

variable "backstage_url" {
  description = "Base URL of the operator's Backstage deployment, for example https://backstage.example.com. Empty disables BACKSTAGE_URL injection and the token requirement; it does not remove a BACKSTAGE_TOKEN explicitly mapped in owner_secret_env. A nonempty value reaches the workspace as BACKSTAGE_URL and requires every starting owner to have a BACKSTAGE_TOKEN Secret reference in owner_secret_env. The URL itself never carries credentials."
  type        = string
  default     = ""

  validation {
    condition     = var.backstage_url == "" || (length(var.backstage_url) <= 2048 && can(regex("^https://[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?(?:/[A-Za-z0-9._~-]*)*$", var.backstage_url)))
    error_message = "backstage_url must be empty or an absolute https:// base URL of at most 2048 characters with no userinfo, query, fragment or whitespace."
  }
}
