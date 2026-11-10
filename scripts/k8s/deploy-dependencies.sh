#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
k8s_init
databases=(identity-db tickets-db orders-db payments-db)

# Check prerequisites before applying any resources. Only names are displayed.
k8s_kubectl get namespace "$namespace" -o name
for database in "${databases[@]}"; do
  k8s_kubectl -n "$namespace" get secret "${database%-db}-secret" -o name
done

deploy_group() {
  local dependency
  for dependency in "$@"; do
    printf 'Applying %s in namespace %s.\n' "$dependency" "$namespace"
    k8s_kubectl -n "$namespace" apply -f "$repo_root/infra/k8s/$dependency"
  done
  for dependency in "$@"; do
    k8s_kubectl -n "$namespace" rollout status "deployment/$dependency" --timeout=600s
  done
}

deploy_group redis mailpit
deploy_group nats
deploy_group "${databases[@]}"

printf 'All seven dependency Deployments are ready in namespace %s.\n' "$namespace"
