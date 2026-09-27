# One target today (dev); add more as images grow.
variable "OPENCODE_VERSION" {
  default = "2.0.16"
}
variable "GO_VERSION" {
  default = "1.25.0"
}
variable "TAG" {
  default = "takt-ai:dev"
}
variable "WORKSPACE_TAG" {
  default = "takt-ai:workspace"
}

target "dev" {
  context    = "."
  dockerfile = "docker/Dockerfile.dev"
  platforms  = ["linux/arm64", "linux/amd64"]
  tags       = ["${TAG}"]
  args = {
    OPENCODE_VERSION = "${OPENCODE_VERSION}"
    GO_VERSION       = "${GO_VERSION}"
  }
  labels = {
    "org.opencontainers.image.title"       = "takt dev"
    "org.opencontainers.image.description" = "Disposable dev environment: takt-ai CLI + OpenCode, no onboarding (B03)"
  }
}

# The Kubernetes workspace currently targets the cluster's ARM64 nodes only.
target "workspace" {
  context    = "."
  dockerfile = "docker/Dockerfile.workspace"
  platforms  = ["linux/arm64"]
  tags       = ["${WORKSPACE_TAG}"]
  args = {
    OPENCODE_VERSION = "${OPENCODE_VERSION}"
    GO_VERSION       = "${GO_VERSION}"
  }
  labels = {
    "org.opencontainers.image.title"       = "takt ephemeral workspace"
    "org.opencontainers.image.description" = "Prepared Takt AI + OpenCode v2 workspace runtime for Kubernetes"
  }
}
