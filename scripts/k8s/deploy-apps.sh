#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
k8s_init

# Verify prerequisites before applying applications. Only names are displayed.
k8s_kubectl get namespace "$namespace" -o name
for service in "${secret_services[@]}"; do
  k8s_kubectl -n "$namespace" get secret "$service-secret" -o name
done
for dependency in "${dependencies[@]}"; do
  k8s_kubectl -n "$namespace" rollout status "deployment/$dependency" --timeout=600s
done

for service in "${services[@]}"; do
  printf 'Applying %s in namespace %s.\n' "$service" "$namespace"
  k8s_kubectl -n "$namespace" apply -k "$repo_root/infra/k8s/$service"
done
for service in "${services[@]}"; do
  k8s_kubectl -n "$namespace" rollout status "deployment/$service" --timeout=600s
done

printf 'All six application Deployments are ready in namespace %s.\n' "$namespace"
