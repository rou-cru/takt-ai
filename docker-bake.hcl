# Single source of truth for every image takt-ai builds: the two published
# product images (deploy/images/Dockerfile, targets `workspace` and `dev`)
# and the two CI-only test images. CI (release.yml, ci.yml,
# test-containerized.sh) builds through these same targets instead of
# reimplementing the Dockerfile references, so there is exactly one
# definition of each image.
variable "OPENCODE_VERSION" {
  default = "2.0.20"
}
variable "GO_VERSION" {
  default = "1.27.1"
}
variable "VERSION" {
  default = "dev"
}

# Local-testing defaults only; CI overrides these with the real registry
# tags computed by docker/metadata-action before pushing.
variable "WORKSPACE_TAG" {
  default = "roucru/takt-ai:workspace-local"
}
variable "DEV_TAG" {
  default = "roucru/takt-ai:dev"
}
variable "DEV_UID" {
  default = "1000"
}
variable "DEV_GID" {
  default = "1000"
}
variable "TEST_TAG" {
  default = "takt-test:local"
}
variable "E2E_TAG" {
  default = "takt-e2e:local"
}

# Published as docker.io/roucru/takt-ai (+ ghcr.io/rou-cru/takt-ai) by the
# release workflow, which builds each platform on a native runner (the
# builder stage is target-arch) and pushes `workspace` and `dev` together so
# the shared `toolchain` stage above them is only built once per arch.
target "workspace" {
  context    = "."
  dockerfile = "deploy/images/Dockerfile"
  target     = "workspace"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["${WORKSPACE_TAG}"]
  args = {
    OPENCODE_VERSION = "${OPENCODE_VERSION}"
    GO_VERSION       = "${GO_VERSION}"
    VERSION          = "${VERSION}"
  }
}

# Published as docker.io/roucru/takt-ai:dev (+ ghcr.io mirror): same
# toolchain as `workspace`, interactive shell instead of a server.
target "dev" {
  context    = "."
  dockerfile = "deploy/images/Dockerfile"
  target     = "dev"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["${DEV_TAG}"]
  args = {
    OPENCODE_VERSION = "${OPENCODE_VERSION}"
    GO_VERSION       = "${GO_VERSION}"
    VERSION          = "${VERSION}"
    DEV_UID          = "${DEV_UID}"
    DEV_GID          = "${DEV_GID}"
  }
}

# Never published: development/testing/test-containerized.sh's isolated
# Go-test runner image (codegraph + deadcode + Go).
target "test" {
  context    = "."
  dockerfile = "development/testing/Dockerfile"
  tags       = ["${TEST_TAG}"]
}

# Never published: ci.yml's `e2e-container` job, the mandatory setup CLI
# E2E contract.
target "e2e-test" {
  context    = "."
  dockerfile = "development/testing/e2e/Dockerfile"
  tags       = ["${E2E_TAG}"]
  args = {
    OPENCODE_VERSION = "${OPENCODE_VERSION}"
  }
}
