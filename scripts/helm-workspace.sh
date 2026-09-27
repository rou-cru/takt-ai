#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 install|upgrade|uninstall RELEASE [--namespace NAMESPACE] [Helm options...]" >&2
  echo "Default namespace: ${WORKSPACE_DEFAULT_NAMESPACE:-takt-workspaces} (override with WORKSPACE_DEFAULT_NAMESPACE or --namespace)" >&2
  exit 2
}

[[ $# -ge 2 ]] || usage
action=$1
release=$2
shift 2
case "$action" in install|upgrade|uninstall) ;; *) usage ;; esac
namespace=${WORKSPACE_DEFAULT_NAMESPACE:-takt-workspaces}
helm_args=()
while (($#)); do
  case "$1" in
    -n|--namespace)
      (($# >= 2)) || usage
      namespace=$2
      shift 2
      ;;
    --namespace=*) namespace=${1#*=}; shift ;;
    *) helm_args+=("$1"); shift ;;
  esac
done
[[ -n "$namespace" ]] || usage

if [[ "$action" != uninstall ]]; then
  kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f -
fi

case "$action" in
  install) helm install "$release" charts/takt-workspace --namespace "$namespace" "${helm_args[@]}" ;;
  upgrade) helm upgrade --install "$release" charts/takt-workspace --namespace "$namespace" "${helm_args[@]}" ;;
  uninstall) helm uninstall "$release" --namespace "$namespace" "${helm_args[@]}" ;;
esac
