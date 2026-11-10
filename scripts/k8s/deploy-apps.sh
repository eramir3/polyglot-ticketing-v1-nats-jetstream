#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
namespace="${K8S_NAMESPACE:-polyglot-ticketing}"
profile="${MINIKUBE_PROFILE:-minikube}"
services=(tickets identity orders payments expiration api-gateway)
secret_services=(identity tickets orders payments expiration)
dependencies=(redis mailpit nats identity-db tickets-db orders-db payments-db)

command -v kubectl >/dev/null 2>&1 || { printf 'Error: kubectl is required.\n' >&2; exit 1; }
context="$(kubectl config current-context)"
if [[ "$context" != "$profile" ]]; then
  printf "Error: Current kubectl context '%s' does not match minikube profile '%s'. Select the matching context or set MINIKUBE_PROFILE.\n" "$context" "$profile" >&2
  exit 1
fi

# Verify prerequisites before applying applications. Only names are displayed.
kubectl get namespace "$namespace" -o name
for service in "${secret_services[@]}"; do
  kubectl -n "$namespace" get secret "$service-secret" -o name
done
for dependency in "${dependencies[@]}"; do
  kubectl -n "$namespace" rollout status "deployment/$dependency" --timeout=600s
done

for service in "${services[@]}"; do
  printf 'Applying %s in namespace %s.\n' "$service" "$namespace"
  kubectl -n "$namespace" apply -k "$repo_root/infra/k8s/$service"
done
for service in "${services[@]}"; do
  kubectl -n "$namespace" rollout status "deployment/$service" --timeout=600s
done

printf 'All six application Deployments are ready in namespace %s.\n' "$namespace"
