#!/usr/bin/env bash
# Shared target selection. Sourcing this file does not contact a cluster.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
target="${K8S_TARGET:-minikube}"
namespace="${K8S_NAMESPACE:-polyglot-ticketing}"
profile="${MINIKUBE_PROFILE:-minikube}"
services=(tickets identity orders payments expiration api-gateway)
secret_services=(identity tickets orders payments expiration)
dependencies=(redis mailpit nats identity-db tickets-db orders-db payments-db)

fail() { printf 'Error: %s\n' "$1" >&2; exit 1; }
require_tool() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required."; }
case "$target" in
  minikube) secrets_default=/tmp/k8s-polyglot-ticketing-secrets ;;
  floci) secrets_default=/tmp/k8s-polyglot-ticketing-floci-secrets ;;
  *) fail "Unknown K8S_TARGET '$target'; choose minikube or floci." ;;
esac
destination="${K8S_SECRETS_DIR:-$secrets_default}"

k8s_kubectl() { "${kubectl_command[@]}" "$@"; }

k8s_init() {
  require_tool kubectl
  if [[ "$target" == minikube ]]; then
    local context
    context="$(kubectl config current-context)"
    [[ "$context" == "$profile" ]] || fail "Current kubectl context '$context' does not match minikube profile '$profile'. Select the matching context or set MINIKUBE_PROFILE."
    kubectl_command=(kubectl --context "$profile")
  else
    require_tool terraform
    require_tool python3
    local root="$repo_root/infra/terraform/environments/local" endpoint deadline ready=false
    floci_cluster="$(terraform -chdir="$root" output -raw cluster_name)"
    endpoint="$(terraform -chdir="$root" output -raw cluster_endpoint)"
    python3 "$repo_root/scripts/k8s/validate-floci-config.py" "$root/kubeconfig.json" "$floci_cluster" "$endpoint"
    kubectl_command=(kubectl --kubeconfig "$root/kubeconfig.json" --context "$floci_cluster" --request-timeout=30s)
    printf 'Checking local Floci Kubernetes readiness (up to 180 seconds).\n'
    deadline=$((SECONDS + 180))
    while (( SECONDS < deadline )); do
      if [[ "$(k8s_kubectl get --raw=/readyz --request-timeout=5s 2>/dev/null)" == ok ]]; then
        ready=true
        break
      fi
      sleep 2
    done
    [[ "$ready" == true ]] || fail 'Floci Kubernetes API is not ready; inspect the child container logs. No infrastructure will be recreated.'
    k8s_kubectl wait --for=condition=Ready node --all --timeout=180s
  fi
}

floci_find_container() {
  local ids
  ids="$(docker ps --filter label=io.floci.service=eks --filter "label=io.floci.resource-id=$floci_cluster" --format '{{.ID}}')"
  [[ -n "$ids" && "$ids" != *$'\n'* ]] || fail "Expected exactly one running Floci EKS container for '$floci_cluster'."
  floci_container="$ids"
}
