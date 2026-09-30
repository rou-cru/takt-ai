# Local development and deployable workspace image builds.
variable "OPENCODE_VERSION" {
  default = "2.0.20"
}
variable "GO_VERSION" {
  default = "1.27.1"
}
variable "TAG" {
  default = "takt-ai:dev"
}
variable "WORKSPACE_TAG" {
  default = "roucru/takt-ai:dev"
}
variable "VERSION" {
  default = "dev"
}

target "dev" {
  context    = "."
  dockerfile = "development/environment/Dockerfile"
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

# Published as docker.io/roucru/takt-ai by the release workflow, which builds
# each platform on a native runner (the builder stage is target-arch).
target "workspace" {
  context    = "."
  dockerfile = "deploy/workspace/Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["${WORKSPACE_TAG}"]
  args = {
    OPENCODE_VERSION = "${OPENCODE_VERSION}"
    GO_VERSION       = "${GO_VERSION}"
    VERSION          = "${VERSION}"
  }
}
