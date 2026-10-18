# Provider and version boundary for the phase-one Coder Kubernetes template.
#
# These pins are frozen by the phase-one architecture contract. Widening,
# substituting or silently upgrading them is an explicit dependency change, not
# a change made to make a plan or a test pass.

terraform {
  required_version = ">= 1.11.0, < 2.0.0"

  required_providers {
    coder = {
      source  = "coder/coder"
      version = "= 2.0.2"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "= 2.16.0"
    }
  }
}

# Both providers read the Coder provisioner's own environment. No kubeconfig
# path, token variable, external credential provider, namespace creation or
# cluster RBAC management ships here, and the workspace Pod receives no cluster
# identity.
provider "coder" {}

provider "kubernetes" {}
