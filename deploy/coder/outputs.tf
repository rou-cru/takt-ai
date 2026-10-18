# Nonsecret workspace summary only. No agent token, kubeconfig, bootstrap script
# or secret environment value is exposed.

output "workspace_name" {
  value       = data.coder_workspace.me.name
  description = "Coder workspace name that owns these resources."
}

output "owner_id" {
  value       = data.coder_workspace_owner.me.id
  description = "UUID of the human who owns this workspace."
}

output "pod_name" {
  value       = length(kubernetes_pod_v1.workspace) > 0 ? kubernetes_pod_v1.workspace[0].metadata[0].name : null
  description = "Name of the workspace Pod, or null while the workspace is stopped."
}

output "namespace" {
  value       = var.namespace
  description = "Existing namespace that holds every workspace resource."
}

output "image" {
  value       = var.workspace_image
  description = "Digest-pinned workspace image used by both Pod containers."
}

output "architecture" {
  value       = var.architecture
  description = "Target CPU architecture of the agent, the node selector and the image manifest."
}

output "persistence_mode" {
  value       = "workspace-pvc"
  description = "Workspace storage mode: one persistent claim per workspace, retained across stop and resume."
}

output "pvc_name" {
  value       = kubernetes_persistent_volume_claim_v1.workspace[0].metadata[0].name
  description = "Name of the persistent workspace claim, present for the whole workspace lifetime including while stopped."
}
